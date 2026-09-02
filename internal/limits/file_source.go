package limits

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/KorhanOzturk90/obscost/internal/config"
)

type fileSource struct {
	path string
}

func newFileSource(cfg config.LimitsSource) *fileSource {
	return &fileSource{path: cfg.Path}
}

// overridesFile is a runtime-overrides.yaml (spec §3's offline fallback):
// per-tenant overrides plus an optional shared defaults block.
type overridesFile struct {
	Defaults  map[string]any            `yaml:"defaults,omitempty"`
	Overrides map[string]map[string]any `yaml:"overrides"`
}

func (s *fileSource) Load(_ context.Context) (Provider, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read limits file %s: %w", s.path, err)
	}
	var doc overridesFile
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse limits file %s: %w", s.path, err)
	}

	tenants := make(map[string]Tenant, len(doc.Overrides))
	for tenant, raw := range doc.Overrides {
		tenants[tenant] = mergeTenant(doc.Defaults, raw)
	}
	return &mapProvider{tenants: tenants}, nil
}

// keyAliases maps promcost's canonical (underscore) key name to every
// spelling the loader must tolerate — spec §3 explicitly notes exact key
// names vary by Mimir version between CLI-flag style (hyphens) and YAML
// style (underscores).
var keyAliases = map[string][]string{
	"max_fetched_series_per_query":     {"max_fetched_series_per_query", "max-fetched-series-per-query"},
	"max_fetched_chunk_bytes_per_query": {"max_fetched_chunk_bytes_per_query", "max-fetched-chunk-bytes-per-query"},
	"ruler_max_rules_per_rule_group":   {"ruler_max_rules_per_rule_group", "ruler-max-rules-per-rule-group"},
	"ruler_max_rule_groups_per_tenant": {"ruler_max_rule_groups_per_tenant", "ruler-max-rule-groups-per-tenant"},
	"max_global_series_per_user":       {"max_global_series_per_user", "max-global-series-per-user"},
	"ingestion_rate":                   {"ingestion_rate", "ingestion-rate"},
}

var aliasToCanonical = buildAliasToCanonical()

func buildAliasToCanonical() map[string]string {
	m := map[string]string{}
	for canonical, aliases := range keyAliases {
		for _, a := range aliases {
			m[a] = canonical
		}
	}
	return m
}

// normalizeKeys rewrites every recognized alias key to its canonical form
// so overrides correctly shadow defaults even when they use different key
// spellings for the same limit.
func normalizeKeys(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		if canonical, ok := aliasToCanonical[k]; ok {
			out[canonical] = v
			continue
		}
		out[k] = v
	}
	return out
}

func mergeTenant(defaults, overrides map[string]any) Tenant {
	merged := normalizeKeys(defaults)
	for k, v := range normalizeKeys(overrides) {
		merged[k] = v
	}

	var t Tenant
	if v, ok := merged["max_fetched_series_per_query"]; ok {
		t.MaxFetchedSeriesPerQuery = toInt64(v)
	}
	if v, ok := merged["max_fetched_chunk_bytes_per_query"]; ok {
		t.MaxFetchedChunkBytesPerQuery = toInt64(v)
	}
	if v, ok := merged["ruler_max_rules_per_rule_group"]; ok {
		t.RulerMaxRulesPerRuleGroup = int(toInt64(v))
	}
	if v, ok := merged["ruler_max_rule_groups_per_tenant"]; ok {
		t.RulerMaxRuleGroupsPerTenant = int(toInt64(v))
	}
	if v, ok := merged["max_global_series_per_user"]; ok {
		t.MaxGlobalSeriesPerUser = toInt64(v)
	}
	if v, ok := merged["ingestion_rate"]; ok {
		t.IngestionRate = toFloat64(v)
	}
	return t
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	default:
		return 0
	}
}
