package analyzer_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/analyzer/checks"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/limits"
	"github.com/KorhanOzturk90/obscost/internal/loader/dir"
	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

// comparableFinding is the reduced, deterministic shape golden fixtures
// assert on: check ID, severity, tenant, and location. Message/Values/
// Remediation text is intentionally excluded — this harness verifies check
// *logic* against real rule files, not exact wording, which would make
// fixtures brittle against harmless message tweaks.
type comparableFinding struct {
	CheckID  string `json:"check_id"`
	Severity string `json:"severity"`
	Tenant   string `json:"tenant"`
	File     string `json:"file"`
	Group    string `json:"group"`
	Rule     string `json:"rule"`
}

func toComparable(findings []rule.Finding) []comparableFinding {
	out := make([]comparableFinding, 0, len(findings))
	for _, f := range findings {
		out = append(out, comparableFinding{
			CheckID:  f.CheckID,
			Severity: f.Severity.String(),
			Tenant:   f.Tenant,
			File:     f.Location.File,
			Group:    f.Location.Group,
			Rule:     f.Location.Rule,
		})
	}
	sortComparable(out)
	return out
}

func sortComparable(fs []comparableFinding) {
	sort.Slice(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.CheckID != b.CheckID {
			return a.CheckID < b.CheckID
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Rule < b.Rule
	})
}

// findFixtures returns every directory under root that directly contains a
// rules/ subdirectory (the rule-file input for that fixture). Config
// (promcost.yaml) and limits (limits.yaml) files live as siblings of
// rules/, not inside it, so the dir loader never mistakes them for rule
// files.
func findFixtures(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "rules" {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(dirs)
	return dirs
}

func TestGoldenCorpus(t *testing.T) {
	for _, fixtureDir := range findFixtures(t, "../../testdata/corpus") {
		fixtureDir := fixtureDir
		t.Run(fixtureDir, func(t *testing.T) {
			cfg := config.Default()
			cfgPath := filepath.Join(fixtureDir, "promcost.yaml")
			if _, err := os.Stat(cfgPath); err == nil {
				loaded, err := config.Load(cfgPath)
				if err != nil {
					t.Fatalf("config.Load(%s): %v", cfgPath, err)
				}
				cfg = loaded
			}

			limitsPath := filepath.Join(fixtureDir, "limits.yaml")
			if _, err := os.Stat(limitsPath); err == nil {
				cfg.Limits.Sources = []config.LimitsSource{{Type: "file", Path: limitsPath}}
			}

			policy, err := tenancy.ParseUnmappedPolicy(cfg.Tenancy.Unmapped)
			if err != nil {
				t.Fatalf("ParseUnmappedPolicy: %v", err)
			}
			resolver := tenancy.NewResolver(cfg.Tenancy)

			l := dir.New(dir.Config{
				Dir:      filepath.Join(fixtureDir, "rules"),
				Resolver: resolver,
				Policy:   policy,
			})
			rules, loadErrs, err := l.Load(context.Background())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(loadErrs) > 0 {
				t.Fatalf("unexpected load errors: %v", loadErrs)
			}

			limitsProvider, err := limits.NewChainProvider(context.Background(), cfg.Limits.Sources)
			if err != nil {
				t.Fatalf("NewChainProvider: %v", err)
			}

			reg := analyzer.NewRegistry()
			for _, c := range checks.All() {
				reg.Register(c)
			}

			findings, err := analyzer.New(reg).Run(context.Background(), rules, analyzer.CheckContext{
				Config: cfg,
				Limits: limitsProvider,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			wantRaw, err := os.ReadFile(filepath.Join(fixtureDir, "expected.json"))
			if err != nil {
				t.Fatalf("read expected.json: %v", err)
			}
			var want []comparableFinding
			if err := json.Unmarshal(wantRaw, &want); err != nil {
				t.Fatalf("unmarshal expected.json: %v", err)
			}
			sortComparable(want)

			got := toComparable(findings)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("fixture %s: findings mismatch.\ngot:  %+v\nwant: %+v", fixtureDir, got, want)
			}
		})
	}
}
