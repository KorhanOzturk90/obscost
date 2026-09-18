package tenancy

import (
	"errors"
	"testing"
)

func TestParseUnmappedPolicy(t *testing.T) {
	cases := []struct {
		raw          string
		wantMode     string
		wantFallback string
		wantErr      bool
	}{
		{raw: "", wantMode: "error"},
		{raw: "error", wantMode: "error"},
		{raw: "skip", wantMode: "skip"},
		{raw: "tenant:platform", wantMode: "tenant", wantFallback: "platform"},
		{raw: "tenant:", wantErr: true},
		{raw: "bogus", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseUnmappedPolicy(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseUnmappedPolicy(%q): expected error, got nil", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseUnmappedPolicy(%q): %v", c.raw, err)
			continue
		}
		if got.Mode != c.wantMode || got.FallbackTenant != c.wantFallback {
			t.Errorf("ParseUnmappedPolicy(%q) = %+v, want mode=%q fallback=%q", c.raw, got, c.wantMode, c.wantFallback)
		}
	}
}

func TestUnmappedPolicyApply(t *testing.T) {
	errPolicy, _ := ParseUnmappedPolicy("error")
	if _, keep, err := errPolicy.Apply("", false); keep || !errors.Is(err, ErrUnmapped) {
		t.Errorf("error policy on unresolved: keep=%v err=%v, want false, ErrUnmapped", keep, err)
	}

	skipPolicy, _ := ParseUnmappedPolicy("skip")
	if _, keep, err := skipPolicy.Apply("", false); keep || err != nil {
		t.Errorf("skip policy on unresolved: keep=%v err=%v, want false, nil", keep, err)
	}

	tenantPolicy, _ := ParseUnmappedPolicy("tenant:fallback")
	if tenant, keep, err := tenantPolicy.Apply("", false); !keep || tenant != "fallback" || err != nil {
		t.Errorf("tenant policy on unresolved: tenant=%q keep=%v err=%v, want \"fallback\", true, nil", tenant, keep, err)
	}

	// A resolved fact always keeps its resolved tenant regardless of policy.
	if tenant, keep, err := errPolicy.Apply("platform", true); !keep || tenant != "platform" || err != nil {
		t.Errorf("resolved fact under error policy: tenant=%q keep=%v err=%v, want \"platform\", true, nil", tenant, keep, err)
	}
}
