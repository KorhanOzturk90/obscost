package mimirlogs

import (
	"fmt"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// exprIndex recovers a rule's group/name identity from Mimir's ruler
// query-stats log line, which carries only the raw PromQL expression text
// and tenant — never a rule group or rule name. The recovery is an exact
// string match of that expression text against the Expr of an already-
// loaded rule.AnnotatedRule, scoped to the same tenant.
//
// A tenant+expression pair that matches 2+ loaded definitions is marked
// ambiguous rather than resolved to an arbitrary one of them: picking one
// would be a guess presented as a fact, which is exactly what
// PRODUCT-DIRECTION.md's "conservative, explainable, calibrated" principle
// rules out. lookup refuses ambiguous and no-match cases alike, distinctly.
type exprIndex struct {
	byTenantAndExpr map[string]map[string]rule.AnnotatedRule
	ambiguous       map[string]map[string]bool
}

func buildExprIndex(definitions []rule.AnnotatedRule) *exprIndex {
	idx := &exprIndex{
		byTenantAndExpr: make(map[string]map[string]rule.AnnotatedRule),
		ambiguous:       make(map[string]map[string]bool),
	}
	for _, d := range definitions {
		byExpr := idx.byTenantAndExpr[d.Tenant]
		if byExpr == nil {
			byExpr = make(map[string]rule.AnnotatedRule)
			idx.byTenantAndExpr[d.Tenant] = byExpr
		}
		if _, exists := byExpr[d.Expr]; exists {
			ambig := idx.ambiguous[d.Tenant]
			if ambig == nil {
				ambig = make(map[string]bool)
				idx.ambiguous[d.Tenant] = ambig
			}
			ambig[d.Expr] = true
			continue
		}
		byExpr[d.Expr] = d
	}
	return idx
}

func (idx *exprIndex) lookup(tenant, expr string) (rule.AnnotatedRule, error) {
	if idx.ambiguous[tenant][expr] {
		return rule.AnnotatedRule{}, fmt.Errorf(
			"ambiguous rule identity: multiple loaded rule definitions for tenant %q share identical expression text %q — refusing to guess which one produced this execution",
			tenant, expr,
		)
	}
	def, ok := idx.byTenantAndExpr[tenant][expr]
	if !ok {
		return rule.AnnotatedRule{}, fmt.Errorf(
			"no match: no loaded rule definition for tenant %q has expression text matching %q",
			tenant, expr,
		)
	}
	return def, nil
}
