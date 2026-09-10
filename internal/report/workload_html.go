package report

import (
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

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
	RuleName       string // short display name (RuleID.Name) — the full ID is a lot of repeated tenant/namespace/group noise to read on every row
	RuleIDFull     string // full RuleID string, shown as a title tooltip
	Kind           string
	Executions     string // formatted with thousands separators
	MetricMeasured bool
	MetricValue    string    // formatted per Report.RankMetric's unit, or "" if unmeasured
	MetricShare    htmlShare // this rule's share of its GROUP's rank-metric total
}

// htmlGroupAggregate is a display-only rollup of a tenant's rules by
// RuleID.Group — attribution.Aggregate has no group-level aggregation tier
// of its own (Group is just a field on RuleID), so this reporter buckets
// the already-sorted, already-computed RuleAggregate slice itself rather
// than adding a new core domain type for a presentation concern.
type htmlGroupAggregate struct {
	Group          string
	Executions     string
	MetricMeasured bool
	MetricValue    string
	MetricShare    htmlShare // this group's share of its TENANT's rank-metric total
	Rules          []htmlRuleAggregate
	// DefaultOpen marks the top groups (by rank metric) within a tenant as
	// expanded by default; the rest render collapsed behind <details> so a
	// tenant with dozens of groups doesn't force a wall of scrolling to
	// reach the ones that matter.
	DefaultOpen bool
}

type htmlTenantAggregate struct {
	Tenant              string
	RuleCount           string
	Executions          string
	MetricMeasured      bool
	MetricValue         string
	MetricShare         htmlShare // this tenant's share of the report TOTAL rank-metric value
	Groups              []htmlGroupAggregate
	UnmatchedExecutions int
	UnmatchedSamples    uint64
	DefaultOpen         bool // top tenants by rank metric expand by default
}

type htmlUnmatchedExecution struct {
	RuleID    string
	Timestamp string
	Samples   string // "unknown" if the source didn't report this stat
}

type workloadHTMLData struct {
	Window        string // the requested --since filter, "" if none
	ObservedRange string // the actual observed capture window, "" if no executions
	GeneratedAt   string

	MetricLabel string // e.g. "query wall time" — what Tenants/Rules are ranked by

	TotalExecutions string
	RuleDefinitions string

	CoverageCaptured  int // telemetry records observed, whether or not usable
	CoverageMatched   int
	CoverageUnmatched int
	CoverageSkipped   int
	CoverageMatchPct  string

	TopSummary string // one-sentence "what's driving workload" answer

	Tenants   []htmlTenantAggregate
	Unmatched []htmlUnmatchedExecution
}

