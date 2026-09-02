package checks

import (
	"context"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestPCS03_Negative_AtThreshold(t *testing.T) {
	// R=10000s, interval=1s -> ratio = 10000, not > 10000.
	r := newAlertingRule(t, "r", "rate(m[10000s]) > 0", time.Second)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS03{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none at exactly ratio 10000", findings)
	}
}

func TestPCS03_Positive_AboveThreshold(t *testing.T) {
	// R=10001s, interval=1s -> ratio = 10001 > 10000.
	r := newAlertingRule(t, "r", "rate(m[10001s]) > 0", time.Second)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS03{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityWarn {
		t.Fatalf("findings = %+v, want one warn finding", findings)
	}
}
