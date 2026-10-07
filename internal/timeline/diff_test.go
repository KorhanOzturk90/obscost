package timeline

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

var t0 = time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)

// snapAt builds a snapshot requested at t0+minute and observed 2s later.
func snapAt(minute int, groups ...Group) Snapshot {
	req := t0.Add(time.Duration(minute) * time.Minute)
	return Snapshot{
		Version:     FormatVersion,
		Tenant:      "team-a",
		RequestedAt: req,
		ObservedAt:  req.Add(2 * time.Second),
		Groups:      groups,
	}
}

func group(ns, name string, interval float64, rules ...Rule) Group {
	return Group{Namespace: ns, Name: name, IntervalSeconds: interval, Rules: rules}
}

func rec(name, expr string) Rule {
	return Rule{Kind: "recording", Name: name, Expr: expr}
}

func alert(name, expr string, forSec float64, labels map[string]string) Rule {
	return Rule{Kind: "alerting", Name: name, Expr: expr, ForSeconds: forSec, Labels: labels}
}

func id(ns, g, name string) rule.RuleID {
	return rule.RuleID{Tenant: "team-a", Namespace: ns, Group: g, Name: name}
}

// summary renders changes compactly so table cases can compare them.
func summary(changes []Change) []string {
	var out []string
	for _, c := range changes {
		s := string(c.Type) + " " + c.Rule.String()
		if len(c.Fields) > 0 {
			var fs []string
			for _, f := range c.Fields {
				fs = append(fs, string(f))
			}
			s += " [" + strings.Join(fs, ",") + "]"
		}
		if c.PreviousRule != nil {
			s += " from " + c.PreviousRule.String()
		}
		out = append(out, s)
	}
	return out
}

