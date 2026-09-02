// Package meter defines the one I/O seam live- and fleet-tier checks are
// allowed to use (spec §5, design rule #1). No implementation ships this
// milestone — CheckContext.Meter is nil under --offline and whenever no
// backend is configured; static-tier checks never read it.
package meter

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

// Meter is the only network I/O interface an estimation function or check
// may depend on, so every check is unit-testable with a fake implementation
// and the analyzer itself never executes a tenant's expression.
type Meter interface {
	// SeriesCount is count(last_over_time(sel[W])), W = presence_window.
	SeriesCount(tenant, selector string) (int64, error)
	// GroupedCount is count(count by(...)(last_over_time(sel[W]))).
	GroupedCount(tenant, selector string, by []string) (int64, error)
	// SampleSeries is the series API with a limit, falling back to
	// topk(k, sel).
	SampleSeries(tenant, selector string, k int) ([]labels.Labels, error)
	// RangeSampleCount is count_over_time over window for one exact series.
	RangeSampleCount(tenant string, exact labels.Labels, window time.Duration) (int64, error)
}
