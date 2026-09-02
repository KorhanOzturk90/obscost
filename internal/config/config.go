// Package config loads and represents promcost.yaml (spec §3). This
// milestone parses the full schema (so a spec-example config always loads
// without error) but only the static-tier-relevant subset is consumed:
// checks.disable, checks.thresholds, tenancy, and limits.sources (file type
// only). backend, cost_model, and pint are parsed and carried for later
// milestones.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type BackendAuth struct {
	BearerTokenEnv string `yaml:"bearer_token_env,omitempty"`
}

type BackendConfig struct {
	Type                 string      `yaml:"type,omitempty"`
	URL                  string      `yaml:"url,omitempty"`
	Auth                 BackendAuth `yaml:"auth,omitempty"`
	Timeout              Duration    `yaml:"timeout,omitempty"`
	MaxConcurrentQueries int         `yaml:"max_concurrent_queries,omitempty"`
}

// DiscoverySource is one entry in tenancy.discovery. Only Source=="static"
// is resolvable this milestone (see internal/tenancy); the other types
// parse cleanly so a full spec-example config loads.
type DiscoverySource struct {
	Source    string            `yaml:"source"`
	Key       string            `yaml:"key,omitempty"`
	Transform string            `yaml:"transform,omitempty"`
	Map       map[string]string `yaml:"map,omitempty"`
}

type TenancyConfig struct {
	Header    string            `yaml:"header,omitempty"`
	Discovery []DiscoverySource `yaml:"discovery,omitempty"`
	// Unmapped is "error" | "skip" | "tenant:<name>".
	Unmapped string `yaml:"unmapped,omitempty"`
}

// LimitsSource is one entry in limits.sources. Only Type=="file" is
// implemented this milestone (internal/limits); other types parse cleanly
// but return limits.ErrUnsupportedSource if selected.
type LimitsSource struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url,omitempty"`
	Name string `yaml:"name,omitempty"`
	Key  string `yaml:"key,omitempty"`
	Path string `yaml:"path,omitempty"`
}

type LimitsConfig struct {
	Sources []LimitsSource `yaml:"sources,omitempty"`
}

type CostModelConfig struct {
	Currency                         string   `yaml:"currency,omitempty"`
	EURPerMillionActiveSeriesMonth   float64  `yaml:"eur_per_million_active_series_month,omitempty"`
	EURPerBillionProcessedSamples    float64  `yaml:"eur_per_billion_processed_samples,omitempty"`
	EURPerBillionFetchedStoreSamples float64  `yaml:"eur_per_billion_fetched_store_samples,omitempty"`
	StoreAfter                       Duration `yaml:"store_after,omitempty"`
	BytesPerSample                   float64  `yaml:"bytes_per_sample,omitempty"`
}

// ThresholdsConfig covers checks.thresholds. Only the static-relevant
// fields (SubquerySteps*, RecordingRangeWarn) and HighCardinalityLabels
// (PC-S04's wordlist — an addition beyond spec §3's shown example, since
// S04's own prose calls the wordlist "configurable") are consumed this
// milestone; the rest are parsed for forward compatibility with the live
// tier.
type ThresholdsConfig struct {
	SubqueryStepsWarn     int      `yaml:"subquery_steps_warn,omitempty"`
	SubqueryStepsError    int      `yaml:"subquery_steps_error,omitempty"`
	RecordingRangeWarn    Duration `yaml:"recording_range_warn,omitempty"`
	LimitHeadroomWarnPct  float64  `yaml:"limit_headroom_warn_pct,omitempty"`
	LimitHeadroomErrorPct float64  `yaml:"limit_headroom_error_pct,omitempty"`
	OutputCardinalityWarn int      `yaml:"output_cardinality_warn,omitempty"`
	PresenceWindow        Duration `yaml:"presence_window,omitempty"`
	HighCardinalityLabels []string `yaml:"high_cardinality_labels,omitempty"`
}

type ChecksConfig struct {
	Disable    []string         `yaml:"disable,omitempty"`
	Thresholds ThresholdsConfig `yaml:"thresholds,omitempty"`
}

type PintConfig struct {
	Enabled        bool   `yaml:"enabled,omitempty"`
	Binary         string `yaml:"binary,omitempty"`
	ConfigTemplate string `yaml:"config_template,omitempty"`
}

type Config struct {
	Backend   BackendConfig   `yaml:"backend,omitempty"`
	Tenancy   TenancyConfig   `yaml:"tenancy,omitempty"`
	Limits    LimitsConfig    `yaml:"limits,omitempty"`
	CostModel CostModelConfig `yaml:"cost_model,omitempty"`
	Checks    ChecksConfig    `yaml:"checks,omitempty"`
	Pint      PintConfig      `yaml:"pint,omitempty"`
}

// Load reads promcost.yaml from path, starting from Default() so any
// section or field the file omits keeps its default value. An empty path
// returns Default() unchanged (no config file supplied).
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
