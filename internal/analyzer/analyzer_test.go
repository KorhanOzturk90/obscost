package analyzer_test

import (
	"context"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

type fakeCheck struct {
	id       string
	findings []rule.Finding
}

func (f fakeCheck) ID() string          { return f.id }
func (f fakeCheck) Tier() analyzer.Tier { return analyzer.TierStatic }
func (f fakeCheck) Run(_ context.Context, _ []rule.AnnotatedRule, _ analyzer.CheckContext) ([]rule.Finding, error) {
	return f.findings, nil
}

func TestAnalyzerDisableFiltering(t *testing.T) {
	reg := analyzer.NewRegistry()
	reg.Register(fakeCheck{id: "PC-S01", findings: []rule.Finding{{CheckID: "PC-S01", Location: rule.SourceLocation{File: "a.yaml"}}}})
	reg.Register(fakeCheck{id: "PC-S02", findings: []rule.Finding{{CheckID: "PC-S02", Location: rule.SourceLocation{File: "b.yaml"}}}})

	a := analyzer.New(reg)
	cc := analyzer.CheckContext{Config: config.Config{Checks: config.ChecksConfig{Disable: []string{"PC-S02"}}}}

	findings, err := a.Run(context.Background(), nil, cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].CheckID != "PC-S01" {
		t.Fatalf("findings = %+v, want only PC-S01", findings)
	}
}

func TestAnalyzerStableSort(t *testing.T) {
	reg := analyzer.NewRegistry()
	reg.Register(fakeCheck{id: "PC-S02", findings: []rule.Finding{
		{CheckID: "PC-S02", Location: rule.SourceLocation{File: "b.yaml", Group: "g", Rule: "r1"}},
		{CheckID: "PC-S02", Location: rule.SourceLocation{File: "a.yaml", Group: "g", Rule: "r1"}},
	}})
	reg.Register(fakeCheck{id: "PC-S01", findings: []rule.Finding{
		{CheckID: "PC-S01", Location: rule.SourceLocation{File: "a.yaml", Group: "g", Rule: "r1"}},
	}})

	a := analyzer.New(reg)
	findings, err := a.Run(context.Background(), nil, analyzer.CheckContext{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("len(findings) = %d, want 3", len(findings))
	}
	want := []string{"PC-S01", "PC-S02", "PC-S02"}
	wantFiles := []string{"a.yaml", "a.yaml", "b.yaml"}
	for i, f := range findings {
		if f.CheckID != want[i] || f.Location.File != wantFiles[i] {
			t.Errorf("findings[%d] = %s/%s, want %s/%s", i, f.CheckID, f.Location.File, want[i], wantFiles[i])
		}
	}
}
