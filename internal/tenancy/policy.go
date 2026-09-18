package tenancy

import (
	"errors"
	"fmt"
	"strings"
)

// UnmappedPolicy implements spec §3's tenancy.unmapped setting: error | skip
// | tenant:<name>.
//
// "error" makes an unresolved rule a load error: a rule with no tenant
// can't be attributed to anyone, so it fails the load rather than being
// silently dropped or reported under an empty tenant.
type UnmappedPolicy struct {
	Mode           string // "error" | "skip" | "tenant"
	FallbackTenant string
}

func ParseUnmappedPolicy(raw string) (UnmappedPolicy, error) {
	switch {
	case raw == "" || raw == "error":
		return UnmappedPolicy{Mode: "error"}, nil
	case raw == "skip":
		return UnmappedPolicy{Mode: "skip"}, nil
	case strings.HasPrefix(raw, "tenant:"):
		name := strings.TrimPrefix(raw, "tenant:")
		if name == "" {
			return UnmappedPolicy{}, fmt.Errorf("empty tenant name in unmapped policy %q", raw)
		}
		return UnmappedPolicy{Mode: "tenant", FallbackTenant: name}, nil
	default:
		return UnmappedPolicy{}, fmt.Errorf("unknown tenancy.unmapped policy %q, want error|skip|tenant:<name>", raw)
	}
}

// ErrUnmapped is returned by Apply under the "error" policy for a rule
// that did not resolve to a tenant.
var ErrUnmapped = errors.New("no tenant mapping (tenancy.unmapped: error)")

// Apply decides the tenant to use and whether to keep a rule that resolved
// (or failed to resolve) as described. "skip" drops it silently; "error"
// drops it and returns ErrUnmapped for the caller to report as a load
// error.
func (p UnmappedPolicy) Apply(tenant string, resolved bool) (finalTenant string, keep bool, err error) {
	if resolved {
		return tenant, true, nil
	}
	switch p.Mode {
	case "skip":
		return "", false, nil
	case "tenant":
		return p.FallbackTenant, true, nil
	default: // "error"
		return "", false, ErrUnmapped
	}
}