// workloadHTMLTemplateSrc, unlike workload_md.go's text/template, uses
// html/template specifically: tenant/group/rule names originate from rule
// files and telemetry (untrusted-ish input as far as this renderer is
// concerned), and html/template auto-escapes them into the markup. As in
// workload_md.go, every figure here is an OBSERVED measurement, never an
// estimate or a euro figure — no cost column exists yet because the cost
// model constants are uncalibrated placeholders (see AGENTS.md).
//
// Which figure counts as "the" workload share is not fixed to sample count:
// Report.RankMetric (see its doc comment) picks whichever resource-cost
// proxy the telemetry source actually measured, so a rule with a small
// sample count but a large query wall time still surfaces as the real
// driver of load rather than being buried by an execution-count-only view.
// A tenant/rule that never measured the chosen metric shows "not measured"
// rather than a fabricated "0" (see attribution.TenantAggregate.Observed).
const workloadHTMLTemplateSrc = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>promcost workload report{{ if .Window }} — {{ .Window }}{{ end }}</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; margin: 2rem; color: #1a1a1a; background: #fff; }
  h1 { font-size: 1.4rem; }
  h2 { font-size: 1.15rem; margin-top: 2.5rem; border-bottom: 1px solid #ddd; padding-bottom: .25rem; }
  .meta { color: #555; font-size: .9rem; margin-bottom: .5rem; }
  .coverage { color: #333; font-size: .9rem; margin-bottom: .5rem; }
  .summary { font-size: 1rem; margin: 1rem 0 1.5rem; padding: .6rem .9rem; background: #f4f6fb; border-left: 3px solid #4c78a8; }
  table { border-collapse: collapse; width: 100%; margin: .5rem 0 1rem; font-size: .9rem; }
  th, td { text-align: left; padding: .35rem .6rem; border-bottom: 1px solid #eee; }
  th { color: #555; font-weight: 600; }
  td.num, th.num { text-align: right; }
  a.tenant-link { text-decoration: none; color: #205; }
  .share { display: inline-flex; align-items: center; gap: .5rem; white-space: nowrap; }
  .bar-track { width: 90px; height: 10px; background: #eee; border-radius: 3px; overflow: hidden; display: inline-block; }
  .bar-fill { height: 100%; background: #4c78a8; }
  .metricval { color: #555; }
  .empty, .unmeasured { color: #777; font-style: italic; }
  .warn { color: #8a5a00; }
  details { margin: .75rem 0; }
  summary { cursor: pointer; font-size: 1rem; margin-top: 1.5rem; color: #333; padding: .25rem 0; }
  summary .meta { display: inline; margin-bottom: 0; }
</style>
</head>
<body>
<h1>promcost workload report (observed)</h1>
<p class="meta">
  Generated: {{ .GeneratedAt }}<br>
  {{ if .Window }}Filter: {{ .Window }}<br>{{ end }}
  Observed capture window: {{ if .ObservedRange }}{{ .ObservedRange }}{{ else }}n/a (no executions in range){{ end }}<br>
  Ranked by: {{ .MetricLabel }} &middot;
  Total executions: {{ .TotalExecutions }} &middot;
  Rule definitions loaded: {{ .RuleDefinitions }}
</p>
<p class="coverage">
  Attribution coverage: {{ .CoverageMatched }} matched / {{ .CoverageCaptured }} captured ({{ .CoverageMatchPct }})
  {{- if .CoverageUnmatched }} &middot; {{ .CoverageUnmatched }} unmatched (no known rule definition){{ end }}
  {{- if .CoverageSkipped }} &middot; {{ .CoverageSkipped }} skipped before matching (see command output/logs){{ end }}
</p>
<p class="summary">{{ .TopSummary }}</p>

<h2>Tenant summary</h2>
<table>
<tr><th>tenant</th><th class="num">rules</th><th class="num">executions</th><th>{{ .MetricLabel }} share</th></tr>
{{- range .Tenants }}
<tr>
  <td><a class="tenant-link" href="#tenant-{{ .Tenant }}">{{ .Tenant }}</a></td>
  <td class="num">{{ .RuleCount }}</td>
  <td class="num">{{ .Executions }}</td>
  <td>{{ if .MetricMeasured }}{{ template "share" .MetricShare }} <span class="metricval">({{ .MetricValue }})</span>{{ else }}<span class="unmeasured">not measured</span>{{ end }}</td>
</tr>
{{- end }}
</table>

{{ range .Tenants }}
<h2 id="tenant-{{ .Tenant }}">{{ .Tenant }}</h2>
{{ if .Groups }}
{{- range .Groups }}
<details{{ if .DefaultOpen }} open{{ end }}>
<summary>{{ .Group }} <span class="meta">— {{ .Executions }} executions, {{ if .MetricMeasured }}{{ template "share" .MetricShare }} ({{ .MetricValue }}){{ else }}<span class="unmeasured">not measured</span>{{ end }} of tenant</span></summary>
<table>
<tr><th>rule</th><th>kind</th><th class="num">executions</th><th>{{ $.MetricLabel }} share</th></tr>
{{- range .Rules }}
<tr>
  <td><span title="{{ .RuleIDFull }}">{{ .RuleName }}</span></td>
  <td>{{ .Kind }}</td>
  <td class="num">{{ .Executions }}</td>
  <td>{{ if .MetricMeasured }}{{ template "share" .MetricShare }} <span class="metricval">({{ .MetricValue }})</span>{{ else }}<span class="unmeasured">not measured</span>{{ end }}</td>
</tr>
{{- end }}
</table>
</details>
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
	metric := result.RankMetric
	if metric == "" {
		metric = attribution.RankMetricExecutions
	}

	data := workloadHTMLData{
		Window:          result.Window,
		ObservedRange:   formatObservedRange(result.ObservedStart, result.ObservedEnd),
		GeneratedAt:     result.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		MetricLabel:     metric.Label(),
		TotalExecutions: formatInt(result.TotalExecutions),
		RuleDefinitions: formatInt(result.RuleDefinitions),

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

	var reportRankTotal float64
	for _, ta := range result.Tenants {
		reportRankTotal += ta.RankValue
	}

	for i, ta := range result.Tenants {
		hta := htmlTenantAggregate{
			Tenant:              ta.Tenant,
			RuleCount:           formatInt(ta.RuleCount),
			Executions:          formatInt(ta.Executions),
			MetricMeasured:      ta.Observed(metric),
			Groups:              groupRules(ta.Rules, metric, ta.RankValue),
			UnmatchedExecutions: ta.UnmatchedExecutions,
			UnmatchedSamples:    ta.UnmatchedSamples,
			DefaultOpen:         i < 3,
		}
		if hta.MetricMeasured {
			hta.MetricValue = formatMetricValue(metric, ta.RankValue)
			hta.MetricShare = newShare(pctf(ta.RankValue, reportRankTotal))
		} else {
			hta.MetricShare = htmlShare{Width: 0}
		}
		data.Tenants = append(data.Tenants, hta)
	}

	data.TopSummary = topSummary(data.Tenants, metric)

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
// no new attribution-package aggregation tier). Groups and the rules within
// them are ranked by metric's RankValue, the same figure attribution.
// Aggregate itself already sorted the input Rules slice by — see PR #23's
// review point #5: this used to be hardcoded to SamplesProcessed, which
// silently fell back to alphabetical order for any telemetry source (like
// mimirlogs) that never measures samples, hiding the actual largest group.
func groupRules(rules []attribution.RuleAggregate, metric attribution.RankMetric, tenantRankValue float64) []htmlGroupAggregate {
	type accum struct {
		group      string
		executions int
		rankValue  float64
		measured   bool
		rules      []attribution.RuleAggregate
	}

	byGroup := make(map[string]*accum)
	var accums []*accum
	for _, r := range rules {
		g := r.RuleID.Group
		a, ok := byGroup[g]
		if !ok {
			a = &accum{group: g}
			byGroup[g] = a
			accums = append(accums, a)
		}
		a.executions += r.Executions
		a.rankValue += r.RankValue
		if r.Observed(metric) {
			a.measured = true
		}
		a.rules = append(a.rules, r)
	}

	sort.Slice(accums, func(i, j int) bool {
		if accums[i].rankValue != accums[j].rankValue {
			return accums[i].rankValue > accums[j].rankValue
		}
		return accums[i].group < accums[j].group
	})

	groups := make([]htmlGroupAggregate, 0, len(accums))
	for idx, a := range accums {
		hg := htmlGroupAggregate{
			Group:          a.group,
			Executions:     formatInt(a.executions),
			MetricMeasured: a.measured,
			DefaultOpen:    idx < 3,
		}
		if a.measured {
			hg.MetricValue = formatMetricValue(metric, a.rankValue)
			hg.MetricShare = newShare(pctf(a.rankValue, tenantRankValue))
		} else {
			hg.MetricShare = htmlShare{Width: 0}
		}
		for _, r := range a.rules {
			hr := htmlRuleAggregate{
				RuleName:       r.RuleID.Name,
				RuleIDFull:     r.RuleID.String(),
				Kind:           r.Kind.String(),
				Executions:     formatInt(r.Executions),
				MetricMeasured: r.Observed(metric),
			}
			if hr.MetricMeasured {
				hr.MetricValue = formatMetricValue(metric, r.RankValue)
				hr.MetricShare = newShare(pctf(r.RankValue, a.rankValue))
			} else {
				hr.MetricShare = htmlShare{Width: 0}
			}
			hg.Rules = append(hg.Rules, hr)
		}
		groups = append(groups, hg)
	}
	return groups
}

// topSummary answers PR #23 review's "what is driving observed rule
// workload?" ask in one sentence, naming the top (up to 3) tenants by
// whichever metric the report is ranked by — the reader shouldn't have to
// scan every table on the page just to find the headline.
func topSummary(tenants []htmlTenantAggregate, metric attribution.RankMetric) string {
	if len(tenants) == 0 {
		return "No matched rule executions in this window."
	}
	n := min(len(tenants), 3)
	parts := make([]string, 0, n)
	for _, t := range tenants[:n] {
		if t.MetricMeasured {
			parts = append(parts, fmt.Sprintf("%s (%s)", t.Tenant, t.MetricShare.Label))
		} else {
			parts = append(parts, t.Tenant)
		}
	}
	return fmt.Sprintf("Top tenants by %s: %s.", metric.Label(), strings.Join(parts, ", "))
}

// formatObservedRange renders the actual observed capture window — see
// WorkloadResult.ObservedStart's doc comment on why this exists instead of
// trusting the --since flag's label alone (PR #23 review point #4).
func formatObservedRange(start, end *time.Time) string {
	if start == nil || end == nil {
		return ""
	}
	s, e := start.UTC(), end.UTC()
	if s.Format("2006-01-02") == e.Format("2006-01-02") {
		return fmt.Sprintf("%s %s–%s UTC", s.Format("2006-01-02"), s.Format("15:04"), e.Format("15:04"))
	}
	return fmt.Sprintf("%s UTC – %s UTC", s.Format("2006-01-02 15:04"), e.Format("2006-01-02 15:04"))
}

// formatMetricValue renders a RankValue in the unit appropriate to metric —
// seconds as a human duration, bytes as a human byte count, everything else
// (series/chunks/samples/executions are all plain counts) with thousands
// separators.
func formatMetricValue(metric attribution.RankMetric, v float64) string {
	switch metric {
	case attribution.RankMetricDurationSeconds:
		return formatDuration(v)
	case attribution.RankMetricFetchedBytes:
		return formatBytes(v)
	default:
		return formatUint64(uint64(v))
	}
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}

func formatBytes(n float64) string {
	const unit = 1024.0
	if n < unit {
		return fmt.Sprintf("%.0f B", n)
	}
	div, exp := unit, 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	units := "KMGTPE"
	return fmt.Sprintf("%.1f %ciB", n/div, units[exp])
}

// formatInt and formatUint64 render counts with thousands separators
// ("3,894" not "3894") — PR #23 review's readability ask.
func formatInt(n int) string { return formatThousands(strconv.Itoa(n)) }

func formatUint64(n uint64) string { return formatThousands(strconv.FormatUint(n, 10)) }

func formatThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	n := len(s)
	if n <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	pre := n % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		b.WriteByte(',')
	}
	for i := pre; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	out := b.String()
	if neg {
		return "-" + out
	}
	return out
}

// pctf returns 100*part/total, or 0 (never NaN) when total is 0 — same
// zero-total guard as attribution's own pctf, needed here too since a
// group/rule/tenant can be computed against a total that happens to be 0.
func pctf(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * part / total
}

func newShare(pct float64) htmlShare {
	return htmlShare{Label: fmt.Sprintf("%.1f%%", pct), Width: pct}
}
