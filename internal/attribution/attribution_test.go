package attribution

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func def(tenant, namespace, group, name string, kind rule.Kind) rule.AnnotatedRule {
	r := rule.Rule{Kind: kind}
	if kind == rule.KindRecording {
		r.Record = name
	} else {
		r.Alert = name
	}
	return rule.AnnotatedRule{
		Rule:     r,
		Tenant:   tenant,
		Group:    rule.RuleGroupMeta{Name: group},
		Location: rule.SourceLocation{File: namespace, Group: group, Rule: name},
	}
}

func exec(tenant, namespace, group, name string, samples uint64) rule.RuleExecution {
	return rule.RuleExecution{
		Tenant:           tenant,
		Namespace:        namespace,
		Group:            group,
		RuleName:         name,
		Timestamp:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		SamplesProcessed: rule.Ptr(samples),
	}
}

// execNoSamples builds an execution that reports every stat as genuinely
// unmeasured (nil), not zero — exercises Aggregate's missing-vs-zero
// handling (see rule.RuleExecution's doc comment).
func execNoSamples(tenant, namespace, group, name string) rule.RuleExecution {
	return rule.RuleExecution{
		Tenant:    tenant,
		Namespace: namespace,
		Group:     group,
		RuleName:  name,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestAggregate_MultiTenantShareSumsCorrectly(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "r1", rule.KindRecording),
		def("payments", "p/rules.yaml", "g", "r2", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		exec("analytics", "a/rules.yaml", "g", "r1", 40),
		exec("analytics", "a/rules.yaml", "g", "r1", 40),
		exec("analytics", "a/rules.yaml", "g", "r1", 40),
		exec("payments", "p/rules.yaml", "g", "r2", 20),
	}

	report := Aggregate(executions, definitions)

	if report.TotalExecutions != 4 {
		t.Fatalf("TotalExecutions = %d, want 4", report.TotalExecutions)
	}
	if report.TotalSamples != 140 {
		t.Fatalf("TotalSamples = %d, want 140", report.TotalSamples)
	}
	var sumExecutions int
	var sumSharePct float64
	for _, ta := range report.Tenants {
		sumExecutions += ta.Executions
		sumSharePct += ta.ExecutionSharePct
	}
	if sumExecutions != report.TotalExecutions {
		t.Errorf("sum(TenantAggregate.Executions) = %d, want %d", sumExecutions, report.TotalExecutions)
	}
	if math.Abs(sumSharePct-100) > 1e-9 {
		t.Errorf("sum(ExecutionSharePct) = %v, want 100", sumSharePct)
	}

	// analytics has more executions -> ranked first.
	if report.Tenants[0].Tenant != "analytics" {
		t.Errorf("Tenants[0] = %s, want analytics (higher share ranks first)", report.Tenants[0].Tenant)
	}
}

func TestAggregate_TenantWithDefinitionsButZeroExecutions(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("quiet", "q/rules.yaml", "g", "r1", rule.KindRecording),
		def("quiet", "q/rules.yaml", "g", "r2", rule.KindRecording),
	}
	report := Aggregate(nil, definitions)

	if len(report.Tenants) != 1 {
		t.Fatalf("len(Tenants) = %d, want 1", len(report.Tenants))
	}
	ta := report.Tenants[0]
	if ta.Tenant != "quiet" || ta.RuleCount != 2 {
		t.Errorf("tenant = %+v, want Tenant=quiet RuleCount=2", ta)
	}
	if ta.Executions != 0 || ta.ExecutionSharePct != 0 || ta.SampleSharePct != 0 {
		t.Errorf("expected all-zero workload, got %+v", ta)
	}
}

func TestAggregate_EmptyInputHasNoNaNAndMarshalsCleanly(t *testing.T) {
	report := Aggregate(nil, nil)

	if report.TotalExecutions != 0 || report.TotalSamples != 0 || len(report.Tenants) != 0 {
		t.Fatalf("expected a fully empty report, got %+v", report)
	}

	// The real regression this guards: encoding/json hard-errors on NaN/Inf
	// float64 values, so a naive part/0 division anywhere would break this.
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json.Marshal(empty report): %v", err)
	}
}

