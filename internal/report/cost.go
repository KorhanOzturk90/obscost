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
)

// CostResult is everything a CostReporter needs to render one
// `promcost cost` run: one allocation per pool, and the cross-pool total
// with the coverage that qualifies it. The total is rendered only through
// its coverage (see cost.CrossPool), never as a bare blended percentage.
type CostResult struct {
	Pools       []cost.PoolAllocation `json:"pools"`
	CrossPool   cost.CrossPool        `json:"cross_pool"`
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
	Name         string
	Aggregation  string
	Window       string
	End          string
	Capacity     string
	Headline     string
	DriverUnit   string
	ReplicaNoun  string
	ShowReplicas bool
	ShowCost     bool
	CostHeader   string
	Rows         []mdCostRow
	Total        mdCostRow
	Notes        []string
	Assumptions  []cost.Assumption
}

// mdCrossPool is the cross-pool section. Statement always carries the
// coverage; Headers/Rows are the per-tenant table, empty when no pool is
// priced.
type mdCrossPool struct {
	Show       bool
	Statement  string
	Currency   string
	Headers    []string
	Rows       []mdCrossRow
	Notes      []string
	NotModeled string
}

type mdCrossRow struct {
	Tenant string
	Cells  []string
	Total  string
	Share  string
}

// costMDTemplateSrc renders pre-formatted data only. Every "not measured",
// "not costed" and "—" is decided in costMDData, never here.
const costMDTemplateSrc = `# promcost tenant cost report
{{range $p := .Pools}}
## {{.Name}}

{{.Aggregation}} over the last {{.Window}}, ending {{.End}}. {{.Capacity}}

{{.Headline}}

| Tenant | {{.DriverUnit}} | Share |{{if .ShowReplicas}} ≈ {{.ReplicaNoun}}s |{{end}}{{if .ShowCost}} {{.CostHeader}} |{{end}}
|---|---:|---:|{{if .ShowReplicas}}---:|{{end}}{{if .ShowCost}}---:|{{end}}
{{range .Rows}}| {{.Tenant}} | {{.Driver}} | {{.Share}} |{{if $p.ShowReplicas}} {{.Replicas}} |{{end}}{{if $p.ShowCost}} {{.Cost}} |{{end}}
{{end}}| **{{.Total.Tenant}}** | **{{.Total.Driver}}** | **{{.Total.Share}}** |{{if .ShowReplicas}} **{{.Total.Replicas}}** |{{end}}{{if .ShowCost}} **{{.Total.Cost}}** |{{end}}
{{if .Notes}}
**Read before quoting:**
{{range .Notes}}
- {{.}}{{end}}
{{end}}
### Assumptions

| Input | Value | Source |
|---|---|---|
{{range .Assumptions}}| {{.Name}} | {{mdcell .Value}} | {{mdcell .Source}} |
{{end}}{{end}}{{with .CrossPool}}{{if .Show}}
## Cost across pools

{{.Statement}}
{{if .Rows}}
| Tenant |{{range .Headers}} {{.}} |{{end}} Total {{.Currency}}/month | Share of priced cost |
|---|{{range .Headers}}---:|{{end}}---:|---:|
{{range .Rows}}| {{.Tenant}} |{{range .Cells}} {{.}} |{{end}} {{.Total}} | {{.Share}} |
{{end}}{{end}}{{if .Notes}}
**Read before quoting:**
{{range .Notes}}
- {{.}}{{end}}
{{end}}
Not modelled yet: {{.NotModeled}}.
{{end}}{{end}}
Generated {{.GeneratedAt}}.
`

func (costMDReporter) Render(w io.Writer, result CostResult) error {
	data := costMDData(result)
	return costMDTemplate.Execute(w, data)
}

var costMDTemplate = template.Must(template.New("cost").Funcs(template.FuncMap{
	// mdcell code-formats a table cell. A pipe must be escaped even inside
	// backticks — GFM splits table cells before parsing code spans, and
	// the ingest-storage driver query contains a regex alternation.
	"mdcell": func(s string) string {
		s = strings.ReplaceAll(s, "`", "'")
		return "`" + strings.ReplaceAll(s, "|", `\|`) + "`"
	},
}).Parse(costMDTemplateSrc))

type costMDDoc struct {
	Pools       []mdCostPool
	CrossPool   mdCrossPool
	GeneratedAt string
}

