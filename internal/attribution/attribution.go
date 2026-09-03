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

// Report is the full ranked-workload result of one Aggregate call.
type Report struct {
	TotalExecutions int
	TotalSamples    uint64
	RuleDefinitions int
	// Tenants is sorted by ExecutionSharePct desc, tie-break Tenant asc.
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
	RuleCount           int             `json:"rule_count"`
	Executions          int             `json:"executions"`
	ExecutionSharePct   float64         `json:"execution_share_pct"` // share of Report.TotalExecutions; 0 (never NaN) if that's 0
	SamplesProcessed    uint64          `json:"samples_processed"`
	SampleSharePct      float64         `json:"sample_share_pct"` // share of Report.TotalSamples; 0 (never NaN) if that's 0
	Rules               []RuleAggregate `json:"rules,omitempty"`
	UnmatchedExecutions int             `json:"unmatched_executions,omitempty"`
	UnmatchedSamples    uint64          `json:"unmatched_samples,omitempty"`
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
	SampleSharePct     float64     `json:"sample_share_pct"` // share of THIS TENANT's samples; 0 if that's 0 — the "top rules" ranking key
	DurationSecondsSum float64     `json:"duration_seconds_sum"`
	FetchedSeries      uint64      `json:"fetched_series"`
	FetchedChunks      uint64      `json:"fetched_chunks"`
	FetchedBytes       uint64      `json:"fetched_bytes"`
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
	var totalSamples uint64
	for _, e := range executions {
		totalExecutions++
		totalSamples += e.SamplesProcessed
	}

	var tenants []TenantAggregate
	var unmatched []rule.RuleExecution

	for tenant := range tenantSet {
		tenantExecutions := executionsByTenant[tenant]

		ruleAccum := make(map[rule.RuleID]*RuleAggregate)
		var tenantSamples uint64
		var unmatchedExecutions int
		var unmatchedSamples uint64

		for _, e := range tenantExecutions {
			id := e.RuleID()
			tenantSamples += e.SamplesProcessed

			def, ok := defsByID[id]
			if !ok {
				unmatchedExecutions++
				unmatchedSamples += e.SamplesProcessed
				unmatched = append(unmatched, e)
				continue
			}

			ra, ok := ruleAccum[id]
			if !ok {
				ra = &RuleAggregate{RuleID: id, Kind: def.Kind}
				ruleAccum[id] = ra
			}
			ra.Executions++
			ra.SamplesProcessed += e.SamplesProcessed
			ra.DurationSecondsSum += e.DurationSeconds
			ra.FetchedSeries += e.FetchedSeries
			ra.FetchedChunks += e.FetchedChunks
			ra.FetchedBytes += e.FetchedBytes
		}

		tenantExecutionCount := len(tenantExecutions)

		ruleAggs := make([]RuleAggregate, 0, len(ruleAccum))
		for _, ra := range ruleAccum {
			ra.ExecutionSharePct = pct(uint64(ra.Executions), uint64(tenantExecutionCount))
			ra.SampleSharePct = pct(ra.SamplesProcessed, tenantSamples)
			ruleAggs = append(ruleAggs, *ra)
		}
		sort.Slice(ruleAggs, func(i, j int) bool {
			if ruleAggs[i].SamplesProcessed != ruleAggs[j].SamplesProcessed {
				return ruleAggs[i].SamplesProcessed > ruleAggs[j].SamplesProcessed
			}
			return ruleAggs[i].RuleID.String() < ruleAggs[j].RuleID.String()
		})

		tenants = append(tenants, TenantAggregate{
			Tenant:              tenant,
			RuleCount:           len(ruleDefsByTenant[tenant]),
			Executions:          tenantExecutionCount,
			ExecutionSharePct:   pct(uint64(tenantExecutionCount), uint64(totalExecutions)),
			SamplesProcessed:    tenantSamples,
			SampleSharePct:      pct(tenantSamples, totalSamples),
			Rules:               ruleAggs,
			UnmatchedExecutions: unmatchedExecutions,
			UnmatchedSamples:    unmatchedSamples,
		})
	}

	sort.Slice(tenants, func(i, j int) bool {
		if tenants[i].ExecutionSharePct != tenants[j].ExecutionSharePct {
			return tenants[i].ExecutionSharePct > tenants[j].ExecutionSharePct
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
