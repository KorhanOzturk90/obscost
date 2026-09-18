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
