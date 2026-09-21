#!/usr/bin/env python3
"""Cross-check our pod-cost allocation against OpenCost's.

    ./scripts/compare-opencost.py [--window 1h]

ADR 0006 decision 3 says a pool's cost on a shared cluster is derived:
`price × max(request, usage) / node capacity`, per pod, summed per
component. OpenCost is the de-facto reference implementation of exactly
that idea, so running both over the same window against the same synthetic
node prices tests our arithmetic without waiting for a cloud invoice.

Agreement within a few percent means the allocation is sound. Disagreement
is a finding either way: either our formula is wrong, or OpenCost's
defaults (how it weights requests against usage, what it does with idle)
encode a choice we should be making deliberately rather than inheriting.

Needs `make opencost` first.
"""
import argparse
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from podcost import allocate, component_of  # noqa: E402

PORT = 19003


def opencost_allocation(window):
    """{component: cost over the window} from OpenCost's allocation API."""
    pf = subprocess.Popen(
        ["kubectl", "-n", "mimir", "port-forward", "deploy/opencost", f"{PORT}:9003"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        url = (f"http://localhost:{PORT}/allocation/compute"
               f"?window={window}&aggregate=namespace,controller"
               f"&accumulate=true&includeIdle=true")
        payload = None
        for _ in range(30):
            try:
                with urllib.request.urlopen(url, timeout=30) as resp:
                    payload = json.load(resp)
                break
            except (OSError, urllib.error.URLError):
                time.sleep(1)
        if payload is None:
            raise SystemExit("could not reach OpenCost; is `make opencost` done?")
    finally:
        pf.terminate()

    data = payload.get("data") or []
    if not data:
        raise SystemExit(f"OpenCost returned no data for window {window}")

    out = {}
    for key, row in data[0].items():
        if key == "__idle__":
            out["idle / headroom"] = row.get("totalCost", 0.0)
            continue
        namespace, _, controller = key.partition("/")
        # OpenCost keys a controller as "deployment:mimir-querier" or
        # "statefulset:mimir-ingester"; podcost.py names it by workload.
        controller = controller.partition(":")[2] or controller
        component = component_of(namespace, controller or namespace)
        out[component] = out.get(component, 0.0) + (row.get("totalCost") or 0.0)
    return out


def window_hours(window):
    unit, value = window[-1], window[:-1]
    return float(value) * {"m": 1 / 60, "h": 1, "d": 24}[unit]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--window", default="1h")
    ap.add_argument("--cpu-hour", type=float, default=0.04)
    ap.add_argument("--gib-hour", type=float, default=0.005)
    args = ap.parse_args()

    hours = window_hours(args.window)
    node_cost_hour, ours, idle = allocate(args.window, args.cpu_hour, args.gib_hour)
    theirs = opencost_allocation(args.window)

    mine = {name: row["cost"] * hours for name, row in ours.items()}
    mine["idle / headroom"] = idle * hours

    print(f"Window {args.window}; node basis {node_cost_hour * hours:.4f} at "
          f"{args.cpu_hour}/vCPU-h, {args.gib_hour}/GiB-h\n")
    print(f"  {'component':<30} {'podcost.py':>11} {'OpenCost':>11} {'diff':>9}")
    for name in sorted(set(mine) | set(theirs), key=lambda n: -max(mine.get(n, 0), theirs.get(n, 0))):
        a, b = mine.get(name, 0.0), theirs.get(name, 0.0)
        if max(a, b) < 1e-6:
            continue
        diff = (b - a) / a * 100 if a else float("inf")
        print(f"  {name:<30} {a:>11.5f} {b:>11.5f} {diff:>8.1f}%")

    total_mine = sum(v for k, v in mine.items() if k != "idle / headroom")
    total_theirs = sum(v for k, v in theirs.items() if k != "idle / headroom")
    print(f"\n  {'allocated (excl. idle)':<30} {total_mine:>11.5f} {total_theirs:>11.5f} "
          f"{(total_theirs - total_mine) / total_mine * 100 if total_mine else 0:>8.1f}%")
    print("\nA few percent apart is agreement. Larger gaps are a finding: check whether")
    print("OpenCost is weighting requests against usage differently, or counting")
    print("pods this script groups elsewhere.")


if __name__ == "__main__":
    main()
