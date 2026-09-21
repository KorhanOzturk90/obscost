package mimirdrivers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/promapi"
)

// counterQuery is the shape of every pool 2/6/7 driver: a per-tenant
// counter, increased over the window and summed across the replicas that
// export it (ADR 0003 [A]: "counter — increase() over the window, never a
// bare difference; sum by (user)"). There is no replication divisor: these
// counters are recorded once per request, not once per replica of a series.
//
// The one %s pair is the metric and the window.
const counterQuery = `sum by (user) (increase(%s[%s]))`

// queryPathDriver is one candidate driver for pool 6.
type queryPathDriver struct {
	metric string
	kind   string
	unit   cost.Unit
}

// queryPathChain is pool 6's drivers in preference order. The first one with
// data wins, mirroring attribution.pickRankMetric for the same reason its
// doc comment gives: data volume is what the ingesters and store-gateways
// feel, and wall time is a contention-sensitive proxy kept only for when no
// volume was measured. Query seconds is the fallback and the only entry
// that marks the measurement as one.
var queryPathChain = []queryPathDriver{
	{"cortex_query_fetched_chunk_bytes_total", "chunk bytes fetched", cost.UnitBytes},
	{"cortex_query_samples_processed_total", "samples processed", cost.UnitCount},
	{"cortex_query_fetched_series_total", "series fetched", cost.UnitCount},
	{"cortex_query_seconds_total", "query seconds", cost.UnitSeconds},
}

const queryPathFallbackNote = "no fetched-volume metric returned data, so query time is the driver. " +
	"Time depends on load as well as on what a tenant asked for: a tenant's queries run slower when a neighbour loads the cluster, " +
	"so these shares can move without anyone's behaviour changing. " +
	"Sharded queries also report CPU time summed across shards, not elapsed time"

