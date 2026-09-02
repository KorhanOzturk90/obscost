package checks

import (
	"context"
	"fmt"
	"math"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS01 = "PC-S01"

// PCS01 flags subqueries whose step count crosses the configured
// warn/error thresholds: steps = ceil(R/S). [30d:5m] = 8,640 steps.
type PCS01 struct{}

func (PCS01) ID() string          { return idPCS01 }
func (PCS01) Tier() analyzer.Tier { return analyzer.TierStatic }

func (PCS01) Run(_ context.Context, rules []rule.AnnotatedRule, cc analyzer.CheckContext) ([]rule.Finding, error) {
	th := cc.Config.Checks.Thresholds

	var findings []rule.Finding
	for _, r := range rules {
		if r.AST == nil {
			continue
		}
		for _, span := range analyzer.RangeSpans(r.AST) {
			if !span.IsSubquery() {
				continue
			}
			step := span.Step
			if step <= 0 {
				// Omitted subquery step: approximate with the rule
				// group's own evaluation interval, the only static
				// signal available offline.
				step = r.Group.Interval
			}
			if step <= 0 {
				continue
			}

			steps := int(math.Ceil(span.Range.Seconds() / step.Seconds()))
			sev, flagged := severityForSteps(steps, th.SubqueryStepsWarn, th.SubqueryStepsError)
			if !flagged {
				continue
			}

			findings = append(findings, rule.Finding{
				CheckID:  idPCS01,
				Severity: sev,
				Tenant:   r.Tenant,
				Location: r.Location,
				Message:  fmt.Sprintf("subquery [%s:%s] evaluates %d steps per invocation", span.Range, step, steps),
				Values: map[string]any{
					"range_seconds": span.Range.Seconds(),
					"step_seconds":  step.Seconds(),
					"steps":         steps,
				},
				Remediation: "reduce the subquery range, increase its step, or rewrite as a chained recording-rule window (see `promcost rewrite`)",
			})
		}
	}
	return findings, nil
}

func severityForSteps(steps, warn, err int) (rule.Severity, bool) {
	switch {
	case steps > err:
		return rule.SeverityError, true
	case steps > warn:
		return rule.SeverityWarn, true
	default:
		return 0, false
	}
}
