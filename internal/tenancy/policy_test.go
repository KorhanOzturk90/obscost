package tenancy

import "testing"

func TestParseUnmappedPolicy(t *testing.T) {
	cases := []struct {
		raw            string
		wantMode       string
		wantFallback   string
		wantErr        bool
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
	if tenant, keep := errPolicy.Apply("", false); keep != true || tenant != "" {
		t.Errorf("error policy on unresolved: tenant=%q keep=%v, want \"\", true", tenant, keep)
	}

	skipPolicy, _ := ParseUnmappedPolicy("skip")
	if _, keep := skipPolicy.Apply("", false); keep != false {
		t.Errorf("skip policy on unresolved: keep=%v, want false", keep)
	}

	tenantPolicy, _ := ParseUnmappedPolicy("tenant:fallback")
	if tenant, keep := tenantPolicy.Apply("", false); keep != true || tenant != "fallback" {
		t.Errorf("tenant policy on unresolved: tenant=%q keep=%v, want \"fallback\", true", tenant, keep)
	}

	// A resolved fact always keeps its resolved tenant regardless of policy.
	if tenant, keep := skipPolicy.Apply("platform", true); keep != true || tenant != "platform" {
		t.Errorf("resolved fact under skip policy: tenant=%q keep=%v, want \"platform\", true", tenant, keep)
	}
}
