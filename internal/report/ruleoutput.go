package report

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/ruleoutput"
)

type mdRuleOutputRow struct {
	Rule, Metric, Series, Share string
}

type mdRuleOutputTenant struct {
	Tenant      string
	Summary     string
	Rows        []mdRuleOutputRow
	Alerting    string
	NotMeasured []string
}

type mdRuleOutputDoc struct {
	MeasuredAt  string
	Queries     int
	Tenants     []mdRuleOutputTenant
	Notes       []string
	Assumptions []cost.Assumption
}

// ruleOutputMDTemplateSrc, like costMDTemplateSrc, renders pre-formatted
// data only. Every "not measured" and "—" is decided in ruleOutputMDData.
const ruleOutputMDTemplateSrc = `
## Rule output series (point-in-time)

Counted at {{.MeasuredAt}} by querying each tenant for the series named after each recording rule's ` + "`record:`" + ` ({{.Queries}} batched {{if eq .Queries 1}}query{{else}}queries{{end}}). This names which rule wrote which series in ingester memory. It is not a breakdown of the tenant's ingestion: most active series usually come from scraped or remote-written data, and how much comes from rules depends on the rule corpus.
{{range .Tenants}}
### ` + "`{{.Tenant}}`" + `

{{.Summary}}
{{if .Rows}}
| Rule | Output metric | Series now | Share of tenant's active series |
|---|---|---:|---:|
{{range .Rows}}| {{.Rule}} | {{.Metric}} | {{.Series}} | {{.Share}} |
{{end}}{{end}}{{if .Alerting}}
{{.Alerting}}
{{end}}{{if .NotMeasured}}
Not measured:
{{range .NotMeasured}}
- {{.}}{{end}}
{{end}}{{end}}{{if .Notes}}
**Read before quoting:**
{{range .Notes}}
- {{.}}{{end}}
{{end}}
### Assumptions (rule output)

| Input | Value | Source |
|---|---|---|
{{range .Assumptions}}| {{.Name}} | {{mdcell .Value}} | {{mdcell .Source}} |
{{end}}`

var ruleOutputMDTemplate = template.Must(template.New("ruleoutput").Funcs(mdFuncs).Parse(ruleOutputMDTemplateSrc))

func ruleOutputMDData(r ruleoutput.Report) mdRuleOutputDoc {
	doc := mdRuleOutputDoc{
		MeasuredAt:  r.MeasuredAt.UTC().Format(time.RFC3339),
		Queries:     r.Queries,
		Notes:       r.Notes,
		Assumptions: r.Assumptions,
	}
	for _, t := range r.Tenants {
		doc.Tenants = append(doc.Tenants, ruleOutputMDTenant(t))
	}
	return doc
}

func ruleOutputMDTenant(t ruleoutput.TenantOutputs) mdRuleOutputTenant {
	name := t.Tenant
	if name == "" {
		name = "(no tenant resolved)"
	}
	out := mdRuleOutputTenant{Tenant: name, Summary: ruleOutputSummary(t)}

	reasons := map[string][]string{}
	for _, ro := range t.Rules {
		if ro.Status == ruleoutput.StatusNoOutput {
			continue
		}
		ref := ruleRefCell(ro)
		if len(ro.SharedWith) > 0 {
			ref += fmt.Sprintf(" (shares its metric with %d other rule(s))", len(ro.SharedWith))
		}
		row := mdRuleOutputRow{Rule: ref, Metric: "`" + mdEscape(ro.OutputMetric) + "`", Share: "—"}
		switch ro.Status {
		case ruleoutput.StatusSeries:
			row.Series = thousands(float64(*ro.Series))
		case ruleoutput.StatusNoSeries:
			row.Series = "0 (no series now)"
		default:
			row.Series = "not measured"
			reasons[ro.Reason] = append(reasons[ro.Reason], ro.OutputMetric)
		}
		if ro.ShareOfTenantActiveSeries != nil {
			row.Share = sharePercent(*ro.ShareOfTenantActiveSeries)
		}
		out.Rows = append(out.Rows, row)
	}

	if t.AlertingRules > 0 {
		out.Alerting = fmt.Sprintf("%d alerting rule(s) write no series, so they have no output-series figure. That is not zero cost: their cost is ruler CPU and query load, which this section does not measure.", t.AlertingRules)
	}

	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		metrics := dedupeSorted(reasons[k])
		out.NotMeasured = append(out.NotMeasured, fmt.Sprintf("%d metric(s): %s", len(metrics), k))
	}
	return out
}

func ruleOutputSummary(t ruleoutput.TenantOutputs) string {
	s := fmt.Sprintf("%d recording rule(s) writing %d distinct metric(s)", t.RecordingRules, t.OutputMetrics)
	switch {
	case t.OutputMetrics == 0:
		return s + "."
	case t.OutputSeries == nil:
		return s + "; none could be counted."
	}
	total := thousands(float64(*t.OutputSeries))
	if !t.OutputSeriesComplete {
		total = "at least " + total
	}
	s += fmt.Sprintf(": %s series now", total)
	if t.ShareOfActiveSeries != nil && t.ActiveSeries != nil {
		s += fmt.Sprintf(", %s of its %s active series", sharePercent(*t.ShareOfActiveSeries), thousands(math.Round(*t.ActiveSeries)))
	}
	return s + "."
}

// sharePercent keeps small shares visible: rule output is often well
// under 1% of a tenant, and "0.0%" would read as a measured zero.
func sharePercent(v float64) string {
	if v > 0 && v < 0.001 {
		return "<0.1%"
	}
	return percent(v)
}

func ruleRefCell(ro ruleoutput.RuleOutput) string {
	parts := []string{}
	if ro.Namespace != "" {
		parts = append(parts, ro.Namespace)
	}
	parts = append(parts, ro.Group, ro.Rule)
	return mdEscape(strings.Join(parts, " / "))
}

func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

func dedupeSorted(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}
