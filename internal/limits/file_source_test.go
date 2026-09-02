package limits

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/config"
)

func writeLimitsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-overrides.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write limits file: %v", err)
	}
	return path
}

func TestFileSourceUnderscoreKeys(t *testing.T) {
	path := writeLimitsFile(t, `
overrides:
  team-a:
    ruler_max_rules_per_rule_group: 20
    max_fetched_series_per_query: 100000
`)
	p, err := (&fileSource{path: path}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tenant, ok := p.Limits("team-a")
	if !ok {
		t.Fatal("Limits(team-a) not found")
	}
	if tenant.RulerMaxRulesPerRuleGroup != 20 {
		t.Errorf("RulerMaxRulesPerRuleGroup = %d, want 20", tenant.RulerMaxRulesPerRuleGroup)
	}
	if tenant.MaxFetchedSeriesPerQuery != 100000 {
		t.Errorf("MaxFetchedSeriesPerQuery = %d, want 100000", tenant.MaxFetchedSeriesPerQuery)
	}
}

func TestFileSourceHyphenKeys(t *testing.T) {
	path := writeLimitsFile(t, `
overrides:
  team-b:
    ruler-max-rules-per-rule-group: 30
    max-fetched-series-per-query: 50000
`)
	p, err := (&fileSource{path: path}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tenant, ok := p.Limits("team-b")
	if !ok {
		t.Fatal("Limits(team-b) not found")
	}
	if tenant.RulerMaxRulesPerRuleGroup != 30 {
		t.Errorf("RulerMaxRulesPerRuleGroup = %d, want 30", tenant.RulerMaxRulesPerRuleGroup)
	}
	if tenant.MaxFetchedSeriesPerQuery != 50000 {
		t.Errorf("MaxFetchedSeriesPerQuery = %d, want 50000", tenant.MaxFetchedSeriesPerQuery)
	}
}

func TestFileSourceDefaultsMergeWithOverrides(t *testing.T) {
	path := writeLimitsFile(t, `
defaults:
  ruler_max_rules_per_rule_group: 15
  ruler_max_rule_groups_per_tenant: 100
overrides:
  team-c:
    ruler_max_rules_per_rule_group: 5
`)
	p, err := (&fileSource{path: path}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tenant, ok := p.Limits("team-c")
	if !ok {
		t.Fatal("Limits(team-c) not found")
	}
	if tenant.RulerMaxRulesPerRuleGroup != 5 {
		t.Errorf("RulerMaxRulesPerRuleGroup = %d, want override 5", tenant.RulerMaxRulesPerRuleGroup)
	}
	if tenant.RulerMaxRuleGroupsPerTenant != 100 {
		t.Errorf("RulerMaxRuleGroupsPerTenant = %d, want default 100", tenant.RulerMaxRuleGroupsPerTenant)
	}
}

func TestChainProviderUnsupportedSource(t *testing.T) {
	_, err := NewChainProvider(context.Background(), []config.LimitsSource{{Type: "user_limits_endpoint"}})
	if err == nil {
		t.Fatal("expected error for unsupported source type")
	}
}

func TestChainProviderEmptySources(t *testing.T) {
	p, err := NewChainProvider(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewChainProvider: %v", err)
	}
	if _, ok := p.Limits("anything"); ok {
		t.Error("expected no limits from an empty provider")
	}
}

func TestChainProviderFirstSuccessWins(t *testing.T) {
	path := writeLimitsFile(t, "overrides:\n  t:\n    ruler_max_rules_per_rule_group: 7\n")
	p, err := NewChainProvider(context.Background(), []config.LimitsSource{
		{Type: "runtime_config_endpoint", URL: "https://example.invalid"},
		{Type: "file", Path: path},
	})
	if err != nil {
		t.Fatalf("NewChainProvider: %v", err)
	}
	tenant, ok := p.Limits("t")
	if !ok || tenant.RulerMaxRulesPerRuleGroup != 7 {
		t.Errorf("Limits(t) = %+v, ok=%v, want RulerMaxRulesPerRuleGroup=7", tenant, ok)
	}
}
