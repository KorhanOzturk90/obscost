"""The validation checks. Each returns one or more Check records, and every
Check states its tolerance and why that tolerance is the right one — a
reader should be able to disagree with a tolerance, not have to guess it.
"""

from __future__ import annotations

import re
from dataclasses import asdict, dataclass, field

import model

POOL1, POOL6, POOL7 = "ingester_memory", "query_path", "ruler_cpu"
POOL_TITLE = {POOL1: "pool 1 (ingester memory)", POOL6: "pool 6 (query path)", POOL7: "pool 7 (ruler CPU)"}


@dataclass
class Check:
    id: str
    window: str
    title: str
    status: str  # pass | fail | skip | info
    expected: str = ""
    measured: str = ""
    tolerance: str = ""
    why: str = ""
    detail: list[str] = field(default_factory=list)
    issue: str = ""  # a filed issue this result is evidence for

    def as_dict(self) -> dict:
        return asdict(self)


def pool(report: dict, pid: str) -> dict | None:
    for p in report.get("pools") or []:
        if p["pool"]["id"] == pid:
            return p
    return None


def tenants(p: dict) -> dict[str, dict]:
    return {t["tenant"]: t for t in (p.get("tenants") or [])}


def fmt(v: float | None) -> str:
    if v is None:
        return "—"
    if abs(v) >= 100:
        return f"{v:,.0f}"
    return f"{v:,.2f}"


# --- invariants -----------------------------------------------------------

def shares_sum(report: dict, window: str) -> list[Check]:
    out = []
    for p in report.get("pools") or []:
        pid = p["pool"]["id"]
        shares = [t.get("share") for t in p.get("tenants") or []]
        if not shares or (p.get("total_raw") or 0) <= 0:
            out.append(Check(f"shares-sum/{pid}", window, f"{POOL_TITLE[pid]}: shares sum to 100%", "skip",
                             measured="no measured tenant with a non-zero driver",
                             why="shares are undefined when the pool total is 0, and promcost says so instead of rendering 0%"))
            continue
        missing = [t["tenant"] for t in p["tenants"] if t.get("share") is None]
        total = sum(s for s in shares if s is not None) * 100
        ok = abs(total - 100) <= 0.1 and not missing
        out.append(Check(
            f"shares-sum/{pid}", window, f"{POOL_TITLE[pid]}: shares sum to 100%", "pass" if ok else "fail",
            expected="100%", measured=f"{total:.4f}%" + (f"; tenants with no share: {missing}" if missing else ""),
            tolerance="±0.1pp",
            why="shares are computed exactly from one set of drivers; only float rounding may separate them from 100%"))
    return out


def unmeasured_not_zero(report: dict, md: str, window: str) -> list[Check]:
    """A tenant in a pool's `unmeasured` list must not also carry a figure,
    and its markdown row must say "not measured", never 0."""
    rows = md_rows(md)
    out = []
    for p in report.get("pools") or []:
        pid = p["pool"]["id"]
        unmeasured = p.get("unmeasured") or []
        listed = tenants(p)
        bad = []
        for t in unmeasured:
            if t in listed:
                bad.append(f"{t}: in both `unmeasured` and `tenants` (driver {listed[t].get('driver_raw')})")
            cells = rows.get(pid, {}).get(t)
            if cells is None:
                bad.append(f"{t}: no row in the markdown report")
            elif not all(c == "not measured" for c in cells):
                bad.append(f"{t}: markdown row reads {cells}")
        out.append(Check(
            f"not-measured/{pid}", window, f"{POOL_TITLE[pid]}: unmeasured tenants are \"not measured\", never 0",
            "fail" if bad else "pass",
            expected="every cell of each unmeasured tenant's row reads \"not measured\"",
            measured=f"{len(unmeasured)} unmeasured: {', '.join(unmeasured) or 'none'}",
            tolerance="exact", why="ADR 0003 decision 5: absence is a different fact from zero",
            detail=bad))
    return out


_POOL_HEADINGS = {"ingester memory": POOL1, "query path": POOL6, "ruler cpu": POOL7}


def md_rows(md: str) -> dict[str, dict[str, list[str]]]:
    """pool id -> tenant -> the figure cells of its table row."""
    out: dict[str, dict[str, list[str]]] = {}
    current = None
    for line in md.splitlines():
        if line.startswith("### "):
            current = None  # a pool's assumptions or notes, not its tenant table
            continue
        if line.startswith("## "):
            current = _POOL_HEADINGS.get(line[3:].strip().lower())
            continue
        if current and line.startswith("| ") and not line.startswith("| **") and not line.startswith("|---"):
            cells = [c.strip() for c in line.strip().strip("|").split("|")]
            if cells[0] in ("Tenant", "Input"):
                continue
            out.setdefault(current, {})[cells[0]] = cells[1:]
    return out


