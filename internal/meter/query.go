package meter

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// SeriesCount is count(last_over_time(sel[W])), W = presenceWindow.
func (m *promAPIMeter) SeriesCount(tenant, selector string) (int64, error) {
	promql := fmt.Sprintf("count(last_over_time(%s[%s]))", selector, promDuration(m.presenceWindow))
	vec, err := m.instantQuery(tenant, promql)
	if err != nil {
		return 0, err
	}
	return scalarOf(vec), nil
}

// GroupedCount is count(count by(...)(last_over_time(sel[W]))).
func (m *promAPIMeter) GroupedCount(tenant, selector string, by []string) (int64, error) {
	promql := fmt.Sprintf("count(count by(%s)(last_over_time(%s[%s])))", strings.Join(by, ","), selector, promDuration(m.presenceWindow))
	vec, err := m.instantQuery(tenant, promql)
	if err != nil {
		return 0, err
	}
	return scalarOf(vec), nil
}

// SampleSeries is the series API with a limit, falling back to topk(k, sel)
// if the series API errors (some Mimir-family deployments restrict it).
func (m *promAPIMeter) SampleSeries(tenant, selector string, k int) ([]labels.Labels, error) {
	ctx, cancel := context.WithTimeout(withTenant(context.Background(), tenant), m.timeout)
	defer cancel()

	now := time.Now()
	sets, _, err := m.api.Series(ctx, []string{selector}, now.Add(-m.presenceWindow), now, v1.WithLimit(uint64(k)))
	if err == nil {
		out := make([]labels.Labels, 0, len(sets))
		for _, s := range sets {
			out = append(out, labelSetToLabels(s))
		}
		return out, nil
	}

	promql := fmt.Sprintf("topk(%d, %s)", k, selector)
	vec, qerr := m.instantQuery(tenant, promql)
	if qerr != nil {
		return nil, fmt.Errorf("meter: series %q failed (%w), topk fallback also failed: %w", selector, err, qerr)
	}
	out := make([]labels.Labels, 0, len(vec))
	for _, sample := range vec {
		out = append(out, labelSetToLabels(model.LabelSet(sample.Metric)))
	}
	return out, nil
}

// RangeSampleCount is count_over_time over window for one exact series.
func (m *promAPIMeter) RangeSampleCount(tenant string, exact labels.Labels, window time.Duration) (int64, error) {
	promql := fmt.Sprintf("count_over_time(%s[%s])", exact.String(), promDuration(window))
	vec, err := m.instantQuery(tenant, promql)
	if err != nil {
		return 0, err
	}
	return scalarOf(vec), nil
}

// instantQuery runs promql as an instant query scoped to tenant — the only
// query shape any Meter method ever issues (design rule #2: never a range
// query, never a tenant's own expression).
func (m *promAPIMeter) instantQuery(tenant, promql string) (model.Vector, error) {
	ctx, cancel := context.WithTimeout(withTenant(context.Background(), tenant), m.timeout)
	defer cancel()

	val, _, err := m.api.Query(ctx, promql, time.Now())
	if err != nil {
		return nil, fmt.Errorf("meter: query %q: %w", promql, err)
	}
	vec, ok := val.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("meter: query %q: unexpected result type %T", promql, val)
	}
	return vec, nil
}

// scalarOf returns the value of a query's sole expected sample, or 0 for an
// empty vector (no matching series is a legitimate "count is zero", not an
// error).
func scalarOf(vec model.Vector) int64 {
	if len(vec) == 0 {
		return 0
	}
	return int64(vec[0].Value)
}

func labelSetToLabels(ls model.LabelSet) labels.Labels {
	m := make(map[string]string, len(ls))
	for name, value := range ls {
		m[string(name)] = string(value)
	}
	return labels.FromMap(m)
}

// promDuration renders d as a PromQL duration literal. time.Duration's own
// String() (e.g. "1h0m0s") already matches PromQL's duration grammar for
// the whole-unit values promcost.yaml's Duration fields parse from.
func promDuration(d time.Duration) string {
	return d.String()
}
