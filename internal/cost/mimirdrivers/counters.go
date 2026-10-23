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
	// Whether rule queries are inside this pool's driver depends on how the
	// ruler evaluates them (ADR 0003 [V]), and nothing in cortex_query_*
	// says so, so the trail must.
	mode, err := s.ruleEvaluation(ctx, rangeStr)
	if err != nil {
		return cost.Measurement{}, err
	}
	m.RuleEvaluation = mode.mode
	m.Assumptions = append(m.Assumptions, cost.Assumption{
		Name:   "rule queries",
		Value:  queryPathRuleQueries[mode.mode] + " (" + mode.evidence + ")",
		Source: mode.source,
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
		q, err := s.query(counterQuery, d.metric, rangeStr)
		if err != nil {
			return cost.Measurement{}, err
		}
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
		if m.DriverQuery, err = s.query(counterQuery, first.metric, rangeStr); err != nil {
			return cost.Measurement{}, err
		}
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

// queryPathRuleQueries says, per evaluation mode, whether pool 6's driver
// contains rule work. Measured on Mimir 3.2.0 (ADR 0003 [V], E6): under
// local evaluation 1,376 rule evaluations moved cortex_query_* by zero;
// under remote evaluation the query-frontend records every rule query,
// with no label separating it from user queries.
var queryPathRuleQueries = map[cost.RuleEvaluation]string{
	cost.RuleEvaluationLocal: "not included — rules are evaluated inside the ruler, so this pool is user and API queries only; rule work is in the ruler CPU pool",
	cost.RuleEvaluationRemote: "included — rules are evaluated remotely, so every tenant's rule queries are in this pool alongside its user queries, " +
		"and no label separates the two",
	cost.RuleEvaluationUnknown: "unknown — whether rule queries are in this pool depends on the ruler's evaluation mode, which could not be inferred",
}

// ruleEvaluation is the ruler's evaluation mode as inferred for one
// window, with the evidence for it.
type ruleEvaluation struct {
	mode     cost.RuleEvaluation
	evidence string
	// source is the trail's Source for the mode: Mimir's metrics, or the
	// operator.
	source string
	// evalSeconds is the pool-7 driver, read on the way.
	evalSeconds map[string]float64
	evalQuery   string
}

// rulerJob matches the ruler's own scrape job under the mixin's
// <namespace>/<component> convention ("mimir/ruler") and a bare or
// prefixed one ("ruler", "mimir-ruler"). PromQL anchors the regex, so it
// does not match the chart's dedicated ruler read path ("mimir/ruler-querier",
// "mimir/ruler-query-frontend"): those are a querier and a frontend, and a
// querier exports the distributor's query metrics in either mode.
const rulerJob = `job=~".*ruler"`

// rulerDistributorQueryMetric is the local-evaluation signal that needs no
// flag. In Mimir 3.2.0 a ruler with no -ruler.query-frontend.address
// depends on the distributor service (pkg/mimir/modules.go L1716-1717)
// and queries through it (L1288-1291); distributor.New registers this
// histogram unconditionally (pkg/distributor/distributor.go L568-569),
// and every Distributor.QueryStream observes it (pkg/distributor/query.go
// L98). A remote ruler has no distributor and never observes it.
const rulerDistributorQueryMetric = "cortex_distributor_query_duration_seconds_count"

// rulerEvaluationCountMetric, under the ruler's job, says the ruler itself
// is scraped under a job rulerJob recognises — which is what makes the
// absence of rulerDistributorQueryMetric there evidence of remote mode
// rather than of a job label this probe cannot see.
const rulerEvaluationCountMetric = "cortex_prometheus_rule_evaluation_duration_seconds_count"

// presentQuery is non-empty iff metric had a sample under the ruler's job
// in the window.
const presentQuery = `count(increase(%s{` + rulerJob + `}[%s]))`

// ruleEvaluation decides whether the ruler evaluates rules locally or
// through the query-frontend (-ruler.query-frontend.address). An operator
// value (inventory.ruler_evaluation) wins and is labelled as such.
// Otherwise it is inferred from which metrics exist, on Mimir 3.2.0:
//
//   - local: the ruler's job has distributor query data
//     (rulerDistributorQueryMetric, on with default flags), or
//     cortex_ruler_query_seconds_total has data. The latter exists only
//     with -ruler.query-stats-enabled=true (pkg/ruler/compat.go L455-465,
//     default off), so it is a second signal, never a required one (#65);
//   - remote: the ruler's job has rule-evaluation data but no distributor
//     query data, which a local ruler cannot produce;
//   - unknown: anything else — no rule evaluations, or evaluations under no
//     job rulerJob recognises (a monolithic or relabelled deployment).
//     Absence of a local signal there is not evidence of remote mode, and
//     guessing one is what ADR 0003 [V] warns against.
//
// A ruler switched between modes inside the window has local signals for
// part of it and reads as local.
func (s *Source) ruleEvaluation(ctx context.Context, rangeStr string) (ruleEvaluation, error) {
	if r, ok := s.evalModes[rangeStr]; ok {
		return r, nil
	}
	r := ruleEvaluation{source: s.source()}
	var err error
	if r.evalQuery, err = s.query(counterQuery, ruleEvaluationMetric, rangeStr); err != nil {
		return ruleEvaluation{}, err
	}
	r.evalSeconds, err = s.readDrivers(ctx, r.evalQuery)
	if err != nil {
		return ruleEvaluation{}, fmt.Errorf("rule evaluation query: %w", err)
	}
	if err := s.inferRuleEvaluation(ctx, rangeStr, &r); err != nil {
		return ruleEvaluation{}, err
	}
	if s.evalModes == nil {
		s.evalModes = map[string]ruleEvaluation{}
	}
	s.evalModes[rangeStr] = r
	return r, nil
}

func (s *Source) inferRuleEvaluation(ctx context.Context, rangeStr string, r *ruleEvaluation) error {
	if s.cfg.RuleEvaluation != "" {
		r.mode = s.cfg.RuleEvaluation
		r.evidence = "operator-supplied, not inferred"
		r.source = "operator-supplied: inventory.ruler_evaluation"
		return nil
	}
	present := func(format string, args ...any) (bool, error) {
		q, err := s.query(format, args...)
		if err != nil {
			return false, err
		}
		samples, err := s.api.Instant(ctx, q)
		return len(samples) > 0, err
	}
	distributorQueries, err := present(presentQuery, rulerDistributorQueryMetric, rangeStr)
	if err != nil {
		return fmt.Errorf("ruler distributor-query probe: %w", err)
	}
	querySeconds, err := present(counterQuery, rulerQueryMetric, rangeStr)
	if err != nil {
		return fmt.Errorf("ruler query-time query: %w", err)
	}
	rulerEvaluations, err := present(presentQuery, rulerEvaluationCountMetric, rangeStr)
	if err != nil {
		return fmt.Errorf("ruler evaluation probe: %w", err)
	}
	switch {
	case distributorQueries:
		r.mode = cost.RuleEvaluationLocal
		r.evidence = "inferred: the ruler's job (" + rulerJob + ") has " + rulerDistributorQueryMetric + " data, which only a ruler querying ingesters itself records"
	case querySeconds:
		r.mode = cost.RuleEvaluationLocal
		r.evidence = "inferred: " + rulerQueryMetric + " has data, and the ruler records it only for queries it runs itself"
	case rulerEvaluations:
		r.mode = cost.RuleEvaluationRemote
		r.evidence = "inferred: the ruler's job (" + rulerJob + ") evaluated rules but has no " + rulerDistributorQueryMetric +
			" data, which a ruler evaluating locally always records, so its queries go to the query-frontend"
	case len(r.evalSeconds) > 0:
		r.mode = cost.RuleEvaluationUnknown
		r.evidence = "rules were evaluated, but not under a scrape job matching " + rulerJob +
			", so the ruler's own query metrics cannot be told apart; set inventory.ruler_evaluation to say which"
	default:
		r.mode = cost.RuleEvaluationUnknown
		r.evidence = "neither " + ruleEvaluationMetric + " nor any ruler query metric has data in this window: " +
			"no tenant evaluated rules, or the metrics tenant does not scrape the ruler"
	}
	return nil
}

// ReadRulerCPU measures pool 7, the ruler, over [now-window, now]: the
// time each tenant's rules spent evaluating.
//
// The driver is the same in both evaluation modes; what differs is what
// the time is, and the trail says which mode the cluster is in (see
// ruleEvaluation for how it is inferred):
//
//   - Local evaluation: the ruler ran the rule queries in its own process,
//     so every evaluation second is ruler CPU and none of it shows up in
//     the query path (the cortex_query_* metrics are recorded by the
//     query-frontend, which a local ruler never calls). The ruler's query
//     seconds are *nested inside* the evaluation seconds — a rule's
//     evaluation contains its query — so they must not be subtracted:
//     doing so would remove the ruler's main cost and leave only its
//     bookkeeping.
//   - Remote evaluation: the queries ran in the query path and are priced
//     as pool 6. The evaluation time still includes the ruler waiting for
//     them, so it measures how long a tenant's rules occupy the ruler more
//     than CPU, and the measurement says so instead of pretending
//     otherwise. Nothing per-tenant separates the waiting out, which is
//     why this is a note and not a subtraction.
func (s *Source) ReadRulerCPU(ctx context.Context, window time.Duration) (cost.Measurement, error) {
	m, rangeStr, err := s.counterMeasurement(cost.RulerCPU, window)
	if err != nil {
		return cost.Measurement{}, err
	}
	mode, err := s.ruleEvaluation(ctx, rangeStr)
	if err != nil {
		return cost.Measurement{}, err
	}
	m.DriverMetric = ruleEvaluationMetric
	m.DriverKind = "rule-evaluation seconds"
	m.Unit = cost.UnitSeconds
	m.DriverQuery = mode.evalQuery
	m.Drivers = mode.evalSeconds
	m.RuleEvaluation = mode.mode

	switch mode.mode {
	case cost.RuleEvaluationLocal:
		m.Assumptions = append(m.Assumptions, cost.Assumption{
			Name: "rule evaluation",
			Value: "local (" + mode.evidence + "): all of this time is ruler CPU, and none of it is in the query path " +
				"(the ruler's query time is part of it, not added to it)",
			Source: mode.source,
		})
	case cost.RuleEvaluationRemote:
		m.Assumptions = append(m.Assumptions, cost.Assumption{
			Name:   "rule evaluation",
			Value:  "remote (" + mode.evidence + "): the rule queries' work is priced in the query path",
			Source: mode.source,
		})
		m.Notes = append(m.Notes,
			"rules are evaluated remotely, so this time is mostly the ruler waiting on the query path, not ruler CPU: "+
				"it shows how long each tenant's rules occupy the ruler, and the work behind it is already in the query path pool. "+
				"It also depends on load — a tenant's rules run slower when a neighbour loads the cluster")
	default:
		m.Assumptions = append(m.Assumptions, cost.Assumption{
			Name:   "rule evaluation",
			Value:  "unknown (" + mode.evidence + ")",
			Source: mode.source,
		})
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
		Source:       s.source(),
		Drivers:      map[string]float64{},
		Unreplicated: true,
		Assumptions:  s.selectorAssumptions(),
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
