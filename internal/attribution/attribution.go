// Package attribution joins observed rule executions against static rule
// definitions and rolls them up into ranked tenant/rule workload shares —
// the interpretation layer PRODUCT-DIRECTION.md separates from raw
// ingestion (internal/telemetry). Aggregate is pure: no I/O, no wall-clock
// reads, so its output is exactly unit-testable and its ordering is a
// documented postcondition, not incidental.
package attribution

import (
	"math"
	"sort"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// RankMetric identifies which observed stat a Report ranks its tenants and
// rules by. Aggregate picks exactly one, report-wide, via pickRankMetric's
// priority order: data volume actually moved (fetched bytes, then chunks,
// then series) is preferred whenever the telemetry source measured it,
// falling back to query wall time, then samples processed, and finally to
// raw execution counts — a cadence/scheduling measure, not a resource-cost
// one, but the only thing every telemetry source can always provide. See
// pickRankMetric for why volume outranks time. Every TenantAggregate/
// RuleAggregate still carries all of its raw sums regardless of which
// metric wins, so nothing observed is hidden by the choice; RankMetric only
// controls sort order and which figure callers should treat as "the"
// workload share.
type RankMetric string

const (
	RankMetricDurationSeconds  RankMetric = "duration_seconds"
	RankMetricFetchedBytes     RankMetric = "fetched_bytes"
	RankMetricFetchedSeries    RankMetric = "fetched_series"
	RankMetricFetchedChunks    RankMetric = "fetched_chunks"
	RankMetricSamplesProcessed RankMetric = "samples_processed"
	RankMetricExecutions       RankMetric = "executions"
)

// Label returns a short, human-readable name for the metric, suitable for
// report headers and column labels.
func (m RankMetric) Label() string {
	switch m {
	case RankMetricDurationSeconds:
		return "query wall time"
	case RankMetricFetchedBytes:
		return "fetched bytes"
	case RankMetricFetchedSeries:
		return "fetched series"
	case RankMetricFetchedChunks:
		return "fetched chunks"
	case RankMetricSamplesProcessed:
		return "samples processed"
	default:
		return "executions"
	}
}

// pickRankMetric chooses the single report-wide RankMetric from report-wide
// totals, in priority order from "best resource-cost proxy" to "always
// available, but only a cadence measure". The first metric with a nonzero
// total wins; RankMetricExecutions is the final fallback since Executions
// is never nil (an execution either happened or it didn't).
//
// Data volume outranks wall time deliberately (ADR 0002 decision 2). Two
// reasons, both measured against dev/mimir-local rather than assumed:
//
//   - Wall time is the one dimension Mimir already publishes as an ordinary
//     metric (cortex_prometheus_rule_evaluation_duration_seconds_sum), so
//     ranking by it spends the ruler log's only differentiated signal on a
//     number that was already available for free.
//   - It measures the wrong pressure. In a real 240-rule sample, 186 rules
//     fetched zero series yet accounted for 68.4% of total wall time, while
//     the rule pulling the largest share of fetched series (14.9%) ranked
//     22nd by time. Time is felt by the ruler's own CPU; fetched volume is
//     felt by ingesters and store-gateways, which is usually where capacity
//     actually binds.
//
// Wall time stays in the chain — it is still real, and still the right
// answer when no volume stat was measured at all.
func pickRankMetric(totalDuration float64, totalSamples, totalFetchedSeries, totalFetchedChunks, totalFetchedBytes uint64) RankMetric {
	switch {
	case totalFetchedBytes > 0:
		return RankMetricFetchedBytes
	case totalFetchedChunks > 0:
		return RankMetricFetchedChunks
	case totalFetchedSeries > 0:
		return RankMetricFetchedSeries
	case totalDuration > 0:
		return RankMetricDurationSeconds
	case totalSamples > 0:
		return RankMetricSamplesProcessed
	default:
		return RankMetricExecutions
	}
}

// metricValue extracts the raw value of one metric from a set of summed
// stats, shared by both TenantAggregate and RuleAggregate (which carry the
// same five stat sums plus Executions).
func metricValue(m RankMetric, executions int, samples uint64, duration float64, series, chunks, bytesVal uint64) float64 {
	switch m {
	case RankMetricDurationSeconds:
		return duration
	case RankMetricFetchedBytes:
		return float64(bytesVal)
	case RankMetricFetchedSeries:
		return float64(series)
	case RankMetricFetchedChunks:
		return float64(chunks)
	case RankMetricSamplesProcessed:
		return float64(samples)
	default:
		return float64(executions)
	}
}

// Granularity describes how deep a Report's figures reach. It exists so a
// renderer can tell two very different situations apart: "we looked for
// rule-level detail and there wasn't any" versus "this source structurally
// cannot see rules".
type Granularity string

const (
	// GranularityRule means every figure is attributed to an individual
	// rule — what a per-execution telemetry source (internal/telemetry)
	// produces. An empty TenantAggregate.Rules here genuinely means no
	// matched rule executions.
	GranularityRule Granularity = "rule"
	// GranularityGroup means figures stop at the rule-group tier because
	// the source itself stops there. Mimir's own rule metrics carry a
	// rule_group label but no rule name, so no amount of post-processing
	// reaches deeper (ADR 0002). An empty TenantAggregate.Rules here means
	// "this source cannot see rules", NOT "no rules ran" — reporting it as
	// the latter would be the same class of lie as treating an unmeasured
	// stat as zero.
	GranularityGroup Granularity = "group"
)

// Report is the full ranked-workload result of one Aggregate call.
type Report struct {
	TotalExecutions int
	TotalSamples    uint64
	RuleDefinitions int
	// Granularity is how deep these figures reach — see Granularity.
	Granularity Granularity
	// RankMetric is the single metric Tenants and each TenantAggregate's
	// Rules are sorted by — see RankMetric's doc comment.
	RankMetric RankMetric
	// GroupRankMetric is the metric the group tier is ranked by. Usually
	// identical to RankMetric, but a source can measure the two tiers
	// differently: Mimir's rule metrics report per-tenant wall time but
	// only per-group evaluation counts, so tenants rank by time while
	// groups beneath them rank by count. Naming it separately keeps the
	// renderer from labelling a group column with the tenant's metric.
	GroupRankMetric RankMetric
	// Tenants is sorted by RankValue desc, tie-break Tenant asc.
	Tenants []TenantAggregate
	// Unmatched is a flattened, report-level view of every execution that
	// didn't join to a known rule definition, across all tenants — sorted
	// by RuleID().String() then Timestamp asc. Each such execution is also
	// reflected in its own tenant's UnmatchedExecutions/UnmatchedSamples.
	Unmatched []rule.RuleExecution
}

// TenantAggregate is one tenant's observed workload. Grouping is keyed by
// each RuleExecution's own Tenant field directly: every execution with
// that Tenant counts toward Executions/SamplesProcessed here, whether or
// not it joined to a known rule definition — an execution's tenant is an
// observed fact independent of whether we can match it to a definition.
// This is what makes sum(TenantAggregate.Executions) == Report.
// TotalExecutions hold by construction.
type TenantAggregate struct {
	Tenant string `json:"tenant"`
	// RuleCount is the number of distinct rule DEFINITIONS this tenant
	// owns (from Aggregate's definitions argument), independent of
	// whether any of them have executions. NOT a count of executed rules.
	RuleCount          int     `json:"rule_count"`
	Executions         int     `json:"executions"`
	ExecutionSharePct  float64 `json:"execution_share_pct"` // share of Report.TotalExecutions; 0 (never NaN) if that's 0
	SamplesProcessed   uint64  `json:"samples_processed"`
	SampleSharePct     float64 `json:"sample_share_pct"` // share of Report.TotalSamples; 0 (never NaN) if that's 0
	DurationSecondsSum float64 `json:"duration_seconds_sum"`
	FetchedSeries      uint64  `json:"fetched_series"`
	FetchedChunks      uint64  `json:"fetched_chunks"`
	FetchedBytes       uint64  `json:"fetched_bytes"`
	// RankValue/RankSharePct are this tenant's value and report-wide share
	// for Report.RankMetric specifically — the figure Tenants is sorted
	// by. Every other stat above is still the true observed sum for that
	// stat regardless of which one is "the" rank metric.
	RankValue    float64 `json:"rank_value"`
	RankSharePct float64 `json:"rank_share_pct"`
	// The *Observed fields distinguish "measured, genuinely zero" from
	// "never measured" for each stat above — true if at least one
	// execution counted toward this tenant (matched or not) carried a
	// non-nil value for that stat. A stat summing to 0 with its Observed
	// flag false must be rendered as "not available", never as "0" (see
	// rule.RuleExecution's doc comment on why fabricating zero is unsafe).
	SamplesObserved       bool            `json:"samples_observed"`
	DurationObserved      bool            `json:"duration_observed"`
	FetchedSeriesObserved bool            `json:"fetched_series_observed"`
	FetchedChunksObserved bool            `json:"fetched_chunks_observed"`
	FetchedBytesObserved  bool            `json:"fetched_bytes_observed"`
	Rules                 []RuleAggregate `json:"rules,omitempty"`
	// Groups is the rule-group tier, sorted by RankValue desc, tie-break
	// Group asc. For a rule-granularity report it is derived by bucketing
	// Rules (every rule appears under exactly one group, so group sums
	// reconcile to the tenant total by construction). For a
	// group-granularity report it is the deepest tier there is, and each
	// GroupAggregate.Rules is empty.
	Groups              []GroupAggregate `json:"groups,omitempty"`
	UnmatchedExecutions int              `json:"unmatched_executions,omitempty"`
	UnmatchedSamples    uint64           `json:"unmatched_samples,omitempty"`
}

// Observed reports whether this tenant's telemetry ever measured the given
// metric at all (as opposed to measuring it as zero, or never reporting it
// — see the *Observed fields' doc comment). Executions is always observed:
// an execution either happened or it isn't counted, there's no "unmeasured
// execution count".
func (t TenantAggregate) Observed(m RankMetric) bool {
	switch m {
	case RankMetricDurationSeconds:
		return t.DurationObserved
	case RankMetricFetchedBytes:
		return t.FetchedBytesObserved
	case RankMetricFetchedSeries:
		return t.FetchedSeriesObserved
	case RankMetricFetchedChunks:
		return t.FetchedChunksObserved
	case RankMetricSamplesProcessed:
		return t.SamplesObserved
	default:
		return true
	}
}

// GroupAggregate is one rule group's observed workload, nested under its
// tenant. Deliberately leaner than TenantAggregate/RuleAggregate: it carries
// the ranking figure and the rules beneath it, not a full re-sum of every
// stat. For a rule-granularity report the per-stat detail already lives on
// Rules; for a group-granularity report that detail does not exist at all,
// and inventing zero-valued stat fields to fill the struct would misrepresent
// unmeasured data as measured (the same principle as RuleExecution's pointer
// stat fields).
type GroupAggregate struct {
	// Namespace is the rule file the group came from. Mimir's own metrics
	// report this as a ruler storage path; internal/telemetry sources
	// report it as the loaded rule file. Either way it disambiguates two
	// groups that share a name across different files.
	Namespace string `json:"namespace,omitempty"`
	Group     string `json:"group"`
	// Executions is the number of rule evaluations attributed to this
	// group. Sourced from metrics this is a PromQL increase() figure, which
	// extrapolates at window edges — see AggregateObservations.
	Executions int     `json:"executions"`
	RankValue  float64 `json:"rank_value"`
	// RankSharePct is this group's share of the sum of Report.GroupRankMetric
	// across this tenant's *reported* groups — not of the tenant's own
	// total (TenantAggregate.RankValue), which on the metrics path comes
	// from a different PromQL counter and is not guaranteed to agree with
	// the sum of groups. Rendered as "of tenant's reported groups", not
	// "of tenant", so the report doesn't claim a reconciliation nothing
	// computes.
	RankSharePct float64 `json:"rank_share_pct"`
	// RankObserved is false when this group never measured the report's
	// group rank metric, so a 0 RankValue must render as "not measured"
	// rather than as an observed zero.
	RankObserved bool `json:"rank_observed"`
	// Rules is empty for a GranularityGroup report — meaning "not visible
	// to this source", never "none ran".
	Rules []RuleAggregate `json:"rules,omitempty"`
}

// RuleAggregate is one rule's observed workload, nested under its tenant.
// Carries RuleID + Kind, never the whole AnnotatedRule/AST — parser.Expr
// has no safe JSON encoding and RuleID is already sufficient to look up
// anything else the caller needs.
type RuleAggregate struct {
	RuleID             rule.RuleID `json:"rule_id"`
	Kind               rule.Kind   `json:"kind"`
	Executions         int         `json:"executions"`
	ExecutionSharePct  float64     `json:"execution_share_pct"` // share of THIS TENANT's executions; 0 if that's 0
	SamplesProcessed   uint64      `json:"samples_processed"`
	SampleSharePct     float64     `json:"sample_share_pct"` // share of THIS TENANT's samples; 0 if that's 0
	DurationSecondsSum float64     `json:"duration_seconds_sum"`
	FetchedSeries      uint64      `json:"fetched_series"`
	FetchedChunks      uint64      `json:"fetched_chunks"`
	FetchedBytes       uint64      `json:"fetched_bytes"`
	// RankValue/RankSharePct are this rule's value and share of its
	// TENANT's total for Report.RankMetric — the figure a tenant's Rules
	// are sorted by (see TenantAggregate.RankValue's doc comment).
	RankValue    float64 `json:"rank_value"`
	RankSharePct float64 `json:"rank_share_pct"`
	// RankObserved is Observed(Report.RankMetric) resolved at aggregation
	// time, so consumers that roll rules up (the group tier) don't have to
	// re-thread the report's chosen metric to ask the question again.
	RankObserved bool `json:"rank_observed"`
	// See TenantAggregate's identically-named fields for what these mean.
	SamplesObserved       bool `json:"samples_observed"`
	DurationObserved      bool `json:"duration_observed"`
	FetchedSeriesObserved bool `json:"fetched_series_observed"`
	FetchedChunksObserved bool `json:"fetched_chunks_observed"`
	FetchedBytesObserved  bool `json:"fetched_bytes_observed"`
}

// Observed reports whether this rule's telemetry ever measured the given
// metric at all — see TenantAggregate.Observed.
func (r RuleAggregate) Observed(m RankMetric) bool {
	switch m {
	case RankMetricDurationSeconds:
		return r.DurationObserved
	case RankMetricFetchedBytes:
		return r.FetchedBytesObserved
	case RankMetricFetchedSeries:
		return r.FetchedSeriesObserved
	case RankMetricFetchedChunks:
		return r.FetchedChunksObserved
	case RankMetricSamplesProcessed:
		return r.SamplesObserved
	default:
		return true
	}
}

// Aggregate rolls up raw executions into tenant- and rule-level workload
// aggregates, joined against definitions via rule.RuleID.
func Aggregate(executions []rule.RuleExecution, definitions []rule.AnnotatedRule) Report {
	defsByID := make(map[rule.RuleID]rule.AnnotatedRule, len(definitions))
	for _, d := range definitions {
		defsByID[d.RuleID()] = d
	}

	ruleDefsByTenant := make(map[string]map[rule.RuleID]struct{})
	for _, d := range definitions {
		id := d.RuleID()
		if ruleDefsByTenant[d.Tenant] == nil {
			ruleDefsByTenant[d.Tenant] = make(map[rule.RuleID]struct{})
		}
		ruleDefsByTenant[d.Tenant][id] = struct{}{}
	}

	executionsByTenant := make(map[string][]rule.RuleExecution)
	for _, e := range executions {
		executionsByTenant[e.Tenant] = append(executionsByTenant[e.Tenant], e)
	}

	tenantSet := make(map[string]struct{})
	for t := range ruleDefsByTenant {
		tenantSet[t] = struct{}{}
	}
	for t := range executionsByTenant {
		tenantSet[t] = struct{}{}
	}

	var totalExecutions int
	var totalSamples, totalFetchedSeries, totalFetchedChunks, totalFetchedBytes uint64
	var totalDuration float64
	for _, e := range executions {
		totalExecutions++
		totalSamples += valueOr(e.SamplesProcessed, 0)
		totalDuration += valueOr(e.DurationSeconds, 0)
		totalFetchedSeries += valueOr(e.FetchedSeries, 0)
		totalFetchedChunks += valueOr(e.FetchedChunks, 0)
		totalFetchedBytes += valueOr(e.FetchedBytes, 0)
	}
	rankMetric := pickRankMetric(totalDuration, totalSamples, totalFetchedSeries, totalFetchedChunks, totalFetchedBytes)

	var tenants []TenantAggregate
	var unmatched []rule.RuleExecution

	for tenant := range tenantSet {
		tenantExecutions := executionsByTenant[tenant]

		ruleAccum := make(map[rule.RuleID]*RuleAggregate)
		var tenantSamples, tenantFetchedSeries, tenantFetchedChunks, tenantFetchedBytes uint64
		var tenantDuration float64
		var tenantSamplesObserved, tenantDurationObserved, tenantSeriesObserved, tenantChunksObserved, tenantBytesObserved bool
		var unmatchedExecutions int
		var unmatchedSamples uint64

		for _, e := range tenantExecutions {
			id := e.RuleID()
			tenantSamples += valueOr(e.SamplesProcessed, 0)
			tenantDuration += valueOr(e.DurationSeconds, 0)
			tenantFetchedSeries += valueOr(e.FetchedSeries, 0)
			tenantFetchedChunks += valueOr(e.FetchedChunks, 0)
			tenantFetchedBytes += valueOr(e.FetchedBytes, 0)
			if e.SamplesProcessed != nil {
				tenantSamplesObserved = true
			}
			if e.DurationSeconds != nil {
				tenantDurationObserved = true
			}
			if e.FetchedSeries != nil {
				tenantSeriesObserved = true
			}
			if e.FetchedChunks != nil {
				tenantChunksObserved = true
			}
			if e.FetchedBytes != nil {
				tenantBytesObserved = true
			}

			def, ok := defsByID[id]
			if !ok {
				unmatchedExecutions++
				unmatchedSamples += valueOr(e.SamplesProcessed, 0)
				unmatched = append(unmatched, e)
				continue
			}

			ra, ok := ruleAccum[id]
			if !ok {
				ra = &RuleAggregate{RuleID: id, Kind: def.Kind}
				ruleAccum[id] = ra
			}
			ra.Executions++
			ra.SamplesProcessed += valueOr(e.SamplesProcessed, 0)
			ra.DurationSecondsSum += valueOr(e.DurationSeconds, 0)
			ra.FetchedSeries += valueOr(e.FetchedSeries, 0)
			ra.FetchedChunks += valueOr(e.FetchedChunks, 0)
			ra.FetchedBytes += valueOr(e.FetchedBytes, 0)
			if e.SamplesProcessed != nil {
				ra.SamplesObserved = true
			}
			if e.DurationSeconds != nil {
				ra.DurationObserved = true
			}
			if e.FetchedSeries != nil {
				ra.FetchedSeriesObserved = true
			}
			if e.FetchedChunks != nil {
				ra.FetchedChunksObserved = true
			}
			if e.FetchedBytes != nil {
				ra.FetchedBytesObserved = true
			}
		}

		tenantExecutionCount := len(tenantExecutions)
		tenantRankTotal := metricValue(rankMetric, tenantExecutionCount, tenantSamples, tenantDuration, tenantFetchedSeries, tenantFetchedChunks, tenantFetchedBytes)

		ruleAggs := make([]RuleAggregate, 0, len(ruleAccum))
		for _, ra := range ruleAccum {
			ra.ExecutionSharePct = pct(uint64(ra.Executions), uint64(tenantExecutionCount))
			ra.SampleSharePct = pct(ra.SamplesProcessed, tenantSamples)
			ra.RankValue = metricValue(rankMetric, ra.Executions, ra.SamplesProcessed, ra.DurationSecondsSum, ra.FetchedSeries, ra.FetchedChunks, ra.FetchedBytes)
			ra.RankSharePct = pctf(ra.RankValue, tenantRankTotal)
			ra.RankObserved = ra.Observed(rankMetric)
			ruleAggs = append(ruleAggs, *ra)
		}
		sort.Slice(ruleAggs, func(i, j int) bool {
			if ruleAggs[i].RankValue != ruleAggs[j].RankValue {
				return ruleAggs[i].RankValue > ruleAggs[j].RankValue
			}
			return ruleAggs[i].RuleID.String() < ruleAggs[j].RuleID.String()
		})

		tenants = append(tenants, TenantAggregate{
			Tenant:                tenant,
			RuleCount:             len(ruleDefsByTenant[tenant]),
			Executions:            tenantExecutionCount,
			ExecutionSharePct:     pct(uint64(tenantExecutionCount), uint64(totalExecutions)),
			SamplesProcessed:      tenantSamples,
			SampleSharePct:        pct(tenantSamples, totalSamples),
			DurationSecondsSum:    tenantDuration,
			FetchedSeries:         tenantFetchedSeries,
			FetchedChunks:         tenantFetchedChunks,
			FetchedBytes:          tenantFetchedBytes,
			SamplesObserved:       tenantSamplesObserved,
			DurationObserved:      tenantDurationObserved,
			FetchedSeriesObserved: tenantSeriesObserved,
			FetchedChunksObserved: tenantChunksObserved,
			FetchedBytesObserved:  tenantBytesObserved,
			Rules:                 ruleAggs,
			Groups:                groupRules(ruleAggs, tenantRankTotal),
			UnmatchedExecutions:   unmatchedExecutions,
			UnmatchedSamples:      unmatchedSamples,
		})
	}

	reportRankTotal := metricValue(rankMetric, totalExecutions, totalSamples, totalDuration, totalFetchedSeries, totalFetchedChunks, totalFetchedBytes)
	for i := range tenants {
		tenants[i].RankValue = metricValue(rankMetric, tenants[i].Executions, tenants[i].SamplesProcessed, tenants[i].DurationSecondsSum, tenants[i].FetchedSeries, tenants[i].FetchedChunks, tenants[i].FetchedBytes)
		tenants[i].RankSharePct = pctf(tenants[i].RankValue, reportRankTotal)
	}

	sort.Slice(tenants, func(i, j int) bool {
		if tenants[i].RankValue != tenants[j].RankValue {
			return tenants[i].RankValue > tenants[j].RankValue
		}
		return tenants[i].Tenant < tenants[j].Tenant
	})

	sort.Slice(unmatched, func(i, j int) bool {
		idI, idJ := unmatched[i].RuleID().String(), unmatched[j].RuleID().String()
		if idI != idJ {
			return idI < idJ
		}
		return unmatched[i].Timestamp.Before(unmatched[j].Timestamp)
	})

	return Report{
		TotalExecutions: totalExecutions,
		TotalSamples:    totalSamples,
		RuleDefinitions: len(definitions),
		Granularity:     GranularityRule,
		RankMetric:      rankMetric,
		GroupRankMetric: rankMetric,
		Tenants:         tenants,
		Unmatched:       unmatched,
	}
}

// TenantObservation is one tenant's workload as reported by a source that
// measures tenants directly rather than by summing individual executions —
// Mimir's own cortex_prometheus_rule_* metrics, in practice. See
// AggregateObservations.
type TenantObservation struct {
	Tenant string
	// Executions and DurationSeconds are totals over the observation
	// window. DurationObserved distinguishes "measured as zero" from "this
	// source didn't report it", exactly as the pointer stat fields on
	// rule.RuleExecution do for per-execution telemetry.
	Executions       float64
	DurationSeconds  float64
	DurationObserved bool
}

// GroupObservation is one rule group's workload from the same kind of
// source. There is deliberately no rule tier: a source reporting at this
// granularity cannot see individual rules at all (see GranularityGroup).
type GroupObservation struct {
	Tenant     string
	Namespace  string
	Group      string
	Executions float64
}

// AggregateObservations builds a group-granularity Report from figures a
// source measured directly, instead of from individual rule executions.
// This is the metrics path of ADR 0002: Mimir already publishes per-tenant
// rule evaluation time and per-group evaluation counts, exactly and with
// history, so deriving them by parsing and matching log lines would be
// strictly more work for a strictly worse answer.
//
// Two honesty constraints shape the signature:
//
//   - Tenants rank by wall time where it was measured, but groups can only
//     rank by evaluation count, because Mimir publishes no per-group
//     duration. Report.GroupRankMetric carries that difference rather than
//     letting the renderer imply the group column means what the tenant
//     column means.
//   - Executions arrive as float64 because they originate from PromQL
//     increase(), which extrapolates at window boundaries and so is very
//     slightly approximate. Rounding happens here, once, rather than each
//     caller silently deciding — and callers should present these as
//     evaluation counts over a window, not as an exact ledger.
//
// The wall-time ranking is contention-sensitive, and this path cannot do
// better. Rule-evaluation time depends on how loaded the cluster is, not
// only on what a tenant's rules ask for: on one run of the dev/mimir-k8s
// rig an untouched tenant's evaluation and query time roughly doubled while
// a neighbour's expensive rules ran, with nothing about its own rules
// changed (the next two runs saw ×1.15 and ×1.30, so the size of the effect
// is variable — a caveat to state, not a constant to correct for). It is
// still the ranking here because Mimir's rule metrics carry
// no measure of data volume (ADR 0002), so there is nothing better to rank
// by — which is why the caveat is stated in the rendered report
// (report.WorkloadResult.SourceNote) instead of the ranking being changed. `promcost cost`
// avoids the problem where it can, by pricing the query path on fetched
// volume and using time only as a flagged fallback.
func AggregateObservations(tenantObs []TenantObservation, groupObs []GroupObservation) Report {
	groupsByTenant := make(map[string][]GroupObservation)
	for _, g := range groupObs {
		groupsByTenant[g.Tenant] = append(groupsByTenant[g.Tenant], g)
	}

	// A tenant seen only in the group query still deserves a row; dropping
	// it would understate the fleet.
	seen := make(map[string]struct{}, len(tenantObs))
	for _, t := range tenantObs {
		seen[t.Tenant] = struct{}{}
	}
	for tenant := range groupsByTenant {
		if _, ok := seen[tenant]; !ok {
			tenantObs = append(tenantObs, TenantObservation{Tenant: tenant})
		}
	}

	var anyDuration bool
	for _, t := range tenantObs {
		if t.DurationObserved && t.DurationSeconds > 0 {
			anyDuration = true
			break
		}
	}
	rankMetric := RankMetricExecutions
	if anyDuration {
		rankMetric = RankMetricDurationSeconds
	}

	var totalExecutions int
	var reportRankTotal float64
	for _, t := range tenantObs {
		totalExecutions += int(math.Round(t.Executions))
		if anyDuration {
			reportRankTotal += t.DurationSeconds
		} else {
			reportRankTotal += t.Executions
		}
	}

	tenants := make([]TenantAggregate, 0, len(tenantObs))
	for _, t := range tenantObs {
		executions := int(math.Round(t.Executions))

		var tenantGroupTotal float64
		for _, g := range groupsByTenant[t.Tenant] {
			tenantGroupTotal += g.Executions
		}
		groups := make([]GroupAggregate, 0, len(groupsByTenant[t.Tenant]))
		for _, g := range groupsByTenant[t.Tenant] {
			groups = append(groups, GroupAggregate{
				Namespace:    g.Namespace,
				Group:        g.Group,
				Executions:   int(math.Round(g.Executions)),
				RankValue:    g.Executions,
				RankSharePct: pctf(g.Executions, tenantGroupTotal),
				RankObserved: true,
			})
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].RankValue != groups[j].RankValue {
				return groups[i].RankValue > groups[j].RankValue
			}
			if groups[i].Group != groups[j].Group {
				return groups[i].Group < groups[j].Group
			}
			return groups[i].Namespace < groups[j].Namespace
		})

		rankValue := t.Executions
		if anyDuration {
			rankValue = t.DurationSeconds
		}
		tenants = append(tenants, TenantAggregate{
			Tenant:             t.Tenant,
			Executions:         executions,
			ExecutionSharePct:  pct(uint64(executions), uint64(totalExecutions)),
			DurationSecondsSum: t.DurationSeconds,
			DurationObserved:   t.DurationObserved,
			RankValue:          rankValue,
			RankSharePct:       pctf(rankValue, reportRankTotal),
			Groups:             groups,
		})
	}

	sort.Slice(tenants, func(i, j int) bool {
		if tenants[i].RankValue != tenants[j].RankValue {
			return tenants[i].RankValue > tenants[j].RankValue
		}
		return tenants[i].Tenant < tenants[j].Tenant
	})

	return Report{
		TotalExecutions: totalExecutions,
		Granularity:     GranularityGroup,
		RankMetric:      rankMetric,
		GroupRankMetric: RankMetricExecutions,
		Tenants:         tenants,
	}
}

