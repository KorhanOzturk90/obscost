package report

import (
	"fmt"
	"html/template"
	"io"
	"sort"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
)

type workloadHTMLReporter struct{}

// htmlShare is a percentage rendered two ways: Label is the formatted string
// ("37.2%") shown as text, Width is the raw 0-100 value used to size the CSS
// bar next to it — kept alongside each other so the template never redoes
// the arithmetic behind the label it prints.
type htmlShare struct {
	Label string
	Width float64
}

type htmlRuleAggregate struct {
	RuleID         string
	Kind           string
	Executions     int
	ExecutionShare htmlShare // this rule's share of its GROUP's executions
	Samples        uint64
	SampleShare    htmlShare // this rule's share of its GROUP's samples
}

// htmlGroupAggregate is a display-only rollup of a tenant's rules by
// RuleID.Group — attribution.Aggregate has no group-level aggregation tier
// of its own (Group is just a field on RuleID), so this reporter buckets
// the already-sorted, already-computed RuleAggregate slice itself rather
// than adding a new core domain type for a presentation concern.
type htmlGroupAggregate struct {
	Group          string
	Executions     int
	ExecutionShare htmlShare // this group's share of its TENANT's executions
	Samples        uint64
	SampleShare    htmlShare // this group's share of its TENANT's samples
	Rules          []htmlRuleAggregate
}

type htmlTenantAggregate struct {
	Tenant              string
	RuleCount           int
	Executions          int
	ExecutionShare      htmlShare // this tenant's share of the report TOTAL
	Samples             uint64
	SampleShare         htmlShare
	Groups              []htmlGroupAggregate
	UnmatchedExecutions int
	UnmatchedSamples    uint64
}

type htmlUnmatchedExecution struct {
	RuleID    string
	Timestamp string
	Samples   string // "unknown" if the source didn't report this stat
}

type workloadHTMLData struct {
	Window          string
	GeneratedAt     string
	TotalExecutions int
	TotalSamples    uint64
	RuleDefinitions int
	Tenants         []htmlTenantAggregate
	Unmatched       []htmlUnmatchedExecution
}

