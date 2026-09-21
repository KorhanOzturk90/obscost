package cost

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// rulerMeasurement is pool 7: rule-evaluation seconds. `monitoring` runs no
// rules, so it is unmeasured here while it has series (pool 1) and queries
// (pool 6) — the shape the k3d rig really has.
func rulerMeasurement() Measurement {
	return Measurement{
		Pool:         RulerCPU,
		Window:       time.Hour,
		Source:       "test",
		DriverQuery:  "q",
		DriverKind:   "rule-evaluation seconds",
		Unit:         UnitSeconds,
		Unreplicated: true,
		Drivers:      map[string]float64{"analytics": 357.8, "infra": 281.5, "payments": 165.0, "platform": 149.4},
	}
}

// ingesterMeasurement has a tenant (monitoring) the ruler never measures.
func ingesterMeasurement() Measurement {
	m := rigMeasurement()
	m.Drivers["monitoring"] = 12643 * 3
	return m
}

func pricedInventory() Inventory {
	return Inventory{
		Currency: "EUR",
		Pools: map[PoolID]PoolInventory{
			PoolIngesterMemory: {Replicas: intp(3), CostPerReplicaMonth: floatp(100)},
			PoolQueryPath: {Components: map[string]ComponentInventory{
				"querier": {Replicas: intp(1), CostPerReplicaMonth: floatp(100)},
			}},
			// ruler_cpu deliberately unpriced
		},
	}
}

func poolIDs(ids []PoolID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = string(id)
	}
	return strings.Join(s, ",")
}

func TestAllocateAll_TotalNamesWhatItCoversAndWhatItLeftOut(t *testing.T) {
	r := AllocateAll([]Measurement{ingesterMeasurement(), queryPathMeasurement(), rulerMeasurement()}, pricedInventory(), nil)
	cp := r.CrossPool
	if got := poolIDs(cp.Priced); got != "ingester_memory,query_path" {
		t.Errorf("Priced = %s, want ingester_memory,query_path", got)
	}
	if got := poolIDs(cp.Unpriced); got != "ruler_cpu" {
		t.Errorf("Unpriced = %s, want ruler_cpu: measured, but no price in the inventory", got)
	}
	if !approx(cp.PricedCostMonth, 400) {
		t.Errorf("PricedCostMonth = %v, want 400 (3×100 ingesters + 1×100 querier); the unpriced ruler contributes nothing, not 0 as a price", cp.PricedCostMonth)
	}
	if len(cp.NotModelled) == 0 {
		t.Error("NotModelled is empty: pools 2–5 have no reader, and a total must say it leaves them out")
	}
	var sum float64
	for _, tt := range cp.Tenants {
		sum += tt.Share
	}
	if !approx(sum, 1) {
		t.Errorf("tenant shares of priced cost sum to %v, want 1", sum)
	}
}

// Without an operator-supplied platform cost there is nothing to compute a
// priced fraction from, so none is invented (ADR 0003 [A]: "computed from
// the operator's inventory, not assumed").
func TestAllocateAll_PricedFractionNeedsThePlatformCost(t *testing.T) {
	ms := []Measurement{ingesterMeasurement(), queryPathMeasurement()}

	r := AllocateAll(ms, pricedInventory(), nil)
	if r.CrossPool.PricedFraction != nil {
		t.Errorf("PricedFraction = %v with no platform cost, want nil", *r.CrossPool.PricedFraction)
	}

	inv := pricedInventory()
	inv.PlatformCostMonth = floatp(1600)
	r = AllocateAll(ms, inv, nil)
	if r.CrossPool.PricedFraction == nil || !approx(*r.CrossPool.PricedFraction, 0.25) {
		t.Errorf("PricedFraction = %v, want 0.25 (400 of 1600)", r.CrossPool.PricedFraction)
	}

	inv.PlatformCostMonth = floatp(300) // less than the pools already priced
	r = AllocateAll(ms, inv, nil)
	if r.CrossPool.PricedFraction != nil {
		t.Errorf("PricedFraction = %v when priced pools exceed the platform cost, want nil", *r.CrossPool.PricedFraction)
	}
	if !strings.Contains(strings.Join(r.CrossPool.Notes, "\n"), "one of the two figures is wrong") {
		t.Errorf("Notes = %q, want the inconsistent inventory called out", r.CrossPool.Notes)
	}
}