# --- pool 1 against ground truth -----------------------------------------

POOL1_REL_TOL = 0.01


def pool1_truth(report: dict, gt: dict, window: str, window_s: float, pipeline_points: list[float],
                presence: dict[str, int], now: float, event_tol: float | None, event_names: list[str]) -> list[Check]:
    p = pool(report, POOL1)
    if p is None:
        return []
    end = model.ts(p["end"])
    listed = tenants(p)
    out = []
    for name, tt in sorted(gt["tenants"].items()):
        exp = model.expect_active(tt, end, window_s, pipeline_points, now=now)
        lo, hi = exp.time_weighted
        rel = event_tol if event_tol is not None else POOL1_REL_TOL
        lo_t, hi_t = lo * (1 - rel), hi * (1 + rel)
        t = listed.get(name)
        why = ("the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of "
               "the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the "
               "band is the model evaluated at both extremes, plus ±1% for anything else")
        if event_tol is not None:
            why = (f"the window overlaps {', '.join(event_names)}: during an ingester rollout the sum-then-average "
                   f"query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened "
                   f"to ±{rel:.0%}")
        tol = f"band [{fmt(lo)}, {fmt(hi)}] ±{rel:.0%}"
        if hi == 0:
            # No active series anywhere in the window: absent, or an explicit 0.
            ok = t is None or (t.get("driver") or 0) == 0
            out.append(Check(f"pool1-truth/{name}", window, f"pool 1: `{name}` active series", "pass" if ok else "fail",
                             expected="no active series in this window (absent or 0)",
                             measured="not measured" if t is None else fmt(t.get("driver")),
                             tolerance="exact", why="the tenant wrote no sample within the idle timeout of any point"))
            continue
        if t is None:
            out.append(Check(f"pool1-truth/{name}", window, f"pool 1: `{name}` active series", "fail",
                             expected=f"{fmt(lo)}–{fmt(hi)}", measured="not measured", tolerance=tol, why=why))
            continue
        v = t.get("driver")
        ok = v is not None and lo_t <= v <= hi_t
        detail = [f"subquery points with pipeline data: {exp.points}; points this tenant was present (model): "
                  f"{exp.present_points}; (Mimir): {presence.get(name, 0)}"]
        issue = ""
        if not ok and exp.presence_weighted:
            plo, phi = exp.presence_weighted
            if plo * (1 - rel) <= (v or 0) <= phi * (1 + rel):
                detail.append(f"matches the presence-weighted average [{fmt(plo)}, {fmt(phi)}]: the figure is averaged "
                              f"over only the points where the tenant had series, not over the whole window")
                issue = "presence-weighted"
        out.append(Check(f"pool1-truth/{name}", window, f"pool 1: `{name}` active series (after the replication divisor)",
                         "pass" if ok else "fail", expected=f"{fmt(lo)}–{fmt(hi)}", measured=fmt(v),
                         tolerance=tol, why=why, detail=detail, issue=issue))
    return out


def pool1_rank(report: dict, gt: dict, window: str, window_s: float, pipeline_points: list[float], now: float) -> list[Check]:
    p = pool(report, POOL1)
    if p is None:
        return []
    end = model.ts(p["end"])
    exp = {n: sum(model.expect_active(tt, end, window_s, pipeline_points, now=now).time_weighted) / 2
           for n, tt in gt["tenants"].items()}
    got = {n: (t.get("driver") or 0) for n, t in tenants(p).items() if n in gt["tenants"]}
    bad = []
    names = sorted(exp, key=exp.get, reverse=True)
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            if exp[a] > 1.1 * exp[b] and got.get(a, 0) <= got.get(b, 0):
                bad.append(f"{a} (expected {fmt(exp[a])}, got {fmt(got.get(a))}) should outrank "
                           f"{b} (expected {fmt(exp[b])}, got {fmt(got.get(b))})")
    return [Check("pool1-rank", window, "pool 1: tenant rank order matches the workload shapes",
                  "fail" if bad else "pass",
                  expected=" > ".join(f"{n}" for n in names if exp[n] > 0),
                  measured=" > ".join(n for n in sorted(got, key=got.get, reverse=True)),
                  tolerance="pairs whose expected figures are within 10% of each other are not ranked",
                  why="a near-tie can legitimately flip on gauge lag; everything else is a difference of shape",
                  detail=bad)]


# --- pools 6 and 7 --------------------------------------------------------

