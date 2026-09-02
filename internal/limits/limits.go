// Package limits resolves per-tenant Mimir ruler/query limits (spec §3
// limits block). Only the offline "file" source is implemented this
// milestone; the resolved-endpoint sources (user_limits_endpoint,
// runtime_config_endpoint, configmap) are recognized but not yet loadable.
package limits

import "context"

// Tenant holds the subset of Mimir per-tenant limits promcost's static and
// live checks read (spec §3's "keys read per tenant" comment).
type Tenant struct {
	MaxFetchedSeriesPerQuery     int64
	MaxFetchedChunkBytesPerQuery int64
	RulerMaxRulesPerRuleGroup    int
	RulerMaxRuleGroupsPerTenant  int
	MaxGlobalSeriesPerUser       int64
	IngestionRate                float64
}

// Provider looks up resolved limits per tenant.
type Provider interface {
	Limits(tenant string) (Tenant, bool)
}

// Source loads a Provider from one configured limits.sources entry.
type Source interface {
	Load(ctx context.Context) (Provider, error)
}

type mapProvider struct {
	tenants map[string]Tenant
}

func (p *mapProvider) Limits(tenant string) (Tenant, bool) {
	t, ok := p.tenants[tenant]
	return t, ok
}
