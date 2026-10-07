package cost

import (
	"math"
	"strings"
	"testing"
	"time"
)

func intp(v int) *int             { return &v }
func floatp(v float64) *float64   { return &v }
func approx(a, b float64) bool    { return math.Abs(a-b) < 1e-9 }
func share(t TenantShare) float64 { return *t.Share }

// rigMeasurement is ADR 0003's worked example, scaled by RF=3 the way a
// real cluster would report it: each tenant's active series summed across
// ingesters counts every series three times.
func rigMeasurement() Measurement {
	return Measurement{
		Pool:        IngesterMemory,
		Window:      24 * time.Hour,
		Source:      "test",
		DriverQuery: "q",
		Drivers: map[string]float64{
			"analytics": 15005 * 3,
			"infra":     7743 * 3,
			"payments":  1205 * 3,
			"platform":  105 * 3,
		},
		ReplicationFactor: floatp(3),
		RFQuery:           "rfq",
	}
}

func TestAllocate_SharesAreExhaustiveAndSumToOne(t *testing.T) {
	a := Allocate(rigMeasurement(), Inventory{}, nil)
	if len(a.Tenants) != 4 {
		t.Fatalf("len(Tenants) = %d, want every measured tenant (4)", len(a.Tenants))
	}
	var sum float64
	for _, ts := range a.Tenants {
		sum += share(ts)
	}
	if !approx(sum, 1) {
		t.Errorf("shares sum to %v, want 1", sum)
	}
	if a.Tenants[0].Tenant != "analytics" || !approx(share(a.Tenants[0]), 15005.0/24058) {
		t.Errorf("top tenant = %s %.4f, want analytics %.4f", a.Tenants[0].Tenant, share(a.Tenants[0]), 15005.0/24058)
	}
}

// The ADR 0003 [A] acceptance criterion: with RF>1, absolute figures must
// be divided by the replication factor. RF=1 on the rig cannot catch this.
func TestAllocate_ReplicationFactorDividesAbsoluteFigures(t *testing.T) {
	a := Allocate(rigMeasurement(), Inventory{}, nil)
	if a.Total == nil || !approx(*a.Total, 24058) {
		t.Fatalf("Total = %v, want 24058 (raw sum / RF 3)", a.Total)
	}
	if got := a.Tenants[0].Driver; got == nil || !approx(*got, 15005) {
		t.Errorf("analytics Driver = %v, want 15005", got)
	}
	if !approx(a.TotalRaw, 24058*3) {
		t.Errorf("TotalRaw = %v, want the undivided sum %v", a.TotalRaw, 24058*3)
	}
}

func TestAllocate_EquivalentReplicasAndCost(t *testing.T) {
	inv := Inventory{
		Currency: "EUR",
		Pools: map[PoolID]PoolInventory{
			PoolIngesterMemory: {Replicas: intp(8), CostPerReplicaMonth: floatp(300)},
		},
	}
	a := Allocate(rigMeasurement(), inv, nil)
	if !a.Costed || a.PoolCostMonth == nil || *a.PoolCostMonth != 2400 {
		t.Fatalf("Costed=%v PoolCostMonth=%v, want true / 2400", a.Costed, a.PoolCostMonth)
	}
	top := a.Tenants[0]
	if top.EquivalentReplicas == nil || math.Abs(*top.EquivalentReplicas-4.99) > 0.01 {
		t.Errorf("analytics EquivalentReplicas = %v, want ≈ 4.99 (\"5 of your 8 ingesters\")", top.EquivalentReplicas)
	}
	var costSum float64
	for _, ts := range a.Tenants {
		costSum += *ts.Cost
	}
	if !approx(costSum, 2400) {
		t.Errorf("tenant costs sum to %v, want the pool cost 2400", costSum)
	}
}

