package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS04 = "PC-S04"

// PCS04 flags recording rules whose by()/without() aggregation leaves a
// configured high-cardinality label in the output series.
type PCS04 struct{}

func (PCS04) ID() string          { return idPCS04 }
func (PCS04) Tier() analyzer.Tier { return analyzer.TierStatic }

func (PCS04) Run(_ context.Context, rules []rule.AnnotatedRule, cc analyzer.CheckContext) ([]rule.Finding, error) {
	wordlist := cc.Config.Checks.Thresholds.HighCardinalityLabels
	if len(wordlist) == 0 {
		return nil, nil
	}

	var findings []rule.Finding
	for _, r := range rules {
		if r.Kind != rule.KindRecording || r.AST == nil {
			continue
		}

		for _, site := range outputAggregations(r.AST, nil) {
			agg := site.agg
			grouping := make(map[string]bool, len(agg.Grouping))
			for _, g := range agg.Grouping {
				grouping[g] = true
			}

			var retained []string
			for _, l := range wordlist {
				if site.stripped[l] {
					// A wrapping function (e.g. histogram_quantile) removes
					// this label from the actual recorded output regardless
					// of what the inner aggregation's grouping says.
					continue
				}
				switch {
				case agg.Without && !grouping[l]:
					retained = append(retained, l)
				case !agg.Without && grouping[l]:
					retained = append(retained, l)
				}
			}
			if len(retained) == 0 {
				continue
			}
			sort.Strings(retained)

			findings = append(findings, rule.Finding{
				CheckID:  idPCS04,
				Severity: rule.SeverityInfo,
				Tenant:   r.Tenant,
				Location: r.Location,
				Message:  fmt.Sprintf("recording rule output retains high-cardinality label(s): %s", strings.Join(retained, ", ")),
				Values: map[string]any{
					"retained_labels": retained,
					"without":         agg.Without,
				},
				Remediation: "drop these labels from the aggregation's by/without clause unless the recording rule genuinely needs per-value series",
			})
		}
	}
	return findings, nil
}

// outputAggSite is one AggregateExpr that helps determine a rule's actual
// recorded output label set, plus any labels a wrapping function strips
// from that aggregation's grouping before it reaches the output.
type outputAggSite struct {
	agg      *parser.AggregateExpr
	stripped map[string]bool
}

// outputAggregations returns the AggregateExpr node(s) that determine expr's
// own output label set, unwrapping simple pass-through wrappers (parens,
// unary, step-invariant, subqueries, both sides of a binary expr, and
// function-call arguments) rather than walking every aggregation anywhere
// in the tree.
//
// This distinction matters concretely for histogram_quantile: in
// `histogram_quantile(0.99, sum by (le, job) (rate(m_bucket[5m])))`, the
// inner `sum by (le, job)` is merely an argument — histogram_quantile
// always consumes/drops `le` from its result, so the rule's actual
// recorded series never carries `le`. Walking every AggregateExpr in the
// tree (the previous implementation) flagged this as retaining `le`, which
// is a false positive: `le` retention is only real for rules whose
// *output* is the `by (le, ...)` aggregation itself (the pre-aggregation
// pattern that feeds histogram_quantile at query time, not inside the
// recording rule).
func outputAggregations(expr parser.Expr, stripped map[string]bool) []outputAggSite {
	switch n := expr.(type) {
	case nil:
		return nil
	case *parser.AggregateExpr:
		return []outputAggSite{{agg: n, stripped: stripped}}
	case *parser.ParenExpr:
		return outputAggregations(n.Expr, stripped)
	case *parser.UnaryExpr:
		return outputAggregations(n.Expr, stripped)
	case *parser.StepInvariantExpr:
		return outputAggregations(n.Expr, stripped)
	case *parser.SubqueryExpr:
		return outputAggregations(n.Expr, stripped)
	case *parser.BinaryExpr:
		var out []outputAggSite
		out = append(out, outputAggregations(n.LHS, stripped)...)
		out = append(out, outputAggregations(n.RHS, stripped)...)
		return out
	case *parser.Call:
		if n.Func == nil {
			return nil
		}
		if n.Func.Name == "histogram_quantile" && len(n.Args) >= 2 {
			return outputAggregations(n.Args[1], withStripped(stripped, "le"))
		}
		// Most PromQL functions (rate, increase, ceil, clamp_max, ...) pass
		// through the label set of their vector argument(s) unchanged
		// (spec §5.3: "Function calls: pass-through of arg costs"). Union
		// across all args rather than picking one by position/type: scalar
		// and string args (NumberLiteral, StringLiteral, MatrixSelector,
		// VectorSelector) fall through to the default case below and
		// contribute nothing.
		var out []outputAggSite
		for _, arg := range n.Args {
			out = append(out, outputAggregations(arg, stripped)...)
		}
		return out
	default:
		// VectorSelector, MatrixSelector, NumberLiteral, StringLiteral: no
		// aggregation on this branch, nothing to report.
		return nil
	}
}

func withStripped(base map[string]bool, label string) map[string]bool {
	out := make(map[string]bool, len(base)+1)
	for k := range base {
		out[k] = true
	}
	out[label] = true
	return out
}
