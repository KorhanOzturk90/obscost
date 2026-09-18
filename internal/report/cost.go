package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"text/template"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/ruleoutput"
)

// CostResult is everything a CostReporter needs to render one
// `promcost cost` run. It holds one allocation per pool; today that is
// only pool 1, and no cross-pool total is rendered (see internal/cost's
// package doc for why).
//
// RuleOutputs is the optional rule -> output-series drill-down under pool
// 1 (issue #37). It is nil unless `--rule-outputs` was asked for.
type CostResult struct {
	Pools       []cost.PoolAllocation `json:"pools"`
	RuleOutputs *ruleoutput.Report    `json:"rule_outputs,omitempty"`
	GeneratedAt time.Time             `json:"generated_at"`
}

type CostReporter interface {
	Render(w io.Writer, result CostResult) error
}

// NewCost returns a reporter for format. There is no HTML form yet.
func NewCost(format Format) (CostReporter, error) {
	switch format {
	case FormatMD, "":
		return costMDReporter{}, nil
	case FormatJSON:
		return costJSONReporter{}, nil
	default:
		return nil, fmt.Errorf("unknown cost report format %q (md|json)", format)
	}
}

type costJSONReporter struct{}

func (costJSONReporter) Render(w io.Writer, result CostResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

type costMDReporter struct{}

type mdCostRow struct {
	Tenant, Driver, Share, Replicas, Cost string
}

type mdCostPool struct {
	Name        string
	Window      string
	End         string
	Capacity    string
	Headline    string
	DriverUnit  string
	ReplicaNoun string
	ShowCost    bool
	CostHeader  string
	Rows        []mdCostRow
	Total       mdCostRow
	Notes       []string
	Assumptions []cost.Assumption
}

// costMDTemplateSrc renders pre-formatted data only. Every "not measured",
// "not costed" and "—" is decided in costMDData, never here.
const costMDTemplateSrc = `# promcost tenant cost report
{{range $p := .Pools}}
## {{.Name}}

Average over the last {{.Window}}, ending {{.End}}. {{.Capacity}}

{{.Headline}}

| Tenant | {{.DriverUnit}} | Share | ≈ {{.ReplicaNoun}}s |{{if .ShowCost}} {{.CostHeader}} |{{end}}
|---|---:|---:|---:|{{if .ShowCost}}---:|{{end}}
{{range .Rows}}| {{.Tenant}} | {{.Driver}} | {{.Share}} | {{.Replicas}} |{{if $p.ShowCost}} {{.Cost}} |{{end}}
{{end}}| **{{.Total.Tenant}}** | **{{.Total.Driver}}** | **{{.Total.Share}}** | **{{.Total.Replicas}}** |{{if .ShowCost}} **{{.Total.Cost}}** |{{end}}
{{if .Notes}}
**Read before quoting:**
{{range .Notes}}
- {{.}}{{end}}
{{end}}
### Assumptions

| Input | Value | Source |
|---|---|---|
{{range .Assumptions}}| {{.Name}} | {{mdcell .Value}} | {{mdcell .Source}} |
{{end}}{{end}}`

// Render writes the pools, then the rule-output section if one was
// measured, then the generated-at line.
func (costMDReporter) Render(w io.Writer, result CostResult) error {
	data := costMDData(result)
	if err := costMDTemplate.Execute(w, data); err != nil {
		return err
	}
	if result.RuleOutputs != nil {
		if err := ruleOutputMDTemplate.Execute(w, ruleOutputMDData(*result.RuleOutputs)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nGenerated %s.\n", data.GeneratedAt)
	return err
}

var costMDTemplate = template.Must(template.New("cost").Funcs(mdFuncs).Parse(costMDTemplateSrc))

// mdFuncs is shared by the cost and rule-output templates.
var mdFuncs = template.FuncMap{
	// mdcell code-formats a table cell. A pipe must be escaped even inside
	// backticks — GFM splits table cells before parsing code spans, and
	// the ingest-storage driver query contains a regex alternation.
	"mdcell": func(s string) string {
		s = strings.ReplaceAll(s, "`", "'")
		return "`" + strings.ReplaceAll(s, "|", `\|`) + "`"
	},
}

type costMDDoc struct {
	Pools       []mdCostPool
	GeneratedAt string
}

func costMDData(result CostResult) costMDDoc {
	doc := costMDDoc{GeneratedAt: result.GeneratedAt.UTC().Format(time.RFC3339)}
	for _, a := range result.Pools {
		doc.Pools = append(doc.Pools, costMDPool(a))
	}
	return doc
}

func costMDPool(a cost.PoolAllocation) mdCostPool {
	p := mdCostPool{
		Name:        capitalize(a.Pool.Name),
		Window:      a.WindowText,
		End:         a.End.UTC().Format(time.RFC3339),
		DriverUnit:  capitalize(a.Pool.DriverUnit),
		ReplicaNoun: a.Pool.ReplicaNoun,
		ShowCost:    a.Costed,
		Notes:       a.Notes,
		Assumptions: a.Assumptions,
	}
	if a.Costed {
		p.CostHeader = a.Currency + "/month"
	}

	switch {
	case a.Replicas != nil && a.Costed:
		p.Capacity = fmt.Sprintf("Pool: %d %ss, %s %s/month.", *a.Replicas, a.Pool.ReplicaNoun, money(*a.PoolCostMonth), a.Currency)
	case a.Replicas != nil:
		p.Capacity = fmt.Sprintf("Pool: %d %ss. **Not costed** — no price in the inventory.", *a.Replicas, a.Pool.ReplicaNoun)
	default:
		p.Capacity = fmt.Sprintf("**Not costed**, and no %s count in the inventory — shares only.", a.Pool.ReplicaNoun)
	}

	p.Headline = costHeadline(a)

	for _, t := range a.Tenants {
		row := mdCostRow{
			Tenant:   t.Tenant,
			Driver:   driverCell(t.Driver),
			Share:    "undefined",
			Replicas: "—",
		}
		if t.Share != nil {
			row.Share = percent(*t.Share)
		}
		if t.EquivalentReplicas != nil {
			row.Replicas = fmt.Sprintf("%.2f", *t.EquivalentReplicas)
		}
		if t.Cost != nil {
			row.Cost = money(*t.Cost)
		} else if a.Costed {
			row.Cost = "undefined"
		}
		p.Rows = append(p.Rows, row)
	}
	for _, t := range a.Unmeasured {
		row := mdCostRow{Tenant: t, Driver: "not measured", Share: "not measured", Replicas: "not measured"}
		if a.Costed {
			row.Cost = "not measured"
		}
		p.Rows = append(p.Rows, row)
	}

	p.Total = mdCostRow{Tenant: "Total", Driver: driverCell(a.Total), Share: "100%", Replicas: "—"}
	if a.TotalRaw <= 0 {
		p.Total.Share = "—"
	}
	if a.Replicas != nil {
		p.Total.Replicas = fmt.Sprintf("%d", *a.Replicas)
	}
	if a.Costed {
		p.Total.Cost = money(*a.PoolCostMonth)
	}
	return p
}

// costHeadline is the one sentence a platform lead takes into a budget
// conversation (ADR 0003): the largest tenant, in resources first and in
// currency only if priced.
func costHeadline(a cost.PoolAllocation) string {
	if len(a.Tenants) == 0 {
		return fmt.Sprintf("No tenant reported any %s in this window.", a.Pool.DriverUnit)
	}
	top := a.Tenants[0]
	if top.Share == nil {
		return fmt.Sprintf("Every measured tenant reported 0 %s, so there is no share to state.", a.Pool.DriverUnit)
	}
	s := fmt.Sprintf("**`%s` uses %s of %s**", top.Tenant, percent(*top.Share), a.Pool.Name)
	if top.EquivalentReplicas != nil {
		s += fmt.Sprintf(" — the equivalent of %.1f of your %d %ss", *top.EquivalentReplicas, *a.Replicas, a.Pool.ReplicaNoun)
	}
	if top.Cost != nil {
		s += fmt.Sprintf(", %s %s/month", money(*top.Cost), a.Currency)
	}
	return s + "."
}

func driverCell(v *float64) string {
	if v == nil {
		return "withheld (no RF)"
	}
	return thousands(math.Round(*v))
}

func percent(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}

func money(v float64) string {
	whole := math.Floor(v)
	cents := math.Round((v - whole) * 100)
	if cents == 100 {
		whole++
		cents = 0
	}
	return fmt.Sprintf("%s.%02d", thousands(whole), int(cents))
}

// thousands renders a non-negative whole number with comma separators.
func thousands(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
