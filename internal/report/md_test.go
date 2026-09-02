package report

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func sampleResult() Result {
	return Result{
		RulesScanned: 3,
		TenantsSeen:  []string{"platform", "payments"},
		GeneratedAt:  time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		Findings: []rule.Finding{
			{
				CheckID:  "PC-S01",
				Severity: rule.SeverityError,
				Tenant:   "payments",
				Location: rule.SourceLocation{File: "rules.yaml", Group: "heavy", Rule: "HeavySubquery"},
				Message:  "subquery [720h0m0s:5m0s] evaluates 8640 steps per invocation",
				Values: map[string]any{
					"steps":         8640,
					"range_seconds": 2592000.0,
				},
				Remediation: "reduce the subquery range, increase its step, or rewrite as a chained recording-rule window (see `promcost rewrite`)",
			},
			{
				CheckID:  "PC-S05",
				Severity: rule.SeverityError,
				Tenant:   "platform",
				Location: rule.SourceLocation{File: "rules.yaml", Group: "many-rules"},
				Message:  "group \"many-rules\" has 3 rules, above tenant platform's limit of 2",
			},
		},
	}
}

func TestMDReportGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := (mdReporter{}).Render(&buf, sampleResult()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	golden := "testdata/report.golden.md"
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
		t.Errorf("md report does not match golden file %s.\n--- got ---\n%s\n--- want ---\n%s", golden, buf.String(), want)
	}
}

func TestMDReportNoFindings(t *testing.T) {
	var buf bytes.Buffer
	err := (mdReporter{}).Render(&buf, Result{RulesScanned: 1, GeneratedAt: time.Now()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("No findings.")) {
		t.Errorf("expected \"No findings.\" in output:\n%s", buf.String())
	}
}
