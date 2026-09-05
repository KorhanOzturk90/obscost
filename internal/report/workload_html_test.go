package report

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestWorkloadHTMLReportGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := (workloadHTMLReporter{}).Render(&buf, sampleWorkloadResult()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	golden := "testdata/workload_report.golden.html"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file (run with UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if buf.String() != string(want) {
		t.Errorf("workload html report does not match golden file %s.\n--- got ---\n%s\n--- want ---\n%s", golden, buf.String(), want)
	}
}

func TestWorkloadHTMLReport_EmptyReportNoCrash(t *testing.T) {
	var buf bytes.Buffer
	err := (workloadHTMLReporter{}).Render(&buf, WorkloadResult{GeneratedAt: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
}

// TestWorkloadHTMLReport_EscapesUntrustedNames guards the one behavior that
// would be easy to regress silently: workload_md.go's text/template doesn't
// escape anything, but tenant/group/rule names come from rule files and
// telemetry, so this renderer must use html/template and actually escape
// them rather than emitting raw markup.
func TestWorkloadHTMLReport_EscapesUntrustedNames(t *testing.T) {
	result := WorkloadResult{
		GeneratedAt: time.Now(),
		Tenants: []attribution.TenantAggregate{
			{
				Tenant:     `<script>alert(1)</script>`,
				RuleCount:  1,
				Executions: 1,
				Rules: []attribution.RuleAggregate{
					{
						RuleID: rule.RuleID{
							Tenant:    `<script>alert(1)</script>`,
							Namespace: "ns",
							Group:     `"><img src=x onerror=alert(2)>`,
							Name:      "rule&name",
						},
						Kind:       rule.KindRecording,
						Executions: 1,
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := (workloadHTMLReporter{}).Render(&buf, result); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	for _, raw := range []string{
		`<script>alert(1)</script>`,
		`"><img src=x onerror=alert(2)>`,
	} {
		if strings.Contains(out, raw) {
			t.Errorf("output contains unescaped untrusted input %q:\n%s", raw, out)
		}
	}
	if !strings.Contains(out, `&lt;script&gt;`) {
		t.Errorf("expected escaped tenant name in output, got:\n%s", out)
	}
}

// sampleMultiGroupTenant exercises groupRules with multiple groups, each
// with multiple rules, so the grouping math below is checked against
// something more realistic than sampleWorkloadResult()'s single rule.
func sampleMultiGroupTenant() attribution.TenantAggregate {
	rules := []attribution.RuleAggregate{
		{RuleID: rule.RuleID{Tenant: "infra", Namespace: "ns", Group: "mimir_alerts", Name: "r1"}, Executions: 100, SamplesProcessed: 1000},
		{RuleID: rule.RuleID{Tenant: "infra", Namespace: "ns", Group: "mimir_alerts", Name: "r2"}, Executions: 50, SamplesProcessed: 500},
		{RuleID: rule.RuleID{Tenant: "infra", Namespace: "ns", Group: "mimir_queries", Name: "r3"}, Executions: 200, SamplesProcessed: 2000},
	}
	var totalExec int
	var totalSamples uint64
	for _, r := range rules {
		totalExec += r.Executions
		totalSamples += r.SamplesProcessed
	}
	return attribution.TenantAggregate{
		Tenant:           "infra",
		RuleCount:        len(rules),
		Executions:       totalExec,
		SamplesProcessed: totalSamples,
		Rules:            rules,
	}
}

func TestGroupRules_SharesSumToTenantTotals(t *testing.T) {
	ta := sampleMultiGroupTenant()
	groups := groupRules(ta.Rules, ta.Executions, ta.SamplesProcessed)

	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	var groupExecSum int
	var groupSamplesSum uint64
	for _, g := range groups {
		groupExecSum += g.Executions
		groupSamplesSum += g.Samples

		var ruleExecSum int
		var ruleSamplesSum uint64
		for _, r := range g.Rules {
			ruleExecSum += r.Executions
			ruleSamplesSum += r.Samples
		}
		if ruleExecSum != g.Executions {
			t.Errorf("group %s: rule executions sum to %d, want %d", g.Group, ruleExecSum, g.Executions)
		}
		if ruleSamplesSum != g.Samples {
			t.Errorf("group %s: rule samples sum to %d, want %d", g.Group, ruleSamplesSum, g.Samples)
		}
	}
	if groupExecSum != ta.Executions {
		t.Errorf("group executions sum to %d, want tenant total %d", groupExecSum, ta.Executions)
	}
	if groupSamplesSum != ta.SamplesProcessed {
		t.Errorf("group samples sum to %d, want tenant total %d", groupSamplesSum, ta.SamplesProcessed)
	}

	// Groups sorted by samples desc: mimir_queries (2000) before mimir_alerts (1500).
	if groups[0].Group != "mimir_queries" {
		t.Errorf("groups[0] = %q, want mimir_queries (sorted by samples desc)", groups[0].Group)
	}
}

func TestGroupRules_ZeroTotalDoesNotProduceNaN(t *testing.T) {
	groups := groupRules([]attribution.RuleAggregate{
		{RuleID: rule.RuleID{Group: "g"}, Executions: 0, SamplesProcessed: 0},
	}, 0, 0)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].ExecutionShare.Width != 0 || groups[0].SampleShare.Width != 0 {
		t.Errorf("expected 0 share width on zero totals, got %+v", groups[0])
	}
}
