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
		SamplesProcessed: samples,
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
