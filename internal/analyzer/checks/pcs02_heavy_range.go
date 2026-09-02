package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS02 = "PC-S02"

// PCS02 flags any range selector or subquery span inside a recording rule
// wider than the configured recording_range_warn threshold.
type PCS02 struct{}

func (PCS02) ID() string          { return idPCS02 }
func (PCS02) Tier() analyzer.Tier { return analyzer.TierStatic }

func (PCS02) Run(_ context.Context, rules []rule.AnnotatedRule, cc analyzer.CheckContext) ([]rule.Finding, error) {
	warn := cc.Config.Checks.Thresholds.RecordingRangeWarn.Duration()

	var findings []rule.Finding
	for _, r := range rules {
		if r.Kind != rule.KindRecording || r.AST == nil {
			continue
		}
		seen := map[time.Duration]bool{}
		for _, span := range analyzer.RangeSpans(r.AST) {
			if span.Range <= warn || seen[span.Range] {
				continue
			}
			seen[span.Range] = true

			findings = append(findings, rule.Finding{
				CheckID:  idPCS02,
				Severity: rule.SeverityWarn,
				Tenant:   r.Tenant,
				Location: r.Location,
				Message:  fmt.Sprintf("recording rule ranges over %s, above the %s warn threshold", span.Range, warn),
				Values: map[string]any{
					"range_seconds":          span.Range.Seconds(),
					"warn_threshold_seconds": warn.Seconds(),
				},
				Remediation: "rewrite as a chain of shorter-window recording rules (see `promcost rewrite --style chained`)",
			})
		}
	}
	return findings, nil
}
