package report

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
)

func goldenCostResult() CostResult {
	replicas := 8
	end := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	priced := cost.Allocate(cost.Measurement{
		Pool:        cost.IngesterMemory,
		Window:      7 * 24 * time.Hour,
		End:         end,
		Source:      "Mimir metrics (queried as tenant monitoring)",
		DriverQuery: `label_replace(x, "id", "$1", "pod", ".*-(rc|[0-9]+)$")`,
		Drivers:     map[string]float64{"analytics": 45015, "infra": 23229, "payments": 3615},
		RFQuery:     "rfq",
		// No replication factor: shares still render, absolute figures don't.
	}, cost.Inventory{
		Currency: "EUR",
		Pools: map[cost.PoolID]cost.PoolInventory{
			cost.PoolIngesterMemory: {Replicas: &replicas},
		},
	}, []string{"platform"})
	return CostResult{Pools: []cost.PoolAllocation{priced}, GeneratedAt: end}
}

func TestCostMD_Golden(t *testing.T) {
	checkCostGolden(t, goldenCostResult(), "testdata/cost_report.golden.md")
}

// goldenMultiPoolResult is all three pools with the uneven coverage the k3d
// rig really has: pool 1 measured for `monitoring`, pool 6 partly priced
// (query-frontend left out), pool 7 measured but unpriced, and `platform`
// with series but no queries. It renders the cross-pool section in its
// fullest form — fraction of platform cost, a lower-bound tenant, and the
// pools left out.
func goldenMultiPoolResult() CostResult {
	end := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	three := 3.0
	two, three3 := 2, 3
	price100, platform := 100.0, 1600.0
	inv := cost.Inventory{
		Currency:          "EUR",
		PlatformCostMonth: &platform,
		Pools: map[cost.PoolID]cost.PoolInventory{
			cost.PoolIngesterMemory: {Replicas: &three3, CostPerReplicaMonth: &price100},
			cost.PoolQueryPath: {Components: map[string]cost.ComponentInventory{
				"querier": {Replicas: &two, CostPerReplicaMonth: &price100},
			}},
		},
	}
	src := "Mimir metrics (queried as tenant monitoring)"
	r := cost.AllocateAll([]cost.Measurement{
		{
			Pool: cost.IngesterMemory, Window: 7 * 24 * time.Hour, End: end, Source: src,
			DriverQuery: "avg_over_time(...)", RFQuery: "rfq", ReplicationFactor: &three,
			Drivers: map[string]float64{"analytics": 45015, "infra": 23229, "payments": 3615, "platform": 315, "monitoring": 37929},
		},
		{
			Pool: cost.QueryPath, Window: 7 * 24 * time.Hour, End: end, Source: src,
			DriverQuery:  "sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[7d]))",
			DriverMetric: "cortex_query_fetched_chunk_bytes_total", DriverKind: "chunk bytes fetched", Unit: cost.UnitBytes,
			Unreplicated: true,
			Drivers:      map[string]float64{"analytics": 5.294e10, "infra": 4.754e10, "payments": 1.02e9, "monitoring": 6.03e7},
		},
		{
			Pool: cost.RulerCPU, Window: 7 * 24 * time.Hour, End: end, Source: src,
			DriverQuery: "sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[7d]))",
			DriverKind:  "rule-evaluation seconds", Unit: cost.UnitSeconds, Unreplicated: true,
			Assumptions: []cost.Assumption{{Name: "rule evaluation", Value: "remote — the ruler sends its rule queries to the query-frontend", Source: src}},
			Notes:       []string{"rules are evaluated remotely, so this time is mostly the ruler waiting on the query path"},
			Drivers:     map[string]float64{"analytics": 3578.4, "infra": 2815.3, "payments": 1649.7, "platform": 1494.0},
		},
	}, inv, []string{"analytics", "infra", "payments", "platform"})
	return CostResult{Pools: r.Pools, CrossPool: r.CrossPool, GeneratedAt: end}
}

func TestCostMD_MultiPoolGolden(t *testing.T) {
	checkCostGolden(t, goldenMultiPoolResult(), "testdata/cost_report_multipool.golden.md")
}

func checkCostGolden(t *testing.T, result CostResult, golden string) {
	t.Helper()
	rep, err := NewCost(FormatMD)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.Render(&buf, result); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file (run with UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("cost md report does not match golden file %s.\n--- got ---\n%s\n--- want ---\n%s", golden, buf.String(), want)
	}
}

func renderCostMD(t *testing.T, r CostResult) string {
	t.Helper()
	rep, err := NewCost(FormatMD)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buf.String()
}

// twoPools re-allocates the multi-pool fixture under a different inventory,
// so the same measurements can be shown priced, unpriced and partly priced.
func twoPools(inv cost.Inventory) CostResult {
	base := goldenMultiPoolResult()
	end := base.GeneratedAt
	src := "s"
	r := cost.AllocateAll([]cost.Measurement{
		{Pool: cost.IngesterMemory, Window: time.Hour, End: end, Source: src, Unreplicated: true,
			Drivers: map[string]float64{"analytics": 60, "infra": 40}},
		{Pool: cost.RulerCPU, Window: time.Hour, End: end, Source: src, Unreplicated: true, Unit: cost.UnitSeconds,
			Drivers: map[string]float64{"analytics": 10, "infra": 90}},
	}, inv, nil)
	return CostResult{Pools: r.Pools, CrossPool: r.CrossPool, GeneratedAt: end}
}

