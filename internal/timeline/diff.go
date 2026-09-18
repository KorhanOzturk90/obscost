package timeline

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// ChangeType says what happened to a rule between two snapshots.
type ChangeType string

const (
	ChangeAdded   ChangeType = "added"
	ChangeRemoved ChangeType = "removed"
	ChangeChanged ChangeType = "changed"
)

// Field names one aspect of a rule's definition that a ChangeChanged event
// found different.
type Field string

const (
	FieldKind          Field = "kind"
	FieldExpr          Field = "expr"
	FieldFor           Field = "for"
	FieldKeepFiringFor Field = "keep_firing_for"
	FieldLabels        Field = "labels"
	FieldAnnotations   Field = "annotations"
	// FieldInterval is the rule's group evaluation interval. A group's
	// interval change is reported on every rule in the group, since each
	// rule's evaluation rate (and so its cost) changes.
	FieldInterval Field = "interval"
	// FieldNamespace and FieldGroup mean the rule moved: see Change.PreviousRule.
	FieldNamespace Field = "namespace"
	FieldGroup     Field = "group"
)

// RuleState is everything a snapshot knows about one rule's definition,
// including its group's evaluation interval.
type RuleState struct {
	Kind                 string            `json:"kind"`
	Expr                 string            `json:"expr"`
	ForSeconds           float64           `json:"for_seconds,omitempty"`
	KeepFiringForSeconds float64           `json:"keep_firing_for_seconds,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	Annotations          map[string]string `json:"annotations,omitempty"`
	IntervalSeconds      float64           `json:"interval_seconds"`
}

// Window is the interval (After, NotAfter] in which a change became visible
// in the ruler API. After is the previous snapshot's RequestedAt and is
// exclusive. NotAfter is the current snapshot's ObservedAt and is
// inclusive. See the package doc for why a change can't be dated more
// precisely than this.
type Window struct {
	After    time.Time `json:"after"`
	NotAfter time.Time `json:"not_after"`
}

// Change is one rule's difference between two consecutive snapshots of the
// same tenant.
type Change struct {
	Type ChangeType `json:"type"`
	// Rule is the rule's identity after the change, or before it for
	// ChangeRemoved. It is the same key rule.AnnotatedRule.RuleID and
	// rule.RuleExecution.RuleID produce, so a change joins directly to
	// workload attribution.
	Rule rule.RuleID `json:"rule"`
	// PreviousRule is set when a rule moved to a different namespace or
	// group, and holds its identity before the move.
	PreviousRule *rule.RuleID `json:"previous_rule,omitempty"`
	// Fields lists what differs, for ChangeChanged only, in a fixed order.
	Fields []Field    `json:"fields,omitempty"`
	Before *RuleState `json:"before,omitempty"`
	After  *RuleState `json:"after,omitempty"`
	Window Window     `json:"window"`
}

// Diff returns the changes between two snapshots of the same tenant, in a
// deterministic order (by rule identity, then type).
//
// Rules are matched by rule.RuleID (tenant, namespace, group, name). Two
// cases need more than that:
//
//   - Several rules in one group can share a name (a recording rule written
//     twice with different label matchers, say). Within one RuleID, rules
//     that are identical in both snapshots are matched first; the rest are
//     paired in API order and reported as changed, and any left over as
//     added or removed. The pairing among changed duplicates is a
//     best guess, because the ruler API gives them nothing else to go by.
//   - A rule moved to another namespace or group has a new RuleID. When
//     exactly one rule with a given kind and name disappeared and exactly
//     one with the same kind and name appeared, they are reported as one
//     ChangeChanged with FieldNamespace and/or FieldGroup and PreviousRule
//     set. Otherwise they stay separate removed and added events, rather
//     than guessing which is which.
//
// A rule renamed is a removal and an addition: its name is its identity.
// Rule order within a group is ignored.
//
// Diff returns an error if the snapshots are for different tenants, or if
// curr's request started before prev's response was read: the two could
// then describe the ruler in either order.
func Diff(prev, curr Snapshot) ([]Change, error) {
	if prev.Tenant != curr.Tenant {
		return nil, fmt.Errorf("cannot diff snapshots of different tenants %q and %q", prev.Tenant, curr.Tenant)
	}
	if curr.RequestedAt.Before(prev.ObservedAt) {
		return nil, fmt.Errorf("tenant %s: snapshot requested at %s overlaps or precedes the one observed at %s",
			curr.Tenant, curr.RequestedAt.Format(time.RFC3339Nano), prev.ObservedAt.Format(time.RFC3339Nano))
	}
	window := Window{After: prev.RequestedAt, NotAfter: curr.ObservedAt}

	before, after := flatten(prev), flatten(curr)

	var (
		changes []Change
		removed []identified
		added   []identified
	)
	for _, id := range unionKeys(before, after) {
		p, c := before[id], after[id]
		p, c = dropIdentical(p, c)
		n := min(len(p), len(c))
		for i := range n {
			changes = append(changes, changed(id, id, p[i], c[i], window))
		}
		for _, s := range p[n:] {
			removed = append(removed, identified{id, s})
		}
		for _, s := range c[n:] {
			added = append(added, identified{id, s})
		}
	}

	removed, added, moves := pairMoves(removed, added, window)
	changes = append(changes, moves...)
	for _, r := range removed {
		changes = append(changes, Change{Type: ChangeRemoved, Rule: r.id, Before: rule.Ptr(r.state), Window: window})
	}
	for _, a := range added {
		changes = append(changes, Change{Type: ChangeAdded, Rule: a.id, After: rule.Ptr(a.state), Window: window})
	}

	slices.SortStableFunc(changes, func(a, b Change) int {
		return cmp.Or(compareIDs(a.Rule, b.Rule), cmp.Compare(typeOrder(a.Type), typeOrder(b.Type)))
	})
	return changes, nil
}

type identified struct {
	id    rule.RuleID
	state RuleState
}

func flatten(s Snapshot) map[rule.RuleID][]RuleState {
	out := make(map[rule.RuleID][]RuleState)
	for _, g := range s.Groups {
		for _, r := range g.Rules {
			id := rule.RuleID{Tenant: s.Tenant, Namespace: g.Namespace, Group: g.Name, Name: r.Name}
			out[id] = append(out[id], RuleState{
				Kind:                 r.Kind,
				Expr:                 r.Expr,
				ForSeconds:           r.ForSeconds,
				KeepFiringForSeconds: r.KeepFiringForSeconds,
				Labels:               r.Labels,
				Annotations:          r.Annotations,
				IntervalSeconds:      g.IntervalSeconds,
			})
		}
	}
	return out
}

func unionKeys(a, b map[rule.RuleID][]RuleState) []rule.RuleID {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, compareIDs)
	return keys
}

// dropIdentical removes rules present, unchanged, in both lists, keeping
// the remaining ones in their original order.
func dropIdentical(p, c []RuleState) ([]RuleState, []RuleState) {
	usedP := make([]bool, len(p))
	var restC []RuleState
	for _, cs := range c {
		matched := false
		for i, ps := range p {
			if !usedP[i] && len(diffFields(ps, cs)) == 0 {
				usedP[i], matched = true, true
				break
			}
		}
		if !matched {
			restC = append(restC, cs)
		}
	}
	var restP []RuleState
	for i, ps := range p {
		if !usedP[i] {
			restP = append(restP, ps)
		}
	}
	return restP, restC
}

// pairMoves turns a removed and an added rule into one move when they are
// the only removed and the only added rule with that kind and name.
func pairMoves(removed, added []identified, window Window) (restRemoved, restAdded []identified, moves []Change) {
	type key struct{ kind, name string }
	count := func(xs []identified) map[key]int {
		m := make(map[key]int)
		for _, x := range xs {
			m[key{x.state.Kind, x.id.Name}]++
		}
		return m
	}
	nRemoved, nAdded := count(removed), count(added)
	unique := func(k key) bool { return nRemoved[k] == 1 && nAdded[k] == 1 }

	from := make(map[key]identified)
	for _, r := range removed {
		k := key{r.state.Kind, r.id.Name}
		if unique(k) {
			from[k] = r
			continue
		}
		restRemoved = append(restRemoved, r)
	}
	for _, a := range added {
		k := key{a.state.Kind, a.id.Name}
		if r, ok := from[k]; ok {
			moves = append(moves, changed(r.id, a.id, r.state, a.state, window))
			continue
		}
		restAdded = append(restAdded, a)
	}
	return restRemoved, restAdded, moves
}

func changed(prevID, currID rule.RuleID, before, after RuleState, window Window) Change {
	var fields []Field
	if prevID.Namespace != currID.Namespace {
		fields = append(fields, FieldNamespace)
	}
	if prevID.Group != currID.Group {
		fields = append(fields, FieldGroup)
	}
	fields = append(fields, diffFields(before, after)...)

	c := Change{
		Type:   ChangeChanged,
		Rule:   currID,
		Fields: fields,
		Before: rule.Ptr(before),
		After:  rule.Ptr(after),
		Window: window,
	}
	if prevID != currID {
		c.PreviousRule = rule.Ptr(prevID)
	}
	return c
}

// diffFields lists the definition fields that differ between a and b. Maps
// compare by content, so a nil map and an empty one are equal.
func diffFields(a, b RuleState) []Field {
	var fields []Field
	if a.Kind != b.Kind {
		fields = append(fields, FieldKind)
	}
	if a.Expr != b.Expr {
		fields = append(fields, FieldExpr)
	}
	if a.ForSeconds != b.ForSeconds {
		fields = append(fields, FieldFor)
	}
	if a.KeepFiringForSeconds != b.KeepFiringForSeconds {
		fields = append(fields, FieldKeepFiringFor)
	}
	if !maps.Equal(a.Labels, b.Labels) {
		fields = append(fields, FieldLabels)
	}
	if !maps.Equal(a.Annotations, b.Annotations) {
		fields = append(fields, FieldAnnotations)
	}
	if a.IntervalSeconds != b.IntervalSeconds {
		fields = append(fields, FieldInterval)
	}
	return fields
}

func compareIDs(a, b rule.RuleID) int {
	return cmp.Or(
		cmp.Compare(a.Tenant, b.Tenant),
		cmp.Compare(a.Namespace, b.Namespace),
		cmp.Compare(a.Group, b.Group),
		cmp.Compare(a.Name, b.Name),
	)
}

func typeOrder(t ChangeType) int {
	switch t {
	case ChangeRemoved:
		return 0
	case ChangeChanged:
		return 1
	default:
		return 2
	}
}