func TestAggregate_UnmatchedExecutionCountsTowardTenantNotRules(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "known", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		exec("analytics", "a/rules.yaml", "g", "known", 10),
		exec("analytics", "a/rules.yaml", "g", "deleted-rule", 5), // no matching definition
	}

	report := Aggregate(executions, definitions)

	if len(report.Tenants) != 1 {
		t.Fatalf("len(Tenants) = %d, want 1", len(report.Tenants))
	}
	ta := report.Tenants[0]

	if ta.Executions != 2 {
		t.Errorf("tenant Executions = %d, want 2 (unmatched still counts toward tenant total)", ta.Executions)
	}
	if ta.SamplesProcessed != 15 {
		t.Errorf("tenant SamplesProcessed = %d, want 15", ta.SamplesProcessed)
	}
	if len(ta.Rules) != 1 {
		t.Fatalf("len(Rules) = %d, want 1 (only the matched rule)", len(ta.Rules))
	}
	if ta.Rules[0].Executions != 1 {
		t.Errorf("matched rule Executions = %d, want 1", ta.Rules[0].Executions)
	}
	if ta.UnmatchedExecutions != 1 || ta.UnmatchedSamples != 5 {
		t.Errorf("tenant unmatched = executions:%d samples:%d, want 1/5", ta.UnmatchedExecutions, ta.UnmatchedSamples)
	}
	if len(report.Unmatched) != 1 || report.Unmatched[0].RuleName != "deleted-rule" {
		t.Errorf("report.Unmatched = %+v, want one entry for deleted-rule", report.Unmatched)
	}
}

func TestAggregate_NamespaceMismatchIsUnmatchedNotCrash(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "r1", rule.KindRecording),
	}
	// Deliberately wrong namespace string (trailing slash typo) — must not
	// silently mis-join, must land in unmatched.
	executions := []rule.RuleExecution{
		exec("analytics", "a/rules.yaml/", "g", "r1", 10),
	}

	report := Aggregate(executions, definitions)

	if len(report.Tenants) != 1 {
		t.Fatalf("len(Tenants) = %d, want 1", len(report.Tenants))
	}
	ta := report.Tenants[0]
	if len(ta.Rules) != 0 {
		t.Errorf("expected zero matched rules on namespace mismatch, got %+v", ta.Rules)
	}
	if ta.UnmatchedExecutions != 1 {
		t.Errorf("UnmatchedExecutions = %d, want 1", ta.UnmatchedExecutions)
	}
}

func TestAggregate_RuleCountIndependentOfExecutions(t *testing.T) {
	// Three definitions, only one ever executes.
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "r1", rule.KindRecording),
		def("analytics", "a/rules.yaml", "g", "r2", rule.KindRecording),
		def("analytics", "a/rules.yaml", "g", "r3", rule.KindAlerting),
	}
	executions := []rule.RuleExecution{
		exec("analytics", "a/rules.yaml", "g", "r1", 10),
	}

	report := Aggregate(executions, definitions)
	if len(report.Tenants) != 1 {
		t.Fatalf("len(Tenants) = %d, want 1", len(report.Tenants))
	}
	if report.Tenants[0].RuleCount != 3 {
		t.Errorf("RuleCount = %d, want 3 (definition count, independent of execution presence)", report.Tenants[0].RuleCount)
	}
}

func TestAggregate_TieBreakDeterminism(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("beta", "b/rules.yaml", "g", "r", rule.KindRecording),
		def("alpha", "a/rules.yaml", "g", "r", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		exec("beta", "b/rules.yaml", "g", "r", 10),
		exec("alpha", "a/rules.yaml", "g", "r", 10),
	}

	report := Aggregate(executions, definitions)
	if len(report.Tenants) != 2 {
		t.Fatalf("len(Tenants) = %d, want 2", len(report.Tenants))
	}
	// Identical share -> tie-break alphabetically by tenant name.
	if report.Tenants[0].Tenant != "alpha" || report.Tenants[1].Tenant != "beta" {
		t.Errorf("tenant order = [%s, %s], want [alpha, beta]", report.Tenants[0].Tenant, report.Tenants[1].Tenant)
	}
}

