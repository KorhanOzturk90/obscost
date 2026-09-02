package checks

import (
	"context"
	"fmt"
	"sort"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS05 = "PC-S05"

// PCS05 flags rule groups/tenants over their ruler count limits. It needs
// only the rule set and the limits provider — no AST walk at all, which is
// why spec §8.1 calls it out as working "offline" with the least machinery.
type PCS05 struct{}

func (PCS05) ID() string          { return idPCS05 }
func (PCS05) Tier() analyzer.Tier { return analyzer.TierStatic }

type groupKey struct {
	tenant string
	file   string
	group  string
}

func (PCS05) Run(_ context.Context, rules []rule.AnnotatedRule, cc analyzer.CheckContext) ([]rule.Finding, error) {
	if cc.Limits == nil {
		return nil, nil
	}

	rulesPerGroup := map[groupKey]int{}
	groupsPerTenant := map[string]map[groupKey]struct{}{}

	for _, r := range rules {
		key := groupKey{tenant: r.Tenant, file: r.Location.File, group: r.Group.Name}
		rulesPerGroup[key]++
		if groupsPerTenant[r.Tenant] == nil {
			groupsPerTenant[r.Tenant] = map[groupKey]struct{}{}
		}
		groupsPerTenant[r.Tenant][key] = struct{}{}
	}

	var findings []rule.Finding

	reported := map[groupKey]bool{}
	for _, r := range rules {
		key := groupKey{tenant: r.Tenant, file: r.Location.File, group: r.Group.Name}
		if reported[key] {
			continue
		}
		lim, ok := cc.Limits.Limits(r.Tenant)
		if !ok || lim.RulerMaxRulesPerRuleGroup <= 0 {
			continue
		}
		count := rulesPerGroup[key]
		if count <= lim.RulerMaxRulesPerRuleGroup {
			continue
		}
		reported[key] = true

		findings = append(findings, rule.Finding{
			CheckID:  idPCS05,
			Severity: rule.SeverityError,
			Tenant:   r.Tenant,
			Location: rule.SourceLocation{File: r.Location.File, Group: r.Group.Name},
			Message:  fmt.Sprintf("group %q has %d rules, above tenant %s's limit of %d", r.Group.Name, count, r.Tenant, lim.RulerMaxRulesPerRuleGroup),
			Values: map[string]any{
				"rule_count": count,
				"limit":      lim.RulerMaxRulesPerRuleGroup,
			},
			Remediation: "split the group, or reduce the rule count below the tenant's ruler_max_rules_per_rule_group limit",
		})
	}

	tenants := make([]string, 0, len(groupsPerTenant))
	for t := range groupsPerTenant {
		tenants = append(tenants, t)
	}
	sort.Strings(tenants)

	for _, tenant := range tenants {
		groups := groupsPerTenant[tenant]
		lim, ok := cc.Limits.Limits(tenant)
		if !ok || lim.RulerMaxRuleGroupsPerTenant <= 0 {
			continue
		}
		if len(groups) <= lim.RulerMaxRuleGroupsPerTenant {
			continue
		}

		findings = append(findings, rule.Finding{
			CheckID:  idPCS05,
			Severity: rule.SeverityError,
			Tenant:   tenant,
			Location: rule.SourceLocation{},
			Message:  fmt.Sprintf("tenant %s has %d rule groups, above its limit of %d", tenant, len(groups), lim.RulerMaxRuleGroupsPerTenant),
			Values: map[string]any{
				"group_count": len(groups),
				"limit":       lim.RulerMaxRuleGroupsPerTenant,
			},
			Remediation: "consolidate or remove rule groups to fit within ruler_max_rule_groups_per_tenant",
		})
	}

	return findings, nil
}
