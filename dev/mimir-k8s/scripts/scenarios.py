#!/usr/bin/env python3
"""Run the README's scenarios against `promcost cost` and log what moved.

Each scenario states its prediction before acting, then measures. Output
is plain text meant to be pasted into an issue or PR.

    ./scripts/scenarios.py [path/to/promcost] [scenario ...]

Scenario names: rollout, scale, expensive. Default: all three, in order.

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


def team_series():
    # Cost-attribution tracker output, raw across ingesters.
    return by(query('sum by (team) (cortex_ingester_attributed_active_series{tenant="analytics"})'), "team")


# The two forms of "real active series per tenant", as promcost's pool-1
# query uses them: classic (sum across ingesters, divided by the
# replication factor) and ingest storage (max across the zone replicas of
# a partition). active_series_expr picks whichever the cluster answers.
CLASSIC_SERIES = ("sum by (user) ({inner} unless on (cluster, namespace, job) cortex_partition_ring_partitions)"
                  " / on () group_left max(cortex_distributor_replication_factor)")
INGEST_SERIES = ('sum by (user) (max by (ingester_id, user) (label_replace({inner}'
                 ' and on (cluster, namespace, job) cortex_partition_ring_partitions,'
                 ' "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+|[0-9]+)$")))')


def active_series_expr(inner="cortex_ingester_active_series"):
    """The active-series expression this cluster's write path answers."""
    classic = CLASSIC_SERIES.format(inner=inner)
    return classic if query(classic) else INGEST_SERIES.format(inner=inner)


def settle(tenant="analytics", tolerance=0.01, timeout_s=35 * 60):
    """Block until `tenant`'s active series stop moving.

    Every measurement here is a delta, and a delta against a baseline that
    is still drifting is meaningless: the first run of these scenarios
    started while the rig was still filling up and read +10,608 where the
    true change was +8,000. Series also *leave* slowly — Mimir keeps a
    series active for 20 minutes after its last sample — so this is the
    only honest way to sequence scenarios that add and remove series.
    """
    expr = active_series_expr()
    deadline = time.time() + timeout_s
    previous = None
    while time.time() < deadline:
        current = by(query(expr), "user").get(tenant, 0)
        if previous and abs(current - previous) <= tolerance * max(previous, 1):
            log(f"settled: {tenant} steady at {current:,.0f} series")
            return current
        log(f"settling: {tenant} at {current:,.0f} series" + ("" if previous is None else f" (was {previous:,.0f})"))
        previous = current
        time.sleep(120)
    log(f"WARNING: {tenant} still moving after {timeout_s // 60}m; measurements below are suspect")
    return previous


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
    settle()
    before = show_cost("10m", "before")
    t0 = time.time()
    kubectl("-n", "mimir", "rollout", "restart", "statefulset/mimir-ingester")
    kubectl("-n", "mimir", "rollout", "status", "statefulset/mimir-ingester", "--timeout=15m")
    took = int(time.time() - t0)
    log(f"rollout finished in {took // 60}m{took % 60:02d}s")
    window = f"{(took // 60) + 3}m"
    after = show_cost(window, "window spanning the rollout")

    per_series_first = by(query(active_series_expr(f"avg_over_time(cortex_ingester_active_series[{window}])")), "user")
    dip = by(query(f"min_over_time(({active_series_expr()})[{window}:15s])"), "user")
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
    baseline = settle()
    before = show_cost("5m", "before")
    teams_before = team_series()
    kubectl("-n", "tenants", "scale", "deploy/avalanche-analytics-bi", "--replicas=2")
    kubectl("-n", "tenants", "rollout", "status", "deploy/avalanche-analytics-bi", "--timeout=120s")
    wait(6 * 60, "one full 5m window at the new size")
    after = show_cost("5m", "scaled to 2")
    teams_after = team_series()
    d = after["analytics"]["driver"] - baseline
    log(f"analytics delta vs settled baseline: {d:+,.0f} series (predicted +8,000)")
    for team in sorted(set(teams_before) | set(teams_after)):
        log(f"    by-team {team:<12} {teams_before.get(team, 0):>9,.0f} -> {teams_after.get(team, 0):>9,.0f} (raw tracker)")

    kubectl("-n", "tenants", "scale", "deploy/avalanche-analytics-bi", "--replicas=1")
    t0 = time.time()
    log("scaled back to 1; the removed replica's series stay active until Mimir's")
    log("20m idle timeout expires them — watching them go:")
    settle()
    log(f"analytics returned to its baseline {int(time.time() - t0) // 60}m after scaling down")
    show_cost("5m", "after scaling back to 1")


# --------------------------------------------------------------------------- 3

def increase_by_user(metric, window):
    return by(query(f"sum by (user) (increase({metric}[{window}]))"), "user")


def component_cpu(window):
    # `container` here is the Kubernetes container name, which the chart sets
    # to the component name. cAdvisor's pod-level aggregate (container="") and
    # the pause container (container="POD") never reach Mimir: alloy/config.alloy
    # drops both before remote_write, so this regex matches exactly one series
    # per component pod.
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
    baseline = settle()
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

    d = after["analytics"]["driver"] - baseline
    log(f"pool 1 — analytics active series delta vs settled baseline: {d:+,.0f} (predicted +2)")
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


SCENARIOS = {"rollout": scenario_rollout, "scale": scenario_scale, "expensive": scenario_expensive_rule}


def main():
    log(f"promcost: {PROMCOST}")
    chosen = [a for a in sys.argv[2:]] or list(SCENARIOS)
    for name in chosen:
        if name not in SCENARIOS:
            raise SystemExit(f"unknown scenario {name!r}; pick from {', '.join(SCENARIOS)}")
    for name in chosen:
        SCENARIOS[name]()
    log("done")


if __name__ == "__main__":
    main()
