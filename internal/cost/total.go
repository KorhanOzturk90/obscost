package cost

import (
	"fmt"
	"sort"
)

// Report is every pool's allocation plus the one thing that spans them.
type Report struct {
	Pools     []PoolAllocation `json:"pools"`
	CrossPool CrossPool        `json:"cross_pool"`
}

// CrossPool is where a total across pools lives, and the only place. ADR
// 0003 [A]: a cross-pool total always names its own coverage, or it is not
// rendered. Putting the totals (Tenants) inside the struct that describes
// what they cover makes that structural — there is no way to hold a
// per-tenant total without also holding the pools it left out — and when no
// pool is priced Tenants is empty, because a percentage of nothing is not a
// figure.
//
// Pools split three ways, by what is known about them:
//
//   - Priced: costed, and at least one tenant has a share. These are what
//     Tenants sums over.
//   - Unpriced: measured, but the inventory has no price. Their shares
//     stand on their own; they contribute nothing here, and are named so
//     the reader sees what the total leaves out.
//   - Unmeasured: no tenant has a share (no driver data, or all zeros), so
//     even a priced one cannot be split.
type CrossPool struct {
	Priced []PoolID `json:"priced_pools"`
	// Partial names, for each priced pool that is only partly priced, the
	// components left out. Such a pool counts as priced — its cost is a
	// lower bound — but a coverage line that just said "query path" would
	// claim more than was priced.
	Partial    []PartialPool `json:"partially_priced,omitempty"`
	Unpriced   []PoolID      `json:"unpriced_pools,omitempty"`
	Unmeasured []PoolID      `json:"unmeasured_pools,omitempty"`
	// NotModelled names the ADR 0003 pools promcost has no reader for yet
	// (2–5). They are part of the platform's cost whether or not anyone
	// prices them, so a total that omitted them would claim a completeness
	// it does not have.
	NotModelled []string `json:"not_modelled"`

	Currency string `json:"currency,omitempty"`
	// PricedCostMonth is the sum of the priced pools' monthly cost: the
	// denominator of every TenantTotal.Share.
	PricedCostMonth float64 `json:"priced_cost_per_month"`
	// PlatformCostMonth is the operator's own figure for the whole
	// platform, and PricedFraction is PricedCostMonth over it — the "78%
	// of platform cost we can price". Both are nil when the operator gave
	// no platform cost, and PricedFraction is also nil when the priced
	// pools exceed it (an inconsistent inventory, said in Notes): the
	// fraction is computed from the inventory, never assumed.
	PlatformCostMonth *float64 `json:"platform_cost_per_month,omitempty"`
	PricedFraction    *float64 `json:"priced_fraction,omitempty"`

	// Tenants is every tenant with a priced share, largest cost first.
	// Empty when no pool is priced.
	Tenants []TenantTotal `json:"tenants,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

// PartialPool is a priced pool whose inventory left some components out.
type PartialPool struct {
	Pool     PoolID   `json:"pool"`
	Unpriced []string `json:"unpriced_components"`
}

// PoolCost is one pool's contribution to a TenantTotal.
type PoolCost struct {
	Pool      PoolID  `json:"pool"`
	CostMonth float64 `json:"cost_per_month"`
}

// TenantTotal is one tenant's cost summed over the priced pools it was
// measured in. NotMeasuredIn lists priced pools where it had no driver
// value: those contribute nothing to CostMonth because the cost is unknown,
// not zero (ADR 0003 decision 5), so a tenant with any entry there is
// reported at *least* CostMonth.
type TenantTotal struct {
	Tenant        string     `json:"tenant"`
	CostMonth     float64    `json:"cost_per_month"`
	Share         float64    `json:"share"`
	Pools         []PoolCost `json:"pools"`
	NotMeasuredIn []PoolID   `json:"not_measured_in,omitempty"`
}

// AllocateAll allocates each measurement with Allocate and totals what can
// be totalled. expected is passed to every pool: a tenant measured for pool
// 1 but not pool 6 — a tenant with series and no queries — is unmeasured for
// pool 6 only, and the total says so.
func AllocateAll(ms []Measurement, inv Inventory, expected []string) Report {
	r := Report{Pools: make([]PoolAllocation, 0, len(ms))}
	for _, m := range ms {
		r.Pools = append(r.Pools, Allocate(m, inv, expected))
	}
	r.CrossPool = crossPool(r.Pools, inv, expected)
	return r
}

// notModelled is ADR 0003's pools 2–5, by name, until they are built.
var notModelled = []string{"ingester write path", "object storage", "store-gateway", "compactor"}

func crossPool(pools []PoolAllocation, inv Inventory, expected []string) CrossPool {
	cp := CrossPool{Currency: inv.Currency, PlatformCostMonth: inv.PlatformCostMonth, NotModelled: notModelled}

	var priced []PoolAllocation
	for _, a := range pools {
		switch {
		case a.TotalRaw <= 0:
			cp.Unmeasured = append(cp.Unmeasured, a.Pool.ID)
		case a.Costed:
			priced = append(priced, a)
			cp.Priced = append(cp.Priced, a.Pool.ID)
			cp.PricedCostMonth += *a.PoolCostMonth
			if len(a.UnpricedComponents) > 0 {
				cp.Partial = append(cp.Partial, PartialPool{Pool: a.Pool.ID, Unpriced: a.UnpricedComponents})
			}
		default:
			cp.Unpriced = append(cp.Unpriced, a.Pool.ID)
		}
	}
	if len(priced) == 0 {
		return cp
	}

	if cp.PlatformCostMonth != nil {
		if cp.PricedCostMonth <= *cp.PlatformCostMonth {
			f := cp.PricedCostMonth / *cp.PlatformCostMonth
			cp.PricedFraction = &f
		} else {
			cp.Notes = append(cp.Notes, fmt.Sprintf(
				"the priced pools cost %s %s/month, more than inventory.platform_cost_month (%s %s), so no priced fraction is shown: one of the two figures is wrong",
				formatMoney(cp.PricedCostMonth), inv.Currency, formatMoney(*cp.PlatformCostMonth), inv.Currency))
		}
	}

	// The tenant universe is everyone with a share in any priced pool plus
	// everyone the caller expected, so a tenant absent from one priced pool
	// is found and flagged rather than silently short.
	universe := map[string]bool{}
	for _, t := range expected {
		universe[t] = true
	}
	for _, a := range priced {
		for _, t := range a.Tenants {
			universe[t.Tenant] = true
		}
	}
	totals := map[string]*TenantTotal{}
	for t := range universe {
		totals[t] = &TenantTotal{Tenant: t}
	}
	for _, a := range priced {
		measured := map[string]bool{}
		for _, t := range a.Tenants {
			measured[t.Tenant] = true
			if t.Cost == nil {
				continue
			}
			tt := totals[t.Tenant]
			tt.CostMonth += *t.Cost
			tt.Pools = append(tt.Pools, PoolCost{Pool: a.Pool.ID, CostMonth: *t.Cost})
		}
		for t, tt := range totals {
			if !measured[t] {
				tt.NotMeasuredIn = append(tt.NotMeasuredIn, a.Pool.ID)
			}
		}
	}
	for _, tt := range totals {
		// A tenant measured in no priced pool has no priced cost at all:
		// it is expected but unmeasured everywhere the total looks.
		if len(tt.Pools) == 0 {
			continue
		}
		tt.Share = tt.CostMonth / cp.PricedCostMonth
		cp.Tenants = append(cp.Tenants, *tt)
	}
	sort.Slice(cp.Tenants, func(i, j int) bool {
		if cp.Tenants[i].CostMonth != cp.Tenants[j].CostMonth {
			return cp.Tenants[i].CostMonth > cp.Tenants[j].CostMonth
		}
		return cp.Tenants[i].Tenant < cp.Tenants[j].Tenant
	})
	return cp
}
