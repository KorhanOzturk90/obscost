// Package attribution joins observed rule executions against static rule
// definitions and rolls them up into ranked tenant/rule workload shares —
// the interpretation layer PRODUCT-DIRECTION.md separates from raw
// ingestion (internal/telemetry). Aggregate is pure: no I/O, no wall-clock
// reads, so its output is exactly unit-testable and its ordering is a
// documented postcondition, not incidental.
package attribution

import (
	"sort"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// RankMetric identifies which observed stat a Report ranks its tenants and
// rules by. Aggregate picks exactly one, report-wide, via pickRankMetric's
// priority order: a real resource-cost proxy (query wall time, then
// fetched-byte/series/chunk volume) is preferred whenever the telemetry
// source actually measured it, falling back to samples processed and
// finally to raw execution counts — a cadence/scheduling measure, not a
// resource-cost one, but the only thing every telemetry source can always
// provide. Every TenantAggregate/RuleAggregate still carries all of its raw
// sums regardless of which metric wins, so nothing observed is hidden by
// the choice; RankMetric only controls sort order and which figure callers
// should treat as "the" workload share.
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
func pickRankMetric(totalDuration float64, totalSamples, totalFetchedSeries, totalFetchedChunks, totalFetchedBytes uint64) RankMetric {
	switch {
	case totalDuration > 0:
		return RankMetricDurationSeconds
	case totalFetchedBytes > 0:
		return RankMetricFetchedBytes
	case totalFetchedSeries > 0:
		return RankMetricFetchedSeries
	case totalFetchedChunks > 0:
		return RankMetricFetchedChunks
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

// Report is the full ranked-workload result of one Aggregate call.
type Report struct {
	TotalExecutions int
	TotalSamples    uint64
	RuleDefinitions int
	// RankMetric is the single metric Tenants and each TenantAggregate's
	// Rules are sorted by — see RankMetric's doc comment.
	RankMetric RankMetric
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
	UnmatchedExecutions   int             `json:"unmatched_executions,omitempty"`
	UnmatchedSamples      uint64          `json:"unmatched_samples,omitempty"`
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
		RankMetric:      rankMetric,
		Tenants:         tenants,
		Unmatched:       unmatched,
	}
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