func TestDiff(t *testing.T) {
	base := []Group{
		group("rules.yaml", "api", 60,
			rec("job:req:rate5m", "sum by (job) (rate(req_total[5m]))"),
			alert("HighErrors", "err > 0.1", 300, map[string]string{"severity": "page"}),
		),
		group("rules.yaml", "db", 30, rec("db:q:rate5m", "rate(q_total[5m])")),
	}

	tests := []struct {
		name string
		prev []Group
		curr []Group
		want []string
	}{
		{
			name: "identical snapshots",
			prev: base,
			curr: base,
			want: nil,
		},
		{
			name: "rule and group order is ignored",
			prev: base,
			curr: []Group{
				group("rules.yaml", "db", 30, rec("db:q:rate5m", "rate(q_total[5m])")),
				group("rules.yaml", "api", 60,
					alert("HighErrors", "err > 0.1", 300, map[string]string{"severity": "page"}),
					rec("job:req:rate5m", "sum by (job) (rate(req_total[5m]))"),
				),
			},
			want: nil,
		},
		{
			name: "rule added",
			prev: base,
			curr: []Group{base[0], group("rules.yaml", "db", 30,
				rec("db:q:rate5m", "rate(q_total[5m])"),
				rec("db:q:rate1h", "rate(q_total[1h])"),
			)},
			want: []string{"added team-a/rules.yaml/db/db:q:rate1h"},
		},
		{
			name: "whole group removed",
			prev: base,
			curr: base[:1],
			want: []string{"removed team-a/rules.yaml/db/db:q:rate5m"},
		},
		{
			name: "expr changed",
			prev: base,
			curr: []Group{base[0], group("rules.yaml", "db", 30, rec("db:q:rate5m", "rate(q_total[10m])"))},
			want: []string{"changed team-a/rules.yaml/db/db:q:rate5m [expr]"},
		},
		{
			name: "for and labels changed",
			prev: base,
			curr: []Group{
				group("rules.yaml", "api", 60,
					rec("job:req:rate5m", "sum by (job) (rate(req_total[5m]))"),
					alert("HighErrors", "err > 0.1", 600, map[string]string{"severity": "ticket"}),
				),
				base[1],
			},
			want: []string{"changed team-a/rules.yaml/api/HighErrors [for,labels]"},
		},
		{
			name: "group interval change is reported on every rule in the group",
			prev: base,
			curr: []Group{
				{Namespace: "rules.yaml", Name: "api", IntervalSeconds: 15, Rules: base[0].Rules},
				base[1],
			},
			want: []string{
				"changed team-a/rules.yaml/api/HighErrors [interval]",
				"changed team-a/rules.yaml/api/job:req:rate5m [interval]",
			},
		},
		{
			name: "rule moved to another group",
			prev: base,
			curr: []Group{
				group("rules.yaml", "api", 60, base[0].Rules[1]),
				group("rules.yaml", "db", 30,
					rec("db:q:rate5m", "rate(q_total[5m])"),
					rec("job:req:rate5m", "sum by (job) (rate(req_total[5m]))"),
				),
			},
			want: []string{"changed team-a/rules.yaml/db/job:req:rate5m [group,interval] from team-a/rules.yaml/api/job:req:rate5m"},
		},
		{
			name: "rule moved to another namespace and edited",
			prev: base,
			curr: []Group{
				base[0],
				group("db.yaml", "db", 30, rec("db:q:rate5m", "rate(q_total[1m])")),
			},
			want: []string{"changed team-a/db.yaml/db/db:q:rate5m [namespace,expr] from team-a/rules.yaml/db/db:q:rate5m"},
		},
		{
			name: "ambiguous move stays removed and added",
			prev: []Group{
				group("a.yaml", "g", 60, rec("x", "up")),
				group("b.yaml", "g", 60, rec("x", "up")),
			},
			curr: []Group{
				group("c.yaml", "g", 60, rec("x", "up")),
				group("d.yaml", "g", 60, rec("x", "up")),
			},
			want: []string{
				"removed team-a/a.yaml/g/x",
				"removed team-a/b.yaml/g/x",
				"added team-a/c.yaml/g/x",
				"added team-a/d.yaml/g/x",
			},
		},
		{
			name: "same name, different kind is not a move",
			prev: []Group{group("a.yaml", "g", 60, rec("x", "up"))},
			curr: []Group{group("b.yaml", "g", 60, alert("x", "up == 0", 0, nil))},
			want: []string{"removed team-a/a.yaml/g/x", "added team-a/b.yaml/g/x"},
		},
		{
			name: "kind changed in place",
			prev: []Group{group("a.yaml", "g", 60, rec("x", "up"))},
			curr: []Group{group("a.yaml", "g", 60, alert("x", "up", 0, nil))},
			want: []string{"changed team-a/a.yaml/g/x [kind]"},
		},
		{
			name: "duplicate names: reordering is not a change",
			prev: []Group{group("a.yaml", "g", 60, rec("x", `sum(up{job="a"})`), rec("x", `sum(up{job="b"})`))},
			curr: []Group{group("a.yaml", "g", 60, rec("x", `sum(up{job="b"})`), rec("x", `sum(up{job="a"})`))},
			want: nil,
		},
		{
			name: "duplicate names: one of two edited",
			prev: []Group{group("a.yaml", "g", 60, rec("x", `sum(up{job="a"})`), rec("x", `sum(up{job="b"})`))},
			curr: []Group{group("a.yaml", "g", 60, rec("x", `sum(up{job="a"})`), rec("x", `sum(up{job="c"})`))},
			want: []string{"changed team-a/a.yaml/g/x [expr]"},
		},
		{
			name: "duplicate names: one of two removed",
			prev: []Group{group("a.yaml", "g", 60, rec("x", `sum(up{job="a"})`), rec("x", `sum(up{job="b"})`))},
			curr: []Group{group("a.yaml", "g", 60, rec("x", `sum(up{job="b"})`))},
			want: []string{"removed team-a/a.yaml/g/x"},
		},
		{
			name: "nil and empty label maps are equal",
			prev: []Group{group("a.yaml", "g", 60, Rule{Kind: "alerting", Name: "A", Expr: "up == 0", Labels: map[string]string{}})},
			curr: []Group{group("a.yaml", "g", 60, Rule{Kind: "alerting", Name: "A", Expr: "up == 0"})},
			want: nil,
		},
		{
			name: "annotations and keep_firing_for",
			prev: []Group{group("a.yaml", "g", 60, Rule{Kind: "alerting", Name: "A", Expr: "up == 0", Annotations: map[string]string{"summary": "down"}})},
			curr: []Group{group("a.yaml", "g", 60, Rule{Kind: "alerting", Name: "A", Expr: "up == 0", KeepFiringForSeconds: 300, Annotations: map[string]string{"summary": "gone"}})},
			want: []string{"changed team-a/a.yaml/g/A [keep_firing_for,annotations]"},
		},
		{
			name: "tenant with no rules",
			prev: base,
			curr: nil,
			want: []string{
				"removed team-a/rules.yaml/api/HighErrors",
				"removed team-a/rules.yaml/api/job:req:rate5m",
				"removed team-a/rules.yaml/db/db:q:rate5m",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Diff(snapAt(0, tc.prev...), snapAt(5, tc.curr...))
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if s := summary(got); !slices.Equal(s, tc.want) {
				t.Errorf("Diff =\n  %s\nwant\n  %s", strings.Join(s, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

func TestDiff_WindowAndStates(t *testing.T) {
	prev := snapAt(0, group("a.yaml", "g", 60, rec("x", "up")))
	curr := snapAt(5, group("a.yaml", "g", 60, rec("x", "up == 1")), group("a.yaml", "h", 30, rec("y", "up")))

	got, err := Diff(prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d changes, want 2: %v", len(got), summary(got))
	}
	want := Window{After: prev.RequestedAt, NotAfter: curr.ObservedAt}
	for _, c := range got {
		if c.Window != want {
			t.Errorf("%s window = %+v, want (%s, %s]", c.Rule, c.Window, want.After, want.NotAfter)
		}
	}

	changed, added := got[0], got[1]
	if changed.Before == nil || changed.Before.Expr != "up" || changed.After == nil || changed.After.Expr != "up == 1" {
		t.Errorf("changed Before/After = %+v / %+v, want expr up -> up == 1", changed.Before, changed.After)
	}
	if added.Before != nil || added.After == nil || added.After.IntervalSeconds != 30 {
		t.Errorf("added Before/After = %+v / %+v, want only After, with interval 30", added.Before, added.After)
	}
	if added.Rule != id("a.yaml", "h", "y") {
		t.Errorf("added.Rule = %+v", added.Rule)
	}
}

// The key a change carries must be the same one attribution joins on, so a
// change and the workload it affects line up without translation.
func TestDiff_RuleIDMatchesAnnotatedRule(t *testing.T) {
	prev := snapAt(0)
	curr := snapAt(5, group("ns/rules.yaml", "g", 60, rec("x", "up")))
	got, err := Diff(prev, curr)
	if err != nil || len(got) != 1 {
		t.Fatalf("Diff = %v, %v", got, err)
	}
	ar := rule.AnnotatedRule{
		Rule:     rule.Rule{Kind: rule.KindRecording, Record: "x", Expr: "up"},
		Group:    rule.RuleGroupMeta{Name: "g"},
		Tenant:   "team-a",
		Location: rule.SourceLocation{File: "ns/rules.yaml", Group: "g", Rule: "x"},
	}
	if got[0].Rule != ar.RuleID() {
		t.Errorf("change key %+v != AnnotatedRule.RuleID() %+v", got[0].Rule, ar.RuleID())
	}
}

func TestDiff_Errors(t *testing.T) {
	a := snapAt(0)
	other := snapAt(5)
	other.Tenant = "team-b"
	if _, err := Diff(a, other); err == nil {
		t.Error("Diff across tenants: want error")
	}

	overlapping := snapAt(0)
	overlapping.RequestedAt = a.ObservedAt.Add(-time.Second)
	overlapping.ObservedAt = a.ObservedAt.Add(time.Second)
	if _, err := Diff(a, overlapping); err == nil {
		t.Error("Diff of overlapping snapshots: want error")
	}

	if _, err := Diff(snapAt(5), snapAt(0)); err == nil {
		t.Error("Diff with curr before prev: want error")
	}
}
