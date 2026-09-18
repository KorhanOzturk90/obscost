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
// tenant with one snapshot has a baseline but no changes yet.
type TenantTimeline struct {
	Tenant        string    `json:"tenant"`
	Snapshots     int       `json:"snapshots"`
	FirstObserved time.Time `json:"first_observed"`
	LastObserved  time.Time `json:"last_observed"`
	Changes       []Change  `json:"changes"`
}

// Build sorts snapshots per tenant by ObservedAt and diffs each consecutive
// pair. Tenants come out in name order. Two snapshots of one tenant with
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
			LastObserved:  ss[len(ss)-1].ObservedAt,
			Changes:       []Change{},
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
