package checks

import (
	"context"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestPCS06_Positive_UnresolvedTenant(t *testing.T) {
	r := newRecordingRule(t, "r", "up", time.Minute) // Tenant left as "" by the helper
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS06{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityError {
		t.Fatalf("findings = %+v, want one error finding", findings)
	}
}

func TestPCS06_Negative_ResolvedTenant(t *testing.T) {
	r := withTenant(newRecordingRule(t, "r", "up", time.Minute), "platform")
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS06{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: rule resolved to a tenant", findings)
	}
}
