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

// costFixture is an RF=3, remotely-evaluating Mimir. Raw per-tenant active
// series are three times the real counts from ADR 0003's worked example;
// the query-path and ruler figures are per-tenant counters in the shape the
// k3d rig reports them. `platform` has series and rules but ran no queries,
// so it is measured for pools 1 and 7 and unmeasured for pool 6 — exactly
// the uneven coverage ADR 0003 decision 5 is about.
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
		case strings.Contains(q, "cortex_query_fetched_chunk_bytes_total["):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"user":"analytics"},"value":[1757800000,"52940813"]},
				{"metric":{"user":"infra"},"value":[1757800000,"47543702"]},
				{"metric":{"user":"payments"},"value":[1757800000,"1020188"]}]}}`))
		case strings.Contains(q, "cortex_prometheus_rule_evaluation_duration_seconds_sum["):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"user":"analytics"},"value":[1757800000,"357.8"]},
				{"metric":{"user":"infra"},"value":[1757800000,"281.5"]},
				{"metric":{"user":"payments"},"value":[1757800000,"165"]},
				{"metric":{"user":"platform"},"value":[1757800000,"149.4"]}]}}`))
		case strings.Contains(q, "cortex_ruler_query_seconds_total["):
			// Remote evaluation: the ruler records no query time of its own.
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
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
		"--pool", "ingester_memory",
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
	if len(doc.Pools) != 3 || !doc.Pools[0].Costed || len(doc.Pools[0].Tenants) != 4 {
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

// The whole point of pools 6 and 7: one command, three pools, and a total
// that names what it covers.
const threePoolInventory = `inventory:
  currency: EUR
  platform_cost_month: 1600
  pools:
    ingester_memory:
      replicas: 3
      cost_per_replica_month: 100
    query_path:
      components:
        querier:
          replicas: 1
          cost_per_replica_month: 100
`

func TestCost_ThreePoolsAndACoverageQualifiedTotal(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, threePoolInventory),
		"--tenant", "analytics,infra,payments,platform",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"## Ingester memory", "## Query path", "## Ruler CPU",
		"Total over the last 1h",
		"| Tenant | Chunk bytes fetched |",
		"| analytics | 50.5 MiB |",
		"| Tenant | Rule-evaluation seconds |",
		"| analytics | 357.8 |",
		"| platform | not measured | not measured |", // pool 6: series and rules, but no queries
		"## Cost across pools",
		// coverage: 2 of 3 pools priced, one of them only partly; the ruler is
		// measured but unpriced
		"of the 25.0% of platform cost we can price** (ingester memory, query path (without query-frontend); ruler CPU not costed)",
		"Not modelled yet: ingester write path, object storage, store-gateway, compactor.",
		// platform has no pool-6 share, so its total is a lower bound
		"≥",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	// The remote-evaluation caveat and its mode line ride along on pool 7.
	if !strings.Contains(stdout, "rules are evaluated remotely") {
		t.Errorf("pool 7 lacks the remote-evaluation caveat:\n%s", stdout)
	}
}

// Nothing priced: per-pool shares stay, and there is no blended figure.
func TestCost_NothingPricedHasNoBlendedTotal(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, ""),
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "No pool is priced, so there is no cross-pool total") {
		t.Errorf("expected the no-total statement:\n%s", stdout)
	}
	if strings.Contains(stdout, "Share of priced cost") || strings.Contains(stdout, "of platform cost") {
		t.Errorf("a blended cross-pool figure rendered with nothing priced:\n%s", stdout)
	}
	if !strings.Contains(stdout, "analytics") || !strings.Contains(stdout, "%") {
		t.Errorf("per-pool shares vanished:\n%s", stdout)
	}
}

func TestCost_PoolFlagSelectsAndOrders(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, ""),
		"--pool", "ruler_cpu,ingester_memory", // typed out of order
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stderr=%s", code, stderr)
	}
	im, rc := strings.Index(stdout, "## Ingester memory"), strings.Index(stdout, "## Ruler CPU")
	if im < 0 || rc < 0 || im > rc {
		t.Errorf("want ingester memory then ruler CPU whatever the flag order (im=%d rc=%d):\n%s", im, rc, stdout)
	}
	if strings.Contains(stdout, "## Query path") {
		t.Errorf("query path rendered although --pool did not ask for it:\n%s", stdout)
	}
}

func TestCost_SinglePoolKeepsTheOriginalShape(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, _, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, pricedInventory),
		"--pool", "ingester_memory",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(stdout, "Cost across pools") {
		t.Errorf("a one-pool run grew a cross-pool section:\n%s", stdout)
	}
}

func TestCost_JSONCarriesCrossPoolCoverage(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, threePoolInventory),
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stderr=%s", code, stderr)
	}
	var doc struct {
		Pools []struct {
			Pool struct {
				ID string `json:"id"`
			} `json:"pool"`
			DriverKind string `json:"driver_kind"`
			Unit       string `json:"unit"`
		} `json:"pools"`
		CrossPool struct {
			Priced         []string `json:"priced_pools"`
			Unpriced       []string `json:"unpriced_pools"`
			PricedFraction *float64 `json:"priced_fraction"`
			Tenants        []struct {
				Tenant        string   `json:"tenant"`
				NotMeasuredIn []string `json:"not_measured_in"`
			} `json:"tenants"`
		} `json:"cross_pool"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if got := strings.Join(doc.CrossPool.Priced, ","); got != "ingester_memory,query_path" {
		t.Errorf("priced_pools = %s", got)
	}
	if got := strings.Join(doc.CrossPool.Unpriced, ","); got != "ruler_cpu" {
		t.Errorf("unpriced_pools = %s", got)
	}
	if doc.CrossPool.PricedFraction == nil || *doc.CrossPool.PricedFraction != 0.25 {
		t.Errorf("priced_fraction = %v, want 0.25", doc.CrossPool.PricedFraction)
	}
	if doc.Pools[1].Pool.ID != "query_path" || doc.Pools[1].DriverKind != "chunk bytes fetched" || doc.Pools[1].Unit != "bytes" {
		t.Errorf("pool 2 = %+v, want query_path measured in bytes", doc.Pools[1])
	}
}

func TestCost_PoolUsageErrors(t *testing.T) {
	srv := costFixture(t)
	defer srv.Close()
	cfg := writeCostConfig(t, srv.URL, "")
	for name, tt := range map[string]struct {
		args []string
		want string
	}{
		"unknown pool":    {[]string{"--pool", "ingesters"}, "unknown --pool"},
		"only separators": {[]string{"--pool", ","}, "names no pool"},
		"components on ruler": {[]string{"--config", writeCostConfig(t, srv.URL,
			"inventory:\n  currency: EUR\n  pools:\n    ruler_cpu:\n      components:\n        ruler:\n          replicas: 1\n")}, "single component"},
	} {
		t.Run(name, func(t *testing.T) {
			args := append([]string{"cost", "--metrics-tenant", "monitoring", "--config", cfg}, tt.args...)
			_, stderr, code := run(args...)
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit=%d stderr=%q, want exit 1 mentioning %q", code, stderr, tt.want)
			}
		})
	}
}
