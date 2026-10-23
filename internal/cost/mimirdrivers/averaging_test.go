package mimirdrivers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/KorhanOzturk90/obscost/internal/cost"
)

// The window of every test here: 1h at 1m steps, evaluated at t=60m, so
// the subquery points are minutes 1..60.
const (
	evalMinute = 60
	gapStart   = 10 // Mimir's own monitoring is down for minutes 10..14:
	gapEnd     = 15 // every series goes stale, as on a failed scrape.
)

// presence is when each tenant has series, by minute, before the gap is
// applied. latecomer joins at 30m, ghost leaves after 44m — and, as Mimir
// does, its series is dropped rather than reported as 0 (#66).
var presence = map[string]struct {
	size     float64
	from, to int // inclusive minutes
}{
	"steady":    {1000, 0, 60},
	"latecomer": {2000, 30, 60},
	"ghost":     {3000, 0, 44},
}

func inGap(m int) bool { return m >= gapStart && m < gapEnd }

// truth is the time-weighted average over the points Mimir's metrics had
// data at, absence counted as 0 — what pool 1 should report — and the
// number of those points.
func truth(tenant string) (float64, int) {
	p := presence[tenant]
	var sum float64
	points := 0
	for m := 1; m <= evalMinute; m++ {
		if inGap(m) {
			continue
		}
		points++
		if m >= p.from && m <= p.to {
			sum += p.size
		}
	}
	return sum / float64(points), points
}

// loadSeries builds one ingester series per tenant and replica, sampled
// every minute while present, with a staleness marker where it stops — as
// Prometheus writes when a series disappears or a scrape fails.
func loadSeries(lbls func(tenant string, replica int) labels.Labels, replicas int) []storage.Series {
	var out []storage.Series
	for tenant, p := range presence {
		for r := 0; r < replicas; r++ {
			var samples []chunks.Sample
			was := false
			for m := 0; m <= evalMinute; m++ {
				present := m >= p.from && m <= p.to && !inGap(m)
				switch {
				case present:
					samples = append(samples, fsample{int64(m) * 60000, p.size})
				case was:
					samples = append(samples, fsample{int64(m) * 60000, math.Float64frombits(value.StaleNaN)})
				}
				was = present
			}
			out = append(out, storage.NewListSeries(lbls(tenant, r), samples))
		}
	}
	return out
}

// constant is a series present at every minute.
func constant(v float64, lbls labels.Labels) storage.Series {
	var samples []chunks.Sample
	for m := 0; m <= evalMinute; m++ {
		samples = append(samples, fsample{int64(m) * 60000, v})
	}
	return storage.NewListSeries(lbls, samples)
}

type fsample struct {
	t int64
	f float64
}

func (s fsample) T() int64                      { return s.t }
func (s fsample) ST() int64                     { return 0 }
func (s fsample) F() float64                    { return s.f }
func (s fsample) H() *histogram.Histogram       { return nil }
func (s fsample) FH() *histogram.FloatHistogram { return nil }
func (s fsample) Type() chunkenc.ValueType      { return chunkenc.ValFloat }
func (s fsample) Copy() chunks.Sample           { return s }

// memQuerier selects from a fixed list of series by matchers.
type memQuerier struct{ series []storage.Series }

