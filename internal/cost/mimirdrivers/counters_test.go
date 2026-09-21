package mimirdrivers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/cost"
)

// vec builds a Prometheus instant-vector response from tenant -> value.
func vec(pairs ...string) string {
	var b strings.Builder
	b.WriteString(`{"status":"success","data":{"resultType":"vector","result":[`)
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"metric":{"user":"` + pairs[i] + `"},"value":[1757800000,"` + pairs[i+1] + `"]}`)
	}
	b.WriteString(`]}}`)
	return b.String()
}

// Real per-tenant figures from dev/mimir-k8s (10m increase, classic write
// path, remote rule evaluation), taken from the live rig while building
// pools 6 and 7.
var (
	rigFetchedBytes = vec("analytics", "52940813.07", "infra", "47543702.49", "payments", "1020188.73", "platform", "129673.29", "monitoring", "60288.31")
	rigSamples      = vec("analytics", "129163.82", "infra", "136454.21", "payments", "151197.08", "platform", "421.04", "monitoring", "40336.06")
	rigSeries       = vec("analytics", "139260.47", "infra", "157327.49", "payments", "1263.13", "platform", "421.04", "monitoring", "135.79")
	rigQuerySeconds = vec("analytics", "435.49", "infra", "334.32", "payments", "43.38", "platform", "2.08", "monitoring", "8.13")
	rigEvalSeconds  = vec("analytics", "357.79", "infra", "281.53", "payments", "164.97", "platform", "149.40")
)

// metricServer answers each query with the response registered for the
// metric it mentions, and with an empty vector for a metric it has none for —
// which is what a real Mimir does for a series that does not exist. Metrics
// are matched by name, so a fixture cannot answer a query that asked for
// something else by accident.
func metricServer(t *testing.T, responses map[string]string, queries *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "monitoring" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query().Get("query")
		if queries != nil {
			*queries = append(*queries, q)
		}
		w.Header().Set("Content-Type", "application/json")
		for metric, body := range responses {
			if strings.Contains(q, metric+"[") {
				_, _ = w.Write([]byte(body))
				return
			}
		}
		_, _ = w.Write([]byte(empty))
	}))
}

func source(srv *httptest.Server) *Source {
	return New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"})
}

func TestReadQueryPath_PrefersFetchedBytes(t *testing.T) {
	var queries []string
	srv := metricServer(t, map[string]string{
		"cortex_query_fetched_chunk_bytes_total": rigFetchedBytes,
		"cortex_query_samples_processed_total":   rigSamples,
		"cortex_query_seconds_total":             rigQuerySeconds,
	}, &queries)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), 10*time.Minute)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if m.DriverMetric != "cortex_query_fetched_chunk_bytes_total" || m.Unit != cost.UnitBytes || m.DriverKind != "chunk bytes fetched" {
		t.Errorf("driver = %s / %s / %s, want fetched chunk bytes", m.DriverMetric, m.DriverKind, m.Unit)
	}
	if m.Fallback {
		t.Error("Fallback set although the preferred volume driver answered")
	}
	if len(queries) != 1 {
		t.Errorf("issued %d queries, want 1: the chain stops at the first driver with data", len(queries))
	}
	if want := "sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[10m]))"; m.DriverQuery != want {
		t.Errorf("DriverQuery = %q, want %q", m.DriverQuery, want)
	}
	if !m.Unreplicated {
		t.Error("Unreplicated = false: a per-tenant counter has no replication divisor")
	}
	if m.Drivers["analytics"] < 52940813 || m.Drivers["analytics"] > 52940814 {
		t.Errorf("analytics driver = %v, want the increase undivided", m.Drivers["analytics"])
	}
	a := cost.Allocate(m, cost.Inventory{}, nil)
	if a.Tenants[0].Tenant != "analytics" {
		t.Errorf("top tenant = %s, want analytics (52.9 MB vs infra's 47.5 MB)", a.Tenants[0].Tenant)
	}
}

func TestReadQueryPath_FallsThroughToSamplesAndNamesWhatItSkipped(t *testing.T) {
	srv := metricServer(t, map[string]string{
		"cortex_query_samples_processed_total": rigSamples,
		"cortex_query_seconds_total":           rigQuerySeconds,
	}, nil)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if m.DriverMetric != "cortex_query_samples_processed_total" || m.Unit != cost.UnitCount {
		t.Errorf("driver = %s (%s), want samples processed", m.DriverMetric, m.Unit)
	}
	if m.Fallback {
		t.Error("Fallback set for samples processed: it is a volume driver, only time is the fallback")
	}
	var skipped string
	for _, as := range m.Assumptions {
		if as.Name == "preferred drivers skipped" {
			skipped = as.Value
		}
	}
	if !strings.Contains(skipped, "cortex_query_fetched_chunk_bytes_total (no data)") {
		t.Errorf("trail's skipped drivers = %q, want the bytes metric named", skipped)
	}
}

// Series fetched is the third volume driver: used when neither bytes nor
// samples processed answered, and still not a fallback.
func TestReadQueryPath_FallsThroughToSeriesFetched(t *testing.T) {
	srv := metricServer(t, map[string]string{
		"cortex_query_fetched_series_total": rigSeries,
		"cortex_query_seconds_total":        rigQuerySeconds,
	}, nil)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if m.DriverMetric != "cortex_query_fetched_series_total" || m.DriverKind != "series fetched" || m.Fallback {
		t.Errorf("driver = %s / %s fallback=%v, want series fetched, not a fallback", m.DriverMetric, m.DriverKind, m.Fallback)
	}
	skipped := assumption(m, "preferred drivers skipped")
	for _, want := range []string{"cortex_query_fetched_chunk_bytes_total (no data)", "cortex_query_samples_processed_total (no data)"} {
		if !strings.Contains(skipped, want) {
			t.Errorf("skipped drivers = %q, want %q named", skipped, want)
		}
	}
}

func TestReadQueryPath_FallsBackToTimeAndSaysSo(t *testing.T) {
	srv := metricServer(t, map[string]string{"cortex_query_seconds_total": rigQuerySeconds}, nil)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if m.DriverMetric != "cortex_query_seconds_total" || m.Unit != cost.UnitSeconds {
		t.Errorf("driver = %s (%s), want query seconds", m.DriverMetric, m.Unit)
	}
	if !m.Fallback || !strings.Contains(m.FallbackNote, "depends on load") {
		t.Errorf("Fallback=%v note=%q, want the fallback flagged with why time is weaker", m.Fallback, m.FallbackNote)
	}
	a := cost.Allocate(m, cost.Inventory{}, nil)
	if !a.Fallback || !strings.Contains(strings.Join(a.Notes, "\n"), "depends on load") {
		t.Errorf("the allocation did not carry the fallback warning: %+v", a.Notes)
	}
}

// A volume metric that exists but is zero for every tenant carries no
// signal, so the chain moves on (as attribution.pickRankMetric does) — and
// if that lands on time, the fallback is flagged.
func TestReadQueryPath_AllZeroVolumeFallsThroughToTheNextDriverWithSignal(t *testing.T) {
	srv := metricServer(t, map[string]string{
		"cortex_query_fetched_chunk_bytes_total": vec("analytics", "0", "infra", "0"),
		"cortex_query_seconds_total":             rigQuerySeconds,
	}, nil)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if m.DriverMetric != "cortex_query_seconds_total" || !m.Fallback {
		t.Errorf("driver = %s fallback=%v, want time, flagged as the fallback", m.DriverMetric, m.Fallback)
	}
	if skipped := assumption(m, "preferred drivers skipped"); !strings.Contains(skipped, "cortex_query_fetched_chunk_bytes_total (all zero)") {
		t.Errorf("skipped drivers = %q, want the all-zero bytes metric named", skipped)
	}
}

// If nothing has signal, the report says every tenant reported 0 — it does
// not pretend there was no data, and it does not call the result a fallback.
func TestReadQueryPath_AllZeroEverywhereReportsZero(t *testing.T) {
	srv := metricServer(t, map[string]string{
		"cortex_query_fetched_chunk_bytes_total": vec("analytics", "0", "infra", "0"),
		"cortex_query_samples_processed_total":   vec("analytics", "0"),
	}, nil)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if m.DriverMetric != "cortex_query_fetched_chunk_bytes_total" || m.Fallback {
		t.Errorf("driver = %s fallback=%v, want the first (volume) metric kept", m.DriverMetric, m.Fallback)
	}
	a := cost.Allocate(m, cost.Inventory{}, nil)
	if len(a.Tenants) != 2 || a.Tenants[0].Share != nil {
		t.Errorf("tenants = %+v, want two tenants with undefined shares", a.Tenants)
	}
}

func TestReadQueryPath_NoDataAtAllIsEmptyNotAnError(t *testing.T) {
	srv := metricServer(t, nil, nil)
	defer srv.Close()

	m, err := source(srv).ReadQueryPath(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadQueryPath: %v", err)
	}
	if len(m.Drivers) != 0 || m.Fallback {
		t.Errorf("Drivers=%v Fallback=%v, want empty and not a fallback", m.Drivers, m.Fallback)
	}
	if !strings.Contains(strings.Join(m.Notes, "\n"), "no data for any query-path metric") {
		t.Errorf("Notes = %q, want the empty pool explained", m.Notes)
	}
	a := cost.Allocate(m, cost.Inventory{}, []string{"analytics"})
	if len(a.Unmeasured) != 1 {
		t.Errorf("Unmeasured = %v, want analytics: no data is not zero", a.Unmeasured)
	}
}

// Remote evaluation, as on the k3d rig: the ruler records no query time of
// its own, and the evaluation seconds are the driver, flagged as mostly
// waiting on the query path.
func TestReadRulerCPU_RemoteEvaluation(t *testing.T) {
	var queries []string
	srv := metricServer(t, map[string]string{
		"cortex_prometheus_rule_evaluation_duration_seconds_sum": rigEvalSeconds,
	}, &queries)
	defer srv.Close()

	m, err := source(srv).ReadRulerCPU(context.Background(), 10*time.Minute)
	if err != nil {
		t.Fatalf("ReadRulerCPU: %v", err)
	}
	if got := m.Drivers["analytics"]; got < 357.7 || got > 357.9 {
		t.Errorf("analytics = %v, want the evaluation seconds as reported", got)
	}
	if _, ok := m.Drivers["monitoring"]; ok {
		t.Error("monitoring runs no rules and must be absent (unmeasured), not 0")
	}
	if m.Unit != cost.UnitSeconds || !m.Unreplicated {
		t.Errorf("Unit=%s Unreplicated=%v, want seconds and no replication factor", m.Unit, m.Unreplicated)
	}
	mode := assumption(m, "rule evaluation")
	if !strings.HasPrefix(mode, "remote") || !strings.Contains(mode, "priced in the query path") {
		t.Errorf("rule evaluation mode = %q, want remote, with the query cost in pool 6", mode)
	}
	if !strings.Contains(strings.Join(m.Notes, "\n"), "waiting on the query path") {
		t.Errorf("Notes = %q, want the caveat that remote time is mostly waiting", m.Notes)
	}
	if len(queries) != 2 {
		t.Errorf("issued %d queries, want 2 (evaluation seconds, and the local/remote probe)", len(queries))
	}
}

// Local evaluation: the ruler's own query time is nested inside the
// evaluation time. It must not be subtracted — that would delete the
// ruler's main cost and leave its bookkeeping.
func TestReadRulerCPU_LocalEvaluationKeepsTheNestedQueryTime(t *testing.T) {
	srv := metricServer(t, map[string]string{
		// ADR 0003's compose-rig figures for infra: 292.05s of evaluation,
		// of which 221.65s were the ruler's own queries.
		"cortex_prometheus_rule_evaluation_duration_seconds_sum": vec("infra", "292.05"),
		"cortex_ruler_query_seconds_total":                       vec("infra", "221.65"),
	}, nil)
	defer srv.Close()

	m, err := source(srv).ReadRulerCPU(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadRulerCPU: %v", err)
	}
	if got := m.Drivers["infra"]; got != 292.05 {
		t.Errorf("infra = %v, want 292.05 whole: the ruler ran the queries, so all of it is ruler time", got)
	}
	mode := assumption(m, "rule evaluation")
	if !strings.HasPrefix(mode, "local") {
		t.Errorf("rule evaluation mode = %q, want local", mode)
	}
	if strings.Contains(strings.Join(m.Notes, "\n"), "waiting") {
		t.Errorf("Notes = %q: the waiting-on-query-path caveat is for remote evaluation only", m.Notes)
	}
}

func assumption(m cost.Measurement, name string) string {
	for _, as := range m.Assumptions {
		if as.Name == name {
			return as.Value
		}
	}
	return ""
}

func TestCounterReaders_QueriesAreValidPromQL(t *testing.T) {
	p := parser.NewParser(parser.Options{})
	for _, w := range []time.Duration{5 * time.Minute, 24 * time.Hour, 30 * 24 * time.Hour} {
		var queries []string
		srv := metricServer(t, nil, &queries) // every metric empty: the whole chain is walked
		src := source(srv)
		if _, err := src.ReadQueryPath(context.Background(), w); err != nil {
			t.Fatalf("window %v: %v", w, err)
		}
		if _, err := src.ReadRulerCPU(context.Background(), w); err != nil {
			t.Fatalf("window %v: %v", w, err)
		}
		srv.Close()
		if len(queries) != 6 {
			t.Errorf("window %v: issued %d queries, want 6 (four query-path drivers, two ruler)", w, len(queries))
		}
		for _, q := range queries {
			if _, err := p.ParseExpr(q); err != nil {
				t.Errorf("window %v: %q is not valid PromQL: %v", w, q, err)
			}
		}
	}
}

func TestCounterReaders_MetricsTenantRequired(t *testing.T) {
	src := New(Config{BaseURL: "http://unused"})
	if _, err := src.ReadQueryPath(context.Background(), time.Hour); err == nil {
		t.Error("ReadQueryPath: expected an error without MetricsTenant")
	}
	if _, err := src.ReadRulerCPU(context.Background(), time.Hour); err == nil {
		t.Error("ReadRulerCPU: expected an error without MetricsTenant")
	}
}

// The end-to-end rule the ADR 0003 [A2] scenario relies on: two rules that
// write one series each move pool 7 and pool 6 a lot and pool 1 not at all.
// Figures are analytics' scenario 3 on the k3d rig, per 5m: rule evaluation
// 3.2s -> 15.2s, query time 16.0s -> 76.3s, against three other tenants that
// did not change.
func TestExpensiveRulesMoveRulerAndQueryPathShares(t *testing.T) {
	read := func(analyticsEval, analyticsQuery string) (cost.PoolAllocation, cost.PoolAllocation) {
		srv := metricServer(t, map[string]string{
			"cortex_prometheus_rule_evaluation_duration_seconds_sum": vec("analytics", analyticsEval, "infra", "3.4", "payments", "1.1", "platform", "0.4"),
			"cortex_query_seconds_total":                             vec("analytics", analyticsQuery, "infra", "14.1", "payments", "2.0", "platform", "0.5"),
		}, nil)
		defer srv.Close()
		src := source(srv)
		ruler, err := src.ReadRulerCPU(context.Background(), 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		qp, err := src.ReadQueryPath(context.Background(), 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return cost.Allocate(ruler, cost.Inventory{}, nil), cost.Allocate(qp, cost.Inventory{}, nil)
	}
	shareOf := func(a cost.PoolAllocation) float64 {
		for _, ts := range a.Tenants {
			if ts.Tenant == "analytics" {
				return *ts.Share
			}
		}
		t.Fatal("analytics missing")
		return 0
	}
	rulerBefore, qpBefore := read("3.2", "16.0")
	rulerAfter, qpAfter := read("15.2", "76.3")
	if shareOf(rulerAfter) < shareOf(rulerBefore)+0.2 {
		t.Errorf("analytics pool-7 share %.2f -> %.2f, want a rise of more than 20 points", shareOf(rulerBefore), shareOf(rulerAfter))
	}
	if shareOf(qpAfter) < shareOf(qpBefore)+0.2 {
		t.Errorf("analytics pool-6 share %.2f -> %.2f, want a rise of more than 20 points", shareOf(qpBefore), shareOf(qpAfter))
	}
	if !qpAfter.Fallback {
		t.Error("only query seconds were served, so pool 6 must be flagged as the time fallback")
	}
}
