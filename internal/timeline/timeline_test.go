package timeline

import (
	"slices"
	"testing"
)

func TestBuild(t *testing.T) {
	s0 := snapAt(0, group("a.yaml", "g", 60, rec("x", "up")))
	s1 := snapAt(5, group("a.yaml", "g", 60, rec("x", "up"), rec("y", "up")))
	s2 := snapAt(10, group("a.yaml", "g", 60, rec("y", "up")))
	b0 := snapAt(0)
	b0.Tenant = "team-b"

	// Input order must not matter.
	tl, err := Build([]Snapshot{s2, b0, s0, s1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(tl.Tenants) != 2 || tl.Tenants[0].Tenant != "team-a" || tl.Tenants[1].Tenant != "team-b" {
		t.Fatalf("tenants = %+v, want team-a then team-b", tl.Tenants)
	}

	a := tl.Tenants[0]
	if a.Snapshots != 3 || !a.FirstObserved.Equal(s0.ObservedAt) || !a.LastObserved.Equal(s2.ObservedAt) {
		t.Errorf("team-a coverage = %d snapshots, %s..%s", a.Snapshots, a.FirstObserved, a.LastObserved)
	}
	want := []string{"added team-a/a.yaml/g/y", "removed team-a/a.yaml/g/x"}
	if got := summary(a.Changes); !slices.Equal(got, want) {
		t.Errorf("team-a changes = %v, want %v", got, want)
	}
	if !a.Changes[0].Window.After.Equal(s0.RequestedAt) || !a.Changes[0].Window.NotAfter.Equal(s1.ObservedAt) {
		t.Errorf("first change window = %+v, want (s0.requested, s1.observed]", a.Changes[0].Window)
	}
	if !a.Changes[1].Window.After.Equal(s1.RequestedAt) || !a.Changes[1].Window.NotAfter.Equal(s2.ObservedAt) {
		t.Errorf("second change window = %+v, want (s1.requested, s2.observed]", a.Changes[1].Window)
	}

	b := tl.Tenants[1]
	if b.Snapshots != 1 || b.Changes == nil || len(b.Changes) != 0 {
		t.Errorf("team-b = %+v, want 1 snapshot and an empty, non-nil change list", b)
	}
}

func TestBuild_Empty(t *testing.T) {
	tl, err := Build(nil)
	if err != nil || tl.Tenants == nil || len(tl.Tenants) != 0 {
		t.Errorf("Build(nil) = %+v, %v; want no tenants and no error", tl, err)
	}
}

func TestBuild_DuplicateObservationIsAnError(t *testing.T) {
	if _, err := Build([]Snapshot{snapAt(0), snapAt(0)}); err == nil {
		t.Error("two snapshots of one tenant observed at the same time: want error")
	}
}
