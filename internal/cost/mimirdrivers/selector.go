package mimirdrivers

import (
	"context"
	"fmt"
)

// query is fmt.Sprintf with the Source's selector applied: every PromQL
// string a Source sends is built through it.
func (s *Source) query(format string, args ...any) (string, error) {
	return s.cfg.Selector.Apply(fmt.Sprintf(format, args...))
}

// Clusters returns the cluster label's values across every pool's driver
// metrics; see promapi.Client.Clusters.
func (s *Source) Clusters(ctx context.Context) ([]string, error) {
	if err := s.requireMetricsTenant(); err != nil {
		return nil, err
	}
	return s.api.Clusters(ctx, s.cfg.Selector, "cortex_ingester_active_series", "cortex_query_seconds_total", ruleEvaluationMetric)
}
