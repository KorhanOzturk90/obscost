#!/usr/bin/env python3
"""Calibration sweeps: turn ADR 0003's confidence column into coefficients.

    ./scripts/calibrate.py series [--steps 4]   # E2: ingester memory vs active series (~1h)
    ./scripts/calibrate.py query  [--steps 3]   # E1 + E3: what query load actually costs

A scenario (scripts/scenarios.py) changes one thing and checks a
prediction. A sweep changes the same thing repeatedly and fits a line,
because the answer wanted here is a *coefficient* and how well it holds —
"ingester memory is driven by active series" is only useful once it reads
"about N KB per series, R² = 0.9x".

Both sweeps restore the rig when they finish, and wait for Mimir's 20
minute active-series idle timeout where that matters.
"""
import argparse
import os
import subprocess
import sys
import time  # noqa: F401

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from scenarios import by, kubectl, log, query, settle, sh  # noqa: E402

CACHE = ".cache/rules"


def fit(points):
    """Least squares y = a·x + b over [(x, y)], with R²."""
    n = len(points)
    if n < 2:
        return 0.0, 0.0, 0.0
    sx = sum(x for x, _ in points)
    sy = sum(y for _, y in points)
    sxx = sum(x * x for x, _ in points)
    sxy = sum(x * y for x, y in points)
    denom = n * sxx - sx * sx
    if denom == 0:
        return 0.0, sy / n, 0.0
    a = (n * sxy - sx * sy) / denom
    b = (sy - a * sx) / n
    mean = sy / n
    ss_tot = sum((y - mean) ** 2 for _, y in points)
    ss_res = sum((y - (a * x + b)) ** 2 for x, y in points)
    r2 = 1 - ss_res / ss_tot if ss_tot else 1.0
    return a, b, r2


def scalar(promql, attempts=5):
    """One number out of Mimir, retrying while the rig is busy.

    A sweep deliberately loads the cluster, and a loaded Mimir answers
    slowly — the first run of the query sweep died on a read timeout
    mid-measurement and threw away the steps it had already taken.
    """
    for attempt in range(attempts):
        try:
            rows = query(promql)
            return float(rows[0]["value"][1]) if rows else 0.0
        except OSError as err:
            if attempt == attempts - 1:
                raise
            log(f"    query timed out ({err}); retrying in 20s")
            time.sleep(20)
    return 0.0


def node_cpu_used():
    """Cores currently burned across the whole cluster."""
    return scalar("sum(rate(container_cpu_usage_seconds_total[3m]))")


def node_cpu_budget():
    """Cores the k3d node may use, as `make up CPUS=` set it."""
    out = subprocess.run(["docker", "inspect", "k3d-obscost-server-0",
                          "--format", "{{.HostConfig.NanoCpus}}"],
                         capture_output=True, text=True)
    nanos = int(out.stdout.strip() or 0)
    return nanos / 1e9 if nanos else 0.0


# Everything measured here is averaged over a window rather than sampled.
# The first run of this sweep read instantaneous gauges and produced
# R² = 0.15 nonsense: cortex_ingester_memory_series is a sawtooth, falling
# by a third every time the TSDB head compacts (104k to 181k within half an
# hour on an unchanged rig), so a single sample says more about compaction
# phase than about the tenants.
MEASURE_WINDOW = "10m"


def averaged(inner):
    return scalar(f"avg_over_time(({inner})[{MEASURE_WINDOW}:1m])")


def report(title, unit, points, scale=1.0):
    a, b, r2 = fit(points)
    log(f"  fit — {title}: slope {a * scale:,.3f} {unit}, intercept {b:,.3f}, R² {r2:.4f}")
    return a, b, r2


# --------------------------------------------------------------------- E2

def sweep_series(steps):
    """Ingester memory against active series (ADR 0003 pool 1's driver)."""
    log("=" * 72)
    log("SWEEP E2 — ingester memory vs active series")
    log("Scales avalanche-analytics-bi 1, 3, 5, 7 replicas (+16,000 real series a")
    log("step) and fits ingester memory against active series. ADR 0003 calls pool")
    log("1's mapping 'high confidence'; this is where that gets a number.")
    points, heap_points = [], []
    expected = None
    try:
        for replicas in [1 + 2 * i for i in range(steps)]:
            kubectl("-n", "tenants", "scale", "deploy/avalanche-analytics-bi", f"--replicas={replicas}")
            kubectl("-n", "tenants", "rollout", "status", "deploy/avalanche-analytics-bi", "--timeout=180s")
            analytics = settle()
            if expected is None:
                expected = analytics
            elif analytics < expected + 15000:
                log(f"  WARNING: analytics at {analytics:,.0f}, expected ~{expected + 16000:,.0f} "
                    f"after scaling to {replicas} replicas — step may not have landed")
            expected = analytics
            log(f"  step {replicas} replicas: waiting {MEASURE_WINDOW} for an averaged measurement")
            time.sleep(10 * 60)
            series = averaged("sum(cortex_ingester_active_series)")
            memory = averaged('sum(container_memory_working_set_bytes{namespace="mimir", container="ingester"})')
            heap = averaged('sum(go_memstats_heap_inuse_bytes{job="mimir/ingester"})')
            points.append((series, memory))
            heap_points.append((series, heap))
            log(f"  step {replicas}: {series:,.0f} active series (raw, all ingesters), "
                f"{memory / 2 ** 20:,.0f} MiB working set, {heap / 2 ** 20:,.0f} MiB Go heap")
    finally:
        kubectl("-n", "tenants", "scale", "deploy/avalanche-analytics-bi", "--replicas=1")
    a, b, _ = report("working set per raw active series", "KiB/series", points, scale=1 / 1024)
    report("Go heap per raw active series", "KiB/series", heap_points, scale=1 / 1024)
    log(f"  fixed cost (intercept): {b / 2 ** 20:,.0f} MiB across all ingesters, independent of series")
    log("  note: the rig runs GOGC=50, so these coefficients are lower than a default Go runtime would give")
    log("  restoring: series stay active for 20m after the last sample")
    settle()


