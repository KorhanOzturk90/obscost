package checks

import (
	"context"
	"fmt"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS06 = "PC-S06"

// PCS06 flags rules that never resolved to a tenant. The loader keeps such
// rules loaded (rather than treating unresolved tenancy as a load error) so
// this check can report it as a normal, remediation-carrying Finding
// subject to --fail-on; an empty Tenant is the loader's signal for "the
// configured unmapped policy left this unresolved" (see internal/tenancy).
type PCS06 struct{}

func (PCS06) ID() string          { return idPCS06 }
func (PCS06) Tier() analyzer.Tier { return analyzer.TierStatic }

func (PCS06) Run(_ context.Context, rules []rule.AnnotatedRule, _ analyzer.CheckContext) ([]rule.Finding, error) {
	var findings []rule.Finding
	for _, r := range rules {
		if r.Tenant != "" {
			continue
		}
		findings = append(findings, rule.Finding{
			CheckID:     idPCS06,
			Severity:    rule.SeverityError,
			Location:    r.Location,
			Message:     fmt.Sprintf("rule %q did not resolve to a tenant via the configured tenancy discovery chain", r.Name()),
			Remediation: "add a tenancy.discovery mapping that covers this rule's source location, or set tenancy.unmapped to skip or tenant:<name>",
		})
	}
	return findings, nil
}
