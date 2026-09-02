package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fullExampleYAML = `
backend:
  type: mimir
  url: https://mimir.internal/prometheus
  auth:
    bearer_token_env: PROMCOST_TOKEN
  timeout: 10s
  max_concurrent_queries: 4

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

limits:
  sources:
    - type: user_limits_endpoint
    - type: runtime_config_endpoint
      url: https://mimir.internal/runtime_config
    - type: configmap
      name: mimir-runtime
      key: runtime.yaml
    - type: file
      path: ./runtime-overrides.yaml

cost_model:
  currency: EUR
  eur_per_million_active_series_month: 85
  eur_per_billion_processed_samples: 0.40
  eur_per_billion_fetched_store_samples: 0.15
  store_after: 12h
  bytes_per_sample: 1.5

checks:
  disable: []
  thresholds:
    subquery_steps_warn: 500
    subquery_steps_error: 2000
    recording_range_warn: 24h
    limit_headroom_warn_pct: 60
    limit_headroom_error_pct: 90
    output_cardinality_warn: 10000
    presence_window: 1h

pint:
  enabled: true
  binary: pint
  config_template: ./pint.tpl.hcl
`

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promcost.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadFullSpecExample(t *testing.T) {
	path := writeTemp(t, fullExampleYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Backend.Type != "mimir" {
		t.Errorf("Backend.Type = %q, want mimir", cfg.Backend.Type)
	}
	if got, want := cfg.Checks.Thresholds.RecordingRangeWarn.Duration(), 24*time.Hour; got != want {
		t.Errorf("RecordingRangeWarn = %v, want %v", got, want)
	}
	if got, want := cfg.Backend.Timeout.Duration(), 10*time.Second; got != want {
		t.Errorf("Backend.Timeout = %v, want %v", got, want)
	}
	if len(cfg.Tenancy.Discovery) != 3 {
		t.Errorf("len(Tenancy.Discovery) = %d, want 3", len(cfg.Tenancy.Discovery))
	}
	if len(cfg.Limits.Sources) != 4 {
		t.Errorf("len(Limits.Sources) = %d, want 4", len(cfg.Limits.Sources))
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeTemp(t, "checks:\n  disable: []\n  bogus_field: true\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load with unknown field: expected error, got nil")
	}
}

func TestLoadAppliesDefaultsOnOmission(t *testing.T) {
	path := writeTemp(t, "checks:\n  disable: [PC-S04]\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Checks.Thresholds.SubqueryStepsWarn, 500; got != want {
		t.Errorf("SubqueryStepsWarn = %d, want default %d", got, want)
	}
	if got, want := cfg.Checks.Thresholds.RecordingRangeWarn.Duration(), 24*time.Hour; got != want {
		t.Errorf("RecordingRangeWarn = %v, want default %v", got, want)
	}
	if len(cfg.Checks.Disable) != 1 || cfg.Checks.Disable[0] != "PC-S04" {
		t.Errorf("Checks.Disable = %v, want [PC-S04]", cfg.Checks.Disable)
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
	path := writeTemp(t, "checks:\n  thresholds:\n    recording_range_warn: not-a-duration\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load with invalid duration: expected error, got nil")
	}
}
