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
	// "last 7d", or "all time" when no --since was given. Display-only —
	// filtering itself already happened before Aggregate ran.
	Window          string                         `json:"window"`
	Tenants         []attribution.TenantAggregate `json:"tenants"`
	Unmatched       []rule.RuleExecution          `json:"unmatched,omitempty"`
	TotalExecutions int                           `json:"total_executions"`
	TotalSamples    uint64                        `json:"total_samples"`
	RuleDefinitions int                           `json:"rule_definitions"`
	GeneratedAt     time.Time                     `json:"generated_at"`
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
	default:
		return nil, fmt.Errorf("unknown report format %q", format)
	}
}