def dominates(report: dict, pid: str, tenant: str, window: str, min_share: float = 0.5) -> Check:
    p = pool(report, pid)
    title = f"{POOL_TITLE[pid]}: `{tenant}` dominates"
    if p is None or not p.get("tenants") or (p.get("total_raw") or 0) <= 0:
        return Check(f"dominates/{pid}", window, title, "fail", expected=f"`{tenant}` first, ≥{min_share:.0%}",
                     measured="no measured tenant in this pool")
    ts_ = sorted(p["tenants"], key=lambda t: t.get("share") or 0, reverse=True)
    top = ts_[0]
    mine = tenants(p).get(tenant)
    share = (mine or {}).get("share") or 0
    ok = top["tenant"] == tenant and share >= min_share
    return Check(f"dominates/{pid}", window, title, "pass" if ok else "fail",
                 expected=f"`{tenant}` first, with at least {min_share:.0%}",
                 measured="; ".join(f"{t['tenant']} {100 * (t.get('share') or 0):.1f}%" for t in ts_[:4]),
                 tolerance=f"rank 1 and ≥{min_share:.0%}",
                 why=f"the workload makes `{tenant}` this pool's driver by a wide margin by construction; a simple "
                     f"majority is a weaker claim than the margin built in, so it should hold under noise",
                 detail=[f"driver: {p.get('driver_kind')} ({p['pool'].get('driver')})"])


def eval_mode(report: dict, expected: str, window: str) -> Check:
    seen = {p["pool"]["id"]: p.get("rule_evaluation") for p in report.get("pools") or [] if p["pool"]["id"] in (POOL6, POOL7)}
    ok = bool(seen) and all(v == expected for v in seen.values())
    return Check("eval-mode", window, "ruler evaluation mode detected correctly", "pass" if ok else "fail",
                 expected=expected, measured=", ".join(f"{POOL_TITLE[k]}: {v}" for k, v in seen.items()),
                 tolerance="exact", why="the environment's ruler configuration is known")


def absent_from_pool(report: dict, gt: dict, window: str, window_s: float, eval_expected: str,
                     observer: set[str]) -> list[Check]:
    """Tenants that by construction put nothing in pool 6 must be "not
    measured" there, not 0. Under local evaluation that is every tenant that
    issues no queries; under remote evaluation a tenant's rules are queries
    too. `observer` is the tenants promcost itself queried (--rule-outputs)
    within the window, which make the tenant measured through no action of
    its own."""
    p = pool(report, POOL6)
    if p is None:
        return []
    listed = tenants(p)
    out = []
    for name, tt in sorted(gt["tenants"].items()):
        queries = bool(tt.get("queries"))
        rules = bool((tt.get("rules") or {}).get("recording") or (tt.get("rules") or {}).get("alerting"))
        if queries or (eval_expected == "remote" and rules):
            continue
        t = listed.get(name)
        if t is None:
            out.append(Check(f"pool6-absent/{name}", window, f"pool 6: `{name}` (never queries) is not measured", "pass",
                             expected="not measured", measured="not measured", tolerance="exact",
                             why="the tenant issues no queries and rules are evaluated locally"))
            continue
        issue = "observer" if name in observer else ""
        out.append(Check(
            f"pool6-absent/{name}", window, f"pool 6: `{name}` (never queries) is not measured", "fail",
            expected="not measured", measured=f"measured: {t.get('driver_raw')} {p.get('unit')} ({100 * (t.get('share') or 0):.2f}%)",
            tolerance="exact", why="the tenant issues no queries and rules are evaluated locally",
            detail=(["promcost --rule-outputs ran count queries as this tenant inside the window: that is promcost's own "
                     "query traffic, charged to the tenant"] if issue else []),
            issue=issue))
    return out


# --- mid-run tenants ------------------------------------------------------

def lifecycle(report: dict, gt: dict, window: str, window_s: float, now: float) -> list[Check]:
    """The mid-run start and the mid-run silence, stated in words a reader
    can check against the timeline."""
    out = []
    p1 = pool(report, POOL1)
    if p1 is None:
        return out
    end = model.ts(p1["end"])
    start = end - window_s
    listed = tenants(p1)
    for name, tt in sorted(gt["tenants"].items()):
        segs = model.segments(tt, now)
        planned_start = tt.get("planned_start_after_s") or 0
        planned_stop = tt.get("planned_stop_after_s")
        if not planned_start and not planned_stop:
            continue
        run_start = model.ts(gt["started_at"])
        first = segs[0][0] if segs else None
        last = segs[-1][1] if segs else None
        if first is None or first > end:
            # Not started yet as of this window.
            t = listed.get(name)
            ok = t is None and name in (p1.get("unmeasured") or [])
            out.append(Check(f"lifecycle/{name}", window, f"`{name}` before it starts: not measured", "pass" if ok else "fail",
                             expected="not measured (listed in `unmeasured`)",
                             measured="not measured" if t is None else f"driver {t.get('driver')}",
                             tolerance="exact", why="the tenant has written nothing yet",
                             detail=[f"planned start: {planned_start / 60:.0f}m after the run began"]))
        elif planned_stop and last is not None and last + model.IDLE_TIMEOUT_S + 120 < start:
            t = listed.get(name)
            v = None if t is None else t.get("driver")
            ok = t is None or v == 0
            out.append(Check(f"lifecycle/{name}", window, f"`{name}` after it went silent: 0 or not measured",
                             "pass" if ok else "fail", expected="0 active series, or not measured",
                             measured="not measured" if t is None else fmt(v), tolerance="exact",
                             why="its last sample is more than the 20m idle timeout before the window opens",
                             detail=[f"last sample {(last - run_start) / 60:.0f}m into the run"]))
    return out


