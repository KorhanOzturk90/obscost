// Package ruleoutput joins recording rules to the series they write
// (issue #37): a recording rule's `record:` field is the name of the
// metric it produces, so the series it created can be counted directly in
// the owning tenant's TSDB. That gives rule -> output series -> share of
// the tenant's active series, a sub-tenant driver of ADR 0003's pool 1
// that names a rule, which label-based attribution cannot do.
//
// Sizing, so this is not over-sold: ADR 0003 [A] measured rule output at
// about 3.6% of ingestion for a mixin-shaped corpus. Rule output is one
// input to pool 1 whose size depends on the corpus. It is not presumed to
// be the main ingestion driver. What this join adds is the ability to name
// *which* rule, which matters most in the failure case of a recording rule
// that aggregates `by` high-cardinality labels.
//
// Count (count.go) does the I/O; Join is pure. Four outcomes are kept
// apart and never folded into each other:
//
//   - series: the output metric has N > 0 series right now
//   - no_series: it was counted and has none right now (never evaluated,
//     evaluates to an empty vector, or its selectors match nothing). This
//     is a measured zero.
//   - not_measured: no count exists (the query failed, or the rule has no
//     tenant to query as). Series is nil, never 0.
//   - no_output: an alerting rule. It writes no series by design, so it
//     has no output-series figure at all. That does not make it free: its
//     cost is ruler CPU and query load, which other pools carry.
package ruleoutput

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// Status is what is known about one rule's output.
type Status string

const (
	StatusSeries      Status = "series"
	StatusNoSeries    Status = "no_series"
	StatusNotMeasured Status = "not_measured"
	StatusNoOutput    Status = "no_output"
)

// RuleOutput is one rule joined to its output metric.
type RuleOutput struct {
	Namespace string    `json:"namespace,omitempty"`
	Group     string    `json:"group"`
	Rule      string    `json:"rule"`
	Kind      rule.Kind `json:"kind"`
	// OutputMetric is the recording rule's record: name. Empty for an
	// alerting rule.
	OutputMetric string `json:"output_metric,omitempty"`
	Status       Status `json:"status"`
	// Series is the output metric's point-in-time series count. nil when
	// not measured and for alerting rules; a pointer to 0 for no_series.
	Series *int64 `json:"series,omitempty"`
	// ShareOfTenantActiveSeries is Series over the tenant's active series
	// (pool 1's figure, deduplicated by the replication factor). nil when
	// either side is unknown.
	ShareOfTenantActiveSeries *float64 `json:"share_of_tenant_active_series,omitempty"`
	// SharedWith lists the other rules in this tenant that record the same
	// metric name. When non-empty, Series is the metric's total and cannot
	// be split between the rules that write it, so it is not this rule's
	// alone.
	SharedWith []string `json:"shared_with,omitempty"`
	// Reason explains a not_measured, no_series or no_output status.
	Reason string `json:"reason,omitempty"`
}

// TenantOutputs is one tenant's rules and their output.
type TenantOutputs struct {
	Tenant         string `json:"tenant"`
	RecordingRules int    `json:"recording_rules"`
	AlertingRules  int    `json:"alerting_rules"`
	// OutputMetrics counts distinct record: names. It can be lower than
	// RecordingRules, because several rules may record the same name.
	OutputMetrics      int `json:"output_metrics"`
	NotMeasuredMetrics int `json:"not_measured_metrics"`
	// OutputSeries sums Series over distinct measured output metrics, so a
	// metric shared by several rules is counted once. nil when no output
	// metric was measured. When NotMeasuredMetrics > 0 it is a lower
	// bound, and OutputSeriesComplete is false.
	OutputSeries         *int64 `json:"output_series,omitempty"`
	OutputSeriesComplete bool   `json:"output_series_complete"`
	// ActiveSeries is the denominator: the tenant's active series from
	// pool 1, divided by the replication factor. nil if unknown.
	ActiveSeries        *float64     `json:"active_series,omitempty"`
	ShareOfActiveSeries *float64     `json:"share_of_active_series,omitempty"`
	Rules               []RuleOutput `json:"rules"`
}

// Report is the join for every tenant.
type Report struct {
	// MeasuredAt is when the counts were taken. They are point-in-time
	// figures, not averages over a window.
	MeasuredAt time.Time       `json:"measured_at"`
	CountQuery string          `json:"count_query"`
	Queries    int             `json:"queries_issued"`
	Tenants    []TenantOutputs `json:"tenants"`
	// Assumptions uses the cost package's assumption-trail shape so the
	// share can be traced to its denominator and replication factor.
	Assumptions []cost.Assumption `json:"assumptions"`
	Notes       []string          `json:"notes,omitempty"`
}

// Denominator is each tenant's active series, deduplicated, for turning an
// output-series count into a share.
type Denominator struct {
	// PerTenant holds each tenant's active series already divided by the
	// replication factor. A tenant absent from it gets no share.
	PerTenant map[string]float64
	// Description says what PerTenant is, for the assumption trail.
	Description string
	// ReplicationFactor and RFSource are copied from the pool's own
	// assumption trail, so the report states which divisor was used.
	ReplicationFactor string
	RFSource          string
	// Withheld explains why PerTenant is empty, if it is.
	Withheld string
}

