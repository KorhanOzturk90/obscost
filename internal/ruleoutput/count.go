package ruleoutput

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/promapi"
)

// Querier is the one promapi.Client method the counter needs, so tests can
// substitute a fake without a server.
type Querier interface {
	Instant(ctx context.Context, promql string) ([]promapi.Sample, error)
}

// CountConfig configures Count.
type CountConfig struct {
	// NewQuerier returns a client whose queries run *as* tenant, i.e. with
	// the tenancy header set to it. That differs from the cost drivers,
	// which run as the metrics tenant: a recording rule's output lives in
	// the TSDB of the tenant that owns the rule.
	NewQuerier func(tenant string) Querier
	// MaxNamesPerQuery caps how many metric names share one regex.
	// Default 50.
	MaxNamesPerQuery int
	// MaxRegexBytes caps the regex's length, because the query travels in
	// a GET URL and proxies commonly cap those around 8KiB. Default 4000.
	// A single name longer than this still gets a query of its own.
	MaxRegexBytes int
	// Concurrency caps queries in flight across all tenants. Default 4.
	Concurrency int
	// Now stamps the measurement. Defaults to time.Now.
	Now func() time.Time
}

// MetricCount is one output metric's series count. Series is nil when it
// was not measured, and then Err says why. A measured metric with no
// series right now has Series pointing at 0: that is a real measurement,
// not a missing one.
type MetricCount struct {
	Series *int64
	Err    string
}

// Counts is the result of Count.
type Counts struct {
	// At is when the queries were issued. Every count is an instant query
	// evaluated at the server's current time, so this is a point-in-time
	// figure, not a window.
	At time.Time
	// QueryShape is the PromQL issued, with the name list elided.
	QueryShape string
	// Queries is how many instant queries were issued.
	Queries int
	// Tenants maps tenant -> metric name -> count. Every requested
	// (tenant, metric) pair is present, measured or not.
	Tenants map[string]map[string]MetricCount
}

// CountQueryShape is the query Count issues per chunk of names.
const CountQueryShape = `count by (__name__) ({__name__=~"<name>|<name>|..."})`

// Count counts, per tenant, the series currently present for each metric
// name in want (tenant -> metric names).
//
// It batches: rather than one count() per recording rule, which on a
// mixin-sized corpus is hundreds of queries per tenant, each query counts
// a chunk of names at once with count by (__name__) over an alternation
// regex, and a name absent from the result has no series. Queries run with
// at most Concurrency in flight.
//
// A failed query marks every name in its chunk not measured, with the
// error as the reason; the other chunks are unaffected. Count itself never
// fails, so one tenant's bad credentials or one chunk hitting a
// max-fetched-series limit cannot take down the rest of the report.
func Count(ctx context.Context, cfg CountConfig, want map[string][]string) Counts {
	if cfg.MaxNamesPerQuery <= 0 {
		cfg.MaxNamesPerQuery = 50
	}
	if cfg.MaxRegexBytes <= 0 {
		cfg.MaxRegexBytes = 4000
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	out := Counts{
		At:         cfg.Now(),
		QueryShape: CountQueryShape,
		Tenants:    map[string]map[string]MetricCount{},
	}

	type job struct {
		tenant string
		q      Querier
		names  []string
	}
	var jobs []job
	tenants := make([]string, 0, len(want))
	for t := range want {
		tenants = append(tenants, t)
	}
	sort.Strings(tenants)
	for _, t := range tenants {
		names := dedupe(want[t])
		out.Tenants[t] = make(map[string]MetricCount, len(names))
		if len(names) == 0 {
			continue
		}
		q := cfg.NewQuerier(t)
		for _, c := range chunk(names, cfg.MaxNamesPerQuery, cfg.MaxRegexBytes) {
			jobs = append(jobs, job{tenant: t, q: q, names: c})
		}
	}
	out.Queries = len(jobs)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, cfg.Concurrency)
	)
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				for _, n := range j.names {
					out.Tenants[j.tenant][n] = MetricCount{Err: ctx.Err().Error()}
				}
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			res := countChunk(ctx, j.q, j.names)
			mu.Lock()
			for n, c := range res {
				out.Tenants[j.tenant][n] = c
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func countChunk(ctx context.Context, q Querier, names []string) map[string]MetricCount {
	res := make(map[string]MetricCount, len(names))
	samples, err := q.Instant(ctx, CountQuery(names))
	if err != nil {
		for _, n := range names {
			res[n] = MetricCount{Err: err.Error()}
		}
		return res
	}
	asked := make(map[string]bool, len(names))
	for _, n := range names {
		asked[n] = true
	}
	found := map[string]int64{}
	bad := map[string]bool{}
	for _, s := range samples {
		name := s.Metric["__name__"]
		if !asked[name] {
			continue
		}
		v, ok := s.Float()
		if !ok || v < 0 {
			bad[name] = true
			continue
		}
		found[name] += int64(math.Round(v))
	}
	for _, n := range names {
		switch {
		case bad[n]:
			res[n] = MetricCount{Err: "count returned a value that is not a finite non-negative number"}
		default:
			// Absent from a successful count by (__name__) means no series
			// matched right now: a measured zero, not a missing value.
			v := found[n]
			res[n] = MetricCount{Series: &v}
		}
	}
	return res
}

// CountQuery builds the batched count for names. Each name is regex-quoted
// (a recording rule name may contain '.' under UTF-8 metric names, and must
// match only itself) and the whole regex is then a PromQL string literal,
// which PromQL unquotes with Go's rules, so strconv.Quote is exact.
func CountQuery(names []string) string {
	return fmt.Sprintf(`count by (__name__) ({__name__=~%s})`, strconv.Quote(regexFor(names)))
}

func regexFor(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return strings.Join(quoted, "|")
}

// chunk splits sorted names into groups of at most maxNames whose regex
// stays within maxBytes.
func chunk(names []string, maxNames, maxBytes int) [][]string {
	var (
		out  [][]string
		cur  []string
		size int
	)
	for _, n := range names {
		l := len(regexp.QuoteMeta(n)) + 1 // +1 for the '|'
		if len(cur) > 0 && (len(cur) >= maxNames || size+l > maxBytes) {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, n)
		size += l
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func dedupe(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