// groupRules buckets one tenant's already-ranked rules into the group tier.
// Rules arrive sorted by RankValue desc, and bucketing preserves relative
// order within a bucket, so each group's Rules stay correctly ranked without
// re-sorting. Group RankValue is the sum of its rules' — which means group
// shares reconcile to the tenant total by construction rather than by a
// second, independently-computed pass that could drift from it.
//
// This lived in internal/report's HTML renderer until ADR 0002 made the
// group tier first-class: a metrics-derived report has groups but no rules
// at all, so the tier can no longer be a presentation-time derivation of
// something deeper.
func groupRules(rules []RuleAggregate, tenantRankTotal float64) []GroupAggregate {
	if len(rules) == 0 {
		return nil
	}

	byGroup := make(map[string]*GroupAggregate)
	var order []*GroupAggregate
	for _, r := range rules {
		// Namespace is part of the key: two files can define groups with
		// the same name, and collapsing them would silently merge the
		// workload of unrelated groups.
		key := r.RuleID.Namespace + "\x00" + r.RuleID.Group
		g, ok := byGroup[key]
		if !ok {
			g = &GroupAggregate{Namespace: r.RuleID.Namespace, Group: r.RuleID.Group}
			byGroup[key] = g
			order = append(order, g)
		}
		g.Executions += r.Executions
		g.RankValue += r.RankValue
		if r.RankObserved {
			g.RankObserved = true
		}
		g.Rules = append(g.Rules, r)
	}

	groups := make([]GroupAggregate, 0, len(order))
	for _, g := range order {
		g.RankSharePct = pctf(g.RankValue, tenantRankTotal)
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].RankValue != groups[j].RankValue {
			return groups[i].RankValue > groups[j].RankValue
		}
		if groups[i].Group != groups[j].Group {
			return groups[i].Group < groups[j].Group
		}
		return groups[i].Namespace < groups[j].Namespace
	})
	return groups
}

// pct returns 100*part/total, or 0 (never NaN) when total is 0.
// encoding/json hard-errors on NaN/Inf float64 values, so this guard is a
// correctness requirement for any zero-workload report, not just a nicety.
func pct(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(part) / float64(total)
}

// pctf is pct's float64 counterpart, used for RankValue shares since
// RankMetricDurationSeconds's underlying value is itself a float64.
func pctf(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * part / total
}

// valueOr returns *p, or fallback if p is nil. Every stat sum in Aggregate
// goes through this: a RuleExecution whose source didn't measure a given
// stat (nil) contributes nothing to that sum, rather than the sum silently
// treating "unknown" as "zero". This is a deliberate, documented tradeoff:
// aggregate totals are "sum of what was observed", not "sum with unknowns
// backfilled as zero" — a rule evaluated entirely via Mimir's remote
// query-frontend path (see internal/telemetry/mimirlogs) may report
// Executions > 0 with SamplesProcessed/FetchedSeries/etc. under-counted
// relative to reality if some of its executions carried no stats at all,
// rather than the totals being wrong in the other, more dangerous
// direction (fabricating workload that was never observed).
func valueOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}
