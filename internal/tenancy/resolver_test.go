package tenancy

import (
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/config"
)

func TestStaticResolverHit(t *testing.T) {
	r := NewResolver(config.TenancyConfig{
		Discovery: []config.DiscoverySource{
			{Source: "static", Map: map[string]string{"team-payments": "platform"}},
		},
	})
	tenant, ok := r.Resolve(Facts{Namespace: "team-payments"})
	if !ok || tenant != "platform" {
		t.Errorf("Resolve(team-payments) = %q, %v, want platform, true", tenant, ok)
	}
}

func TestStaticResolverMiss(t *testing.T) {
	r := NewResolver(config.TenancyConfig{
		Discovery: []config.DiscoverySource{
			{Source: "static", Map: map[string]string{"team-payments": "platform"}},
		},
	})
	_, ok := r.Resolve(Facts{Namespace: "unknown-team"})
	if ok {
		t.Error("Resolve(unknown-team) resolved, want unresolved")
	}
}

func TestUnimplementedSourcesFallThrough(t *testing.T) {
	r := NewResolver(config.TenancyConfig{
		Discovery: []config.DiscoverySource{
			{Source: "crd_annotation", Key: "obs.example.com/tenant"},
			{Source: "namespace", Transform: "s/^team-//"},
			{Source: "static", Map: map[string]string{"team-payments": "platform"}},
		},
	})
	tenant, ok := r.Resolve(Facts{Namespace: "team-payments"})
	if !ok || tenant != "platform" {
		t.Errorf("Resolve fell through to static source incorrectly: %q, %v", tenant, ok)
	}
}
