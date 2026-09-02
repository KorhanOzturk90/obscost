package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

const idPCS04 = "PC-S04"

// PCS04 flags recording rules whose by()/without() aggregation leaves a
// configured high-cardinality label in the output series.
type PCS04 struct{}

func (PCS04) ID() string          { return idPCS04 }
func (PCS04) Tier() analyzer.Tier { return analyzer.TierStatic }

func (PCS04) Run(_ context.Context, rules []rule.AnnotatedRule, cc analyzer.CheckContext) ([]rule.Finding, error) {
	wordlist := cc.Config.Checks.Thresholds.HighCardinalityLabels
	if len(wordlist) == 0 {
		return nil, nil
	}

	var findings []rule.Finding
	for _, r := range rules {
		if r.Kind != rule.KindRecording || r.AST == nil {
			continue
		}

		analyzer.WalkPostOrder(r.AST, func(node parser.Node, _ []parser.Node) {
			agg, ok := node.(*parser.AggregateExpr)
			if !ok {
				return
			}

			grouping := make(map[string]bool, len(agg.Grouping))
			for _, g := range agg.Grouping {
				grouping[g] = true
			}

			var retained []string
			for _, l := range wordlist {
				switch {
				case agg.Without && !grouping[l]:
					retained = append(retained, l)
				case !agg.Without && grouping[l]:
					retained = append(retained, l)
				}
			}
			if len(retained) == 0 {
				return
			}
			sort.Strings(retained)

			findings = append(findings, rule.Finding{
				CheckID:  idPCS04,
				Severity: rule.SeverityInfo,
				Tenant:   r.Tenant,
				Location: r.Location,
				Message:  fmt.Sprintf("recording rule output retains high-cardinality label(s): %s", strings.Join(retained, ", ")),
				Values: map[string]any{
					"retained_labels": retained,
					"without":         agg.Without,
				},
				Remediation: "drop these labels from the aggregation's by/without clause unless the recording rule genuinely needs per-value series",
			})
		})
	}
	return findings, nil
}
