package analyzer

import "github.com/prometheus/prometheus/promql/parser"

// Visitor is called once per AST node in post-order, with the chain of
// ancestors from the root down to (but not including) node.
type Visitor func(node parser.Node, ancestors []parser.Node)

// WalkPostOrder visits every child of a node before the node itself. This
// matches spec §5.3's per-node cost accumulation shape (a BinaryExpr's cost
// needs both operands' costs computed first) — every later live/fleet/
// rewrite/explain feature built on §5 depends on a post-order walk, so it's
// built once here rather than reinvented per feature. Upstream
// parser.Inspect is pre-order only, hence this small purpose-built walker.
func WalkPostOrder(expr parser.Expr, visit Visitor) {
	walkPostOrder(expr, nil, visit)
}

func walkPostOrder(node parser.Node, ancestors []parser.Node, visit Visitor) {
	if node == nil {
		return
	}
	childAncestors := make([]parser.Node, len(ancestors)+1)
	copy(childAncestors, ancestors)
	childAncestors[len(ancestors)] = node

	for _, child := range parser.Children(node) {
		walkPostOrder(child, childAncestors, visit)
	}
	visit(node, ancestors)
}
