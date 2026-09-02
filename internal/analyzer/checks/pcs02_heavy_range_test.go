package checks

import (
	"context"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestPCS02_Negative_AtThreshold(t *testing.T) {
	r := newRecordingRule(t, "r", "sum(rate(m[24h]))", time.Minute) // exactly the 24h warn threshold
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS02{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none at exactly the threshold", findings)
	}
}

func TestPCS02_Positive_AboveThreshold(t *testing.T) {
	r := newRecordingRule(t, "r", "sum(rate(m[25h]))", time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS02{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityWarn {
		t.Fatalf("findings = %+v, want one warn finding", findings)
	}
}

func TestPCS02_Negative_AlertingRuleIgnored(t *testing.T) {
	r := newAlertingRule(t, "r", "sum(rate(m[25h])) > 0", time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS02{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: PC-S02 only applies to recording rules", findings)
	}
}
