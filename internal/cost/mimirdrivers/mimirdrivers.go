// Package mimirdrivers reads ADR 0003's per-tenant cost drivers out of
// Mimir's own self-monitoring metrics, over its PromQL API, and hands them
// to internal/cost as a Measurement.
//
// Like internal/telemetry/mimirmetrics, the queries run *as* the tenant
// that scrapes Mimir's own /metrics (Config.MetricsTenant, commonly a
// monitoring tenant); the tenants being costed appear only as values of
// the `user` label inside it. Read that package's doc comment if this is
// surprising.
//
// Pools 1 (ingester memory), 6 (query path) and 7 (ruler CPU) are
// implemented. Pool 1's query follows ADR 0003 "[A] How each driver must be
// read", which in turn follows the mimir-top-tenants mixin — see
// ingesterMemoryQuery. Pools 6 and 7 are per-tenant counters read by
// counters.go.
package mimirdrivers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/promapi"
)

// Config mirrors mimirmetrics.Config; see there for MetricsTenant.
type Config struct {
	BaseURL       string
	Header        string
	MetricsTenant string
	BearerToken   string
	BasicAuth     *promapi.BasicAuth
	// Selector scopes every query to one Mimir cluster; SelectorSource
	// names where it came from, for the assumption trail.
	Selector       Selector
	SelectorSource string
	// RuleEvaluation is the operator's inventory.ruler_evaluation, if set.
	// It replaces the inference in ruleEvaluation, and the trail labels it
	// operator-supplied.
	RuleEvaluation cost.RuleEvaluation
	Timeout        time.Duration
	HTTPClient     *http.Client
}

type Source struct {
	cfg Config
	api *promapi.Client
	now func() time.Time

	// evalModes caches ruleEvaluation by window, so reading pools 6 and 7
	// in one run asks Mimir once. A Source is used by one run.
	evalModes map[string]ruleEvaluation
}

func New(cfg Config) *Source {
	return &Source{
		cfg: cfg,
		api: promapi.New(promapi.Config{
			BaseURL:     cfg.BaseURL,
			Header:      cfg.Header,
			Tenant:      cfg.MetricsTenant,
			BearerToken: cfg.BearerToken,
			BasicAuth:   cfg.BasicAuth,
			Timeout:     cfg.Timeout,
			HTTPClient:  cfg.HTTPClient,
		}),
		now: time.Now,
	}
}

// ingesterMemoryQuery is pool 1's driver under the classic architecture:
// each tenant's in-memory active series, summed across ingesters, averaged
// over the window.
//
// Three choices in it are load-bearing:
//
//   - The sum happens *inside* a subquery and the average outside it,
//     rather than avg_over_time per series and then sum. An ingester
//     rollout replaces every series (new pod label), and a per-series
//     average credits a pod that lived for half the window with its full
//     average — a rollout would then read as double the memory. Summing
//     first and averaging the sum is what "average active series over the
//     window" actually means.
//   - `unless on (cluster, namespace, job) cortex_partition_ring_partitions`
//     drops ingesters running ingest storage, whose series are counted by
//     ingestStorageMemoryQuery instead. On a classic cluster no ingester
//     exports that metric and the guard is a no-op.
//   - There is no replication divisor here. Drivers stay summed across
//     replicas, and internal/cost divides by the replication factor it
//     reads separately (replicationFactorQuery), so the divisor used is
//     visible in the assumption trail rather than buried in PromQL.
//
// The two %s are the window and the subquery step.
const ingesterMemoryQuery = `avg_over_time((sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions))[%s:%s])`

// ingestStorageMemoryQuery is pool 1's driver under ingest storage, the
// mimir-distributed chart's default since 6.0 (Mimir 3.x). There is no
// distributor->ingester replication there: each ingester owns one Kafka
// partition, and replication comes from running the same partition's
// owner in more than one zone (mimir-ingester-zone-a-0, -zone-b-0, ...).
// So the query collapses zone replicas of one partition with max, keyed
// on the pod's trailing ordinal, and sums across partitions — the
// "# Ingest storage" branch of the mixin's top-tenants query, verified
// against the k3d rig in dev/mimir-k8s where the classic query returns
// nothing at all. The result already counts each series once.
const ingestStorageMemoryQuery = `avg_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+|[0-9]+)$"))))[%s:%s])`

// replicationFactorQuery reads the distributor's configured replication
// factor. max, not avg: during a change the higher value is the one
// ingesters were still replicating to for part of the window, and a
// fractional replication factor means nothing.
const replicationFactorQuery = `max(max_over_time(cortex_distributor_replication_factor[%s]))`

