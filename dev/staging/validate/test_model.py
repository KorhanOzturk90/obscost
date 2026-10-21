"""Unit tests for the ground-truth model: python3 -m unittest discover -s dev/staging/validate"""

import datetime as dt
import unittest

import checks
import model

T0 = 1_800_000_000.0  # a multiple of 600, so churn generations align with T0


def iso(t: float) -> str:
    return dt.datetime.fromtimestamp(t, dt.timezone.utc).isoformat().replace("+00:00", "Z")


def tenant(live=1000, first=T0, last=None, stopped=False, churn=None, rules=None, pushed=None):
    seg = {"first_sample_at": iso(first), "last_sample_at": iso(last if last is not None else first)}
    if stopped:
        seg["stopped_at"] = seg["last_sample_at"]
    tt = {"series": {"live": live, "interval_s": 15}, "segments": [seg]}
    if churn:
        tt["churn_period_s"] = churn
    if rules:
        tt["rules"] = rules
        tt["rules_pushed_at"] = iso(pushed if pushed is not None else first)
    return tt


class Active(unittest.TestCase):
    def test_steady(self):
        tt = tenant(last=T0 + 7200)
        e = model.expect_active(tt, T0 + 7200, 3600, now=T0 + 7200)
        self.assertEqual(e.time_weighted, (1000, 1000))

    def test_churn_steady_state_is_live_times_generations_in_idle_window(self):
        # 10m generations, 20m idle timeout: the current generation plus the
        # two before it are active — 3x live, give or take the last minute.
        tt = tenant(first=T0, last=T0 + 4 * 3600, churn=600)
        gs = model.groups(tt, T0 + 4 * 3600)
        self.assertEqual(model.active_at(gs, T0 + 3 * 3600 + 300), 3000)

    def test_silent_tenant_decays_after_idle_timeout(self):
        stop = T0 + 3600
        tt = tenant(last=stop, stopped=True)
        gs = model.groups(tt, T0 + 7200)
        self.assertEqual(model.active_at(gs, stop + 19 * 60), 1000)
        self.assertEqual(model.active_at(gs, stop + 21 * 60), 0)

    def test_latecomer_time_weighted_vs_presence_weighted(self):
        # Present for the last 15m of a 60m window: 1/4 of the points.
        end = T0 + 3600
        tt = tenant(first=end - 900, last=end)
        e = model.expect_active(tt, end, 3600, now=end)
        lo, hi = e.time_weighted
        self.assertTrue(200 <= lo <= hi <= 300, e.time_weighted)
        plo, phi = e.presence_weighted
        # The first present point reads 0 at the lagged edge, hence not 1000.
        self.assertTrue(850 <= plo <= phi <= 1000, e.presence_weighted)

    def test_rule_outputs_count_as_active_series(self):
        rules = {"record_series_total": 11, "interval_s": 60, "range_s": 300, "record_series": {"a": 10, "b": 1}}
        tt = tenant(last=T0 + 3600, rules=rules)
        gs = model.groups(tt, T0 + 3600)
        self.assertEqual(model.active_at(gs, T0 + 1800), 1011)


class Subquery(unittest.TestCase):
    def test_points_are_step_aligned_and_left_open(self):
        pts = model.subquery_points(T0 + 90, 600, 60)
        self.assertEqual(pts[0], T0 - 480)
        self.assertEqual(pts[-1], T0 + 60)
        self.assertEqual(len(pts), 10)

    def test_step_matches_promcost(self):
        self.assertEqual(model.subquery_step(3600), 60)
        self.assertEqual(model.subquery_step(24 * 3600), 60)
        self.assertEqual(model.subquery_step(7 * 86400), 420)


class RuleOutputs(unittest.TestCase):
    rules = {"record_series_total": 11, "interval_s": 60, "range_s": 300, "record_series": {"a": 10, "b": 1}}

    def test_present_mid_run(self):
        tt = tenant(last=T0 + 3600, rules=self.rules)
        self.assertEqual(model.record_outputs_expected(tt, T0 + 1800), ("present", {"a": 10, "b": 1}))

    def test_absent_long_after_stop(self):
        tt = tenant(last=T0 + 3600, stopped=True, rules=self.rules)
        self.assertEqual(model.record_outputs_expected(tt, T0 + 7200), ("absent", {"a": 0, "b": 0}))

    def test_transition_near_start(self):
        tt = tenant(last=T0 + 3600, rules=self.rules)
        self.assertEqual(model.record_outputs_expected(tt, T0 + 120)[0], "transition")


class Markdown(unittest.TestCase):
    MD = """# promcost tenant cost report

## Ingester memory

| Tenant | Active series | Share | ≈ ingesters |
|---|---:|---:|---:|
| steady | 30,000 | 43.6% | 0.44 |
| latecomer | not measured | not measured | not measured |
| **Total** | **68,739** | **100%** | **1** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `x` | `y` |
"""

    def test_rows_are_read_per_pool(self):
        rows = checks.md_rows(self.MD)
        self.assertEqual(rows[checks.POOL1]["latecomer"], ["not measured"] * 3)
        self.assertEqual(rows[checks.POOL1]["steady"][0], "30,000")
        self.assertNotIn("driver", rows[checks.POOL1])

    def test_unmeasured_rendered_as_zero_fails(self):
        report = {"pools": [{"pool": {"id": checks.POOL1}, "unmeasured": ["latecomer"], "tenants": []}]}
        md = self.MD.replace("| latecomer | not measured | not measured | not measured |", "| latecomer | 0 | 0.0% | 0.00 |")
        [c] = checks.unmeasured_not_zero(report, md, "1h")
        self.assertEqual(c.status, "fail")


if __name__ == "__main__":
    unittest.main()
