package report

import (
	"bytes"
	"os"
	"strconv"
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

// sampleMultiGroupTenant builds an already-grouped tenant the way
// attribution.Aggregate now hands one over — the bucketing itself is tested
// in internal/attribution, so these tests cover rendering only.
func sampleMultiGroupTenant() attribution.TenantAggregate {
	return attribution.TenantAggregate{
		Tenant:           "infra",
		RuleCount:        3,
		Executions:       350,
		SamplesProcessed: 3500,
		SamplesObserved:  true,
		RankValue:        3500,
		Groups: []attribution.GroupAggregate{
			{
				Namespace: "ns", Group: "mimir_queries", Executions: 200,
				RankValue: 2000, RankSharePct: 57.1, RankObserved: true,
				Rules: []attribution.RuleAggregate{
					{RuleID: rule.RuleID{Tenant: "infra", Namespace: "ns", Group: "mimir_queries", Name: "r3"}, Executions: 200, RankValue: 2000, RankObserved: true},
				},
			},
			{
				Namespace: "ns", Group: "mimir_alerts", Executions: 150,
				RankValue: 1500, RankSharePct: 42.9, RankObserved: true,
				Rules: []attribution.RuleAggregate{
					{RuleID: rule.RuleID{Tenant: "infra", Namespace: "ns", Group: "mimir_alerts", Name: "r1"}, Executions: 100, RankValue: 1000, RankObserved: true},
					{RuleID: rule.RuleID{Tenant: "infra", Namespace: "ns", Group: "mimir_alerts", Name: "r2"}, Executions: 50, RankValue: 500, RankObserved: true},
				},
			},
		},
	}
}

func TestBuildGroups_RendersPrecomputedTierInOrder(t *testing.T) {
	ta := sampleMultiGroupTenant()
	groups := buildGroups(ta.Groups, attribution.RankMetricSamplesProcessed)

	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	// Order is attribution's, preserved verbatim — not re-derived here.
	if groups[0].Group != "mimir_queries" {
		t.Errorf("groups[0] = %q, want mimir_queries", groups[0].Group)
	}

	var groupExecSum int
	for _, g := range groups {
		n, err := strconv.Atoi(strings.ReplaceAll(g.Executions, ",", ""))
		if err != nil {
			t.Fatalf("group %s: Executions %q not a number: %v", g.Group, g.Executions, err)
		}
		groupExecSum += n

		var ruleExecSum int
		for _, r := range g.Rules {
			rn, err := strconv.Atoi(strings.ReplaceAll(r.Executions, ",", ""))
			if err != nil {
				t.Fatalf("rule %s: Executions %q not a number: %v", r.RuleName, r.Executions, err)
			}
			ruleExecSum += rn
		}
		if ruleExecSum != n {
			t.Errorf("group %s: rule executions sum to %d, want %d", g.Group, ruleExecSum, n)
		}
	}
	if groupExecSum != ta.Executions {
		t.Errorf("group executions sum to %d, want tenant total %d", groupExecSum, ta.Executions)
	}
}

func TestBuildGroups_ZeroTotalDoesNotProduceNaN(t *testing.T) {
	groups := buildGroups([]attribution.GroupAggregate{
		{Group: "g", Executions: 0, RankValue: 0, RankObserved: false},
	}, attribution.RankMetricSamplesProcessed)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].MetricShare.Width != 0 {
		t.Errorf("expected 0 share width on zero totals, got %+v", groups[0])
	}
	if groups[0].MetricMeasured {
		t.Errorf("expected MetricMeasured=false (never observed), got %+v", groups[0])
	}
}

// A group-granularity report must never describe its missing rule tier as
// "no matched rule executions" — that reports a limit of the source as if it
// were a fact about the workload (ADR 0002).
func TestWorkloadHTMLReport_GroupGranularityExplainsMissingRuleTier(t *testing.T) {
	result := WorkloadResult{
		GeneratedAt:     time.Now(),
		Granularity:     attribution.GranularityGroup,
		RankMetric:      attribution.RankMetricDurationSeconds,
		GroupRankMetric: attribution.RankMetricExecutions,
		SourceLabel:     "Mimir rule metrics",
		TotalExecutions: 305,
		Tenants: []attribution.TenantAggregate{
			{
				Tenant: "infra", Executions: 305, DurationSecondsSum: 10.5,
				DurationObserved: true, RankValue: 10.5, RankSharePct: 100,
				Groups: []attribution.GroupAggregate{
					{Namespace: "alerts.yaml", Group: "mimir_alerts", Executions: 305, RankValue: 305, RankSharePct: 100, RankObserved: true},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := (workloadHTMLReporter{}).Render(&buf, result); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "No matched rule executions") {
		t.Errorf("group-granularity report claims no rule executions matched:\n%s", out)
	}
	if !strings.Contains(out, "rule-group granularity") {
		t.Errorf("expected an explanation of the missing rule tier, got:\n%s", out)
	}
	if !strings.Contains(out, "Mimir rule metrics") {
		t.Errorf("expected the source to be named in the header, got:\n%s", out)
	}
	// Tenants rank by wall time but groups can only rank by count, so each
	// tier must be labelled with its own metric rather than inheriting the
	// tenant's and implying the group figure means something it doesn't.
	if !strings.Contains(out, "query wall time share") {
		t.Errorf("expected the tenant tier labelled with wall time, got:\n%s", out)
	}
	if !strings.Contains(out, "of tenant's reported groups by executions") {
		t.Errorf("expected the group tier labelled with executions, got:\n%s", out)
	}
}