func costMDData(result CostResult) costMDDoc {
	doc := costMDDoc{GeneratedAt: result.GeneratedAt.UTC().Format(time.RFC3339)}
	for _, a := range result.Pools {
		doc.Pools = append(doc.Pools, costMDPool(a))
	}
	doc.CrossPool = costMDCrossPool(result)
	return doc
}

func costMDPool(a cost.PoolAllocation) mdCostPool {
	p := mdCostPool{
		Name:        capitalize(a.Pool.Name),
		Aggregation: "Average",
		Window:      a.WindowText,
		End:         a.End.UTC().Format(time.RFC3339),
		DriverUnit:  capitalize(a.DriverKind),
		ReplicaNoun: a.Pool.ReplicaNoun,
		// A pool priced per component has no single replica count to
		// express a share against, so it gets a cost column and no
		// replica-equivalent column.
		ShowReplicas: len(a.Components) == 0,
		ShowCost:     a.Costed,
		Notes:        a.Notes,
		Assumptions:  a.Assumptions,
	}
	if a.Pool.Cumulative {
		p.Aggregation = "Total"
	}
	if a.Costed {
		p.CostHeader = a.Currency + "/month"
	}

	switch {
	case len(a.Components) > 0:
		p.Capacity = componentCapacity(a)
	case a.Replicas != nil && a.Costed:
		p.Capacity = fmt.Sprintf("Pool: %s, %s %s/month.", counted(*a.Replicas, a.Pool.ReplicaNoun), money(*a.PoolCostMonth), a.Currency)
	case a.Replicas != nil:
		p.Capacity = fmt.Sprintf("Pool: %s. **Not costed** — no price in the inventory.", counted(*a.Replicas, a.Pool.ReplicaNoun))
	default:
		p.Capacity = fmt.Sprintf("**Not costed**, and no %s count in the inventory — shares only.", a.Pool.ReplicaNoun)
	}

	p.Headline = costHeadline(a)

	for _, t := range a.Tenants {
		row := mdCostRow{
			Tenant:   t.Tenant,
			Driver:   driverCell(t.Driver, a.Unit),
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

	p.Total = mdCostRow{Tenant: "Total", Driver: driverCell(a.Total, a.Unit), Share: "100%", Replicas: "—"}
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
		return fmt.Sprintf("No tenant reported any %s in this window.", a.DriverKind)
	}
	top := a.Tenants[0]
	if top.Share == nil {
		return fmt.Sprintf("Every measured tenant reported 0 %s, so there is no share to state.", a.DriverKind)
	}
	s := fmt.Sprintf("**`%s` uses %s of %s**", top.Tenant, percent(*top.Share), a.Pool.Name)
	if top.EquivalentReplicas != nil && a.Replicas != nil {
		s += fmt.Sprintf(" — the equivalent of %.1f of your %s", *top.EquivalentReplicas, counted(*a.Replicas, a.Pool.ReplicaNoun))
	}
	if top.Cost != nil {
		s += fmt.Sprintf(", %s %s/month", money(*top.Cost), a.Currency)
	}
	return s + "."
}

// componentCapacity describes a component-priced pool: what was priced,
// what it comes to, and — because a partial list prices a lower bound —
// what was left out.
func componentCapacity(a cost.PoolAllocation) string {
	var parts []string
	for _, c := range a.Components {
		if c.CostMonth == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d × %s", *c.Replicas, c.Name))
	}
	if len(parts) == 0 {
		return "**Not costed** — no component has both a replica count and a price in the inventory — shares only."
	}
	out := fmt.Sprintf("Pool: %s, %s %s/month.", strings.Join(parts, ", "), money(*a.PoolCostMonth), a.Currency)
	if len(a.UnpricedComponents) > 0 {
		out += " **Not priced:** " + strings.Join(a.UnpricedComponents, ", ") + "."
	}
	return out
}

// costMDCrossPool builds the cross-pool section. The section is skipped
// when a single pool was costed: a "total" over one pool is that pool's own
// table again, and repeating it would only invite reading it as more.
//
// Every percentage in it is stated together with what it covers (ADR 0003
// [A]); there is deliberately no code path here that prints a share of "the
// total" without the coverage sentence.
func costMDCrossPool(result CostResult) mdCrossPool {
	cp := result.CrossPool
	out := mdCrossPool{
		Show:       len(result.Pools) > 1,
		Currency:   cp.Currency,
		NotModeled: strings.Join(cp.NotModelled, ", "),
		Notes:      cp.Notes,
	}
	if !out.Show {
		return out
	}
	names := map[cost.PoolID]string{}
	for _, a := range result.Pools {
		names[a.Pool.ID] = a.Pool.Name
	}
	list := func(ids []cost.PoolID) []string {
		var l []string
		for _, id := range ids {
			l = append(l, names[id])
		}
		return l
	}

	// What is left out of the total, named, so the coverage is never just a
	// number.
	var left []string
	for _, n := range list(cp.Unpriced) {
		left = append(left, n+" not costed")
	}
	for _, n := range list(cp.Unmeasured) {
		left = append(left, n+" not measured")
	}

	if len(cp.Priced) == 0 {
		out.Statement = "**No pool is priced, so there is no cross-pool total** — each pool's shares above are complete within that pool, " +
			"but a percentage of the whole needs at least one price. Under `inventory.pools`, give a pool `replicas` and `cost_per_replica_month` " +
			"(or, for `query_path`, `components`)."
		if len(left) > 0 {
			out.Statement += " Left out: " + strings.Join(left, "; ") + "."
		}
		return out
	}

	partial := map[cost.PoolID][]string{}
	for _, pp := range cp.Partial {
		partial[pp.Pool] = pp.Unpriced
	}
	var pricedNames []string
	for _, id := range cp.Priced {
		n := names[id]
		if un := partial[id]; len(un) > 0 {
			n += " (without " + strings.Join(un, ", ") + ")"
		}
		pricedNames = append(pricedNames, n)
	}
	top := cp.Tenants[0]
	covers := strings.Join(pricedNames, ", ")
	if len(left) > 0 {
		covers += "; " + strings.Join(left, "; ")
	}
	if cp.PricedFraction != nil {
		out.Statement = fmt.Sprintf("**`%s` is %s of the %s of platform cost we can price** (%s).",
			top.Tenant, percent(top.Share), percent(*cp.PricedFraction), covers)
	} else {
		out.Statement = fmt.Sprintf("**`%s` is %s of the cost of the priced pools** (%s). "+
			"What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.",
			top.Tenant, percent(top.Share), covers)
	}

	for _, id := range cp.Priced {
		out.Headers = append(out.Headers, fmt.Sprintf("%s %s/month", capitalize(names[id]), cp.Currency))
	}
	lowerBound := false
	for _, t := range cp.Tenants {
		row := mdCrossRow{Tenant: t.Tenant, Total: money(t.CostMonth), Share: percent(t.Share)}
		byPool := map[cost.PoolID]float64{}
		for _, pc := range t.Pools {
			byPool[pc.Pool] = pc.CostMonth
		}
		missing := map[cost.PoolID]bool{}
		for _, id := range t.NotMeasuredIn {
			missing[id] = true
		}
		for _, id := range cp.Priced {
			if missing[id] {
				row.Cells = append(row.Cells, "not measured")
				continue
			}
			row.Cells = append(row.Cells, money(byPool[id]))
		}
		if len(t.NotMeasuredIn) > 0 {
			lowerBound = true
			row.Total = "≥ " + row.Total
			row.Share = "≥ " + row.Share
		}
		out.Rows = append(out.Rows, row)
	}
	if lowerBound {
		out.Notes = append(out.Notes, "a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, "+
			"and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated")
	}
	return out
}

// counted is "1 ruler" / "3 ingesters".
func counted(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func driverCell(v *float64, unit cost.Unit) string {
	if v == nil {
		return "withheld (no RF)"
	}
	switch unit {
	case cost.UnitBytes:
		return humanBytes(*v)
	case cost.UnitSeconds:
		return seconds(*v)
	default:
		return thousands(math.Round(*v))
	}
}

// humanBytes is IEC units to one decimal. Fetched volume spans bytes to
// terabytes across tenants, and a column of 52,940,813 is harder to compare
// than 50.5 MiB.
func humanBytes(v float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f B", v)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// seconds keeps one decimal: rule-evaluation and query time sit around
// single seconds per window on a quiet tenant, where rounding to whole
// seconds would erase the difference the report exists to show.
func seconds(v float64) string {
	whole := math.Floor(v)
	frac := math.Round((v-whole)*10) / 10
	if frac >= 1 {
		whole++
		frac = 0
	}
	return fmt.Sprintf("%s.%d", thousands(whole), int(math.Round(frac*10)))
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
