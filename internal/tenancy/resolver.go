// Package tenancy resolves a loaded rule's source into a tenant name (spec
// §3 tenancy block) — the piece pint structurally lacks. Only the "static"
// discovery source is implemented this milestone; "namespace" and
// "crd_annotation" entries parse cleanly (so a full spec-example config
// loads) but never match, since directory-mode loading has no annotations
// and the "namespace" transform pipeline isn't built yet.
package tenancy

import "github.com/KorhanOzturk90/obscost/internal/config"

// Facts carries whatever tenant-discovery signals a given Loader can
// supply. Directory loading only has a synthetic namespace (see
// internal/loader/dir); a future CRD loader would populate Annotations too.
type Facts struct {
	Namespace   string
	Annotations map[string]string
}

// Resolver maps Facts to a tenant name.
type Resolver interface {
	// Resolve returns the tenant for f. resolved is false if no discovery
	// source in the chain matched.
	Resolve(f Facts) (tenant string, resolved bool)
}

type chainResolver struct {
	sources []config.DiscoverySource
}

// NewResolver builds a Resolver from the tenancy config's discovery chain,
// evaluated in order — first match wins, per spec §3.
func NewResolver(cfg config.TenancyConfig) Resolver {
	return &chainResolver{sources: cfg.Discovery}
}

func (r *chainResolver) Resolve(f Facts) (string, bool) {
	for _, src := range r.sources {
		if src.Source == "static" {
			if tenant, ok := src.Map[f.Namespace]; ok {
				return tenant, true
			}
		}
	}
	return "", false
}
