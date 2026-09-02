package analyzer

import (
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// Span describes a range-selector or subquery span found in an expression.
// Shared by PC-S01, PC-S02, and PC-S03 so range/subquery AST matching isn't
// implemented three times.
type Span struct {
	Range time.Duration
	// Step is zero for a plain matrix selector ([R]) and non-zero for a
	// subquery ([R:S]). A subquery with an omitted step ([R:]) also reports
	// zero here — the effective step is a runtime default this milestone
	// has no static way to know, so callers needing a step must supply
	// their own fallback (e.g. the rule group's evaluation interval).
	Step time.Duration
	Node parser.Node
}

// IsSubquery reports whether this span came from a subquery ([R:S] syntax)
// rather than a plain range selector ([R]).
func (s Span) IsSubquery() bool {
	_, ok := s.Node.(*parser.SubqueryExpr)
	return ok
}

// RangeSpans returns every MatrixSelector and SubqueryExpr span in expr.
func RangeSpans(expr parser.Expr) []Span {
	var spans []Span
	WalkPostOrder(expr, func(node parser.Node, _ []parser.Node) {
		switch n := node.(type) {
		case *parser.MatrixSelector:
			spans = append(spans, Span{Range: n.Range, Node: n})
		case *parser.SubqueryExpr:
			spans = append(spans, Span{Range: n.Range, Step: n.Step, Node: n})
		}
	})
	return spans
}
