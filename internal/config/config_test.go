package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fullExampleYAML = `
backend:
  url: https://mimir.internal/prometheus
  auth:
    bearer_token_env: PROMCOST_TOKEN
  timeout: 10s

tenancy:
  header: X-Scope-OrgID
  discovery:
    - source: crd_annotation
      key: obs.example.com/tenant
    - source: namespace
      transform: "s/^team-//"
    - source: static
      map:
        monitoring: platform
        kube-system: platform
  unmapped: error
`

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promcost.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadFullExample(t *testing.T) {
	path := writeTemp(t, fullExampleYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Backend.URL != "https://mimir.internal/prometheus" {
		t.Errorf("Backend.URL = %q", cfg.Backend.URL)
	}
	if got, want := cfg.Backend.Timeout.Duration(), 10*time.Second; got != want {
		t.Errorf("Backend.Timeout = %v, want %v", got, want)
	}
	if len(cfg.Tenancy.Discovery) != 3 {
		t.Errorf("len(Tenancy.Discovery) = %d, want 3", len(cfg.Tenancy.Discovery))
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeTemp(t, "tenancy:\n  bogus_field: true\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load with unknown field: expected error, got nil")
	}
}

func TestLoadRejectsRemovedSectionsByName(t *testing.T) {
	cases := map[string]string{
		"checks":                         "checks:\n  disable: [PC-S04]\n",
		"pint":                           "pint:\n  enabled: true\n",
		"limits":                         "limits:\n  sources: []\n",
		"cost_model":                     "cost_model:\n  currency: EUR\n",
		"backend.type":                   "backend:\n  type: mimir\n",
		"backend.max_concurrent_queries": "backend:\n  url: http://x\n  max_concurrent_queries: 4\n",
	}
	for name, yml := range cases {
		_, err := Load(writeTemp(t, yml))
		if err == nil || !strings.Contains(err.Error(), `"`+name+`"`) {
			t.Errorf("%s: err = %v, want an error naming %q", name, err, name)
		}
	}
}

func TestLoadAppliesDefaultsOnOmission(t *testing.T) {
	path := writeTemp(t, "backend:\n  url: http://x\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Tenancy.Header, "X-Scope-OrgID"; got != want {
		t.Errorf("Tenancy.Header = %q, want default %q", got, want)
	}
	if got, want := cfg.Tenancy.Unmapped, "error"; got != want {
		t.Errorf("Tenancy.Unmapped = %q, want default %q", got, want)
	}
}

func TestLoadEmptyPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if cfg.Tenancy.Unmapped != "error" {
		t.Errorf("Tenancy.Unmapped = %q, want error", cfg.Tenancy.Unmapped)
	}
}

func TestDurationRejectsInvalid(t *testing.T) {
	path := writeTemp(t, "backend:\n  timeout: not-a-duration\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load with invalid duration: expected error, got nil")
	}
}

func TestLoadInventory_PartialIsNotZero(t *testing.T) {
	path := writeTemp(t, `inventory:
  currency: EUR
  pools:
    ingester_memory:
      replicas: 8
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pool, ok := cfg.Inventory.Pools["ingester_memory"]
	if !ok {
		t.Fatal("ingester_memory pool missing from inventory")
	}
	if pool.Replicas == nil || *pool.Replicas != 8 {
		t.Errorf("Replicas = %v, want 8", pool.Replicas)
	}
	// An omitted price must stay distinguishable from a price of 0: the
	// former means "not costed", the latter would claim the pool is free.
	if pool.CostPerReplicaMonth != nil {
		t.Errorf("CostPerReplicaMonth = %v, want nil (not supplied)", *pool.CostPerReplicaMonth)
	}
	if cfg.Inventory.ReplicationFactor != nil {
		t.Errorf("ReplicationFactor = %v, want nil (not supplied)", *cfg.Inventory.ReplicationFactor)
	}
}

func TestLoadInventory_RejectsUnknownPoolField(t *testing.T) {
	path := writeTemp(t, "inventory:\n  pools:\n    ingester_memory:\n      replica: 8\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load with misspelt pool field: expected error, got nil")
	}
}