// ReadQueryPath measures pool 6, the query path, over [now-window, now].
//
// It walks queryPathChain and takes the first driver whose total is
// non-zero, as attribution.pickRankMetric does: a metric that exists but is
// zero for every tenant says nothing about who used the query path, so it
// does not outrank one that does. If every driver that answered is all
// zeros, the first of them is kept, so the report reads "every tenant
// reported 0" rather than "no data". If nothing answered at all, the
// measurement is empty and says so.
func (s *Source) ReadQueryPath(ctx context.Context, window time.Duration) (cost.Measurement, error) {
	m, rangeStr, err := s.counterMeasurement(cost.QueryPath, window)
	if err != nil {
		return cost.Measurement{}, err
	}
	m.Assumptions = append(m.Assumptions, cost.Assumption{
		Name:   "driver preference",
		Value:  "fetched volume over time: " + chainNames() + "; the first with data is used",
		Source: "ADR 0003 pool 6, ADR 0002 decision 8",
	})

	type answer struct {
		driver  queryPathDriver
		query   string
		drivers map[string]float64
	}
	type skip struct{ metric, why string }
	var (
		chosen  *answer
		zeroed  *answer // first driver that answered, but only with zeros
		skipped []skip
	)
	for _, d := range queryPathChain {
		q := fmt.Sprintf(counterQuery, d.metric, rangeStr)
		drivers, err := s.readDrivers(ctx, q)
		if err != nil {
			return cost.Measurement{}, fmt.Errorf("%s query: %w", d.metric, err)
		}
		a := &answer{driver: d, query: q, drivers: drivers}
		switch {
		case len(drivers) == 0:
			skipped = append(skipped, skip{d.metric, "no data"})
		case sumOf(drivers) > 0:
			chosen = a
		default:
			if zeroed == nil {
				zeroed = a
			}
			skipped = append(skipped, skip{d.metric, "all zero"})
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		chosen = zeroed
	}
	if chosen == nil {
		first := queryPathChain[0]
		m.DriverQuery = fmt.Sprintf(counterQuery, first.metric, rangeStr)
		m.DriverMetric, m.DriverKind, m.Unit = first.metric, first.kind, first.unit
		m.Notes = append(m.Notes, "Mimir returned no data for any query-path metric ("+chainNames()+"): no tenant ran a query in this window, or the metrics tenant does not scrape the query-frontend")
		return m, nil
	}

	m.DriverQuery = chosen.query
	m.DriverMetric, m.DriverKind, m.Unit = chosen.driver.metric, chosen.driver.kind, chosen.driver.unit
	m.Drivers = chosen.drivers
	if chosen.driver.unit == cost.UnitSeconds {
		m.Fallback = true
		m.FallbackNote = queryPathFallbackNote
	}
	// Anything tried before the winner is worth naming: it is the reason a
	// stronger-looking driver is not in use.
	var tried []string
	for _, sk := range skipped {
		if sk.metric != chosen.driver.metric {
			tried = append(tried, sk.metric+" ("+sk.why+")")
		}
	}
	if len(tried) > 0 {
		m.Assumptions = append(m.Assumptions, cost.Assumption{
			Name:   "preferred drivers skipped",
			Value:  strings.Join(tried, ", "),
			Source: m.Source,
		})
	}
	return m, nil
}

func chainNames() string {
	names := make([]string, len(queryPathChain))
	for i, d := range queryPathChain {
		names[i] = d.metric
	}
	return strings.Join(names, " → ")
}

const (
	ruleEvaluationMetric = "cortex_prometheus_rule_evaluation_duration_seconds_sum"
	rulerQueryMetric     = "cortex_ruler_query_seconds_total"
)

// ReadRulerCPU measures pool 7, the ruler, over [now-window, now]: the
// time each tenant's rules spent evaluating.
//
// The driver is the same in both evaluation modes; what differs is what
// the time is, and the trail says which mode the cluster is in. That is
// decided by cortex_ruler_query_seconds_total, which the ruler only
// records for queries it runs itself:
//
//   - Local evaluation: it has data. The ruler ran the rule queries in its
//     own process, so every evaluation second is ruler CPU and none of it
//     shows up in the query path (the cortex_query_* metrics are recorded
//     by the query-frontend, which a local ruler never calls). The
//     query seconds are *nested inside* the evaluation seconds — a rule's
//     evaluation contains its query — so they must not be subtracted:
//     doing so would remove the ruler's main cost and leave only its
//     bookkeeping.
//   - Remote evaluation: it is absent. The queries ran in the query path
//     and are priced as pool 6. The evaluation time still includes the
//     ruler waiting for them, so it measures how long a tenant's rules
//     occupy the ruler more than CPU, and the measurement says so instead
//     of pretending otherwise. Nothing per-tenant separates the waiting
//     out, which is why this is a note and not a subtraction.
func (s *Source) ReadRulerCPU(ctx context.Context, window time.Duration) (cost.Measurement, error) {
	m, rangeStr, err := s.counterMeasurement(cost.RulerCPU, window)
	if err != nil {
		return cost.Measurement{}, err
	}
	m.DriverMetric = ruleEvaluationMetric
	m.DriverKind = "rule-evaluation seconds"
	m.Unit = cost.UnitSeconds
	m.DriverQuery = fmt.Sprintf(counterQuery, ruleEvaluationMetric, rangeStr)

	m.Drivers, err = s.readDrivers(ctx, m.DriverQuery)
	if err != nil {
		return cost.Measurement{}, fmt.Errorf("rule evaluation query: %w", err)
	}
	localQuery := fmt.Sprintf(counterQuery, rulerQueryMetric, rangeStr)
	local, err := s.readDrivers(ctx, localQuery)
	if err != nil {
		return cost.Measurement{}, fmt.Errorf("ruler query-time query: %w", err)
	}

	if len(local) > 0 {
		m.Assumptions = append(m.Assumptions, cost.Assumption{
			Name: "rule evaluation",
			Value: "local — " + rulerQueryMetric + " has data, so the ruler ran its rule queries itself: " +
				"all of this time is ruler CPU, and none of it is in the query path (its query time is part of it, not added to it)",
			Source: m.Source,
		})
		return m, nil
	}
	m.Assumptions = append(m.Assumptions, cost.Assumption{
		Name: "rule evaluation",
		Value: "remote — " + rulerQueryMetric + " has no data, so the ruler sends its rule queries to the query-frontend " +
			"and their work is priced in the query path",
		Source: m.Source,
	})
	if len(m.Drivers) > 0 {
		m.Notes = append(m.Notes,
			"rules are evaluated remotely, so this time is mostly the ruler waiting on the query path, not ruler CPU: "+
				"it shows how long each tenant's rules occupy the ruler, and the work behind it is already in the query path pool. "+
				"It also depends on load — a tenant's rules run slower when a neighbour loads the cluster")
	}
	return m, nil
}

// counterMeasurement is the part of a pool 2/6/7 Measurement that does not
// depend on which driver answered.
func (s *Source) counterMeasurement(pool cost.Pool, window time.Duration) (cost.Measurement, string, error) {
	if err := s.requireMetricsTenant(); err != nil {
		return cost.Measurement{}, "", err
	}
	rangeStr, err := promapi.Duration(window)
	if err != nil {
		return cost.Measurement{}, "", err
	}
	return cost.Measurement{
		Pool:         pool,
		Window:       window,
		End:          s.now(),
		Source:       fmt.Sprintf("Mimir metrics (queried as tenant %s)", s.cfg.MetricsTenant),
		Drivers:      map[string]float64{},
		Unreplicated: true,
	}, rangeStr, nil
}

// readDrivers runs one per-tenant instant query and returns its values by
// tenant. An empty map means Mimir had no such series in the window, which
// callers treat as "not measured", never as zero.
func (s *Source) readDrivers(ctx context.Context, query string) (map[string]float64, error) {
	samples, err := s.api.Instant(ctx, query)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, smp := range samples {
		tenant := smp.Metric["user"]
		v, ok := smp.Float()
		if tenant == "" || !ok {
			continue
		}
		// += for the same reason as ReadIngesterMemory: correct if a proxy
		// ever splits one tenant's sample.
		out[tenant] += v
	}
	return out, nil
}

func sumOf(m map[string]float64) float64 {
	var t float64
	for _, v := range m {
		t += v
	}
	return t
}
