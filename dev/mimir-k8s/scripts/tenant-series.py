#!/usr/bin/env python3
"""Print per-tenant active series from the monitoring tenant, raw and divided
by the replication factor — a promcost-independent cross-check for
`make cost`.

Usage: ./scripts/tenant-series.py [mimir-url]
"""
import json
import sys
import urllib.parse
import urllib.request

URL = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8090"


def query(q):
    req = urllib.request.Request(
        URL + "/prometheus/api/v1/query?" + urllib.parse.urlencode({"query": q}),
        headers={"X-Scope-OrgID": "monitoring"},
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        return json.load(resp)["data"]["result"]


def main():
    try:
        rows = query("sum by (user) (cortex_ingester_active_series)")
        rf = query("max(cortex_distributor_replication_factor)")
    except OSError as e:
        print(f"  cannot reach Mimir at {URL}: {e}")
        return
    if not rows:
        print("  no data yet — self-monitoring takes a minute or two to arrive")
        return
    factor = float(rf[0]["value"][1]) if rf else None
    print(f"  replication factor: {factor if factor else 'not reported'}")
    print(f"  {'tenant':<12} {'raw':>10} {'÷ RF':>10}")
    for r in sorted(rows, key=lambda r: -float(r["value"][1])):
        raw = float(r["value"][1])
        real = f"{raw / factor:>10,.0f}" if factor else f"{'?':>10}"
        print(f"  {r['metric']['user']:<12} {raw:>10,.0f} {real}")


if __name__ == "__main__":
    main()