# ----------------------------------------------------------------- E1 + E3

EXPENSIVE_RULE = """namespace: sweep{n}
groups:
  - name: sweep_{n}
    interval: 1m
    rules:
      - record: analytics:sweep{n}:p99_6h
        expr: max(quantile_over_time(0.99, label_replace({{__name__=~"avalanche_gauge_metric_.*"}}, "src", "$1", "__name__", "(.*)")[1h:1m]))
"""


def sweep_query(steps):
    """What query load costs: ingesters (E1) and queriers (E3)."""
    log("=" * 72)
    log("SWEEP E1/E3 — what query load actually costs")
    log("Adds 0..N expensive recording rules to analytics at constant ingestion,")
    log("then fits: ingester CPU vs bytes fetched (ADR 0006 decisions 1-2 — is")
    log("ingester CPU really a write-path cost?) and querier CPU vs bytes fetched")
    log("against querier CPU vs query seconds (which driver predicts pool 6?).")
    os.makedirs(CACHE, exist_ok=True)
    budget = node_cpu_budget()
    log(f"  node CPU budget: {budget:.1f} cores" if budget else "  node CPU budget: uncapped")
    settle()
    rows = []
    try:
        for n in range(0, steps + 1):
            if budget and rows:
                used = node_cpu_used()
                if used > 0.7 * budget:
                    log(f"  stopping at {n - 1} rules: the cluster is using {used:.2f} of "
                        f"{budget:.1f} cores, and a saturated rig measures the CPU cap, not the query")
                    break
            files = []
            for i in range(1, n + 1):
                path = f"{CACHE}/expensive{i}.yaml"
                with open(path, "w") as f:
                    f.write(EXPENSIVE_RULE.format(n=i))
                files.append(path)
            sh("./scripts/sync-rules.sh", "analytics", *files)
            log(f"  step {n}: {n} expensive rule(s) active; waiting 6m for a clean window")
            time.sleep(6 * 60)
            w = "5m"
            bytes_fetched = scalar(f"sum(increase(cortex_query_fetched_chunk_bytes_total[{w}]))")
            query_seconds = scalar(f"sum(increase(cortex_query_seconds_total[{w}]))")
            ingester_cpu = scalar(f'sum(rate(container_cpu_usage_seconds_total{{namespace="mimir", container="ingester"}}[{w}]))')
            querier_cpu = scalar(f'sum(rate(container_cpu_usage_seconds_total{{namespace="mimir", container="querier"}}[{w}]))')
            routes = by(query(f'sum by (route) (increase(cortex_request_duration_seconds_sum{{job="mimir/ingester"}}[{w}]))'), "route")
            push = routes.get("/cortex.Ingester/Push", 0.0)
            read = routes.get("/cortex.Ingester/QueryStream", 0.0)
            rows.append({"n": n, "bytes": bytes_fetched, "seconds": query_seconds,
                         "ingester_cpu": ingester_cpu, "querier_cpu": querier_cpu,
                         "push": push, "read": read})
            log(f"    {bytes_fetched / 2 ** 20:8,.0f} MiB fetched | {query_seconds:7.1f} query-s | "
                f"ingester {ingester_cpu:.3f} cores | querier {querier_cpu:.3f} cores | "
                f"ingester time push {push:.0f}s / read {read:.0f}s ({read / (push + read) * 100 if push + read else 0:.0f}% reads)")
    finally:
        sh("./scripts/sync-rules.sh", "analytics")
        for name in os.listdir(CACHE) if os.path.isdir(CACHE) else []:
            os.remove(os.path.join(CACHE, name))

    mib = [(r["bytes"] / 2 ** 20, r["ingester_cpu"]) for r in rows]
    report("E1 ingester CPU vs data fetched", "cores per MiB/5m", mib)
    report("E3 querier CPU vs data fetched", "cores per MiB/5m",
           [(r["bytes"] / 2 ** 20, r["querier_cpu"]) for r in rows])
    report("E3 querier CPU vs query seconds", "cores per query-second",
           [(r["seconds"], r["querier_cpu"]) for r in rows])
    log("  the better R² names the driver pool 6 should use")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("sweep", choices=["series", "query"])
    ap.add_argument("--steps", type=int, default=0, help="sweep length (default 4 for series, 3 for query)")
    args = ap.parse_args()
    if args.sweep == "series":
        sweep_series(args.steps or 4)
    else:
        sweep_query(args.steps or 3)
    log("done")


if __name__ == "__main__":
    main()
