package analyzer_test

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
)

var testParser = parser.NewParser(parser.Options{})

func mustParse(t *testing.T, expr string) parser.Expr {
	t.Helper()
	e, err := testParser.ParseExpr(expr)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", expr, err)
	}
	return e
}

func TestWalkPostOrderVisitsChildrenFirst(t *testing.T) {
	expr := mustParse(t, "rate(a[5m]) + rate(b[5m])")

	var order []string
	analyzer.WalkPostOrder(expr, func(node parser.Node, _ []parser.Node) {
		switch n := node.(type) {
		case *parser.MatrixSelector:
			order = append(order, "matrix:"+n.String())
		case *parser.Call:
			order = append(order, "call:"+n.Func.Name)
		case *parser.BinaryExpr:
			order = append(order, "binary")
		}
	})

	if len(order) != 5 {
		t.Fatalf("visited %d nodes, want 5: %v", len(order), order)
	}
	if order[len(order)-1] != "binary" {
		t.Errorf("last visited node = %q, want \"binary\" (post-order: root visited last)", order[len(order)-1])
	}
	// Each rate() call must be visited after its own matrix selector.
	for i, tok := range order {
		if tok == "call:rate" {
			foundMatrixBefore := false
			for j := 0; j < i; j++ {
				if order[j][:7] == "matrix:" {
					foundMatrixBefore = true
				}
			}
			if !foundMatrixBefore {
				t.Errorf("call:rate at index %d has no matrix selector visited before it: %v", i, order)
			}
		}
	}
}

func TestWalkPostOrderAncestors(t *testing.T) {
	expr := mustParse(t, "sum(rate(a[5m]))")

	var gotAncestors int
	analyzer.WalkPostOrder(expr, func(node parser.Node, ancestors []parser.Node) {
		if _, ok := node.(*parser.MatrixSelector); ok {
			gotAncestors = len(ancestors)
		}
	})
	// root(AggregateExpr) -> Call -> MatrixSelector: 2 ancestors above the matrix selector.
	if gotAncestors != 2 {
		t.Errorf("ancestors above MatrixSelector = %d, want 2", gotAncestors)
	}
}
