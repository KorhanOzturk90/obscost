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

// histogram_quantile always drops `le` from its result: the inner `by
// (le, ...)` aggregation is an argument, not the rule's actual output.
// Regression test for the false positive found running against
// mimir-mixin's `histogram_quantile(0.99, sum by (le, job) (...))`
// recording rules.
func TestPCS04_Negative_HistogramQuantileDropsLE(t *testing.T) {
	r := newRecordingRule(t, "r", `histogram_quantile(0.99, sum by (le, job) (rate(m_bucket[5m])))`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: histogram_quantile strips le from its output", findings)
	}
}

// The pre-aggregation half of the same pattern — a rule whose output *is*
// the `by (le, ...)` aggregation (meant to feed histogram_quantile later,
// at query time) — must still be flagged: its recorded series genuinely
// carries `le`.
func TestPCS04_Positive_HistogramBucketPreaggregationRetainsLE(t *testing.T) {
	r := newRecordingRule(t, "r", `sum by (le, job) (rate(m_bucket[5m]))`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one finding: this rule's own output retains le", findings)
	}
	labels, _ := findings[0].Values["retained_labels"].([]string)
	if len(labels) != 1 || labels[0] != "le" {
		t.Errorf("retained_labels = %v, want [le]", labels)
	}
}

// A wordlist label retained elsewhere in a histogram_quantile call (not le,
// which histogram_quantile itself drops) must still be caught.
func TestPCS04_Positive_HistogramQuantileRetainsOtherLabel(t *testing.T) {
	r := newRecordingRule(t, "r", `histogram_quantile(0.99, sum by (le, pod) (rate(m_bucket[5m])))`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one finding: pod survives histogram_quantile", findings)
	}
	labels, _ := findings[0].Values["retained_labels"].([]string)
	if len(labels) != 1 || labels[0] != "pod" {
		t.Errorf("retained_labels = %v, want [pod]", labels)
	}
}

// ceil(sum by (...) (x) / const) — a plain arithmetic wrapper around an
// aggregation, the mimir-mixin `required_replicas` pattern — must still be
// caught via the BinaryExpr/Call pass-through paths.
func TestPCS04_Positive_ArithmeticWrapperAroundAggregation(t *testing.T) {
	r := newRecordingRule(t, "r", `ceil(sum by (pod, namespace) (m) / 240000)`, time.Minute)
	cc := analyzer.CheckContext{Config: config.Default()}
	findings, err := PCS04{}.Run(context.Background(), []rule.AnnotatedRule{r}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one finding: pod retained through ceil()/binary-expr wrapping", findings)
	}
}