func TestAllocate_PartialInventory(t *testing.T) {
	tests := []struct {
		name           string
		pinv           PoolInventory
		wantEquivalent bool
		wantCosted     bool
		wantNote       string
	}{
		{name: "nothing supplied", pinv: PoolInventory{}},
		{name: "replicas only", pinv: PoolInventory{Replicas: intp(8)}, wantEquivalent: true},
		{name: "price only", pinv: PoolInventory{CostPerReplicaMonth: floatp(300)}, wantNote: "without a replica count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := Inventory{Currency: "EUR", Pools: map[PoolID]PoolInventory{PoolIngesterMemory: tt.pinv}}
			a := Allocate(rigMeasurement(), inv, nil)
			if a.Costed != tt.wantCosted {
				t.Errorf("Costed = %v, want %v", a.Costed, tt.wantCosted)
			}
			for _, ts := range a.Tenants {
				if ts.Share == nil {
					t.Errorf("%s: shares must render whatever the inventory holds", ts.Tenant)
				}
				if (ts.EquivalentReplicas != nil) != tt.wantEquivalent {
					t.Errorf("%s: EquivalentReplicas = %v, want present=%v", ts.Tenant, ts.EquivalentReplicas, tt.wantEquivalent)
				}
				if ts.Cost != nil {
					t.Errorf("%s: Cost = %v, want nil (not costed, never 0)", ts.Tenant, *ts.Cost)
				}
			}
			if tt.wantNote != "" && !strings.Contains(strings.Join(a.Notes, "\n"), tt.wantNote) {
				t.Errorf("Notes = %q, want one mentioning %q", a.Notes, tt.wantNote)
			}
		})
	}
}

// ADR 0003 decision 5: an expected tenant with no driver value is reported
// as unmeasured and kept out of the denominator — folding it in as 0 would
// understate it and leave everyone else's share unchanged only by luck.
func TestAllocate_UnmeasuredTenantIsNotZero(t *testing.T) {
	m := rigMeasurement()
	delete(m.Drivers, "payments")
	a := Allocate(m, Inventory{}, []string{"payments", "infra", "payments"})
	if len(a.Unmeasured) != 1 || a.Unmeasured[0] != "payments" {
		t.Fatalf("Unmeasured = %v, want [payments]", a.Unmeasured)
	}
	for _, ts := range a.Tenants {
		if ts.Tenant == "payments" {
			t.Fatal("payments appears among measured tenants")
		}
	}
	var sum float64
	for _, ts := range a.Tenants {
		sum += share(ts)
	}
	if !approx(sum, 1) {
		t.Errorf("shares over the measured set sum to %v, want 1", sum)
	}
}

func TestAllocate_UnknownReplicationFactorKeepsShares(t *testing.T) {
	m := rigMeasurement()
	m.ReplicationFactor = nil
	a := Allocate(m, Inventory{}, nil)
	if a.Total != nil || a.Tenants[0].Driver != nil {
		t.Errorf("absolute figures present with no replication factor: Total=%v Driver=%v", a.Total, a.Tenants[0].Driver)
	}
	if a.Tenants[0].Share == nil || !approx(*a.Tenants[0].Share, 15005.0/24058) {
		t.Errorf("share = %v, want %v regardless of RF", a.Tenants[0].Share, 15005.0/24058)
	}
	if len(a.Notes) == 0 {
		t.Error("expected a note explaining withheld absolute figures")
	}
}

func TestAllocate_InventoryReplicationFactorOverrides(t *testing.T) {
	a := Allocate(rigMeasurement(), Inventory{ReplicationFactor: intp(1)}, nil)
	if a.Total == nil || !approx(*a.Total, 24058*3) {
		t.Errorf("Total = %v, want the raw sum under an RF=1 override", a.Total)
	}
	if !strings.Contains(strings.Join(a.Notes, "\n"), "overrides") {
		t.Errorf("Notes = %q, want the override disagreement surfaced", a.Notes)
	}
}

func TestAllocate_ZeroTotalLeavesSharesUndefined(t *testing.T) {
	m := rigMeasurement()
	m.Drivers = map[string]float64{"a": 0, "b": 0}
	a := Allocate(m, Inventory{}, nil)
	for _, ts := range a.Tenants {
		if ts.Share != nil {
			t.Errorf("%s: Share = %v, want nil (0/0 is undefined, not 0%%)", ts.Tenant, *ts.Share)
		}
	}
}

