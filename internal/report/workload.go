package report

import (
	"fmt"
	"io"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// WorkloadResult is everything a WorkloadReporter needs to render one
// `promcost report` run. Parallel to Result/Reporter — the check command's
// existing Finding-oriented types are untouched.
type WorkloadResult struct {
	// Window describes the --since filter applied before aggregation, e.g.
	// "last 7d", or "" when no --since was given. Display-only — filtering
	// itself already happened before Aggregate ran. This is the requested
	// filter, not the actual observed range — see ObservedStart/End for
	// that.
	Window          string                        `json:"window"`
	Tenants         []attribution.TenantAggregate `json:"tenants"`
	Unmatched       []rule.RuleExecution          `json:"unmatched,omitempty"`
	TotalExecutions int                           `json:"total_executions"`
	TotalSamples    uint64                        `json:"total_samples"`
	RuleDefinitions int                           `json:"rule_definitions"`
	// RankMetric is the metric Tenants (and each tenant's Rules) are
	// sorted by — see attribution.RankMetric's doc comment.
	RankMetric attribution.RankMetric `json:"rank_metric"`
	// ObservedStart/ObservedEnd are the earliest and latest Timestamp
	// across every execution that fed this report (after --since
	// filtering), nil when there were none. This is the ground truth of
	// what window the report actually covers — distinct from Window,
	// which just echoes the --since flag and lies ("all time") when the
	// flag was omitted even though the underlying telemetry only spans a
	// few minutes.
	ObservedStart *time.Time `json:"observed_start,omitempty"`
	ObservedEnd   *time.Time `json:"observed_end,omitempty"`
	// SkippedTelemetry is the number of raw telemetry records the source
	// refused to turn into a rule.RuleExecution at all (e.g. an ambiguous
	// mimirlogs query-text match) — never included in TotalExecutions or
	// Unmatched, since those never became executions in the first place.
	SkippedTelemetry int       `json:"skipped_telemetry,omitempty"`
	GeneratedAt      time.Time `json:"generated_at"`
}

type WorkloadReporter interface {
	Render(w io.Writer, result WorkloadResult) error
}

func NewWorkload(format Format) (WorkloadReporter, error) {
	switch format {
	case FormatMD, "":
		return workloadMDReporter{}, nil
	case FormatJSON:
		return workloadJSONReporter{}, nil
	case FormatHTML:
		return workloadHTMLReporter{}, nil
	default:
		return nil, fmt.Errorf("unknown report format %q", format)
	}
}
