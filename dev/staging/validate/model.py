"""What Mimir should report, given what loadgen actually produced.

loadgen's ground-truth file records facts only: when each tenant's samples
really arrived (segments), how big it is, which rules it has and when they
were pushed. This module turns those facts into the figure promcost's pool 1
should show, using Mimir's own semantics, each stated where it is used:

- a series is *active* while it has had a sample within the idle timeout
  (-ingester.active-series-metrics-idle-timeout, 20m by default);
- the ingester recomputes cortex_ingester_active_series every minute, and
  that gauge is scraped every 15s, so the value read at time t describes
  the ingester somewhere in [t - 75s, t];
- a recording rule writes output while its source has samples inside the
  rule's rate() range, and its outputs are active series of the same tenant.

Everything is in unix seconds.
"""

from __future__ import annotations

import datetime as dt
import math
from dataclasses import dataclass

IDLE_TIMEOUT_S = 20 * 60
# Gauge staleness: the 1m active-series update period plus a 15s scrape.
GAUGE_LAG_S = 75
# Idle series are purged at the next 1m update, not the instant they expire.
PURGE_SLACK_S = 60


def ts(s: str | None) -> float | None:
    if not s:
        return None
    return dt.datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


@dataclass(frozen=True)
class Group:
    """`count` series that received samples from `first` to `last`."""

    count: int
    first: float
    last: float
    what: str


def segments(tt: dict, now: float) -> list[tuple[float, float]]:
    """Periods the tenant's samples were accepted. A segment still running
    (no stopped_at, last sample recent) is extended to `now`."""
    out = []
    for seg in tt.get("segments") or []:
        first, last = ts(seg.get("first_sample_at")), ts(seg.get("last_sample_at"))
        if first is None or last is None:
            continue
        if not seg.get("stopped_at") and now - last < 60:
            last = max(last, now)
        out.append((first, min(last, now)))
    return out


def groups(tt: dict, now: float) -> list[Group]:
    segs = segments(tt, now)
    live = tt["series"]["live"]
    period = tt.get("churn_period_s")
    out: list[Group] = []
    for first, last in segs:
        if period:
            # Generations are aligned to unix time: generation g is written
            # during [g*P, (g+1)*P).
            g0, g1 = int(first // period), int(last // period)
            for g in range(g0, g1 + 1):
                gf = max(first, g * period)
                gl = min(last, (g + 1) * period - tt["series"]["interval_s"])
                if gl >= gf:
                    out.append(Group(live, gf, gl, f"generation {g}"))
        else:
            out.append(Group(live, first, last, "series"))

    rules = tt.get("rules") or {}
    pushed = ts(tt.get("rules_pushed_at"))
    n_out = rules.get("record_series_total") or 0
    if n_out and pushed is not None:
        interval, rng = rules["interval_s"], rules["range_s"]
        for first, last in segs:
            # First output: the first evaluation after both the push and two
            # source samples (rate() needs two). Last output: the last
            # evaluation whose range still holds two source samples, which
            # can come up to one evaluation interval after that moment
            # (evaluation times are the ruler's, not ours).
            of = max(first + tt["series"]["interval_s"], pushed) + interval
            ol = last + rng - tt["series"]["interval_s"] + interval
            if ol >= of:
                out.append(Group(n_out, of, min(ol, now), "recording-rule outputs"))
    return out


def active_at(gs: list[Group], u: float, idle: float = IDLE_TIMEOUT_S, restarts: tuple[float, ...] = ()) -> int:
    """Series with a sample within the idle timeout of u. A restarted
    ingester rebuilds its active-series tracker only from samples it receives
    after the restart, so a group whose last sample came before a restart
    that ended at or before u is not counted again: measured on staging, a
    churny tenant read 4,011 instead of 6,011 for up to 20m after each
    ingester rollout, while Mimir still held those series in memory."""
    return sum(g.count for g in gs if g.first <= u and g.last > u - idle
               and not any(g.last < r <= u for r in restarts))


def subquery_points(end: float, window_s: float, step_s: float) -> list[float]:
    """Prometheus subquery evaluation times: multiples of step in (end-window, end]."""
    k_hi = math.floor(end / step_s)
    k_lo = math.floor((end - window_s) / step_s) + 1
    return [k * step_s for k in range(k_lo, k_hi + 1)]


def subquery_step(window_s: float) -> float:
    """promcost's subqueryStep: window/1440, at least 1m, whole seconds."""
    return max(60.0, float(int(window_s / 1440)))


@dataclass
class Expectation:
    # Time-weighted: the tenant's average over every point at which the
    # metrics pipeline itself had data, counting points where the tenant had
    # no active series as 0. This is what "average over the window" means for
    # a cost figure.
    time_weighted: tuple[float, float]
    # Presence-weighted: the average over only the points where the tenant
    # had any active series — what avg_over_time of a per-tenant series
    # computes when the tenant is absent for part of the window.
    presence_weighted: tuple[float, float] | None
    points: int
    present_points: int


def expect_active(tt: dict, end: float, window_s: float, pipeline_points: list[float] | None = None,
                  now: float | None = None, restarts: tuple[float, ...] = ()) -> Expectation:
    """The pool-1 figure for one tenant over (end-window, end], as a
    (low, high) band covering the gauge's lag and purge cadence."""
    now = end if now is None else now
    gs = groups(tt, now)
    pts = pipeline_points if pipeline_points is not None else subquery_points(end, window_s, subquery_step(window_s))
    # The low bound also applies ingester restarts (see active_at); the high
    # bound does not, so the band covers a tracker that re-learned a series
    # and one that did not.
    lo_vals = [active_at(gs, p - GAUGE_LAG_S, restarts=restarts) for p in pts]
    hi_vals = [active_at(gs, p, IDLE_TIMEOUT_S + PURGE_SLACK_S) for p in pts]
    # The two bounds are not ordered point by point (lag delays both rises
    # and falls), so take each point's min and max.
    lows = [min(a, b) for a, b in zip(lo_vals, hi_vals)]
    highs = [max(a, b) for a, b in zip(lo_vals, hi_vals)]
    n = len(pts)
    tw = (sum(lows) / n, sum(highs) / n) if n else (0.0, 0.0)
    present = [i for i in range(n) if highs[i] > 0]
    pw = None
    if present:
        pw = (sum(lows[i] for i in present) / len(present), sum(highs[i] for i in present) / len(present))
    return Expectation(tw, pw, n, len(present))


def record_outputs_expected(tt: dict, at: float) -> tuple[str, dict[str, int]]:
    """Expected series per record: name at instant `at`.

    Returns ("present", counts), ("absent", zeros) or ("transition", {}) when
    `at` is too close to an edge (start, stop, push) for an exact answer.
    """
    rules = tt.get("rules") or {}
    rec = rules.get("record_series") or {}
    if not rec:
        return "absent", {}
    pushed = ts(tt.get("rules_pushed_at"))
    if pushed is None:
        return "absent", {k: 0 for k in rec}
    margin = 2 * rules["interval_s"] + 5 * 60  # two evaluations + PromQL's 5m lookback
    for g in groups(tt, at):
        if g.what != "recording-rule outputs":
            continue
        if g.first + margin <= at <= g.last:
            return "present", dict(rec)
        if g.first - margin <= at <= g.last + margin:
            return "transition", {}
    return "absent", {k: 0 for k in rec}
