#!/usr/bin/env python3
"""Validate promcost's cost report against loadgen's ground truth.

For each window it runs `$PROMCOST cost` three times (all pools as JSON,
the same as markdown, and JSON with --rule-outputs), checks the output
against the ground truth and the invariants in checks.py, and writes
report.md, checks.json and every raw output to the results directory.

    PROMCOST=/path/to/promcost ./validate.py \
        --env local --url http://localhost:8080 --metrics-tenant infra \
        --ground-truth ../workload/.run/ground-truth.json \
        --expect-eval local --expect-write-path classic --windows 1h,6h

Python 3.11+, standard library only. See README.md.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import checks  # noqa: E402
import model  # noqa: E402
import report  # noqa: E402

# The product is being renamed: its binary is named in exactly this one place.
PROMCOST = os.environ.get("PROMCOST", "promcost")

HERE = Path(__file__).resolve().parent
RESULTS = HERE.parent / "results"


def parse_window(s: str) -> float:
    units = {"m": 60, "h": 3600, "d": 86400}
    return float(s[:-1]) * units[s[-1]]


def load_ground_truth(src: str) -> dict:
    if src.startswith("http://") or src.startswith("https://"):
        with urllib.request.urlopen(src, timeout=30) as r:
            return json.load(r)
    return json.loads(Path(src).read_text())


class Mimir:
    """Direct PromQL reads the harness needs that are about the environment,
    not about tenants: which subquery points the metrics pipeline had data
    for, and when each tenant was present."""

    def __init__(self, url: str, tenant: str, headers: dict[str, str]):
        self.url, self.tenant, self.headers = url.rstrip("/"), tenant, headers

    def range_query(self, q: str, start: float, end: float, step: float) -> list[dict]:
        body = urllib.parse.urlencode({"query": q, "start": start, "end": end, "step": step}).encode()
        req = urllib.request.Request(self.url + "/prometheus/api/v1/query_range", data=body,
                                     headers={"X-Scope-OrgID": self.tenant, **self.headers})
        with urllib.request.urlopen(req, timeout=120) as r:
            return json.load(r)["data"]["result"]

    def pipeline_points(self, end: float, window_s: float) -> tuple[list[float], dict[str, int]]:
        step = model.subquery_step(window_s)
        pts = model.subquery_points(end, window_s, step)
        if not pts:
            return [], {}
        total = self.range_query("sum(cortex_ingester_active_series)", pts[0], pts[-1], step)
        have = sorted(float(v[0]) for v in total[0]["values"]) if total else []
        by_user = self.range_query("sum by (user) (cortex_ingester_active_series)", pts[0], pts[-1], step)
        presence = {s["metric"].get("user", ""): len(s["values"]) for s in by_user}
        return have, presence


def run_promcost(args: list[str], out: Path) -> str:
    cmd = [PROMCOST, "cost", *args]
    res = subprocess.run(cmd, capture_output=True, text=True, timeout=900)
    out.write_text(res.stdout)
    if res.stderr:
        out.with_suffix(out.suffix + ".stderr").write_text(res.stderr)
    if res.returncode != 0:
        raise RuntimeError(f"{' '.join(cmd)} exited {res.returncode}: {res.stderr.strip()[:500]}")
    return res.stdout


def promcost_version() -> str:
    for flag in ("version", "--version"):
        try:
            r = subprocess.run([PROMCOST, flag], capture_output=True, text=True, timeout=30)
            if r.returncode == 0 and r.stdout.strip():
                return r.stdout.strip().splitlines()[0]
        except OSError:
            break
    return f"{PROMCOST} (no version command)"


def load_events(path: str | None) -> list[dict]:
    if not path:
        return []
    evs = json.loads(Path(path).read_text())
    for e in evs:
        e["_start"], e["_end"] = model.ts(e["start"]), model.ts(e.get("end") or e["start"])
    return evs


def observer_tenants(log: Path, start: float, end: float) -> set[str]:
    """Tenants promcost --rule-outputs queried in [start - 5m, end]: each such
    query is query-path traffic charged to that tenant."""
    out: set[str] = set()
    if not log.exists():
        return out
    for line in log.read_text().splitlines():
        rec = json.loads(line)
        if start - 300 <= rec["at"] <= end:
            out.update(rec["tenants"])
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--env", required=True, help="environment label, e.g. local, staging-ingest, staging-classic")
    ap.add_argument("--url", required=True, help="Mimir gateway URL as reachable from here")
    ap.add_argument("--metrics-tenant", required=True)
    ap.add_argument("--ground-truth", required=True, help="loadgen ground-truth file or http(s) URL")
    ap.add_argument("--windows", default="1h,6h,24h")
    ap.add_argument("--expect-eval", choices=["local", "remote"], required=True)
    ap.add_argument("--expect-write-path", choices=["classic", "ingest storage"], required=True)
    ap.add_argument("--query-heavy", default="dashboards")
    ap.add_argument("--rule-heavy", default="rules")
    ap.add_argument("--events", help="JSON list of {name, start, end} operational events (ISO times)")
    ap.add_argument("--observer-log", help="where to record promcost's own tenant queries (default: next to the ground truth)")
    ap.add_argument("--out", help="results directory (default: ../results/<date>-<env>)")
    ap.add_argument("--header", action="append", default=[], help="extra HTTP header Name=value for direct Mimir reads")
    ap.add_argument("--config-extra", help="YAML file whose inventory: block is used in promcost.yaml")
    a = ap.parse_args()

    headers = dict(h.split("=", 1) for h in a.header)
    now = time.time()
    date = dt.datetime.fromtimestamp(now, dt.timezone.utc).strftime("%Y-%m-%d")
    out = Path(a.out) if a.out else RESULTS / f"{date}-{a.env}"
    raw = out / "raw"
    raw.mkdir(parents=True, exist_ok=True)

    gt = load_ground_truth(a.ground_truth)
    (raw / "ground-truth.json").write_text(json.dumps(gt, indent=2))
    controlled = sorted(gt["tenants"])
    obs_log = Path(a.observer_log) if a.observer_log else (
        Path(a.ground_truth).with_name("observer.jsonl") if not a.ground_truth.startswith("http") else out / "observer.jsonl")

    cfg = raw / "promcost.yaml"
    inventory = Path(a.config_extra).read_text() if a.config_extra else (
        "inventory:\n  pools:\n    ingester_memory: {replicas: 1}\n")
    cfg.write_text(f"backend:\n  url: {a.url}\n  timeout: 5m\n{inventory}")
    base = ["--metrics-tenant", a.metrics_tenant, "--config", str(cfg), "--tenant", ",".join(controlled)]

    mimir = Mimir(a.url, a.metrics_tenant, headers)
    events = load_events(a.events)
    run_start = model.ts(gt["started_at"])
    results, windows = [], []
    for w in a.windows.split(","):
        w = w.strip()
        ws = parse_window(w)
        print(f"[{w}] running {PROMCOST} cost …", file=sys.stderr)
        js = json.loads(run_promcost([*base, "--since", w, "--format", "json"], raw / f"cost-{w}.json"))
        md = run_promcost([*base, "--since", w, "--format", "md"], raw / f"cost-{w}.md")
        ro_tenants = [n for n, tt in gt["tenants"].items() if (tt.get("rules") or {}).get("recording") and tt.get("rules_pushed_at")]
        with obs_log.open("a") as f:
            f.write(json.dumps({"at": time.time(), "tenants": ro_tenants}) + "\n")
        ro = json.loads(run_promcost([*base, "--since", w, "--format", "json", "--rule-outputs"], raw / f"cost-{w}-rule-outputs.json"))
        now = time.time()

        p1 = checks.pool(js, checks.POOL1)
        end = model.ts(p1["end"]) if p1 else now
        pts, presence = mimir.pipeline_points(end, ws)
        start = end - ws
        overlapping = [e for e in events if e["_start"] <= end and e["_end"] >= start]
        ev_names = [e["name"] for e in overlapping]
        event_tol = 0.15 if overlapping else None
        observer = observer_tenants(obs_log, start, end)
        coverage = (pts[-1] - pts[0] + model.subquery_step(ws)) / ws if pts else 0.0

        cs: list[checks.Check] = []
        cs += checks.shares_sum(js, w)
        cs += checks.unmeasured_not_zero(js, md, w)
        cs += checks.pool1_truth(js, gt, w, ws, pts, presence, now, event_tol, ev_names)
        cs += checks.pool1_rank(js, gt, w, ws, pts, now)
        cs.append(checks.dominates(js, checks.POOL6, a.query_heavy, w))
        cs.append(checks.dominates(js, checks.POOL7, a.rule_heavy, w))
        cs.append(checks.eval_mode(js, a.expect_eval, w))
        cs += checks.absent_from_pool(js, gt, w, ws, a.expect_eval, observer)
        cs += checks.lifecycle(js, gt, w, ws, now)
        cs += checks.rule_outputs(ro, gt, w)
        wp = next((x["value"] for x in (p1 or {}).get("assumptions", []) if x["name"] == "write path"), None)
        cs.append(checks.Check("write-path", w, "write path detected correctly", "pass" if wp == a.expect_write_path else "fail",
                               expected=a.expect_write_path, measured=str(wp), tolerance="exact",
                               why="the environment's architecture is known"))
        if overlapping:
            cs += checks.counters_sane(js, w, ev_names)

        windows.append({
            "window": w, "window_s": ws, "end": end, "coverage": coverage, "events": ev_names,
            "run_age_s": end - run_start, "report": js, "rule_outputs": ro.get("rule_outputs"),
            "expected": {n: model.expect_active(tt, end, ws, pts, now=now).__dict__ for n, tt in gt["tenants"].items()},
            "presence": presence, "points": len(pts),
        })
        results += cs

    doc = {
        "env": a.env, "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(), "promcost": promcost_version(),
        "url": a.url, "metrics_tenant": a.metrics_tenant, "expect_eval": a.expect_eval,
        "expect_write_path": a.expect_write_path, "events": [{k: v for k, v in e.items() if not k.startswith("_")} for e in events],
        "checks": [c.as_dict() for c in results],
    }
    (out / "checks.json").write_text(json.dumps(doc, indent=2))
    (out / "report.md").write_text(report.render(doc, gt, windows))
    if a.events:
        shutil.copy(a.events, raw / "events.json")
    fails = [c for c in results if c.status == "fail"]
    print(f"{len(results)} checks: {sum(c.status == 'pass' for c in results)} pass, {len(fails)} fail, "
          f"{sum(c.status == 'skip' for c in results)} skip → {out / 'report.md'}", file=sys.stderr)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
