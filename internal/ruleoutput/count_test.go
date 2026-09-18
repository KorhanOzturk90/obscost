package ruleoutput

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/promapi"
)

// fakeMimir answers batched count queries from a per-tenant table of
// metric -> series count, the way Mimir would: it parses the PromQL with
// the real parser, applies the __name__ matcher to the tenant's metric
// names, and returns one sample per matching name. Names it has no series
// for are absent from the result, as with a real count().
type fakeMimir struct {
	series   map[string]map[string]int // tenant -> metric -> series
	failFor  map[string]bool           // metric names whose chunk returns 422
	queries  atomic.Int64
	inFlight atomic.Int64
	maxSeen  atomic.Int64
	delay    time.Duration

	mu     sync.Mutex
	tenant []string
}

func (f *fakeMimir) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.queries.Add(1)
		n := f.inFlight.Add(1)
		defer f.inFlight.Add(-1)
		for {
			m := f.maxSeen.Load()
			if n <= m || f.maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(f.delay)

		tenant := r.Header.Get("X-Scope-OrgID")
		f.mu.Lock()
		f.tenant = append(f.tenant, tenant)
		f.mu.Unlock()

		q := r.URL.Query().Get("query")
		expr, err := parser.NewParser(parser.Options{}).ParseExpr(q)
		if err != nil {
			t.Errorf("server got invalid PromQL %q: %v", q, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		agg, ok := expr.(*parser.AggregateExpr)
		if !ok || agg.Op.String() != "count" || len(agg.Grouping) != 1 || agg.Grouping[0] != "__name__" {
			t.Errorf("want count by (__name__), got %q", q)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		vs := agg.Expr.(*parser.VectorSelector)
		var m *labels.Matcher
		for _, lm := range vs.LabelMatchers {
			if lm.Name == "__name__" {
				m = lm
			}
		}
		var result []map[string]any
		for name, n := range f.series[tenant] {
			if !m.Matches(name) {
				continue
			}
			if f.failFor[name] {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"status":"error","errorType":"execution","error":"the query exceeded the maximum number of series"}`))
				return
			}
			if n == 0 {
				continue
			}
			result = append(result, map[string]any{
				"metric": map[string]string{"__name__": name},
				"value":  []any{1757800000, fmt.Sprint(n)},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": result},
		})
	})
}

func countAgainst(t *testing.T, srv *httptest.Server, cfg CountConfig, want map[string][]string) Counts {
	t.Helper()
	cfg.NewQuerier = func(tenant string) Querier {
		return promapi.New(promapi.Config{BaseURL: srv.URL, Tenant: tenant})
	}
	return Count(context.Background(), cfg, want)
}

func TestCount_BatchesAndDistinguishesZeroFromMissing(t *testing.T) {
	f := &fakeMimir{series: map[string]map[string]int{
		"infra": {
			"job:up:sum":       3,
			"instance:cpu:sum": 120,
			// never_evaluated:sum has no series at all
		},
		"payments": {"job:up:sum": 7},
	}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	got := countAgainst(t, srv, CountConfig{}, map[string][]string{
		"infra":    {"job:up:sum", "instance:cpu:sum", "never_evaluated:sum", "job:up:sum"},
		"payments": {"job:up:sum"},
	})

	if got.Queries != 2 {
		t.Errorf("Queries = %d, want 2 (one batched query per tenant)", got.Queries)
	}
	if f.queries.Load() != 2 {
		t.Errorf("server saw %d queries, want 2", f.queries.Load())
	}
	check := func(tenant, metric string, want int64) {
		t.Helper()
		c, ok := got.Tenants[tenant][metric]
		if !ok || c.Series == nil {
			t.Fatalf("%s/%s not measured: %+v", tenant, metric, c)
		}
		if *c.Series != want {
			t.Errorf("%s/%s = %d, want %d", tenant, metric, *c.Series, want)
		}
	}
	check("infra", "job:up:sum", 3)
	check("infra", "instance:cpu:sum", 120)
	check("infra", "never_evaluated:sum", 0) // measured zero, not missing
	check("payments", "job:up:sum", 7)

	// Each tenant's names were counted as that tenant, not as some shared
	// metrics tenant.
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := strings.Join(f.tenant, ",")
	if !strings.Contains(seen, "infra") || !strings.Contains(seen, "payments") {
		t.Errorf("queries ran as tenants %q, want infra and payments", seen)
	}
}

func TestCount_FailedChunkIsNotMeasuredOthersSurvive(t *testing.T) {
	f := &fakeMimir{
		series: map[string]map[string]int{"infra": {
			"a:x": 1, "b:x": 2, "c:x": 3, "d:x": 4,
		}},
		failFor: map[string]bool{"c:x": true},
	}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	got := countAgainst(t, srv, CountConfig{MaxNamesPerQuery: 2}, map[string][]string{
		"infra": {"a:x", "b:x", "c:x", "d:x"},
	})
	if got.Queries != 2 {
		t.Fatalf("Queries = %d, want 2", got.Queries)
	}
	for _, n := range []string{"a:x", "b:x"} {
		if c := got.Tenants["infra"][n]; c.Series == nil {
			t.Errorf("%s should be measured, got %+v", n, c)
		}
	}
	for _, n := range []string{"c:x", "d:x"} {
		c := got.Tenants["infra"][n]
		if c.Series != nil {
			t.Errorf("%s shares a chunk with a failed query and must be not measured (nil), got %d", n, *c.Series)
		}
		if !strings.Contains(c.Err, "422") {
			t.Errorf("%s: reason should carry the server error, got %q", n, c.Err)
		}
	}
}

func TestCount_ConcurrencyIsBounded(t *testing.T) {
	names := make([]string, 40)
	series := map[string]int{}
	for i := range names {
		names[i] = fmt.Sprintf("rule_%02d:sum", i)
		series[names[i]] = i + 1
	}
	f := &fakeMimir{series: map[string]map[string]int{"infra": series}, delay: 20 * time.Millisecond}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	got := countAgainst(t, srv, CountConfig{MaxNamesPerQuery: 1, Concurrency: 3}, map[string][]string{"infra": names})
	if got.Queries != 40 {
		t.Fatalf("Queries = %d, want 40", got.Queries)
	}
	if m := f.maxSeen.Load(); m > 3 {
		t.Errorf("saw %d queries in flight, want at most 3", m)
	}
	for i, n := range names {
		if c := got.Tenants["infra"][n]; c.Series == nil || *c.Series != int64(i+1) {
			t.Errorf("%s = %+v, want %d", n, c, i+1)
		}
	}
}

func TestCountQuery_EscapesNamesExactly(t *testing.T) {
	// A '.' is legal in a UTF-8 metric name and is a regex metacharacter;
	// unescaped, "a.b" would also count a series named "aXb". Quotes and
	// backslashes must survive the PromQL string literal.
	names := []string{"a.b", `we"ird\name`, "job:up:sum", "x+y", "p|q"}
	q := CountQuery(names)
	expr, err := parser.NewParser(parser.Options{}).ParseExpr(q)
	if err != nil {
		t.Fatalf("CountQuery produced invalid PromQL %q: %v", q, err)
	}
	vs := expr.(*parser.AggregateExpr).Expr.(*parser.VectorSelector)
	m := vs.LabelMatchers[0]
	for _, n := range names {
		if !m.Matches(n) {
			t.Errorf("matcher %q does not match %q", m, n)
		}
	}
	for _, n := range []string{"aXb", "a", "b", "x", "xy", "xxy", "p", "q", "job:up:sum2", "job:up:su"} {
		if m.Matches(n) {
			t.Errorf("matcher %q wrongly matches %q", m, n)
		}
	}
}

func TestChunk_RespectsNameAndByteLimits(t *testing.T) {
	names := []string{"aaaa", "bbbb", "cccc", "dddd", "eeee"}
	if got := chunk(names, 2, 1000); len(got) != 3 {
		t.Errorf("by count: %d chunks, want 3: %v", len(got), got)
	}
	// Each name costs 5 bytes (4 + separator); 12 bytes fits two.
	got := chunk(names, 100, 12)
	if len(got) != 3 {
		t.Errorf("by bytes: %d chunks, want 3: %v", len(got), got)
	}
	for _, c := range got {
		if r := regexFor(c); len(r) > 12 {
			t.Errorf("chunk regex %q exceeds 12 bytes", r)
		}
	}
	// A single oversized name still gets its own chunk.
	long := strings.Repeat("z", 50)
	if got := chunk([]string{long}, 10, 12); len(got) != 1 || got[0][0] != long {
		t.Errorf("oversized name: %v", got)
	}
	if !regexp.MustCompile(`^aaaa\|bbbb$`).MatchString(regexFor([]string{"aaaa", "bbbb"})) {
		t.Errorf("regexFor joined incorrectly: %q", regexFor([]string{"aaaa", "bbbb"}))
	}
}

func TestCount_CancelledContextIsNotMeasured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := Count(ctx, CountConfig{NewQuerier: func(string) Querier {
		return promapi.New(promapi.Config{BaseURL: "http://127.0.0.1:1"})
	}}, map[string][]string{"infra": {"a:x"}})
	if c := got.Tenants["infra"]["a:x"]; c.Series != nil || c.Err == "" {
		t.Errorf("want not measured with a reason, got %+v", c)
	}
}
