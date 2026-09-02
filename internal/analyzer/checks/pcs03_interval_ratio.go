package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS03 = "PC-S03"

// s03RatioThreshold is spec §4's hardcoded 10,000 — unlike the other
// static checks' thresholds, this ratio has no corresponding key in the
// checks.thresholds schema (spec §3), so it isn't configurable.
const s03RatioThreshold = 10000

// PCS03 flags a rule group whose interval re-evaluates a window R such
// that R/interval exceeds the ratio threshold: how much history gets
// recomputed to incorporate one new sample.
type PCS03 struct{}

func (PCS03) ID() string          { return idPCS03 }
func (PCS03) Tier() analyzer.Tier { return analyzer.TierStatic }

func (PCS03) Run(_ context.Context, rules []rule.AnnotatedRule, _ analyzer.CheckContext) ([]rule.Finding, error) {
	var findings []rule.Finding
	for _, r := range rules {
		if r.AST == nil || r.Group.Interval <= 0 {
			continue
		}

		var maxRange time.Duration
		for _, span := range analyzer.RangeSpans(r.AST) {
			if span.Range > maxRange {
				maxRange = span.Range
			}
		}
		if maxRange == 0 {
			continue
		}

		ratio := maxRange.Seconds() / r.Group.Interval.Seconds()
		if ratio <= s03RatioThreshold {
			continue
		}

		findings = append(findings, rule.Finding{
			CheckID:  idPCS03,
			Severity: rule.SeverityWarn,
			Tenant:   r.Tenant,
			Location: r.Location,
			Message:  fmt.Sprintf("group interval %s re-evaluates a %s window every cycle (ratio %.0f > %d)", r.Group.Interval, maxRange, ratio, s03RatioThreshold),
			Values: map[string]any{
				"window_seconds":   maxRange.Seconds(),
				"interval_seconds": r.Group.Interval.Seconds(),
				"ratio":            ratio,
			},
			Remediation: "increase the group interval, shrink the window, or chain a recording rule so only new data is recomputed each cycle",
		})
	}
	return findings, nil
}