func TestAllocate_AssumptionTrailNamesEveryInput(t *testing.T) {
	inv := Inventory{Currency: "EUR", Pools: map[PoolID]PoolInventory{
		PoolIngesterMemory: {Replicas: intp(8), CostPerReplicaMonth: floatp(300)},
	}}
	a := Allocate(rigMeasurement(), inv, nil)
	names := map[string]bool{}
	for _, as := range a.Assumptions {
		names[as.Name] = true
		if as.Source == "" {
			t.Errorf("assumption %q has no source", as.Name)
		}
	}
	for _, want := range []string{"driver", "driver query", "replication factor", "ingester replicas", "price"} {
		if !names[want] {
			t.Errorf("assumption trail is missing %q", want)
		}
	}
}

func TestInventoryValidate(t *testing.T) {
	tests := []struct {
		name string
		inv  Inventory
		want string
	}{
		{"price without currency", Inventory{Pools: map[PoolID]PoolInventory{PoolIngesterMemory: {CostPerReplicaMonth: floatp(1)}}}, "currency"},
		{"zero replicas", Inventory{Pools: map[PoolID]PoolInventory{PoolIngesterMemory: {Replicas: intp(0)}}}, "replicas"},
		{"negative price", Inventory{Currency: "EUR", Pools: map[PoolID]PoolInventory{PoolIngesterMemory: {CostPerReplicaMonth: floatp(-1)}}}, "negative"},
		{"zero RF", Inventory{ReplicationFactor: intp(0)}, "replication_factor"},
		{"unknown pool", Inventory{Pools: map[PoolID]PoolInventory{"ingesters": {Replicas: intp(8)}}}, "unknown pool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.inv.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() = %v, want an error mentioning %q", err, tt.want)
			}
		})
	}
	if err := (Inventory{}).Validate(); err != nil {
		t.Errorf("empty inventory: %v, want valid (every field is optional)", err)
	}
}

func TestWindowString(t *testing.T) {
	tests := map[time.Duration]string{
		5 * time.Minute:         "5m",
		90 * time.Minute:        "90m",
		24 * time.Hour:          "24h",
		7 * 24 * time.Hour:      "7d",
		1500 * time.Millisecond: "1.5s",
	}
	for in, want := range tests {
		if got := windowString(in); got != want {
			t.Errorf("windowString(%v) = %q, want %q", in, got, want)
		}
	}
}

// Under ingest storage the driver query has already deduplicated, so no
// divisor applies — and an inventory override must not reintroduce one.
func TestAllocate_DeduplicatedDriversAreNotDivided(t *testing.T) {
	m := rigMeasurement()
	for k, v := range m.Drivers {
		m.Drivers[k] = v / 3
	}
	m.ReplicationFactor = nil
	m.Deduplicated = true
	m.Architecture = "ingest storage"
	a := Allocate(m, Inventory{ReplicationFactor: intp(3)}, nil)
	if a.Total == nil || !approx(*a.Total, 24058) {
		t.Errorf("Total = %v, want 24058 undivided", a.Total)
	}
	if !strings.Contains(strings.Join(a.Notes, "\n"), "ignored") {
		t.Errorf("Notes = %q, want the ignored override surfaced", a.Notes)
	}
}

// --- pools 6 and 7 ---------------------------------------------------------

// queryPathMeasurement is pool 6 as the rig reports it: a counter, already
// counted once, in bytes.
func queryPathMeasurement() Measurement {
	return Measurement{
		Pool:         QueryPath,
		Window:       time.Hour,
		Source:       "test",
		DriverQuery:  "q",
		DriverMetric: "cortex_query_fetched_chunk_bytes_total",
		DriverKind:   "chunk bytes fetched",
		Unit:         UnitBytes,
		Unreplicated: true,
		Drivers:      map[string]float64{"analytics": 52940813, "infra": 47543702, "payments": 1020188, "platform": 129673},
	}
}