func (q memQuerier) Querier(int64, int64) (storage.Querier, error) { return q, nil }
func (memQuerier) LabelValues(context.Context, string, *storage.LabelHints, ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (memQuerier) LabelNames(context.Context, *storage.LabelHints, ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (memQuerier) Close() error { return nil }
func (q memQuerier) Select(_ context.Context, _ bool, _ *storage.SelectHints, ms ...*labels.Matcher) storage.SeriesSet {
	var sel []storage.Series
	for _, s := range q.series {
		ok := true
		for _, m := range ms {
			ok = ok && m.Matches(s.Labels().Get(m.Name))
		}
		if ok {
			sel = append(sel, s)
		}
	}
	slices.SortFunc(sel, func(a, b storage.Series) int { return labels.Compare(a.Labels(), b.Labels()) })
	return &listSet{series: sel, i: -1}
}

type listSet struct {
	series []storage.Series
	i      int
}

func (s *listSet) Next() bool                      { s.i++; return s.i < len(s.series) }
func (s *listSet) At() storage.Series              { return s.series[s.i] }
func (*listSet) Err() error                        { return nil }
func (*listSet) Warnings() annotations.Annotations { return nil }

// engineServer answers /api/v1/query by evaluating the query with the real
// PromQL engine over series, at t=60m: the exact queries ReadIngesterMemory
// builds are run, not matched.
func engineServer(t *testing.T, series []storage.Series) *httptest.Server {
	t.Helper()
	engine := promql.NewEngine(promql.EngineOpts{MaxSamples: 1e6, Timeout: time.Minute, LookbackDelta: 5 * time.Minute})
	q := memQuerier{series: series}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		qs := r.URL.Query().Get("query")
		qry, err := engine.NewInstantQuery(r.Context(), q, nil, qs, time.Unix(evalMinute*60, 0))
		if err != nil {
			t.Errorf("%q: %v", qs, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		res := qry.Exec(r.Context())
		vec, err := res.Vector()
		if res.Err != nil || err != nil {
			t.Errorf("%q: %v %v", qs, res.Err, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		type sample struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		}
		out := []sample{}
		for _, s := range vec {
			out = append(out, sample{Metric: s.Metric.Map(), Value: [2]any{evalMinute * 60, strconv.FormatFloat(s.F, 'f', -1, 64)}})
		}
		body, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": out}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
}

// Issue #66: a tenant present for part of the window is averaged over the
// whole window, its absence counting as 0, and a gap in Mimir's own
// metrics is left out rather than read as every tenant dropping to zero.
// Both write paths, through the real engine; the replication divisor and
// the ingest-storage guard are untouched.
func TestReadIngesterMemory_AveragesOverTheWholeWindow(t *testing.T) {
	ing := func(pod, tenant string) labels.Labels {
		return labels.FromStrings("__name__", "cortex_ingester_active_series", "cluster", "c", "namespace", "mimir", "job", "mimir/ingester", "pod", pod, "user", tenant)
	}
	rf2 := constant(2, labels.FromStrings("__name__", "cortex_distributor_replication_factor"))
	partitions := constant(1, labels.FromStrings("__name__", "cortex_partition_ring_partitions", "cluster", "c", "namespace", "mimir", "job", "mimir/ingester"))
	for name, tc := range map[string]struct {
		series []storage.Series
		arch   string
	}{
		// Two ingesters each holding every tenant: RF 2, divided in internal/cost.
		"classic": {append(loadSeries(func(tenant string, r int) labels.Labels {
			return ing(fmt.Sprintf("ingester-%d", r), tenant)
		}, 2), rf2), "classic"},
		// One partition owned in two zones: collapsed with max, no divisor.
		"ingest storage": {append(loadSeries(func(tenant string, r int) labels.Labels {
			return ing(fmt.Sprintf("ingester-zone-%c-0", 'a'+r), tenant)
		}, 2), rf2, partitions), "ingest storage"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := engineServer(t, tc.series)
			defer srv.Close()

			m, err := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"}).ReadIngesterMemory(context.Background(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if m.Architecture != tc.arch {
				t.Fatalf("Architecture = %q, want %q", m.Architecture, tc.arch)
			}
			a := cost.Allocate(m, cost.Inventory{}, nil)
			if len(a.Tenants) != 3 {
				t.Fatalf("tenants = %+v, want all three measured", a.Tenants)
			}
			for _, ts := range a.Tenants {
				want, _ := truth(ts.Tenant)
				if ts.Driver == nil || math.Abs(*ts.Driver-want) > 0.01 {
					t.Errorf("%s = %v, want the time-weighted %.2f", ts.Tenant, deref(ts.Driver), want)
				}
			}
			_, points := truth("steady")
			want := fmt.Sprintf("points where Mimir's own metrics were missing are excluded (%d of 60)", points)
			if got := assumption(m, "averaging"); !strings.Contains(got, want) ||
				!strings.HasPrefix(got, "averaged over the whole window; points where the tenant had no series count as 0") {
				t.Errorf("averaging = %q, want the rule and %q", got, want)
			}
		})
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
