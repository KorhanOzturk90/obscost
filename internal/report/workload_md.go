package report

import (
	"fmt"
	"io"
	"text/template"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
)

type workloadMDReporter struct{}

type mdRuleAggregate struct {
	RuleName    string
	RuleID      string
	Kind        string
	Executions  string
	MetricValue string // formatted per the report's rank metric, or "not measured"
	MetricPct   string
}

type mdGroupAggregate struct {
	Group       string
	Namespace   string
	Executions  string
	MetricValue string
	MetricPct   string
	Rules       []mdRuleAggregate
}

type mdTenantAggregate struct {
	Tenant              string
	RuleCount           int
	Executions          string
	MetricValue         string
	MetricPct           string
	Groups              []mdGroupAggregate
	UnmatchedExecutions int
	UnmatchedSamples    uint64
}

type mdUnmatchedExecution struct {
	RuleID    string
	Timestamp string
	Samples   string // "unknown" if the source didn't report this stat
}

type workloadMDData struct {
	Window           string
	ObservedRange    string
	GeneratedAt      string
	SourceLabel      string
	SourceNote       string
	MetricLabel      string
	GroupMetricLabel string
	GroupGranularity bool
	// GroupMetricIsExecutions suppresses printing the group's rank figure
	// separately when it is just the execution count again — true whenever
	// the group tier ranks by executions, as the metrics source does.
	GroupMetricIsExecutions bool

	TotalExecutions string
	RuleDefinitions string

	CoverageCaptured  int
	CoverageMatched   int
	CoverageUnmatched int
	CoverageSkipped   int
	CoverageMatchPct  string

	Tenants   []mdTenantAggregate
	Unmatched []mdUnmatchedExecution
}

// workloadMDTemplateSrc, like md.go's mdTemplateSrc, is handed fully
// pre-sorted, pre-formatted data — attribution.Aggregate already guarantees
// deterministic tenant/group/rule ordering, so this template only renders,
// it never computes or re-sorts.
//
// Every figure below is an OBSERVED measurement from ingested telemetry or
// from Mimir's own metrics — never an estimate or a euro figure
// (PRODUCT-DIRECTION.md: "do not start with euros"). Two distinctions this
// template is careful to preserve, both of which it previously got wrong:
//
//   - A stat that reads as a real 0 was measured as 0; a stat the source
//     never measured renders as "not measured" (see rule.RuleExecution).
//   - An absent rule tier means "no rules matched" only at rule
//     granularity. At group granularity the source structurally cannot see
//     rules, and saying "no matched rule executions" there would report a
//     limitation of the source as a finding about the workload (ADR 0002).
const workloadMDTemplateSrc = `# promcost workload report — {{ .Window }} (observed)

Generated: {{ .GeneratedAt }}
{{ if .SourceLabel }}Source: {{ .SourceLabel }}{{ if .SourceNote }} ({{ .SourceNote }}){{ end }}
{{ end }}Observed window: {{ if .ObservedRange }}{{ .ObservedRange }}{{ else }}n/a (no executions in range){{ end }}
Ranked by: {{ .MetricLabel }}
Total executions: {{ .TotalExecutions }}
{{- if not .GroupGranularity }}
Rule definitions loaded: {{ .RuleDefinitions }}
{{- end }}
{{ if .GroupGranularity }}
This source reports at rule-group granularity — it carries no rule names, so
individual rules are not shown. Per-rule attribution needs execution
telemetry (` + "`--telemetry`" + `).
{{- else }}
Attribution coverage: {{ .CoverageMatched }} matched / {{ .CoverageCaptured }} captured ({{ .CoverageMatchPct }})
{{- if .CoverageUnmatched }}, {{ .CoverageUnmatched }} unmatched{{ end }}
{{- if .CoverageSkipped }}, {{ .CoverageSkipped }} skipped before matching{{ end }}
{{- end }}

## Tenant summary

| tenant |{{ if not .GroupGranularity }} rules |{{ end }} executions | {{ .MetricLabel }} | share |
|---|---|---|---|{{ if not .GroupGranularity }}---|{{ end }}
{{- range .Tenants }}
| {{ .Tenant }} |{{ if not $.GroupGranularity }} {{ .RuleCount }} |{{ end }} {{ .Executions }} | {{ .MetricValue }} | {{ .MetricPct }} |
{{- end }}
{{ range .Tenants }}
## {{ .Tenant }}
{{ if .Groups }}
{{- range .Groups }}
### {{ .Group }}{{ if .Namespace }} ({{ .Namespace }}){{ end }}

{{ .Executions }} executions{{ if not $.GroupMetricIsExecutions }}, {{ .MetricValue }}{{ end }} ({{ .MetricPct }} of tenant's reported groups by {{ $.GroupMetricLabel }})
{{ if .Rules }}
| rule | kind | executions | {{ $.GroupMetricLabel }} | share |
|---|---|---|---|---|
{{- range .Rules }}
| {{ .RuleName }} | {{ .Kind }} | {{ .Executions }} | {{ .MetricValue }} | {{ .MetricPct }} |
{{- end }}
{{- else if $.GroupGranularity }}
Rule-level detail not available from this source.
{{- else }}
No matched rule executions in this group.
{{- end }}
{{- end }}
{{- else if $.GroupGranularity }}
No rule groups reported for this tenant in this window.
{{- else }}
No matched rule executions.
{{- end }}
{{- if .UnmatchedExecutions }}

{{ .UnmatchedExecutions }} unmatched execution(s) ({{ .UnmatchedSamples }} samples) — see "Unmatched executions" below.
{{- end }}
{{ end -}}
{{ if .Unmatched }}
## Unmatched executions

Executions whose rule identity didn't match any loaded rule definition (deleted rule, drifted namespace, or a telemetry source that disagrees with --dir).

| rule id | timestamp | samples |
|---|---|---|
{{- range .Unmatched }}
| {{ .RuleID }} | {{ .Timestamp }} | {{ .Samples }} |
{{- end }}
{{ end -}}
`