// Issue #18 / PR #23 review point #5: ranking must adapt to whichever
// resource-cost proxy the telemetry source actually measured, preferring
// query wall time over the cadence-only Executions/SamplesProcessed
// fallback whenever it's available.
func TestAggregate_RankMetric_PrefersDurationOverSamples(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "cheap", rule.KindRecording),
		def("analytics", "a/rules.yaml", "g", "slow", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		// cheap: huge sample count, tiny wall time.
		{
			Tenant: "analytics", Namespace: "a/rules.yaml", Group: "g", RuleName: "cheap",
			Timestamp:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			SamplesProcessed: rule.Ptr(uint64(1_000_000)), DurationSeconds: rule.Ptr(0.001),
		},
		// slow: tiny sample count, huge wall time.
		{
			Tenant: "analytics", Namespace: "a/rules.yaml", Group: "g", RuleName: "slow",
			Timestamp:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			SamplesProcessed: rule.Ptr(uint64(10)), DurationSeconds: rule.Ptr(9.0),
		},
	}

	report := Aggregate(executions, definitions)

	if report.RankMetric != RankMetricDurationSeconds {
		t.Fatalf("RankMetric = %v, want duration_seconds (should prefer wall time when measured)", report.RankMetric)
	}
	if len(report.Tenants) != 1 || len(report.Tenants[0].Rules) != 2 {
		t.Fatalf("unexpected shape: %+v", report.Tenants)
	}
	// "slow" has 9s of wall time vs "cheap"'s 0.001s -> ranked first despite
	// having 100,000x fewer samples processed.
	if report.Tenants[0].Rules[0].RuleID.Name != "slow" {
		t.Errorf("Rules[0] = %s, want slow (ranked by wall time, not sample count)", report.Tenants[0].Rules[0].RuleID.Name)
	}
}

// When nothing but execution counts was ever measured, Aggregate must still
// produce a usable ranking rather than defaulting to alphabetical order.
func TestAggregate_RankMetric_FallsBackToExecutionsWhenNothingMeasured(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "r1", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		execNoSamples("analytics", "a/rules.yaml", "g", "r1"),
		execNoSamples("analytics", "a/rules.yaml", "g", "r1"),
		execNoSamples("analytics", "a/rules.yaml", "g", "r1"),
	}

	report := Aggregate(executions, definitions)

	if report.RankMetric != RankMetricExecutions {
		t.Fatalf("RankMetric = %v, want executions (nothing else was ever measured)", report.RankMetric)
	}
	if report.Tenants[0].RankValue != 3 {
		t.Errorf("Tenants[0].RankValue = %v, want 3", report.Tenants[0].RankValue)
	}
}

// The missing-vs-zero principle (see valueOr's doc comment) must also hold
// for the *Observed flags: a stat that's never measured must be
// distinguishable from one measured as a genuine zero, at both tenant and
// rule level.
func TestAggregate_Observed_DistinguishesUnmeasuredFromZero(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "r1", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		execNoSamples("analytics", "a/rules.yaml", "g", "r1"),
	}

	report := Aggregate(executions, definitions)
	ta := report.Tenants[0]

	if ta.Observed(RankMetricSamplesProcessed) {
		t.Errorf("tenant SamplesObserved = true, want false (source never measured it)")
	}
	if ta.Observed(RankMetricDurationSeconds) {
		t.Errorf("tenant DurationObserved = true, want false")
	}
	if !ta.Observed(RankMetricExecutions) {
		t.Errorf("tenant Observed(executions) = false, want true (executions are always observed)")
	}
	if len(ta.Rules) != 1 {
		t.Fatalf("len(Rules) = %d, want 1", len(ta.Rules))
	}
	if ta.Rules[0].Observed(RankMetricSamplesProcessed) {
		t.Errorf("rule SamplesObserved = true, want false")
	}
}

