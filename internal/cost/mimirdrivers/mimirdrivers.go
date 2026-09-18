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
// Only pool 1 (ingester memory) is implemented. Its query follows ADR 0003
// "[A] How each driver must be read", which in turn follows the
// mimir-top-tenants mixin — see ingesterMemoryQuery.
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
	Timeout       time.Duration
	HTTPClient    *http.Client
}

type Source struct {
	cfg Config
	api *promapi.Client
	now func() time.Time
}

func New(cfg Config) *Source {
	return &Source{
		cfg: cfg,
		api: promapi.New(promapi.Config{
			BaseURL:     cfg.BaseURL,
			Header:      cfg.Header,
			Tenant:      cfg.MetricsTenant,
			BearerToken: cfg.BearerToken,
			Timeout:     cfg.Timeout,
			HTTPClient:  cfg.HTTPClient,
		}),
		now: time.Now,
	}
}

// ingesterMemoryQuery is pool 1's driver: each tenant's in-memory active
// series, summed across ingesters, averaged over the window.
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
//     is the mixin's ingest-storage guard: under ingest storage the same
//     series are counted again by partition-owning instances. On a cluster
//     with no partition ring the guard matches nothing and is a no-op.
//   - There is no replication divisor here. Drivers stay summed across
//     replicas, and internal/cost divides by the replication factor it
//     reads separately (replicationFactorQuery), so the divisor used is
//     visible in the assumption trail rather than buried in PromQL.
//
// The two %s are the window and the subquery step.
const ingesterMemoryQuery = `avg_over_time((sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions))[%s:%s])`

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

// ReadIngesterMemory measures pool 1 over [now-window, now].
func (s *Source) ReadIngesterMemory(ctx context.Context, window time.Duration) (cost.Measurement, error) {
	if s.cfg.MetricsTenant == "" {
		return cost.Measurement{}, errors.New("MetricsTenant is required: it names the tenant whose TSDB holds Mimir's own cortex_* series (commonly a monitoring tenant), which is not one of the tenants being costed")
	}
	rangeStr, err := promapi.Duration(window)
	if err != nil {
		return cost.Measurement{}, err
	}
	stepStr, err := promapi.Duration(subqueryStep(window))
	if err != nil {
		return cost.Measurement{}, err
	}

	m := cost.Measurement{
		Pool:        cost.IngesterMemory,
		Window:      window,
		End:         s.now(),
		Source:      fmt.Sprintf("Mimir metrics (queried as tenant %s)", s.cfg.MetricsTenant),
		DriverQuery: fmt.Sprintf(ingesterMemoryQuery, rangeStr, stepStr),
		RFQuery:     fmt.Sprintf(replicationFactorQuery, rangeStr),
		Drivers:     map[string]float64{},
	}

	samples, err := s.api.Instant(ctx, m.DriverQuery)
	if err != nil {
		return cost.Measurement{}, fmt.Errorf("active series query: %w", err)
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
