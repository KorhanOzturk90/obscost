package timeline

import (
	"context"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/loader/rulerapi"
)

// Fetcher returns one tenant's rule groups from the ruler API.
// *rulerapi.Loader implements it.
type Fetcher interface {
	Fetch(ctx context.Context, tenant string) ([]rulerapi.Group, error)
}

// Capture takes one snapshot of tenant's rules. source is the Mimir base
// URL, recorded without userinfo. now is the clock (time.Now outside
// tests); it is read immediately before and after the fetch, which gives
// the snapshot's RequestedAt and ObservedAt.
//
// A failed fetch returns an error and no snapshot. It must never become an
// empty snapshot, which the next diff would report as every rule removed.
func Capture(ctx context.Context, f Fetcher, source, tenant string, now func() time.Time) (Snapshot, error) {
	if err := ValidateTenant(tenant); err != nil {
		return Snapshot{}, err
	}
	requested := now()
	groups, err := f.Fetch(ctx, tenant)
	if err != nil {
		return Snapshot{}, err
	}
	observed := now()

	snap := Snapshot{
		Version:     FormatVersion,
		Tenant:      tenant,
		Source:      redactURL(source),
		RequestedAt: requested.UTC(),
		ObservedAt:  observed.UTC(),
		Groups:      make([]Group, 0, len(groups)),
	}
	for _, g := range groups {
		out := Group{
			Namespace:       g.File,
			Name:            g.Name,
			IntervalSeconds: g.Interval,
			Rules:           make([]Rule, 0, len(g.Rules)),
		}
		for _, r := range g.Rules {
			out.Rules = append(out.Rules, Rule{
				Kind:                 r.Type,
				Name:                 r.Name,
				Expr:                 r.Query,
				ForSeconds:           r.Duration,
				KeepFiringForSeconds: r.KeepFiringFor,
				Labels:               nonEmpty(r.Labels),
				Annotations:          nonEmpty(r.Annotations),
			})
		}
		snap.Groups = append(snap.Groups, out)
	}
	return snap, snap.Validate()
}

// nonEmpty normalizes an empty map to nil, so `"labels": {}` from the API
// and an omitted labels field encode the same way in a snapshot.
func nonEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