// An execution whose source didn't measure SamplesProcessed (nil, not 0)
// must not be counted as "0 samples processed" — it should contribute
// nothing to any sum, distinct from a source that genuinely measured 0.
func TestAggregate_MissingStatIsExcludedNotZero(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("analytics", "a/rules.yaml", "g", "r1", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		exec("analytics", "a/rules.yaml", "g", "r1", 100),     // measured: 100 samples
		execNoSamples("analytics", "a/rules.yaml", "g", "r1"), // unmeasured: unknown, not 0
	}

	report := Aggregate(executions, definitions)
	if len(report.Tenants) != 1 {
		t.Fatalf("len(Tenants) = %d, want 1", len(report.Tenants))
	}
	ta := report.Tenants[0]

	// Both executions count toward Executions (an execution happened,
	// regardless of whether its stats were measured).
	if ta.Executions != 2 {
		t.Errorf("Executions = %d, want 2 (both executions counted, even the unmeasured one)", ta.Executions)
	}
	// Only the measured execution's 100 samples contribute to the sum — the
	// unmeasured one contributes nothing, not a fabricated 0.
	if ta.SamplesProcessed != 100 {
		t.Errorf("SamplesProcessed = %d, want 100 (nil execution must not count as 0)", ta.SamplesProcessed)
	}
	if len(ta.Rules) != 1 || ta.Rules[0].SamplesProcessed != 100 {
		t.Fatalf("Rules = %+v, want one rule with SamplesProcessed=100", ta.Rules)
	}
}

// ADR 0002 decision 2: data volume outranks wall time. The measured reason
// is that wall time is a poor proxy for what a rule actually costs — a rule
// can be slow while fetching nothing, or fast while pulling a large share
// of all data read. This guards the ordering against a well-meaning revert.
func TestAggregate_RankMetric_VolumeOutranksWallTime(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("infra", "a/rules.yaml", "g", "slow_but_fetches_nothing", rule.KindRecording),
		def("infra", "a/rules.yaml", "g", "fast_but_fetches_lots", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		{
			Tenant: "infra", Namespace: "a/rules.yaml", Group: "g", RuleName: "slow_but_fetches_nothing",
			Timestamp:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			DurationSeconds: rule.Ptr(9.0), FetchedBytes: rule.Ptr(uint64(0)),
		},
		{
			Tenant: "infra", Namespace: "a/rules.yaml", Group: "g", RuleName: "fast_but_fetches_lots",
			Timestamp:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			DurationSeconds: rule.Ptr(0.01), FetchedBytes: rule.Ptr(uint64(5_000_000)),
		},
	}

	report := Aggregate(executions, definitions)

	if report.RankMetric != RankMetricFetchedBytes {
		t.Fatalf("RankMetric = %v, want fetched_bytes (volume must outrank wall time)", report.RankMetric)
	}
	if got := report.Tenants[0].Rules[0].RuleID.Name; got != "fast_but_fetches_lots" {
		t.Errorf("Rules[0] = %s, want fast_but_fetches_lots — the 900x slower rule fetched nothing", got)
	}
}

func TestAggregate_GroupTier_ReconcilesToTenantTotal(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("infra", "a/rules.yaml", "alerts", "r1", rule.KindAlerting),
		def("infra", "a/rules.yaml", "alerts", "r2", rule.KindAlerting),
		def("infra", "a/rules.yaml", "records", "r3", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		exec("infra", "a/rules.yaml", "alerts", "r1", 100),
		exec("infra", "a/rules.yaml", "alerts", "r2", 50),
		exec("infra", "a/rules.yaml", "records", "r3", 900),
	}

	report := Aggregate(executions, definitions)
	if report.Granularity != GranularityRule {
		t.Errorf("Granularity = %v, want rule", report.Granularity)
	}
	ta := report.Tenants[0]
	if len(ta.Groups) != 2 {
		t.Fatalf("len(Groups) = %d, want 2", len(ta.Groups))
	}
	// records (900 samples) outranks alerts (150), so it sorts first.
	if ta.Groups[0].Group != "records" {
		t.Errorf("Groups[0] = %q, want records (higher rank value first)", ta.Groups[0].Group)
	}

	var groupExec int
	var groupRank, groupShare float64
	var nested int
	for _, g := range ta.Groups {
		groupExec += g.Executions
		groupRank += g.RankValue
		groupShare += g.RankSharePct
		nested += len(g.Rules)
	}
	if groupExec != ta.Executions {
		t.Errorf("group executions sum to %d, want tenant total %d", groupExec, ta.Executions)
	}
	if nested != len(ta.Rules) {
		t.Errorf("groups nest %d rules, want all %d", nested, len(ta.Rules))
	}
	if math.Abs(groupShare-100) > 1e-9 {
		t.Errorf("group shares sum to %v, want 100", groupShare)
	}
}

