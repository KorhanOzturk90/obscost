"""Render a validation run as markdown, for a skeptical Mimir operator.

The order is the order such a reader asks questions in: did it pass, what
was it run against, what exactly was expected and measured, and what does
this run *not* show.
"""

from __future__ import annotations

import datetime as dt
import json
from pathlib import Path

import checks
import model

ICON = {"pass": "✅ pass", "fail": "❌ fail", "skip": "⏭ skip", "info": "ℹ info"}
KNOWN = json.loads((Path(__file__).resolve().parent / "known-issues.json").read_text())


def when(t: float) -> str:
    return dt.datetime.fromtimestamp(t, dt.timezone.utc).strftime("%Y-%m-%d %H:%M UTC")


def esc(s: str) -> str:
    return str(s).replace("|", "\\|").replace("\n", " ")


def render(doc: dict, gt: dict, windows: list[dict]) -> str:
    cs = doc["checks"]
    n = {s: sum(c["status"] == s for c in cs) for s in ("pass", "fail", "skip")}
    L: list[str] = []
    w = L.append

    w(f"# promcost validation — `{doc['env']}`, profile `{gt['profile']['name']}`")
    w("")
    w(f"**{n['pass']} passed, {n['fail']} failed, {n['skip']} skipped** across {len(windows)} window(s) "
      f"({', '.join(x['window'] for x in windows)}). Generated {doc['generated_at'][:16].replace('T', ' ')} UTC.")
    w("")
    fails = [c for c in cs if c["status"] == "fail"]
    if fails:
        w("Failures, grouped by cause:")
        w("")
        by_cause: dict[str, list[dict]] = {}
        for c in fails:
            by_cause.setdefault(c.get("issue") or c["id"].split("/")[0], []).append(c)
        for cause, group in by_cause.items():
            k = KNOWN.get(cause)
            label = f"**{k['title']}** ({k['issue']})" if k else f"**{group[0]['title']}**"
            ws = sorted({c['window'] for c in group})
            w(f"- {label} — {len(group)} check(s), window(s) {', '.join(ws)}.")
        w("")
    else:
        w("No check failed.")
        w("")

    w("## What this was run against")
    w("")
    p1 = checks.pool(windows[0]["report"], checks.POOL1) if windows else None
    wp = next((a["value"] for a in (p1 or {}).get("assumptions", []) if a["name"] == "write path"), "—")
    w("| | |")
    w("|---|---|")
    w(f"| environment | `{doc['env']}` |")
    w(f"| Mimir | `{doc['url']}`, Mimir's own metrics in tenant `{doc['metrics_tenant']}` |")
    w(f"| write path | expected `{doc['expect_write_path']}`, detected `{wp}` |")
    w(f"| replication factor | {fmt_rf(p1)} |")
    w(f"| rule evaluation | expected `{doc['expect_eval']}` |")
    w(f"| promcost | `{doc['promcost']}` |")
    w(f"| workload | `loadgen`, profile `{gt['profile']['name']}`, started {when(model.ts(gt['started_at']))} |")
    if doc["events"]:
        w(f"| operational events | " + "; ".join(f"{e['name']} ({e['start']} → {e.get('end') or e['start']})" for e in doc["events"]) + " |")
    w("")
    w("Every tenant below except the metrics tenant is synthetic, and its ground truth is what the generator "
      "*actually* delivered: when its samples started and stopped arriving, how many were accepted, when its rules "
      "were pushed. See [`dev/staging/workload`](../../workload/README.md). The expected pool-1 figures apply "
      "Mimir's own semantics to those facts (the 20m active-series idle timeout, the 1m gauge update); "
      "[`model.py`](../../validate/model.py) states each one where it is used.")
    w("")
    w("| tenant | shape | live series | samples/s | queries/min | rules (rec + alert) | record: series |")
    w("|---|---|---:|---:|---:|---:|---:|")
    for name, tt in sorted(gt["tenants"].items()):
        r = tt.get("rules") or {}
        q = tt.get("queries") or {}
        churn = f" (replaced every {tt['churn_period_s'] / 60:.0f}m)" if tt.get("churn_period_s") else ""
        timing = ""
        if tt.get("planned_start_after_s"):
            timing = f"; starts {tt['planned_start_after_s'] / 60:.0f}m in"
        if tt.get("planned_stop_after_s"):
            timing = f"; silent {tt['planned_stop_after_s'] / 60:.0f}m in"
        c = tt["counters"]
        w(f"| `{name}` | {esc(tt['shape'])}{timing} | {tt['series']['live']:,}{churn} | "
          f"{tt['series']['samples_per_second']:,.1f} | {q.get('per_minute', 0):g} | "
          f"{r.get('recording', 0)} + {r.get('alerting', 0)} | {r.get('record_series_total', 0)} |")
    w("")
    delivery = [(n, tt["counters"]) for n, tt in sorted(gt["tenants"].items())]
    failed = [(n, c) for n, c in delivery if c["samples_failed"] or c["queries_failed"]]
    w("Delivery: " + ("every sample and query the generator sent was accepted." if not failed else
                      "; ".join(f"`{n}`: {c['samples_failed']} samples and {c['queries_failed']} queries rejected "
                                f"(last error: {esc(c.get('last_error', ''))})" for n, c in failed)))
    w("")

    for x in windows:
        w(f"## Window {x['window']}, ending {when(x['end'])}")
        w("")
        notes = []
        if x["coverage"] < 0.99:
            notes.append(f"the metrics pipeline had data for {x['coverage']:.0%} of this window (the run was "
                         f"{x['run_age_s'] / 3600:.1f}h old), so every figure averages over that part")
        if x["events"]:
            notes.append("overlaps " + ", ".join(x["events"]) + ": pool 1 is checked against a band widened to ±15%")
        else:
            notes.append("clean window: no operational event inside it, so pool 1 is checked at the normal ±1%")
        for t in notes:
            w(f"> {t}")
        if notes:
            w("")
        wc = [c for c in cs if c["window"] == x["window"]]
        w("| result | check | expected | measured | tolerance |")
        w("|---|---|---|---|---|")
        for c in wc:
            w(f"| {ICON[c['status']]} | {esc(c['title'])} | {esc(c['expected'])} | {esc(c['measured'])} | {esc(c['tolerance'])} |")
        w("")
        w("<details><summary>Pool 1 per tenant: expected vs measured</summary>")
        w("")
        rep = x["report"]
        p = checks.pool(rep, checks.POOL1)
        listed = checks.tenants(p) if p else {}
        w("| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |")
        w("|---|---:|---:|---:|---:|")
        for name in sorted(listed.keys() | gt["tenants"].keys()):
            e = x["expected"].get(name)
            band = f"{checks.fmt(e['time_weighted'][0])} – {checks.fmt(e['time_weighted'][1])}" if e else "not controlled"
            got = listed.get(name)
            v = "not measured" if got is None else checks.fmt(got.get("driver"))
            pp = f"{x['presence'].get(name, 0)} / {e['present_points'] if e else '—'} of {x['points']}"
            d = x.get("direct", {}).get(name)
            w(f"| `{name}` | {band} | {v} | {checks.fmt(d) if d is not None else '—'} | {pp} |")
        w("")
        w("</details>")
        w("")
        for pid in (checks.POOL6, checks.POOL7):
            p = checks.pool(rep, pid)
            if not p:
                continue
            w(f"<details><summary>{checks.POOL_TITLE[pid]}: shares ({esc(p.get('driver_kind', ''))})</summary>")
            w("")
            w("| tenant | driver | share |")
            w("|---|---:|---:|")
            for t in p.get("tenants") or []:
                w(f"| `{t['tenant']}` | {checks.fmt(t.get('driver_raw'))} | {100 * (t.get('share') or 0):.2f}% |")
            for t in p.get("unmeasured") or []:
                w(f"| `{t}` | not measured | not measured |")
            w("")
            w("</details>")
            w("")
        detailed = [c for c in wc if c["detail"] or c["why"]]
        if detailed:
            w("<details><summary>Why each tolerance, and details</summary>")
            w("")
            for c in detailed:
                w(f"- **{esc(c['title'])}** — {esc(c['why'])}")
                for d in c["detail"]:
                    w(f"  - {esc(d)}")
            w("")
            w("</details>")
            w("")

    w("## What this run does not show")
    w("")
    w("- **Resource calibration.** It checks that promcost reads Mimir's per-tenant drivers correctly. Whether "
      "active series are *the* driver of ingester memory is ADR 0003 decision 6's question, and is not answered here.")
    w("- **Pools 6 and 7 against absolute truth.** Fetched chunk bytes and rule-evaluation seconds depend on "
      "chunk encoding, caches and CPU contention, so only their ranking is checked, not their values.")
    w("- **Real tenants.** Every costed tenant is synthetic (`loadgen`), so its ground truth is known. Real workloads "
      "have shapes this profile does not: native histograms, exemplars, OTLP ingestion, bursty queries, rule groups "
      "that time out.")
    w("- **A production-sized cluster.** The environment is one cloud container running k3s in Docker on 4 vCPUs: "
      "Mimir's microservices with zone-aware ingesters (3 zones, one ingester each), Kafka for ingest storage or RF=3 "
      "for classic, a dedicated ruler query path, kube-prometheus-stack for meta-monitoring. It is not multi-node, so "
      "nothing here exercises network partitions, node loss or real CPU contention between hosts.")
    w("- **Upstream images as shipped by their registries.** Every image is pulled from Docker Hub through "
      "`mirror.gcr.io`, because quay.io, registry.k8s.io and ghcr.io are not reachable from the container. The "
      "staging lane replaced the charts' non-Docker-Hub images with Docker Hub equivalents (see its README's "
      "deviations table), and MinIO with a pullable S3-compatible store. Mimir itself is the upstream "
      "`grafana/mimir:3.2.0`.")
    w("- **Long windows.** Windows reach at most 1h, on a 45-minute workload timeline (`profiles/short.yaml`): the "
      "container is reclaimed whenever the session idles. There is no 24h soak, so nothing here covers compaction, "
      "block upload to the store-gateway, or day-scale counter behaviour.")
    w("- **Anything about tenants it did not generate.** The metrics tenant is real and uncontrolled: it is in "
      "every share and every sum, but no figure for it is checked.")
    w("")
    w("Raw outputs (every promcost run as JSON and markdown, the ground truth, every check as JSON) are in "
      "[`raw/`](raw/) and [`checks.json`](checks.json).")
    w("")
    return "\n".join(L)


def fmt_rf(p1: dict | None) -> str:
    if not p1:
        return "—"
    rf = next((a for a in p1.get("assumptions", []) if a["name"] == "replication factor"), None)
    return f"`{rf['value']}` (from `{rf['source']}`)" if rf else "—"
