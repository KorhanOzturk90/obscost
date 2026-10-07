package timeline

import (
	"fmt"
	"maps"
	"slices"
	"time"
)

// Timeline is the changes found across a set of snapshots, per tenant.
type Timeline struct {
	Tenants []TenantTimeline `json:"tenants"`
}

// TenantTimeline is one tenant's changes, oldest window first. Snapshots,
// FirstObserved and LastObserved say what span the timeline covers: no
// change before FirstObserved or after LastObserved can appear in it, and a
// tenant with one snapshot has a baseline but no changes yet. Snapshots
// counts stored snapshots, one per distinct rule set in a row; Captures
// counts every capture, including the unchanged ones that stored only a
// LastSeen.
type TenantTimeline struct {
	Tenant        string    `json:"tenant"`
	Snapshots     int       `json:"snapshots"`
	Captures      int       `json:"captures"`
	FirstObserved time.Time `json:"first_observed"`
	LastObserved  time.Time `json:"last_observed"`
	// Unchanged lists, oldest first, each span over which the tenant's
	// definitions were captured more than once without changing.
	Unchanged []UnchangedSpan `json:"unchanged"`
	Changes   []Change        `json:"changes"`
}

// UnchangedSpan says the definitions stored in the snapshot observed at From
// were seen, identical, by every one of Captures captures up to Through. It
// says nothing about the time between Through and the next change's window.
type UnchangedSpan struct {
	From     time.Time `json:"from"`
	Through  time.Time `json:"through"`
	Captures int       `json:"captures"`
}

// Build sorts snapshots per tenant by ObservedAt and diffs each consecutive
// pair, using each snapshot's LastSeen (if any) for the window's start. Tenants come out in name order. Two snapshots of one tenant with
// the same ObservedAt, or overlapping requests, are an error (see Diff).
func Build(snaps []Snapshot) (Timeline, error) {
	byTenant := make(map[string][]Snapshot)
	for _, s := range snaps {
		byTenant[s.Tenant] = append(byTenant[s.Tenant], s)
	}

	tl := Timeline{Tenants: []TenantTimeline{}}
	for _, tenant := range slices.Sorted(maps.Keys(byTenant)) {
		ss := byTenant[tenant]
		slices.SortStableFunc(ss, func(a, b Snapshot) int { return a.ObservedAt.Compare(b.ObservedAt) })

		tt := TenantTimeline{
			Tenant:        tenant,
			Snapshots:     len(ss),
			FirstObserved: ss[0].ObservedAt,
			LastObserved:  ss[len(ss)-1].lastObservedAt(),
			Unchanged:     []UnchangedSpan{},
			Changes:       []Change{},
		}
		for _, s := range ss {
			tt.Captures += s.captures()
			if s.LastSeen != nil {
				tt.Unchanged = append(tt.Unchanged, UnchangedSpan{From: s.ObservedAt, Through: s.LastSeen.ObservedAt, Captures: s.LastSeen.Captures})
			}
		}
		for i := 1; i < len(ss); i++ {
			if ss[i].ObservedAt.Equal(ss[i-1].ObservedAt) {
				return Timeline{}, fmt.Errorf("tenant %s: two snapshots observed at %s", tenant, ss[i].ObservedAt.Format(time.RFC3339Nano))
			}
			changes, err := Diff(ss[i-1], ss[i])
			if err != nil {
				return Timeline{}, err
			}
			tt.Changes = append(tt.Changes, changes...)
		}
		tl.Tenants = append(tl.Tenants, tt)
	}
	return tl, nil
}