var workloadMDTmpl = template.Must(template.New("workload_md").Parse(workloadMDTemplateSrc))

func (workloadMDReporter) Render(w io.Writer, result WorkloadResult) error {
	metric := result.RankMetric
	if metric == "" {
		metric = attribution.RankMetricExecutions
	}
	groupMetric := result.GroupRankMetric
	if groupMetric == "" {
		groupMetric = metric
	}

	// Unlike the previous version, an absent --since no longer prints "all
	// time" — a 15-minute log capture is not all of history, and labelling
	// it that way was the same class of overstatement as a fabricated zero
	// (ADR 0002 finding 2). The real observed range is shown instead.
	window := result.Window
	if window == "" {
		window = "observed window"
	}

	data := workloadMDData{
		Window:                  window,
		ObservedRange:           formatObservedRange(result.ObservedStart, result.ObservedEnd),
		GeneratedAt:             result.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"),
		SourceLabel:             result.SourceLabel,
		SourceNote:              result.SourceNote,
		MetricLabel:             metric.Label(),
		GroupMetricLabel:        groupMetric.Label(),
		GroupGranularity:        result.Granularity == attribution.GranularityGroup,
		GroupMetricIsExecutions: groupMetric == attribution.RankMetricExecutions,
		TotalExecutions:         formatInt(result.TotalExecutions),
		RuleDefinitions:         formatInt(result.RuleDefinitions),

		CoverageCaptured:  result.TotalExecutions + result.SkippedTelemetry,
		CoverageMatched:   result.TotalExecutions - len(result.Unmatched),
		CoverageUnmatched: len(result.Unmatched),
		CoverageSkipped:   result.SkippedTelemetry,
	}
	if data.CoverageCaptured > 0 {
		data.CoverageMatchPct = fmt.Sprintf("%.1f%%", 100*float64(data.CoverageMatched)/float64(data.CoverageCaptured))
	} else {
		data.CoverageMatchPct = "n/a"
	}

	for _, ta := range result.Tenants {
		mta := mdTenantAggregate{
			Tenant:              ta.Tenant,
			RuleCount:           ta.RuleCount,
			Executions:          formatInt(ta.Executions),
			MetricValue:         formatMeasured(ta.Observed(metric), metric, ta.RankValue),
			MetricPct:           formatMeasuredPct(ta.Observed(metric), ta.RankSharePct),
			UnmatchedExecutions: ta.UnmatchedExecutions,
			UnmatchedSamples:    ta.UnmatchedSamples,
		}
		for _, g := range ta.Groups {
			mg := mdGroupAggregate{
				Group:       g.Group,
				Namespace:   g.Namespace,
				Executions:  formatInt(g.Executions),
				MetricValue: formatMeasured(g.RankObserved, groupMetric, g.RankValue),
				MetricPct:   formatMeasuredPct(g.RankObserved, g.RankSharePct),
			}
			for _, ra := range g.Rules {
				mg.Rules = append(mg.Rules, mdRuleAggregate{
					RuleName:    ra.RuleID.Name,
					RuleID:      ra.RuleID.String(),
					Kind:        ra.Kind.String(),
					Executions:  formatInt(ra.Executions),
					MetricValue: formatMeasured(ra.RankObserved, groupMetric, ra.RankValue),
					MetricPct:   formatMeasuredPct(ra.RankObserved, pctf(ra.RankValue, g.RankValue)),
				})
			}
			mta.Groups = append(mta.Groups, mg)
		}
		data.Tenants = append(data.Tenants, mta)
	}

	for _, e := range result.Unmatched {
		data.Unmatched = append(data.Unmatched, mdUnmatchedExecution{
			RuleID:    e.RuleID().String(),
			Timestamp: e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Samples:   formatOptionalUint64(e.SamplesProcessed),
		})
	}

	return workloadMDTmpl.Execute(w, data)
}

// formatMeasured renders a rank figure, or the literal "not measured" when
// the source never reported that stat — never 0, which would present an
// unmeasured stat as an observed zero.
func formatMeasured(observed bool, metric attribution.RankMetric, v float64) string {
	if !observed {
		return "not measured"
	}
	return formatMetricValue(metric, v)
}

func formatMeasuredPct(observed bool, pct float64) string {
	if !observed {
		return "—"
	}
	return formatPct(pct)
}

// formatOptionalUint64 renders a RuleExecution stat pointer as its decimal
// value, or the literal string "unknown" when the source didn't report it
// — never as 0, which would misrepresent an unmeasured stat as an observed
// zero.
func formatOptionalUint64(v *uint64) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *v)
}

func formatPct(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}