// ADR 0003 [A]: when no pool is priced there is no blended figure at all.
func TestAllocateAll_NothingPricedMeansSharesOnly(t *testing.T) {
	r := AllocateAll([]Measurement{ingesterMeasurement(), queryPathMeasurement(), rulerMeasurement()}, Inventory{}, nil)
	cp := r.CrossPool
	if len(cp.Priced) != 0 || len(cp.Tenants) != 0 || cp.PricedCostMonth != 0 || cp.PricedFraction != nil {
		t.Errorf("CrossPool = %+v, want no total of any kind when nothing is priced", cp)
	}
	if got := poolIDs(cp.Unpriced); got != "ingester_memory,query_path,ruler_cpu" {
		t.Errorf("Unpriced = %s, want all three named", got)
	}
	for _, a := range r.Pools {
		for _, ts := range a.Tenants {
			if ts.Share == nil {
				t.Errorf("%s/%s: per-pool shares must survive an empty inventory", a.Pool.ID, ts.Tenant)
			}
		}
	}
}

// A tenant can have series but no queries: measured for pool 1, unmeasured
// for pool 6. Its pool-6 cost is unknown, not zero, and the total says so.
func TestAllocateAll_TenantMeasuredInOnePoolOnly(t *testing.T) {
	qp := queryPathMeasurement()
	delete(qp.Drivers, "platform")
	r := AllocateAll([]Measurement{ingesterMeasurement(), qp}, pricedInventory(), []string{"platform", "monitoring"})

	var platform, analytics *TenantTotal
	for i := range r.CrossPool.Tenants {
		switch r.CrossPool.Tenants[i].Tenant {
		case "platform":
			platform = &r.CrossPool.Tenants[i]
		case "analytics":
			analytics = &r.CrossPool.Tenants[i]
		}
	}
	if platform == nil || analytics == nil {
		t.Fatalf("Tenants = %+v, want platform and analytics", r.CrossPool.Tenants)
	}
	if got := poolIDs(platform.NotMeasuredIn); got != "query_path" {
		t.Errorf("platform.NotMeasuredIn = %q, want query_path", got)
	}
	if len(platform.Pools) != 1 || platform.Pools[0].Pool != PoolIngesterMemory {
		t.Errorf("platform.Pools = %+v, want only its ingester-memory cost", platform.Pools)
	}
	if len(analytics.NotMeasuredIn) != 0 {
		t.Errorf("analytics.NotMeasuredIn = %v, want none", analytics.NotMeasuredIn)
	}
	// The unmeasured tenant is not folded in as a zero in pool 6's own view either.
	for _, a := range r.Pools {
		if a.Pool.ID == PoolQueryPath && !slices.Contains(a.Unmeasured, "platform") {
			t.Errorf("query_path.Unmeasured = %v, want platform listed", a.Unmeasured)
		}
	}
}

// A pool with no driver data cannot be split even if it is priced, so it is
// listed as unmeasured and adds nothing to the priced cost.
func TestAllocateAll_PricedButUnmeasuredPoolIsNotInTheTotal(t *testing.T) {
	inv := pricedInventory()
	qp := queryPathMeasurement()
	qp.Drivers = map[string]float64{}
	r := AllocateAll([]Measurement{ingesterMeasurement(), qp}, inv, nil)
	if got := poolIDs(r.CrossPool.Unmeasured); got != "query_path" {
		t.Errorf("Unmeasured = %q, want query_path", got)
	}
	if !approx(r.CrossPool.PricedCostMonth, 300) {
		t.Errorf("PricedCostMonth = %v, want 300 (ingesters only)", r.CrossPool.PricedCostMonth)
	}
}

func TestAllocateAll_OneMeasurementPerPoolInOrder(t *testing.T) {
	r := AllocateAll([]Measurement{ingesterMeasurement(), rulerMeasurement()}, Inventory{}, nil)
	if len(r.Pools) != 2 || r.Pools[0].Pool.ID != PoolIngesterMemory || r.Pools[1].Pool.ID != PoolRulerCPU {
		t.Errorf("Pools = %v, want the caller's order kept", r.Pools)
	}
}
