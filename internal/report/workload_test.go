package report

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func sampleWorkloadResult() WorkloadResult {
	return WorkloadResult{
		GeneratedAt:     time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		TotalExecutions: 2463,
		TotalSamples:    9_800_000,
		RuleDefinitions: 2521,
		Tenants: []attribution.TenantAggregate{
			{
				Tenant:            "analytics",
				RuleCount:         1842,
				Executions:        1842,
				ExecutionSharePct: 37.2,
				SamplesProcessed:  4_096_400,
				SampleSharePct:    41.8,
				Rules: []attribution.RuleAggregate{
					{
						RuleID:            rule.RuleID{Tenant: "analytics", Namespace: "analytics/rules.yaml", Group: "g", Name: "customer_activity:7d"},
						Kind:              rule.KindRecording,
						Executions:        1440,
						ExecutionSharePct: 78.2,
						SamplesProcessed:  3_200_000,
						SampleSharePct:    78.1,
					},
				},
				UnmatchedExecutions: 1,
				UnmatchedSamples:    500,
			},
			{
				Tenant:            "payments",
				RuleCount:         621,
				Executions:        621,
				ExecutionSharePct: 18.4,
				SamplesProcessed:  2_068_000,
				SampleSharePct:    21.1,
			},
		},
		Unmatched: []rule.RuleExecution{
			{
				Tenant:           "analytics",
				Namespace:        "analytics/rules.yaml",
				Group:            "g",
				RuleName:         "deleted_rule",
				Timestamp:        time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
				SamplesProcessed: 500,
			},
		},
	}
}

func TestWorkloadMDReportGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := (workloadMDReporter{}).Render(&buf, sampleWorkloadResult()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	golden := "testdata/workload_report.golden.md"
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
		t.Errorf("workload md report does not match golden file %s.\n--- got ---\n%s\n--- want ---\n%s", golden, buf.String(), want)
	}
}

func TestWorkloadMDReport_EmptyReportNoCrash(t *testing.T) {
	var buf bytes.Buffer
	err := (workloadMDReporter{}).Render(&buf, WorkloadResult{GeneratedAt: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
}

func TestWorkloadJSONReport_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := (workloadJSONReporter{}).Render(&buf, sampleWorkloadResult()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	var got WorkloadResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.TotalExecutions != 2463 {
		t.Errorf("TotalExecutions = %d, want 2463", got.TotalExecutions)
	}
}

func TestWorkloadJSONReport_EmptyReportValidJSON(t *testing.T) {
	var buf bytes.Buffer
	err := (workloadJSONReporter{}).Render(&buf, WorkloadResult{GeneratedAt: time.Now()})
	if err != nil {
		t.Fatalf("Render (zero-executions report must not fail on NaN): %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Errorf("output is not valid JSON:\n%s", buf.String())
	}
}

func TestNewWorkload_UnknownFormat(t *testing.T) {
	if _, err := NewWorkload("xml"); err == nil {
		t.Error("expected an error for an unknown format")
	}
}
