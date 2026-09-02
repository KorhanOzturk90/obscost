package dir_test

import (
	"context"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/loader/dir"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

type staticResolver struct {
	m map[string]string
}

func (s staticResolver) Resolve(f tenancy.Facts) (string, bool) {
	t, ok := s.m[f.Namespace]
	return t, ok
}

func errorPolicy(t *testing.T) tenancy.UnmappedPolicy {
	t.Helper()
	p, err := tenancy.ParseUnmappedPolicy("error")
	if err != nil {
		t.Fatalf("ParseUnmappedPolicy: %v", err)
	}
	return p
}

func TestLoadValidDirectory(t *testing.T) {
	l := dir.New(dir.Config{
		Dir:      "testdata/valid",
		Resolver: staticResolver{m: map[string]string{"team-payments": "platform"}},
		Policy:   errorPolicy(t),
	})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) != 0 {
		t.Fatalf("loadErrs = %v, want none", loadErrs)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2: %+v", len(rules), rules)
	}

	byFile := map[string]bool{}
	for _, r := range rules {
		byFile[r.Location.File] = true
		if r.Location.File == "team-payments/rules.yaml" {
			if r.Tenant != "platform" {
				t.Errorf("payments rule Tenant = %q, want platform", r.Tenant)
			}
			if r.Group.Name != "payments-group" || r.Group.Interval.String() != "30s" {
				t.Errorf("payments rule Group = %+v", r.Group)
			}
			if r.AST == nil {
				t.Error("payments rule AST is nil")
			}
		}
		if r.Location.File == "root.yaml" && r.Tenant != "" {
			t.Errorf("root.yaml rule Tenant = %q, want \"\" (flat file has no namespace, unmapped)", r.Tenant)
		}
	}
	if !byFile["root.yaml"] || !byFile["team-payments/rules.yaml"] {
		t.Errorf("unexpected file set: %v", byFile)
	}
}

func TestLoadBadYAML(t *testing.T) {
	l := dir.New(dir.Config{
		Dir:      "testdata/badyaml",
		Resolver: staticResolver{},
		Policy:   errorPolicy(t),
	})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("rules = %v, want none", rules)
	}
	if len(loadErrs) == 0 {
		t.Fatal("loadErrs is empty, want at least one parse error")
	}
}

func TestLoadBadExpr(t *testing.T) {
	l := dir.New(dir.Config{
		Dir:      "testdata/badexpr",
		Resolver: staticResolver{},
		Policy:   errorPolicy(t),
	})
	_, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) == 0 {
		t.Fatal("loadErrs is empty, want at least one expr parse error")
	}
}
