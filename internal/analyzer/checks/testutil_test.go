package checks

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

var testParser = parser.NewParser(parser.Options{})

func newRecordingRule(t *testing.T, name, expr string, groupInterval time.Duration) rule.AnnotatedRule {
	t.Helper()
	ast, err := testParser.ParseExpr(expr)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", expr, err)
	}
	return rule.AnnotatedRule{
		Rule: rule.Rule{
			Kind:   rule.KindRecording,
			Record: name,
			Expr:   expr,
		},
		AST:      ast,
		Group:    rule.RuleGroupMeta{Name: "g", Interval: groupInterval},
		Location: rule.SourceLocation{File: "rules.yaml", Group: "g", Rule: name},
	}
}

func newAlertingRule(t *testing.T, name, expr string, groupInterval time.Duration) rule.AnnotatedRule {
	t.Helper()
	ast, err := testParser.ParseExpr(expr)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", expr, err)
	}
	return rule.AnnotatedRule{
		Rule: rule.Rule{
			Kind:  rule.KindAlerting,
			Alert: name,
			Expr:  expr,
		},
		AST:      ast,
		Group:    rule.RuleGroupMeta{Name: "g", Interval: groupInterval},
		Location: rule.SourceLocation{File: "rules.yaml", Group: "g", Rule: name},
	}
}