// Two files defining a same-named group must not be merged into one row —
// that would silently combine the workload of unrelated groups.
func TestAggregate_GroupTier_SameGroupNameDifferentFilesStaySeparate(t *testing.T) {
	definitions := []rule.AnnotatedRule{
		def("infra", "a.yaml", "shared", "r1", rule.KindRecording),
		def("infra", "b.yaml", "shared", "r2", rule.KindRecording),
	}
	executions := []rule.RuleExecution{
		exec("infra", "a.yaml", "shared", "r1", 10),
		exec("infra", "b.yaml", "shared", "r2", 20),
	}

	report := Aggregate(executions, definitions)
	if got := len(report.Tenants[0].Groups); got != 2 {
		t.Fatalf("len(Groups) = %d, want 2 (same name, different namespaces)", got)
	}
}

func TestAggregateObservations_GroupGranularityReport(t *testing.T) {
	report := AggregateObservations(
		[]TenantObservation{
			{Tenant: "infra", Executions: 3540.4, DurationSeconds: 10.5, DurationObserved: true},
			{Tenant: "analytics", Executions: 270, DurationSeconds: 1.04, DurationObserved: true},
		},
		[]GroupObservation{
			{Tenant: "infra", Namespace: "alerts.yaml", Group: "mimir_alerts", Executions: 305},
			{Tenant: "infra", Namespace: "alerts.yaml", Group: "gossip_alerts", Executions: 90},
			{Tenant: "analytics", Namespace: "workload.yaml", Group: "analytics_alerts", Executions: 270},
		},
	)

	if report.Granularity != GranularityGroup {
		t.Errorf("Granularity = %v, want group", report.Granularity)
	}
	// Tenants rank by measured wall time; groups can only rank by count.
	if report.RankMetric != RankMetricDurationSeconds {
		t.Errorf("RankMetric = %v, want duration_seconds", report.RankMetric)
	}
	if report.GroupRankMetric != RankMetricExecutions {
		t.Errorf("GroupRankMetric = %v, want executions", report.GroupRankMetric)
	}
	if report.Tenants[0].Tenant != "infra" {
		t.Errorf("Tenants[0] = %s, want infra", report.Tenants[0].Tenant)
	}
	// increase() figures are fractional; rounding happens once, centrally.
	if report.Tenants[0].Executions != 3540 {
		t.Errorf("Executions = %d, want 3540 (rounded from 3540.4)", report.Tenants[0].Executions)
	}
	// No rule tier exists at this granularity — and that must not be
	// confused with "no rules ran".
	if len(report.Tenants[0].Rules) != 0 {
		t.Errorf("expected no rule tier, got %+v", report.Tenants[0].Rules)
	}
	if report.Tenants[0].Groups[0].Group != "mimir_alerts" {
		t.Errorf("Groups[0] = %q, want mimir_alerts", report.Tenants[0].Groups[0].Group)
	}
}

// A tenant that appears only in the per-group query (not the per-tenant one)
// must still get a row, or the fleet total is understated.
func TestAggregateObservations_TenantOnlyInGroupQueryStillAppears(t *testing.T) {
	report := AggregateObservations(
		[]TenantObservation{{Tenant: "infra", Executions: 10, DurationSeconds: 1, DurationObserved: true}},
		[]GroupObservation{{Tenant: "ghost", Namespace: "n.yaml", Group: "g", Executions: 5}},
	)
	var found bool
	for _, ta := range report.Tenants {
		if ta.Tenant == "ghost" {
			found = true
		}
	}
	if !found {
		t.Errorf("tenant present only in the group query was dropped: %+v", report.Tenants)
	}
}

func TestAggregateObservations_EmptyInputMarshalsCleanly(t *testing.T) {
	report := AggregateObservations(nil, nil)
	if len(report.Tenants) != 0 || report.TotalExecutions != 0 {
		t.Fatalf("expected an empty report, got %+v", report)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("json.Marshal(empty observations report): %v", err)
	}
}