// DenominatorFromPool takes pool 1's allocation as the denominator. It
// uses TenantShare.Driver, the figure already divided by whichever
// replication factor internal/cost resolved (the inventory override, or
// cortex_distributor_replication_factor, or 1 under ingest storage), so
// this package applies no replication logic of its own.
//
// The replication factor matters here where it did not for pool 1's
// shares: a count() through the query path is deduplicated by the
// querier, while the raw active-series sum counts each series once per
// replica. The two sides only compare once the denominator is divided, so
// with no known replication factor the share is withheld rather than
// computed against the raw sum.
func DenominatorFromPool(a cost.PoolAllocation) Denominator {
	d := Denominator{PerTenant: map[string]float64{}}
	for _, as := range a.Assumptions {
		if as.Name == cost.AssumptionReplicationFactor {
			d.ReplicationFactor = as.Value
			d.RFSource = as.Source
		}
	}
	for _, t := range a.Tenants {
		if t.Driver != nil {
			d.PerTenant[t.Tenant] = *t.Driver
		}
	}
	d.Description = fmt.Sprintf("%s (%s) per tenant, averaged over the %s ending %s, divided by the replication factor",
		a.Pool.Driver, a.Pool.DriverUnit, a.WindowText, a.End.UTC().Format(time.RFC3339))
	if a.ReplicationFactor == nil {
		d.Withheld = "no replication factor is known, so the tenant's deduplicated active series are unknown and no share of them is computed"
	}
	return d
}

const noTenantKey = ""

// Join resolves each rule to its output metric and attaches counts.
// denom may be nil, in which case no shares are computed.
func Join(rules []rule.AnnotatedRule, counts Counts, denom *Denominator) Report {
	r := Report{
		MeasuredAt: counts.At,
		CountQuery: counts.QueryShape,
		Queries:    counts.Queries,
	}

	byTenant := map[string][]rule.AnnotatedRule{}
	for _, ar := range rules {
		byTenant[ar.Tenant] = append(byTenant[ar.Tenant], ar)
	}

	for tenant, trules := range byTenant {
		t := TenantOutputs{Tenant: tenant, OutputSeriesComplete: true}
		if denom != nil {
			if v, ok := denom.PerTenant[tenant]; ok {
				t.ActiveSeries = &v
			}
		}

		// writers maps each output metric to the rules that record it.
		writers := map[string][]string{}
		for _, ar := range trules {
			if ar.Kind == rule.KindRecording && ar.Record != "" {
				writers[ar.Record] = append(writers[ar.Record], ruleRef(ar))
			}
		}

		tc := counts.Tenants[tenant]
		for _, ar := range trules {
			ro := RuleOutput{
				Namespace: ar.Location.File,
				Group:     ar.Group.Name,
				Rule:      ar.Name(),
				Kind:      ar.Kind,
			}
			if ar.Kind != rule.KindRecording {
				t.AlertingRules++
				ro.Status = StatusNoOutput
				ro.Reason = "alerting rule: writes no series (its cost is ruler CPU and query load, not ingester memory)"
				t.Rules = append(t.Rules, ro)
				continue
			}
			t.RecordingRules++
			ro.OutputMetric = ar.Record
			for _, w := range writers[ar.Record] {
				if w != ruleRef(ar) {
					ro.SharedWith = append(ro.SharedWith, w)
				}
			}

			c, ok := tc[ar.Record]
			switch {
			case tenant == noTenantKey:
				ro.Status = StatusNotMeasured
				ro.Reason = "no tenant was resolved for this rule, so there is no tenant to count its output in"
			case !ok:
				ro.Status = StatusNotMeasured
				ro.Reason = "not queried"
			case c.Series == nil:
				ro.Status = StatusNotMeasured
				ro.Reason = c.Err
			case *c.Series == 0:
				ro.Status = StatusNoSeries
				ro.Series = c.Series
				ro.Reason = "no series right now: never evaluated, evaluates to an empty vector, or its selectors match nothing"
			default:
				ro.Status = StatusSeries
				ro.Series = c.Series
			}
			if ro.Series != nil && t.ActiveSeries != nil && *t.ActiveSeries > 0 {
				s := float64(*ro.Series) / *t.ActiveSeries
				ro.ShareOfTenantActiveSeries = &s
			}
			t.Rules = append(t.Rules, ro)
		}

		// Tenant totals count each distinct output metric once.
		t.OutputMetrics = len(writers)
		for metric := range writers {
			c, ok := tc[metric]
			if tenant == noTenantKey || !ok || c.Series == nil {
				t.NotMeasuredMetrics++
				t.OutputSeriesComplete = false
				continue
			}
			if t.OutputSeries == nil {
				var z int64
				t.OutputSeries = &z
			}
			*t.OutputSeries += *c.Series
		}
		if t.OutputMetrics == 0 {
			t.OutputSeriesComplete = false
		}
		if t.OutputSeries != nil && t.ActiveSeries != nil && *t.ActiveSeries > 0 {
			s := float64(*t.OutputSeries) / *t.ActiveSeries
			t.ShareOfActiveSeries = &s
		}

		sort.SliceStable(t.Rules, func(i, j int) bool { return lessRule(t.Rules[i], t.Rules[j]) })
		r.Tenants = append(r.Tenants, t)
	}

	sort.Slice(r.Tenants, func(i, j int) bool {
		a, b := r.Tenants[i], r.Tenants[j]
		if (a.OutputSeries == nil) != (b.OutputSeries == nil) {
			return a.OutputSeries != nil
		}
		if a.OutputSeries != nil && *a.OutputSeries != *b.OutputSeries {
			return *a.OutputSeries > *b.OutputSeries
		}
		return a.Tenant < b.Tenant
	})

	r.Assumptions = assumptions(counts, denom)
	r.Notes = notes(r, denom)
	return r
}

