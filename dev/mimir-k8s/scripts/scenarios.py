#!/usr/bin/env python3
"""Run the README's scenarios against `promcost cost` and log what moved.

Each scenario states its prediction before acting, then measures. Output
is plain text meant to be pasted into an issue or PR.

    ./scripts/scenarios.py [path/to/promcost]

Order matters and is deliberate:

  1. rollout-ingesters — non-destructive, so it runs on a clean baseline
  2. scale analytics-bi 1 -> 2 -> 1 — then waits out the ingester's
     active-series idle timeout, so the removed replica's series expire
     before the next scenario (that wait is itself a measurement)
  3. expensive recording rules on -> off

Takes about 45 minutes. Needs the rig from `make up` on localhost:8090.
"""
import json
import subprocess
import sys
import time
import urllib.parse
import urllib.request

PROMCOST = sys.argv[1] if len(sys.argv) > 1 else "../../bin/promcost"
MIMIR = "http://localhost:8090"
DISTRIBUTOR_PORT = 18080


def log(msg=""):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}" if msg else "", flush=True)


def sh(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def query(q):
    req = urllib.request.Request(
        MIMIR + "/prometheus/api/v1/query?" + urllib.parse.urlencode({"query": q}),
        headers={"X-Scope-OrgID": "monitoring"},
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)["data"]["result"]


def by(result, label):
    return {r["metric"].get(label, ""): float(r["value"][1]) for r in result}


def promcost(since):
    out = sh(PROMCOST, "cost", "--metrics-tenant", "monitoring", "--config", "promcost.yaml",
             "--since", since, "--format", "json")
    pool = json.loads(out)["pools"][0]
    return pool, {t["tenant"]: t for t in pool["tenants"]}


def show_cost(since, label):
    pool, tenants = promcost(since)
    log(f"promcost cost --since {since}  [{label}]  write path: "
        + next((a["value"] for a in pool["assumptions"] if a["name"] == "write path"), "?"))
    for name, t in tenants.items():
        log(f"    {name:<11} {t['driver']:>9,.0f} series  {t['share'] * 100:5.1f}%  "
            f"{t.get('equivalent_replicas', 0):.2f} ingesters")
    return tenants


def user_stats():
    """/distributor/all_user_stats, via a short-lived port-forward."""
    pf = subprocess.Popen(
        ["kubectl", "-n", "mimir", "port-forward", "deploy/mimir-distributor", f"{DISTRIBUTOR_PORT}:8080"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(20):
            try:
                req = urllib.request.Request(f"http://localhost:{DISTRIBUTOR_PORT}/distributor/all_user_stats",
                                             headers={"Accept": "application/json"})
                with urllib.request.urlopen(req, timeout=5) as resp:
                    return {u["userID"]: u for u in json.load(resp)}
            except OSError:
                time.sleep(0.5)
        return {}
    finally:
        pf.terminate()


def team_series():
    # Cost-attribution tracker output, raw across ingesters.
    return by(query('sum by (team) (cortex_ingester_attributed_active_series{tenant="analytics"})'), "team")


def wait(seconds, why):
    log(f"waiting {seconds // 60}m{seconds % 60:02d}s — {why}")
    time.sleep(seconds)


def kubectl(*args):
    return sh("kubectl", *args)


# --------------------------------------------------------------------------- 1

def scenario_rollout():
    log("=" * 72)
    log("SCENARIO 1 — roll every ingester")
    log("Prediction: sum-then-average (promcost) dips while an ingester is down,")
    log("because its series are briefly missing from the sum; averaging each")
    log("series first and then summing hides the dip. StatefulSet pods keep their")
    log("names, so a restart does NOT create new series here — the double-count")
    log("the subquery guards against needs a pod *rename* (Deployment, zone move).")
    before = show_cost("10m", "before")
    t0 = time.time()
    kubectl("-n", "mimir", "rollout", "restart", "statefulset/mimir-ingester")
    kubectl("-n", "mimir", "rollout", "status", "statefulset/mimir-ingester", "--timeout=15m")
    took = int(time.time() - t0)
    log(f"rollout finished in {took // 60}m{took % 60:02d}s")
    window = f"{(took // 60) + 3}m"
    after = show_cost(window, "window spanning the rollout")

    ingest = ("label_replace({inner}, \"ingester_id\", \"$1\", \"pod\", \".*?-(rc-[0-9]+-[0-9]+|[0-9]+)$\")")
    guard = " and on (cluster, namespace, job) cortex_partition_ring_partitions"
    per_series_first = by(query(
        "sum by (user) (max by (ingester_id, user) ("
        + ingest.format(inner=f"avg_over_time(cortex_ingester_active_series[{window}]){guard}") + "))"), "user")
    dip = by(query(
        "min_over_time((sum by (user) (max by (ingester_id, user) ("
        + ingest.format(inner="cortex_ingester_active_series" + guard) + f")))[{window}:15s])"), "user")
    log(f"comparison over the same {window} window:")
    log(f"    {'tenant':<11} {'before':>9} {'promcost':>9} {'avg-first':>9} {'lowest':>9}")
    for name in before:
        log(f"    {name:<11} {before[name]['driver']:>9,.0f} {after.get(name, {}).get('driver', 0):>9,.0f} "
            f"{per_series_first.get(name, 0):>9,.0f} {dip.get(name, 0):>9,.0f}")


# --------------------------------------------------------------------------- 2

def scenario_scale():
    log("=" * 72)
    log("SCENARIO 2 — scale avalanche-analytics-bi from 1 to 2 replicas")
    log("Prediction: analytics +8,000 series (each replica's series carry their own")
    log("pod label); by-team tracker shows the growth under team=bi only; other")
    log("tenants' series unchanged, their shares fall. After scaling back, analytics")
    log("stays inflated until the ingester's active-series idle timeout (20m default)")
    log("expires the removed replica's series.")
    before = show_cost("5m", "before")
    teams_before = team_series()
    kubectl("-n", "tenants", "scale", "deploy/avalanche-analytics-bi", "--replicas=2")
    kubectl("-n", "tenants", "rollout", "status", "deploy/avalanche-analytics-bi", "--timeout=120s")
    wait(6 * 60, "one full 5m window at the new size")
    after = show_cost("5m", "scaled to 2")
    teams_after = team_series()
    d = after["analytics"]["driver"] - before["analytics"]["driver"]
    log(f"analytics delta: {d:+,.0f} series (predicted +8,000)")
    for team in sorted(set(teams_before) | set(teams_after)):
        log(f"    by-team {team:<12} {teams_before.get(team, 0):>9,.0f} -> {teams_after.get(team, 0):>9,.0f} (raw tracker)")

    kubectl("-n", "tenants", "scale", "deploy/avalanche-analytics-bi", "--replicas=1")
    elapsed = 0
    for minutes in (6, 16, 24):
        wait((minutes - elapsed) * 60, f"scaled back; checking at +{minutes}m")
        elapsed = minutes
        show_cost("5m", f"{minutes}m after scaling back to 1")


# --------------------------------------------------------------------------- 3

def increase_by_user(metric, window):
    return by(query(f"sum by (user) (increase({metric}[{window}]))"), "user")


def component_cpu(window):
    return by(query(
        f'sum by (container) (rate(container_cpu_usage_seconds_total{{namespace="mimir", '
        f'container=~"querier|query-frontend|ruler"}}[{window}]))'), "container")


def scenario_expensive_rule():
    log("=" * 72)
    log("SCENARIO 3 — expensive rules on analytics (ADR 0003 [A2])")
    log("Prediction: two recording rules writing one series each, but running a 6h")
    log("quantile and a 1h/15s subquery over every analytics series. Analytics'")
    log("rule-evaluation and query time jump by a large factor, querier CPU rises,")
    log("and active series grow by 2 — so promcost cost (pool 1) does not move.")
    before = show_cost("5m", "before")
    eval_before = increase_by_user("cortex_prometheus_rule_evaluation_duration_seconds_sum", "5m")
    query_before = increase_by_user("cortex_query_seconds_total", "5m")
    cpu_before = component_cpu("5m")
    sh("./scripts/sync-rules.sh", "analytics", "rules/scenarios/analytics-expensive.yaml")
    wait(7 * 60, "rules evaluate every 1m; one full 5m window after")
    after = show_cost("5m", "expensive rules on")
    eval_after = increase_by_user("cortex_prometheus_rule_evaluation_duration_seconds_sum", "5m")
    query_after = increase_by_user("cortex_query_seconds_total", "5m")
    cpu_after = component_cpu("5m")

    d = after["analytics"]["driver"] - before["analytics"]["driver"]
    log(f"pool 1 — analytics active series delta: {d:+,.0f} (predicted +2)")
    log("rule evaluation / query time per tenant over 5m (seconds):")
    log(f"    {'tenant':<11} {'eval before':>11} {'eval after':>11} {'query before':>12} {'query after':>12}")
    for name in sorted(set(eval_before) | set(eval_after)):
        log(f"    {name:<11} {eval_before.get(name, 0):>11.2f} {eval_after.get(name, 0):>11.2f} "
            f"{query_before.get(name, 0):>12.2f} {query_after.get(name, 0):>12.2f}")
    log("CPU (cores, 5m average):")
    for c in sorted(set(cpu_before) | set(cpu_after)):
        log(f"    {c:<15} {cpu_before.get(c, 0):.3f} -> {cpu_after.get(c, 0):.3f}")
    sh("./scripts/sync-rules.sh", "analytics")
    log("expensive rules removed")


def main():
    log(f"promcost: {PROMCOST}")
    scenario_rollout()
    scenario_scale()
    scenario_expensive_rule()
    log("done")


if __name__ == "__main__":
    main()
