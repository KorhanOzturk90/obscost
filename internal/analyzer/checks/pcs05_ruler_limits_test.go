package checks

import (
	"context"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/limits"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

type fakeLimitsProvider struct {
	tenants map[string]limits.Tenant
}

func (f fakeLimitsProvider) Limits(tenant string) (limits.Tenant, bool) {
	t, ok := f.tenants[tenant]
	return t, ok
}

func withTenant(r rule.AnnotatedRule, tenant string) rule.AnnotatedRule {
	r.Tenant = tenant
	return r
}

func TestPCS05_Positive_RulesPerGroupExceeded(t *testing.T) {
	rules := []rule.AnnotatedRule{
		withTenant(newRecordingRule(t, "a", "up", time.Minute), "team-a"),
		withTenant(newRecordingRule(t, "b", "up", time.Minute), "team-a"),
		withTenant(newRecordingRule(t, "c", "up", time.Minute), "team-a"),
	}
	cc := analyzer.CheckContext{
		Config: config.Default(),
		Limits: fakeLimitsProvider{tenants: map[string]limits.Tenant{
			"team-a": {RulerMaxRulesPerRuleGroup: 2},
		}},
	}
	findings, err := PCS05{}.Run(context.Background(), rules, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityError {
		t.Fatalf("findings = %+v, want one error finding", findings)
	}
}

func TestPCS05_Negative_WithinLimit(t *testing.T) {
	rules := []rule.AnnotatedRule{
		withTenant(newRecordingRule(t, "a", "up", time.Minute), "team-a"),
		withTenant(newRecordingRule(t, "b", "up", time.Minute), "team-a"),
	}
	cc := analyzer.CheckContext{
		Config: config.Default(),
		Limits: fakeLimitsProvider{tenants: map[string]limits.Tenant{
			"team-a": {RulerMaxRulesPerRuleGroup: 2},
		}},
	}
	findings, err := PCS05{}.Run(context.Background(), rules, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: at but not above the limit", findings)
	}
}

func TestPCS05_Positive_GroupsPerTenantExceeded(t *testing.T) {
	r1 := withTenant(newRecordingRule(t, "a", "up", time.Minute), "team-a")
	r1.Location.Group, r1.Group.Name = "group1", "group1"
	r2 := withTenant(newRecordingRule(t, "b", "up", time.Minute), "team-a")
	r2.Location.Group, r2.Group.Name = "group2", "group2"

	cc := analyzer.CheckContext{
		Config: config.Default(),
		Limits: fakeLimitsProvider{tenants: map[string]limits.Tenant{
			"team-a": {RulerMaxRuleGroupsPerTenant: 1},
		}},
	}
	findings, err := PCS05{}.Run(context.Background(), []rule.AnnotatedRule{r1, r2}, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Severity != rule.SeverityError {
		t.Fatalf("findings = %+v, want one error finding for tenant group-count overflow", findings)
	}
}

func TestPCS05_Negative_NoLimitsForTenant(t *testing.T) {
	rules := []rule.AnnotatedRule{
		withTenant(newRecordingRule(t, "a", "up", time.Minute), "unknown-tenant"),
	}
	cc := analyzer.CheckContext{
		Config: config.Default(),
		Limits: fakeLimitsProvider{tenants: map[string]limits.Tenant{}},
	}
	findings, err := PCS05{}.Run(context.Background(), rules, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: no limits data for this tenant", findings)
	}
}
