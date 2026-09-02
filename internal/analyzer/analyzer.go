package analyzer

import (
	"context"
	"fmt"
	"sort"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// Analyzer runs every enabled check over a rule set and returns a
// deterministically ordered finding list.
type Analyzer struct {
	reg *Registry
}

func New(reg *Registry) *Analyzer {
	return &Analyzer{reg: reg}
}

func (a *Analyzer) Run(ctx context.Context, rules []rule.AnnotatedRule, cc CheckContext) ([]rule.Finding, error) {
	var findings []rule.Finding
	for _, c := range a.reg.Enabled(cc.Config.Checks.Disable) {
		fs, err := c.Run(ctx, rules, cc)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", c.ID(), err)
		}
		findings = append(findings, fs...)
	}

	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i].Location, findings[j].Location
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return findings[i].CheckID < findings[j].CheckID
	})
	return findings, nil
}
