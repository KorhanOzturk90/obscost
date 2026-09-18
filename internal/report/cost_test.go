package report

import (
	"bytes"
	"os"
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
		DriverQuery: "q",
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
	rep, err := NewCost(FormatMD)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.Render(&buf, goldenCostResult()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	golden := "testdata/cost_report.golden.md"
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

func TestMoneyAndThousands(t *testing.T) {
	tests := map[float64]string{0: "0.00", 1496.875: "1,496.88", 999.999: "1,000.00", 1234567.5: "1,234,567.50"}
	for in, want := range tests {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
}