// workloadHTMLTemplateSrc, unlike workload_md.go's text/template, uses
// html/template specifically: tenant/group/rule names originate from rule
// files and telemetry (untrusted-ish input as far as this renderer is
// concerned), and html/template auto-escapes them into the markup. As in
// workload_md.go, every figure here is an OBSERVED measurement, never an
// estimate or a euro figure — no cost column exists yet because the cost
// model constants are uncalibrated placeholders (see AGENTS.md); resource
// share (executions, samples processed) is what's shown, with a cost
// column intended to slot in later once real calibration exists.
const workloadHTMLTemplateSrc = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>promcost workload report — {{ .Window }}</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; margin: 2rem; color: #1a1a1a; background: #fff; }
  h1 { font-size: 1.4rem; }
  h2 { font-size: 1.15rem; margin-top: 2.5rem; border-bottom: 1px solid #ddd; padding-bottom: .25rem; }
  h3 { font-size: 1rem; margin-top: 1.5rem; color: #333; }
  .meta { color: #555; font-size: .9rem; margin-bottom: 1.5rem; }
  table { border-collapse: collapse; width: 100%; margin: .5rem 0 1rem; font-size: .9rem; }
  th, td { text-align: left; padding: .35rem .6rem; border-bottom: 1px solid #eee; }
  th { color: #555; font-weight: 600; }
  td.num, th.num { text-align: right; }
  a.tenant-link { text-decoration: none; color: #205; }
  .share { display: inline-flex; align-items: center; gap: .5rem; white-space: nowrap; }
  .bar-track { width: 90px; height: 10px; background: #eee; border-radius: 3px; overflow: hidden; display: inline-block; }
  .bar-fill { height: 100%; background: #4c78a8; }
  .empty { color: #777; font-style: italic; }
  .warn { color: #8a5a00; }
</style>
</head>
<body>
<h1>promcost workload report — {{ .Window }} (observed)</h1>
<p class="meta">
  Generated: {{ .GeneratedAt }}<br>
  Total executions: {{ .TotalExecutions }} &middot;
  Total samples processed: {{ .TotalSamples }} &middot;
  Rule definitions loaded: {{ .RuleDefinitions }}
</p>

<h2>Tenant summary</h2>
<table>
<tr><th>tenant</th><th class="num">rules</th><th class="num">executions</th><th>execution share</th><th class="num">samples</th><th>sample share</th></tr>
{{- range .Tenants }}
<tr>
  <td><a class="tenant-link" href="#tenant-{{ .Tenant }}">{{ .Tenant }}</a></td>
  <td class="num">{{ .RuleCount }}</td>
  <td class="num">{{ .Executions }}</td>
  <td>{{ template "share" .ExecutionShare }}</td>
  <td class="num">{{ .Samples }}</td>
  <td>{{ template "share" .SampleShare }}</td>
</tr>
{{- end }}
</table>

{{ range .Tenants }}
<h2 id="tenant-{{ .Tenant }}">{{ .Tenant }}</h2>
{{ if .Groups }}
{{- range .Groups }}
<h3>{{ .Group }} <span class="meta">— {{ .Executions }} executions ({{ template "share" .ExecutionShare }}), {{ .Samples }} samples ({{ template "share" .SampleShare }}) of tenant</span></h3>
<table>
<tr><th>rule</th><th>kind</th><th class="num">executions</th><th>execution share</th><th class="num">samples</th><th>sample share</th></tr>
{{- range .Rules }}
<tr>
  <td>{{ .RuleID }}</td>
  <td>{{ .Kind }}</td>
  <td class="num">{{ .Executions }}</td>
  <td>{{ template "share" .ExecutionShare }}</td>
  <td class="num">{{ .Samples }}</td>
  <td>{{ template "share" .SampleShare }}</td>
</tr>
{{- end }}
</table>
{{- end }}
{{- else }}
<p class="empty">No matched rule executions.</p>
{{- end }}
{{- if .UnmatchedExecutions }}
<p class="warn">{{ .UnmatchedExecutions }} unmatched execution(s) ({{ .UnmatchedSamples }} samples) — see "Unmatched executions" below.</p>
{{- end }}
{{ end }}

{{ if .Unmatched }}
<h2>Unmatched executions</h2>
<p class="meta">Executions whose rule identity didn't match any loaded rule definition (deleted rule, drifted namespace, or a telemetry source that disagrees with --dir).</p>
<table>
<tr><th>rule id</th><th>timestamp</th><th class="num">samples</th></tr>
{{- range .Unmatched }}
<tr><td>{{ .RuleID }}</td><td>{{ .Timestamp }}</td><td class="num">{{ .Samples }}</td></tr>
{{- end }}
</table>
{{ end }}

</body>
</html>
{{ define "share" -}}
<span class="share">{{ .Label }}<span class="bar-track"><span class="bar-fill" style="width:{{ printf "%.1f" .Width }}%"></span></span></span>
{{- end }}
`

var workloadHTMLTmpl = template.Must(template.New("workload_html").Parse(workloadHTMLTemplateSrc))

func (workloadHTMLReporter) Render(w io.Writer, result WorkloadResult) error {
	window := result.Window
	if window == "" {
		window = "all time"
	}
	data := workloadHTMLData{
		Window:          window,
		GeneratedAt:     result.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"),
		TotalExecutions: result.TotalExecutions,
		TotalSamples:    result.TotalSamples,
		RuleDefinitions: result.RuleDefinitions,
	}

	for _, ta := range result.Tenants {
		hta := htmlTenantAggregate{
			Tenant:              ta.Tenant,
			RuleCount:           ta.RuleCount,
			Executions:          ta.Executions,
			ExecutionShare:      newShare(ta.ExecutionSharePct),
			Samples:             ta.SamplesProcessed,
			SampleShare:         newShare(ta.SampleSharePct),
			Groups:              groupRules(ta.Rules, ta.Executions, ta.SamplesProcessed),
			UnmatchedExecutions: ta.UnmatchedExecutions,
			UnmatchedSamples:    ta.UnmatchedSamples,
		}
		data.Tenants = append(data.Tenants, hta)
	}

	for _, e := range result.Unmatched {
		data.Unmatched = append(data.Unmatched, htmlUnmatchedExecution{
			RuleID:    e.RuleID().String(),
			Timestamp: e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Samples:   formatOptionalUint64(e.SamplesProcessed),
		})
	}

	return workloadHTMLTmpl.Execute(w, data)
}

// groupRules buckets one tenant's already-sorted RuleAggregate slice by
// RuleID.Group, purely for display (see htmlGroupAggregate's doc comment —
// no new attribution-package aggregation tier). Rules within each group
// come out in the same relative order attribution.Aggregate already sorted
// them in (SamplesProcessed desc, RuleID asc tie-break), since bucketing
// doesn't reorder within a bucket; groups themselves are sorted the same
// way for consistency.
func groupRules(rules []attribution.RuleAggregate, tenantExecutions int, tenantSamples uint64) []htmlGroupAggregate {
	type accum struct {
		group      string
		executions int
		samples    uint64
		rules      []attribution.RuleAggregate
	}

	byGroup := make(map[string]*accum)
	var order []string
	for _, r := range rules {
		g := r.RuleID.Group
		a, ok := byGroup[g]
		if !ok {
			a = &accum{group: g}
			byGroup[g] = a
			order = append(order, g)
		}
		a.executions += r.Executions
		a.samples += r.SamplesProcessed
		a.rules = append(a.rules, r)
	}

	groups := make([]htmlGroupAggregate, 0, len(byGroup))
	for _, g := range order {
		a := byGroup[g]
		hg := htmlGroupAggregate{
			Group:          a.group,
			Executions:     a.executions,
			ExecutionShare: newShare(sharePct(uint64(a.executions), uint64(tenantExecutions))),
			Samples:        a.samples,
			SampleShare:    newShare(sharePct(a.samples, tenantSamples)),
		}
		for _, r := range a.rules {
			hg.Rules = append(hg.Rules, htmlRuleAggregate{
				RuleID:         r.RuleID.String(),
				Kind:           r.Kind.String(),
				Executions:     r.Executions,
				ExecutionShare: newShare(sharePct(uint64(r.Executions), uint64(a.executions))),
				Samples:        r.SamplesProcessed,
				SampleShare:    newShare(sharePct(r.SamplesProcessed, a.samples)),
			})
		}
		groups = append(groups, hg)
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Samples != groups[j].Samples {
			return groups[i].Samples > groups[j].Samples
		}
		return groups[i].Group < groups[j].Group
	})
	return groups
}

// sharePct returns 100*part/total, or 0 (never NaN) when total is 0 — same
// zero-total guard as attribution.pct, needed here too since a group/rule
// can be computed against a tenant/group total that happens to be zero.
func sharePct(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(part) / float64(total)
}

func newShare(pct float64) htmlShare {
	return htmlShare{Label: fmt.Sprintf("%.1f%%", pct), Width: pct}
}