func assumptions(counts Counts, denom *Denominator) []cost.Assumption {
	src := "Mimir query API, queried as each rule's own tenant"
	as := []cost.Assumption{
		{
			Name:   "output metric",
			Value:  "a recording rule's record: name is the metric it writes",
			Source: "rule definitions",
		},
		{
			Name:   "series count",
			Value:  fmt.Sprintf("point-in-time, at %s; not an average over the cost window", counts.At.UTC().Format(time.RFC3339)),
			Source: src,
		},
		{Name: "count query", Value: counts.QueryShape, Source: src},
		{
			Name:   "what is counted",
			Value:  "every series named after the rule's record: name in the tenant, including any written by something other than this rule",
			Source: "PromQL semantics",
		},
		{
			Name:   "presence",
			Value:  "an instant query counts series with a sample inside the lookback window (5m by default), which is close to, but not the same as, the ingester's active-series idle timeout",
			Source: "PromQL semantics",
		},
	}
	if denom == nil {
		return append(as, cost.Assumption{Name: "denominator", Value: "none: no shares computed", Source: "not requested"})
	}
	as = append(as, cost.Assumption{Name: "denominator", Value: denom.Description, Source: "pool 1 (ingester memory) above"})
	if denom.ReplicationFactor != "" {
		as = append(as, cost.Assumption{Name: "replication factor", Value: denom.ReplicationFactor, Source: denom.RFSource})
	}
	return as
}

func notes(r Report, denom *Denominator) []string {
	var n []string
	if denom != nil && denom.Withheld != "" {
		n = append(n, denom.Withheld)
	}
	if denom != nil {
		n = append(n, "a share compares a point-in-time count with pool 1's window-average active series; for a rule added or changed during the window, read it as indicative")
	}
	shared := 0
	incomplete := 0
	for _, t := range r.Tenants {
		for _, ro := range t.Rules {
			if len(ro.SharedWith) > 0 {
				shared++
			}
		}
		if t.NotMeasuredMetrics > 0 {
			incomplete++
		}
	}
	if shared > 0 {
		n = append(n, fmt.Sprintf("%d recording rule(s) share their record: name with another rule in the same tenant; their count belongs to the metric and is not split between the rules", shared))
	}
	if incomplete > 0 {
		n = append(n, fmt.Sprintf("%d tenant(s) have output metrics that were not measured, so their output-series totals are lower bounds", incomplete))
	}
	return n
}

func ruleRef(ar rule.AnnotatedRule) string {
	parts := []string{}
	if ar.Location.File != "" {
		parts = append(parts, ar.Location.File)
	}
	parts = append(parts, ar.Group.Name, ar.Name())
	return strings.Join(parts, "/")
}

var statusOrder = map[Status]int{StatusSeries: 0, StatusNoSeries: 1, StatusNotMeasured: 2, StatusNoOutput: 3}

func lessRule(a, b RuleOutput) bool {
	if statusOrder[a.Status] != statusOrder[b.Status] {
		return statusOrder[a.Status] < statusOrder[b.Status]
	}
	if a.Series != nil && b.Series != nil && *a.Series != *b.Series {
		return *a.Series > *b.Series
	}
	if a.Namespace != b.Namespace {
		return a.Namespace < b.Namespace
	}
	if a.Group != b.Group {
		return a.Group < b.Group
	}
	return a.Rule < b.Rule
}

// Wanted returns, per tenant, the distinct output metric names Count
// should measure. Alerting rules and rules without a tenant contribute
// nothing.
func Wanted(rules []rule.AnnotatedRule) map[string][]string {
	want := map[string][]string{}
	for _, ar := range rules {
		if ar.Kind != rule.KindRecording || ar.Record == "" || ar.Tenant == noTenantKey {
			continue
		}
		want[ar.Tenant] = append(want[ar.Tenant], ar.Record)
	}
	return want
}
