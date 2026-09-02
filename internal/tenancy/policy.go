package tenancy

import (
	"fmt"
	"strings"
)

// UnmappedPolicy implements spec §3's tenancy.unmapped setting: error | skip
// | tenant:<name>.
//
// "error" does not itself fail a load: the rule is kept with an empty
// tenant, and PC-S06 (tenant-resolution) is the check that turns that into
// an error-severity Finding subject to --fail-on. This keeps "a rule failed
// to resolve" a reportable, remediation-carrying Finding like everything
// else, rather than a second, differently-shaped failure mode.
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

// Apply decides the tenant to use and whether to keep a rule that resolved
// (or failed to resolve) as described. Only "skip" drops a rule at load
// time; "error" keeps it with an empty tenant for PC-S06 to report.
func (p UnmappedPolicy) Apply(tenant string, resolved bool) (finalTenant string, keep bool) {
	if resolved {
		return tenant, true
	}
	switch p.Mode {
	case "skip":
		return "", false
	case "tenant":
		return p.FallbackTenant, true
	default: // "error"
		return "", true
	}
}
