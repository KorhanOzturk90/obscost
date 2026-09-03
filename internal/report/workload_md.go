package report

import (
	"fmt"
	"io"
	"text/template"
)

type workloadMDReporter struct{}

type mdRuleAggregate struct {
	RuleID       string
	Kind         string
	Executions   int
	ExecutionPct string
	Samples      uint64
	SamplePct    string
}

type mdTenantAggregate struct {
	Tenant              string
	RuleCount           int
	Executions          int
	ExecutionPct        string
	Samples             uint64
	SamplePct           string
	Rules               []mdRuleAggregate
	UnmatchedExecutions int
	UnmatchedSamples    uint64
}

type mdUnmatchedExecution struct {
	RuleID    string
	Timestamp string
	Samples   string // "unknown" if the source didn't report this stat
}

type workloadMDData struct {
	Window          string
	GeneratedAt     string
	TotalExecutions int
	TotalSamples    uint64
	RuleDefinitions int
	Tenants         []mdTenantAggregate
	Unmatched       []mdUnmatchedExecution
}

// workloadMDTemplateSrc, like md.go's mdTemplateSrc, is handed fully
// pre-sorted, pre-formatted data — attribution.Aggregate already guarantees
// deterministic tenant/rule ordering, so this template only renders, it
// never computes or re-sorts.
//
// Every figure below is an OBSERVED measurement from ingested execution
// telemetry — never an estimate or a euro figure (PRODUCT-DIRECTION.md:
// "do not start with euros"). A stat that reads as a real 0 was measured as
// 0; a source that didn't measure a stat at all shows "unknown" instead
// (see rule.RuleExecution's doc comment) — the two are never conflated.
const workloadMDTemplateSrc = `# promcost workload report — {{ .Window }} (observed)

Generated: {{ .GeneratedAt }}
Total executions: {{ .TotalExecutions }}
Total samples processed: {{ .TotalSamples }}
Rule definitions loaded: {{ .RuleDefinitions }}

## Tenant summary

| tenant | rules | executions | execution % | samples | sample % |
|---|---|---|---|---|---|
{{- range .Tenants }}
| {{ .Tenant }} | {{ .RuleCount }} | {{ .Executions }} | {{ .ExecutionPct }} | {{ .Samples }} | {{ .SamplePct }} |
{{- end }}
{{ range .Tenants }}
## {{ .Tenant }}
{{ if .Rules }}
| rule | kind | executions | execution % | samples | sample % |
|---|---|---|---|---|---|
{{- range .Rules }}
| {{ .RuleID }} | {{ .Kind }} | {{ .Executions }} | {{ .ExecutionPct }} | {{ .Samples }} | {{ .SamplePct }} |
{{- end }}
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
	window := result.Window
	if window == "" {
		window = "all time"
	}
	data := workloadMDData{
		Window:          window,
		GeneratedAt:     result.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"),
		TotalExecutions: result.TotalExecutions,
		TotalSamples:    result.TotalSamples,
		RuleDefinitions: result.RuleDefinitions,
	}

	for _, ta := range result.Tenants {
		mta := mdTenantAggregate{
			Tenant:              ta.Tenant,
			RuleCount:           ta.RuleCount,
			Executions:          ta.Executions,
			ExecutionPct:        formatPct(ta.ExecutionSharePct),
			Samples:             ta.SamplesProcessed,
			SamplePct:           formatPct(ta.SampleSharePct),
			UnmatchedExecutions: ta.UnmatchedExecutions,
			UnmatchedSamples:    ta.UnmatchedSamples,
		}
		for _, ra := range ta.Rules {
			mta.Rules = append(mta.Rules, mdRuleAggregate{
				RuleID:       ra.RuleID.String(),
				Kind:         ra.Kind.String(),
				Executions:   ra.Executions,
				ExecutionPct: formatPct(ra.ExecutionSharePct),
				Samples:      ra.SamplesProcessed,
				SamplePct:    formatPct(ra.SampleSharePct),
			})
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
