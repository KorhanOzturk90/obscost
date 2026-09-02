package checks

import (
	"context"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestPCS04_Positive_ByRetainsExplosiveLabel(t *testing.T) {
	r := newRecordingRule(t, "r", `sum by (pod, namespace) (rate(m[5m]))`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityInfo {
		t.Fatalf("findings = %+v, want one info finding", findings)
	}
	labels, _ := findings[0].Values["retained_labels"].([]string)
	if len(labels) != 1 || labels[0] != "pod" {
		t.Errorf("retained_labels = %v, want [pod]", labels)
	}
}

func TestPCS04_Positive_WithoutRetainsExplosiveLabel(t *testing.T) {
	// without (namespace) drops only namespace; pod (and everything else) survives.
	r := newRecordingRule(t, "r", `sum without (namespace) (rate(m[5m]))`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one finding", findings)
	}
}

func TestPCS04_Negative_ByDropsExplosiveLabel(t *testing.T) {
	r := newRecordingRule(t, "r", `sum by (job) (rate(m[5m]))`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: only 'job' is retained, not a wordlist label", findings)
	}
}

func TestPCS04_Negative_NoAggregation(t *testing.T) {
	r := newRecordingRule(t, "r", `rate(m[5m])`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: no aggregation at all", findings)
	}
}