# --- rule outputs ---------------------------------------------------------

def rule_outputs(ro_report: dict, gt: dict, window: str) -> list[Check]:
    ro = ro_report.get("rule_outputs")
    if not ro:
        return [Check("rule-outputs", window, "--rule-outputs counts match the expected record: series", "fail",
                      measured="no rule_outputs section in the report")]
    at = model.ts(ro["measured_at"])
    by_tenant = {t["tenant"]: t for t in ro.get("tenants") or []}
    out = []
    for name, tt in sorted(gt["tenants"].items()):
        rules = tt.get("rules")
        if not rules:
            continue
        got = by_tenant.get(name)
        if tt.get("rules_pushed_at") is None:
            ok = got is None or (got.get("recording_rules", 0) == 0 and got.get("alerting_rules", 0) == 0)
            out.append(Check(f"rule-outputs/{name}", window, f"rule outputs: `{name}` (rules not pushed yet)",
                             "pass" if ok else "fail", expected="no rules", measured="none" if got is None else
                             f"{got.get('recording_rules')} recording, {got.get('alerting_rules')} alerting",
                             tolerance="exact", why="its rules are pushed when it starts"))
            continue
        if got is None:
            out.append(Check(f"rule-outputs/{name}", window, f"rule outputs: `{name}`", "fail",
                             expected=f"{rules['recording']} recording, {rules['alerting']} alerting rules",
                             measured="tenant missing from rule_outputs"))
            continue
        state, expected = model.record_outputs_expected(tt, at)
        bad = []
        if got.get("recording_rules") != rules["recording"] or got.get("alerting_rules") != rules["alerting"]:
            bad.append(f"rule counts: expected {rules['recording']} recording / {rules['alerting']} alerting, "
                       f"got {got.get('recording_rules')} / {got.get('alerting_rules')}")
        measured_total = 0
        if state == "transition":
            out.append(Check(f"rule-outputs/{name}", window, f"rule outputs: `{name}` record: series", "skip",
                             measured="measured within one lookback of a start, stop or push",
                             why="the exact count is undefined at the edge; not checked"))
            continue
        for r in got.get("rules") or []:
            if r.get("kind") != "recording":
                continue
            want = expected.get(r["output_metric"])
            have = r.get("series")
            measured_total += have or 0
            if want is None:
                bad.append(f"{r['output_metric']}: not in the ground truth")
            elif (have or 0) != want:
                bad.append(f"{r['output_metric']}: expected {want}, got {have} ({r.get('status')})")
        want_total = sum(expected.values())
        out.append(Check(f"rule-outputs/{name}", window, f"rule outputs: `{name}` record: series",
                         "fail" if bad else "pass",
                         expected=f"{want_total} series over {len(expected)} record: names ({state})",
                         measured=f"{measured_total} series ({got.get('output_metrics')} output metrics)",
                         tolerance="exact",
                         why="each rule is sum by (shard) or sum over a metric with a known shard count, so its output "
                             "series are known in advance; the count is an instant query",
                         detail=bad[:10] + ([f"… and {len(bad) - 10} more"] if len(bad) > 10 else [])))
    return out


# --- operational events ---------------------------------------------------

def counters_sane(report: dict, window: str, event_names: list[str]) -> list[Check]:
    """Across a counter reset or rollout, increase() must still produce
    finite, non-negative per-tenant figures."""
    bad = []
    for p in report.get("pools") or []:
        if p["pool"]["id"] == POOL1:
            continue
        for t in p.get("tenants") or []:
            v = t.get("driver_raw")
            if v is None or v < 0 or v != v or v in (float("inf"),):
                bad.append(f"{POOL_TITLE[p['pool']['id']]}: {t['tenant']} = {v}")
    return [Check("events/counters", window, f"counter pools stay sane across {', '.join(event_names)}",
                  "fail" if bad else "pass", expected="every per-tenant figure finite and ≥ 0",
                  measured="all finite and ≥ 0" if not bad else f"{len(bad)} bad", tolerance="exact",
                  why="increase() compensates for counter resets; a negative or infinite figure would mean it did not",
                  detail=bad)]


_SLUG = re.compile(r"[^a-z0-9]+")


def slug(s: str) -> str:
    return _SLUG.sub("-", s.lower()).strip("-")