// subqueryStep picks the resolution of the averaging subquery: one point a
// minute, coarsened so a long window never asks for more than 1440 points
// per tenant. A minute is below any realistic scrape-driven change in
// active series, and 1440 points keeps a 30d query cheap enough to run
// from a laptop.
func subqueryStep(window time.Duration) time.Duration {
	step := window / 1440
	if step < time.Minute {
		return time.Minute
	}
	return step.Truncate(time.Second)
}

// source is every Measurement's Source.
func (s *Source) source() string {
	return fmt.Sprintf("Mimir metrics (queried as tenant %s)", s.cfg.MetricsTenant)
}

// selectorAssumptions is the trail line for the cluster selector, if any:
// every query of the pool carries it.
func (s *Source) selectorAssumptions() []cost.Assumption {
	if s.cfg.Selector.IsZero() {
		return nil
	}
	return []cost.Assumption{{
		Name:   "cluster selector",
		Value:  s.cfg.Selector.String() + ", added to every query this pool ran",
		Source: s.cfg.SelectorSource,
	}}
}

func (s *Source) requireMetricsTenant() error {
	if s.cfg.MetricsTenant == "" {
		return errors.New("MetricsTenant is required: it names the tenant whose TSDB holds Mimir's own cortex_* series (commonly a monitoring tenant), which is not one of the tenants being costed")
	}
	return nil
}

// ReadIngesterMemory measures pool 1 over [now-window, now].
func (s *Source) ReadIngesterMemory(ctx context.Context, window time.Duration) (cost.Measurement, error) {
	if err := s.requireMetricsTenant(); err != nil {
		return cost.Measurement{}, err
	}
	rangeStr, err := promapi.Duration(window)
	if err != nil {
		return cost.Measurement{}, err
	}
	stepStr, err := promapi.Duration(subqueryStep(window))
	if err != nil {
		return cost.Measurement{}, err
	}

	classicQuery, err := s.query(ingesterMemoryQuery, rangeStr, stepStr)
	if err != nil {
		return cost.Measurement{}, err
	}
	ingestQuery, err := s.query(ingestStorageMemoryQuery, rangeStr, stepStr)
	if err != nil {
		return cost.Measurement{}, err
	}
	rfQuery, err := s.query(replicationFactorQuery, rangeStr)
	if err != nil {
		return cost.Measurement{}, err
	}
	m := cost.Measurement{
		Pool:        cost.IngesterMemory,
		Window:      window,
		End:         s.now(),
		Source:      s.source(),
		DriverQuery: classicQuery,
		RFQuery:     rfQuery,
		Drivers:     map[string]float64{},
		Assumptions: s.selectorAssumptions(),
	}

	// Classic first: its guard excludes ingest-storage ingesters, so a
	// non-empty answer means the cluster writes classically. Only if it
	// finds nothing is the ingest-storage form tried — running both and
	// adding would double-count a cluster mid-migration, where the mixin's
	// own query also picks one branch per cluster.
	samples, err := s.api.Instant(ctx, m.DriverQuery)
	if err != nil {
		return cost.Measurement{}, fmt.Errorf("active series query: %w", err)
	}
	if len(samples) > 0 {
		m.Architecture = "classic"
	} else {
		samples, err = s.api.Instant(ctx, ingestQuery)
		if err != nil {
			return cost.Measurement{}, fmt.Errorf("active series query (ingest storage): %w", err)
		}
		if len(samples) > 0 {
			m.DriverQuery = ingestQuery
			m.Architecture = "ingest storage"
			m.Deduplicated = true
		}
	}
	if m.Architecture == "" {
		// Neither query returned data, so there is no evidence of either
		// write path, and the trail claims none — as the ruler's
		// evaluation mode reports "unknown" with no rule data.
		m.Assumptions = append(m.Assumptions, cost.Assumption{
			Name:   "write path",
			Value:  "unknown (no data)",
			Source: "neither the classic nor the ingest-storage driver query returned data",
		})
		m.Notes = append(m.Notes, "Mimir returned no data for cortex_ingester_active_series: the metrics tenant may not be the one that scrapes Mimir's ingesters (check --metrics-tenant)")
	}
	for _, smp := range samples {
		tenant := smp.Metric["user"]
		v, ok := smp.Float()
		if tenant == "" || !ok {
			continue
		}
		// += rather than =: `sum by (user)` returns one sample per tenant,
		// but adding is what stays correct if a proxy ever splits one.
		m.Drivers[tenant] += v
	}

	rf, err := s.api.Instant(ctx, m.RFQuery)
	if err != nil {
		return cost.Measurement{}, fmt.Errorf("replication factor query: %w", err)
	}
	if len(rf) == 1 {
		if v, ok := rf[0].Float(); ok {
			m.ReplicationFactor = &v
		}
	}
	return m, nil
}