// ADR 0003 [A]: a cross-pool total always names its coverage or is not
// rendered. There are three renderings — nothing priced, priced with an
// unknown platform cost, priced with a known one — and none may show a
// percentage of "the total" without saying what the total covers.
func TestCostMD_CrossPoolNeverStatesABareBlendedPercentage(t *testing.T) {
	price := 100.0
	replicas := 1
	priced := map[cost.PoolID]cost.PoolInventory{cost.PoolIngesterMemory: {Replicas: &replicas, CostPerReplicaMonth: &price}}
	platform := 400.0

	tests := []struct {
		name     string
		inv      cost.Inventory
		want     []string
		wantNone []string
	}{
		{
			name:     "nothing priced: shares only, no total at all",
			inv:      cost.Inventory{},
			want:     []string{"No pool is priced, so there is no cross-pool total", "Left out: ingester memory not costed; ruler CPU not costed"},
			wantNone: []string{"Share of priced cost", "of platform cost", "of the cost of the priced pools"},
		},
		{
			name: "priced, platform cost unknown: the fraction is not invented",
			inv:  cost.Inventory{Currency: "EUR", Pools: priced},
			want: []string{
				"**`analytics` is 60.0% of the cost of the priced pools** (ingester memory; ruler CPU not costed)",
				"unknown until `inventory.platform_cost_month` is set",
				"| Share of priced cost |",
			},
			wantNone: []string{"of platform cost we can price"},
		},
		{
			name: "priced, platform cost known: the fraction is computed",
			inv:  cost.Inventory{Currency: "EUR", PlatformCostMonth: &platform, Pools: priced},
			want: []string{"**`analytics` is 60.0% of the 25.0% of platform cost we can price** (ingester memory; ruler CPU not costed)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderCostMD(t, twoPools(tt.inv))
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, w := range tt.wantNone {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in:\n%s", w, out)
				}
			}
		})
	}
}

// A run that costed one pool has no cross-pool section: a "total" over one
// pool is that pool's own table again.
func TestCostMD_SinglePoolHasNoCrossPoolSection(t *testing.T) {
	out := renderCostMD(t, goldenCostResult())
	if strings.Contains(out, "Cost across pools") || strings.Contains(out, "Not modelled yet") {
		t.Errorf("a single-pool report grew a cross-pool section:\n%s", out)
	}
}

func TestCostJSON_CarriesTheCrossPoolCoverage(t *testing.T) {
	rep, err := NewCost(FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.Render(&buf, goldenMultiPoolResult()); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		CrossPool struct {
			Priced         []string `json:"priced_pools"`
			Unpriced       []string `json:"unpriced_pools"`
			PricedFraction float64  `json:"priced_fraction"`
			NotModelled    []string `json:"not_modelled"`
		} `json:"cross_pool"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("not one JSON document: %v", err)
	}
	if strings.Join(doc.CrossPool.Priced, ",") != "ingester_memory,query_path" || strings.Join(doc.CrossPool.Unpriced, ",") != "ruler_cpu" {
		t.Errorf("cross_pool = %+v", doc.CrossPool)
	}
	// 3 ingesters × 100 + 2 queriers × 100 = 500 of 1600.
	if got := doc.CrossPool.PricedFraction; got < 0.31249 || got > 0.31251 {
		t.Errorf("priced_fraction = %v, want 500/1600 = 0.3125", got)
	}
	if len(doc.CrossPool.NotModelled) == 0 {
		t.Error("not_modelled is empty: the JSON must name what the total leaves out too")
	}
}

func TestHumanUnits(t *testing.T) {
	bytesTests := map[float64]string{0: "0 B", 900: "900 B", 1024: "1.0 KiB", 52940813: "50.5 MiB", 5.3e10: "49.4 GiB"}
	for in, want := range bytesTests {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%v) = %q, want %q", in, got, want)
		}
	}
	secondsTests := map[float64]string{0: "0.0", 3.2: "3.2", 15.24: "15.2", 357.85: "357.9", 1234.96: "1,235.0", 0.04: "0.0"}
	for in, want := range secondsTests {
		if got := seconds(in); got != want {
			t.Errorf("seconds(%v) = %q, want %q", in, got, want)
		}
	}
	if counted(1, "ruler") != "1 ruler" || counted(3, "ingester") != "3 ingesters" {
		t.Errorf("counted: %q, %q", counted(1, "ruler"), counted(3, "ingester"))
	}
}

func TestMoneyAndThousands(t *testing.T) {
	tests := map[float64]string{0: "0.00", 1496.875: "1,496.88", 999.999: "1,000.00", 1234567.5: "1,234,567.50"}
	for in, want := range tests {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
}
