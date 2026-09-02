package checks

import (
	"context"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestPCS01_Negative_BelowThreshold(t *testing.T) {
	r := newRecordingRule(t, "r", "avg_over_time(m[500m:1m])", time.Minute) // steps = 500, not > 500
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS01{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none at exactly the warn threshold", findings)
	}
}

func TestPCS01_Positive_WarnJustAboveThreshold(t *testing.T) {
	r := newRecordingRule(t, "r", "avg_over_time(m[501m:1m])", time.Minute) // steps = 501
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS01{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityWarn {
		t.Fatalf("findings = %+v, want one warn finding", findings)
	}
}

func TestPCS01_Positive_ErrorAboveThreshold(t *testing.T) {
	r := newRecordingRule(t, "r", "avg_over_time(m[2001m:1m])", time.Minute) // steps = 2001
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS01{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityError {
		t.Fatalf("findings = %+v, want one error finding", findings)
	}
}

func TestPCS01_Negative_PlainRangeSelectorIgnored(t *testing.T) {
	r := newRecordingRule(t, "r", "rate(m[30d])", time.Minute) // huge range but not a subquery
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS01{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: plain range selectors are not subqueries", findings)
	}
}

func TestPCS01_OmittedStepFallsBackToGroupInterval(t *testing.T) {
	// [1000h:] omits the step; steps = 1000h / group interval (5m) = 12000 -> error.
	r := newRecordingRule(t, "r", "avg_over_time(m[1000h:])", 5*time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS01{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityError {
		t.Fatalf("findings = %+v, want one error finding using group interval fallback", findings)
	}
}
