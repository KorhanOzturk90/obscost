package mimirlogs

import (
	"fmt"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// promqlParser is package-level and reused across every parseExpr call —
// mirrors internal/loader/dir's convention of building one parser.Parser
// and calling ParseExpr on it repeatedly rather than constructing a new
// one per call.
var promqlParser = parser.NewParser(parser.Options{})

// parseExpr parses raw PromQL text, e.g. a ruler log line's query field.
func parseExpr(expr string) (parser.Expr, error) {
	return promqlParser.ParseExpr(expr)
}

// canonicalExprString is the join key: two PromQL expressions that parse
// to the same AST produce the same canonical string here, regardless of
// surface differences in the original source text (aggregation-modifier
// placement, whitespace, redundant parens — see this file's package doc).
func canonicalExprString(ast parser.Expr) string {
	return ast.String()
}

// exprIndex recovers a rule's group/name identity from Mimir's ruler
// query-stats log line, which carries only the raw PromQL expression text
// and tenant — never a rule group or rule name. The recovery is a match of
// that expression text against an already-loaded rule.AnnotatedRule,
// scoped to the same tenant.
//
// The match is on each side's parsed AST's canonical String() form, NOT
// the raw source text — confirmed necessary against a real Mimir instance,
// not a hypothetical: Prometheus's rule evaluation engine queries using
// its parsed expression's own String() representation, not the original
// YAML bytes. A mixin author writing `sum(rate(m[1m])) by (cluster, job)`
// shows up in the ruler's query-stats log as `sum by (cluster, job)
// (rate(m[1m]))` — semantically identical, byte-for-byte different. Two
// PromQL expressions that mean the same thing can still print differently
// (aggregation-modifier placement is the common case, but whitespace and
// redundant parens can differ too), so comparing raw text produced almost
// no matches at all against real ruler output; comparing canonical AST
// strings on both sides fixes it because both sides go through the same
// parser and printer.
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
		key := canonicalExprString(d.AST)

		byExpr := idx.byTenantAndExpr[d.Tenant]
		if byExpr == nil {
			byExpr = make(map[string]rule.AnnotatedRule)
			idx.byTenantAndExpr[d.Tenant] = byExpr
		}
		if _, exists := byExpr[key]; exists {
			ambig := idx.ambiguous[d.Tenant]
			if ambig == nil {
				ambig = make(map[string]bool)
				idx.ambiguous[d.Tenant] = ambig
			}
			ambig[key] = true
			continue
		}
		byExpr[key] = d
	}
	return idx
}

// lookup takes the log line's raw query text (not yet canonicalized —
// canonicalization happens here, symmetrically with buildExprIndex, so
// callers never have to remember to do it themselves) and the original
// text is preserved by the caller for RuleExecution.QueryText.
func (idx *exprIndex) lookup(tenant, rawQuery string) (rule.AnnotatedRule, error) {
	ast, err := parseExpr(rawQuery)
	if err != nil {
		return rule.AnnotatedRule{}, fmt.Errorf("query text does not parse as PromQL: %w", err)
	}
	key := canonicalExprString(ast)

	if idx.ambiguous[tenant][key] {
		return rule.AnnotatedRule{}, fmt.Errorf(
			"ambiguous rule identity: multiple loaded rule definitions for tenant %q share identical expression text %q — refusing to guess which one produced this execution",
			tenant, rawQuery,
		)
	}
	def, ok := idx.byTenantAndExpr[tenant][key]
	if !ok {
		return rule.AnnotatedRule{}, fmt.Errorf(
			"no match: no loaded rule definition for tenant %q has expression text matching %q",
			tenant, rawQuery,
		)
	}
	return def, nil
}
