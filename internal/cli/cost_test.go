package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// costFixture is an RF=3 Mimir: raw per-tenant active series are three
// times the real counts from ADR 0003's worked example.
func costFixture(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "monitoring" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q, "cortex_ingester_active_series"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"user":"analytics"},"value":[1757800000,"45015"]},
				{"metric":{"user":"infra"},"value":[1757800000,"23229"]},
				{"metric":{"user":"payments"},"value":[1757800000,"3615"]},
				{"metric":{"user":"platform"},"value":[1757800000,"315"]}]}}`))
		case strings.Contains(q, "cortex_distributor_replication_factor"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{},"value":[1757800000,"3"]}]}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func writeCostConfig(t *testing.T, backendURL, inventory string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promcost.yaml")
	content := "backend:\n  url: " + backendURL + "\n" + inventory
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const pricedInventory = `inventory:
  currency: EUR
  pools:
    ingester_memory:
      replicas: 8
      cost_per_replica_month: 300
`

func TestCost_PricedInventory(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, pricedInventory),
		"--tenant", "analytics,ghost",
		"--since", "24h",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"`analytics` uses 62.4% of ingester memory",
		"the equivalent of 5.0 of your 8 ingesters",
		"1,496.88 EUR/month",
		"| analytics | 15,005 |", // divided by RF 3
		"| ghost | not measured |",
		"| **Total** | **24,058** | **100%** | **8** | **2,400.00** |",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestCost_NoInventoryIsSharesOnly(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, ""),
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "**Not costed**") {
		t.Errorf("expected the pool to be reported as not costed:\n%s", stdout)
	}
	if strings.Contains(stdout, "EUR") || strings.Contains(stdout, "0.00") {
		t.Errorf("an unpriced pool rendered a currency figure:\n%s", stdout)
	}
}

func TestCost_JSONIsOneDocument(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, pricedInventory),
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stderr=%s", code, stderr)
	}
	var doc struct {
		Pools []struct {
			Costed  bool `json:"costed"`
			Tenants []struct {
				Tenant string   `json:"tenant"`
				Share  *float64 `json:"share"`
			} `json:"tenants"`
		} `json:"pools"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if len(doc.Pools) != 1 || !doc.Pools[0].Costed || len(doc.Pools[0].Tenants) != 4 {
		t.Errorf("unexpected document: %+v", doc)
	}
}

func TestCost_UsageErrors(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no metrics tenant", []string{"--config", writeCostConfig(t, srv.URL, "")}, "--metrics-tenant"},
		{"no backend url", []string{"--metrics-tenant", "monitoring"}, "backend.url"},
		{"bad format", []string{"--metrics-tenant", "monitoring", "--format", "html"}, "unknown cost report format"},
		{"price without currency", []string{"--metrics-tenant", "monitoring", "--config",
			writeCostConfig(t, srv.URL, "inventory:\n  pools:\n    ingester_memory:\n      cost_per_replica_month: 1\n")}, "currency"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := run(append([]string{"cost"}, tt.args...)...)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr should mention %q, got:\n%s", tt.want, stderr)
			}
		})
	}
}