func TestAllocate_UnreplicatedCounterHasNoReplicationFactor(t *testing.T) {
	a := Allocate(queryPathMeasurement(), Inventory{ReplicationFactor: intp(3)}, nil)
	if a.ReplicationFactor != nil {
		t.Errorf("ReplicationFactor = %v, want nil: a counter counted once has no divisor", *a.ReplicationFactor)
	}
	if a.Total == nil || !approx(*a.Total, a.TotalRaw) {
		t.Errorf("Total = %v, want the raw sum %v undivided even though the inventory says RF=3", a.Total, a.TotalRaw)
	}
	for _, as := range a.Assumptions {
		if as.Name == "replication factor" {
			t.Errorf("the trail names a replication factor (%q) for a pool that has none", as.Value)
		}
	}
	if strings.Contains(strings.Join(a.Notes, "\n"), "replication") {
		t.Errorf("Notes = %q, want no replication-factor note for an unreplicated pool", a.Notes)
	}
}

func TestAllocate_CumulativeDriverIsWordedAsATotal(t *testing.T) {
	a := Allocate(queryPathMeasurement(), Inventory{}, nil)
	var driver string
	for _, as := range a.Assumptions {
		if as.Name == "driver" {
			driver = as.Value
		}
	}
	for _, want := range []string{"cortex_query_fetched_chunk_bytes_total", "chunk bytes fetched", "total increase over 1h"} {
		if !strings.Contains(driver, want) {
			t.Errorf("driver assumption = %q, want it to contain %q", driver, want)
		}
	}
	if a.DriverKind != "chunk bytes fetched" || a.Unit != UnitBytes {
		t.Errorf("DriverKind/Unit = %q/%q, want the measured kind, not the pool's generic one", a.DriverKind, a.Unit)
	}
}

func TestAllocate_FallbackIsWarnedAbout(t *testing.T) {
	m := queryPathMeasurement()
	m.Fallback, m.FallbackNote = true, "query time is contention-sensitive"
	a := Allocate(m, Inventory{}, nil)
	if !a.Fallback {
		t.Error("Fallback not carried to the allocation")
	}
	if !strings.Contains(strings.Join(a.Notes, "\n"), "contention-sensitive") {
		t.Errorf("Notes = %q, want the fallback warning where the reader will see it", a.Notes)
	}
}

func queryPathInv(components map[string]ComponentInventory) Inventory {
	return Inventory{Currency: "EUR", Pools: map[PoolID]PoolInventory{PoolQueryPath: {Components: components}}}
}

