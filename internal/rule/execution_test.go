package rule

import (
	"testing"
	"time"
)

func TestRuleID_String(t *testing.T) {
	id := RuleID{Tenant: "payments", Namespace: "team-payments/rules.yaml", Group: "g", Name: "r"}
	want := "payments/team-payments/rules.yaml/g/r"
	if got := id.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestRuleExecution_RuleID(t *testing.T) {
	e := RuleExecution{
		Tenant:    "payments",
		Namespace: "team-payments/rules.yaml",
		Group:     "g",
		RuleName:  "r",
		Timestamp: time.Now(),
	}
	want := RuleID{Tenant: "payments", Namespace: "team-payments/rules.yaml", Group: "g", Name: "r"}
	if got := e.RuleID(); got != want {
		t.Errorf("RuleID() = %+v, want %+v", got, want)
	}
}

func TestAnnotatedRule_RuleID(t *testing.T) {
	a := AnnotatedRule{
		Rule:   Rule{Kind: KindRecording, Record: "r"},
		Tenant: "payments",
		Group:  RuleGroupMeta{Name: "g"},
		Location: SourceLocation{
			File:  "team-payments/rules.yaml",
			Group: "g",
			Rule:  "r",
		},
	}
	want := RuleID{Tenant: "payments", Namespace: "team-payments/rules.yaml", Group: "g", Name: "r"}
	if got := a.RuleID(); got != want {
		t.Errorf("RuleID() = %+v, want %+v", got, want)
	}
}

// The two RuleID() derivations must land on exactly the same value for a
// definition/execution pair that "should" join — this is the entire basis
// of the attribution join.
func TestRuleID_ExecutionAndDefinitionAgree(t *testing.T) {
	a := AnnotatedRule{
		Rule:     Rule{Kind: KindAlerting, Alert: "HighLatency"},
		Tenant:   "analytics",
		Group:    RuleGroupMeta{Name: "slo"},
		Location: SourceLocation{File: "analytics/slo.yaml", Group: "slo", Rule: "HighLatency"},
	}
	e := RuleExecution{
		Tenant:    "analytics",
		Namespace: "analytics/slo.yaml",
		Group:     "slo",
		RuleName:  "HighLatency",
	}
	if a.RuleID() != e.RuleID() {
		t.Errorf("definition RuleID %+v != execution RuleID %+v", a.RuleID(), e.RuleID())
	}
}