func TestAllocate_ComponentPoolIsTheSumOfItsPricedComponents(t *testing.T) {
	a := Allocate(queryPathMeasurement(), queryPathInv(map[string]ComponentInventory{
		"querier":        {Replicas: intp(3), CostPerReplicaMonth: floatp(100)},
		"query-frontend": {Replicas: intp(2), CostPerReplicaMonth: floatp(50)},
	}), nil)
	if !a.Costed || a.PoolCostMonth == nil || *a.PoolCostMonth != 400 {
		t.Fatalf("Costed=%v PoolCostMonth=%v, want true / 400 (3×100 + 2×50)", a.Costed, a.PoolCostMonth)
	}
	if len(a.UnpricedComponents) != 0 {
		t.Errorf("UnpricedComponents = %v, want none", a.UnpricedComponents)
	}
	// Rendered in the pool's own order, not map order.
	var names []string
	for _, c := range a.Components {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "query-frontend,querier" {
		t.Errorf("components = %v, want the pool's declared order", names)
	}
	var costSum float64
	for _, ts := range a.Tenants {
		costSum += *ts.Cost
		if ts.EquivalentReplicas != nil {
			t.Errorf("%s has EquivalentReplicas %v: a multi-component pool has no single replica count", ts.Tenant, *ts.EquivalentReplicas)
		}
	}
	if !approx(costSum, 400) {
		t.Errorf("tenant costs sum to %v, want 400", costSum)
	}
}

// A partial list prices what it lists — the pool is costed, and says what
// it left out (ADR 0003 decision 3: never invent a coefficient).
func TestAllocate_PartialComponentListPricesWhatItLists(t *testing.T) {
	a := Allocate(queryPathMeasurement(), queryPathInv(map[string]ComponentInventory{
		"querier": {Replicas: intp(3), CostPerReplicaMonth: floatp(100)},
		// replicas but no price: not priced, and not a price of 0
		"query-frontend": {Replicas: intp(2)},
	}), nil)
	if !a.Costed || *a.PoolCostMonth != 300 {
		t.Fatalf("Costed=%v PoolCostMonth=%v, want true / 300 (querier only)", a.Costed, a.PoolCostMonth)
	}
	if got := strings.Join(a.UnpricedComponents, ","); got != "query-frontend" {
		t.Errorf("UnpricedComponents = %q, want query-frontend (replicas but no price)", got)
	}
	if !strings.Contains(strings.Join(a.Notes, "\n"), "lower bounds") {
		t.Errorf("Notes = %q, want the partial pricing called a lower bound", a.Notes)
	}
	for _, ts := range a.Tenants {
		if ts.Share == nil {
			t.Errorf("%s: shares must not depend on how much of the pool is priced", ts.Tenant)
		}
	}
}

func TestAllocate_AbsentComponentIsUnpricedNotFree(t *testing.T) {
	a := Allocate(queryPathMeasurement(), queryPathInv(map[string]ComponentInventory{
		"querier": {Replicas: intp(3), CostPerReplicaMonth: floatp(100)},
	}), nil)
	if got := strings.Join(a.UnpricedComponents, ","); got != "query-frontend" {
		t.Errorf("UnpricedComponents = %q, want the absent query-frontend named", got)
	}
	if *a.PoolCostMonth != 300 {
		t.Errorf("PoolCostMonth = %v, want 300: an absent component adds nothing, and is not priced at 0 either", *a.PoolCostMonth)
	}
}

func TestAllocate_ComponentsWithNothingPricedIsNotCosted(t *testing.T) {
	a := Allocate(queryPathMeasurement(), queryPathInv(map[string]ComponentInventory{
		"querier": {Replicas: intp(3)},
	}), nil)
	if a.Costed || a.PoolCostMonth != nil {
		t.Errorf("Costed=%v PoolCostMonth=%v, want not costed: no component has both a count and a price", a.Costed, a.PoolCostMonth)
	}
	for _, ts := range a.Tenants {
		if ts.Cost != nil {
			t.Errorf("%s: Cost = %v, want nil (not costed, never 0)", ts.Tenant, *ts.Cost)
		}
	}
}

func TestInventoryValidate_Components(t *testing.T) {
	tests := []struct {
		name string
		inv  Inventory
		want string
	}{
		{"both forms", Inventory{Currency: "EUR", Pools: map[PoolID]PoolInventory{PoolQueryPath: {
			Replicas: intp(3), Components: map[string]ComponentInventory{"querier": {Replicas: intp(1)}}}}}, "both"},
		{"unknown component", queryPathInv(map[string]ComponentInventory{"queirer": {Replicas: intp(1)}}), "unknown component"},
		{"components on a single-component pool", Inventory{Pools: map[PoolID]PoolInventory{PoolRulerCPU: {
			Components: map[string]ComponentInventory{"ruler": {Replicas: intp(1)}}}}}, "single component"},
		{"component price without currency", Inventory{Pools: map[PoolID]PoolInventory{PoolQueryPath: {
			Components: map[string]ComponentInventory{"querier": {CostPerReplicaMonth: floatp(1)}}}}}, "currency"},
		{"zero component replicas", queryPathInv(map[string]ComponentInventory{"querier": {Replicas: intp(0)}}), "replicas"},
		{"zero platform cost", Inventory{Currency: "EUR", PlatformCostMonth: floatp(0)}, "platform_cost_month"},
		{"platform cost without currency", Inventory{PlatformCostMonth: floatp(100)}, "currency"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.inv.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() = %v, want an error mentioning %q", err, tt.want)
			}
		})
	}
	ok := queryPathInv(map[string]ComponentInventory{"querier": {Replicas: intp(3), CostPerReplicaMonth: floatp(100)}})
	if err := ok.Validate(); err != nil {
		t.Errorf("valid component inventory rejected: %v", err)
	}
}
