package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// writeLoop sends one sample per series every Interval. Each tick's samples
// share one timestamp. A churny tenant's series carry a `generation` label
// that changes every churn period, aligned to unix time.
func writeLoop(ctx context.Context, t Tenant, store *truthStore, d tenantDeps) {
	interval := t.Series.Interval.D
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	gen := int64(-1)
	var ss []series
	values := make([]float64, t.Series.Live())
	for now := time.Now(); ; {
		g := int64(0)
		if t.Churn != nil {
			g = now.Unix() / int64(t.Churn.Period.D/time.Second)
		}
		if g != gen {
			ss = buildSeries(t, g)
			clear(values)
			gen = g
		}
		for i := range ss {
			values[i] += float64(1 + i%10)
			ss[i].value = values[i]
		}
		sent, failed, reqFailed, err := pushAll(ctx, d.pusher, ss, now.UnixMilli(), d.batch)
		store.update(t.Name, func(tt *TenantTruth) {
			c := &tt.Counters
			c.SamplesSent += sent
			c.SamplesFailed += failed
			c.WriteRequestsFailed += reqFailed
			if err != nil {
				c.fail(err)
			}
			if sent > 0 {
				seg := &tt.Segments[len(tt.Segments)-1]
				at := now.UTC()
				if seg.FirstSampleAt == nil {
					seg.FirstSampleAt = &at
				}
				seg.LastSampleAt = &at
			}
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("[%s] write: %d/%d samples failed: %v", t.Name, failed, sent+failed, err)
		}
		select {
		case <-ctx.Done():
			return
		case now = <-ticker.C:
		}
	}
}

func buildSeries(t Tenant, generation int64) []series {
	s := t.Series
	out := make([]series, 0, s.Live())
	for m := 0; m < s.Metrics; m++ {
		name := metricName(t.Name, m)
		for j := 0; j < s.PerMetric; j++ {
			ls := []label{
				{"__name__", name},
				{"id", strconv.Itoa(j)},
				{"job", "loadgen"},
				{"shard", strconv.Itoa(j % s.Shards)},
			}
			if t.Churn != nil {
				ls = append(ls, label{"generation", strconv.FormatInt(generation, 10)})
			}
			for k := 0; k < s.ExtraLabels; k++ {
				ls = append(ls, label{fmt.Sprintf("pad_%02d", k), "padding-value-to-make-series-bigger"})
			}
			sort.Slice(ls, func(a, b int) bool { return ls[a].name < ls[b].name })
			out = append(out, series{labels: ls})
		}
	}
	return out
}

// pushAll sends ss in batches, four requests in flight, and reports how
// many samples were accepted and rejected.
func pushAll(ctx context.Context, p *pusher, ss []series, ts int64, batch int) (sent, failed, reqFailed int64, firstErr error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for lo := 0; lo < len(ss); lo += batch {
		hi := min(lo+batch, len(ss))
		wg.Add(1)
		sem <- struct{}{}
		go func(b []series) {
			defer wg.Done()
			defer func() { <-sem }()
			err := p.push(ctx, b, ts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed += int64(len(b))
				reqFailed++
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			sent += int64(len(b))
		}(ss[lo:hi])
	}
	wg.Wait()
	return
}

// expandPanels substitutes $prefix (the tenant's metric-name prefix) into
// each panel's PromQL.
func expandPanels(t Tenant) []string {
	if t.Queries == nil {
		return nil
	}
	out := make([]string, len(t.Queries.Panels))
	for i, q := range t.Queries.Panels {
		out[i] = strings.ReplaceAll(q, "$prefix", sanitize(t.Name))
	}
	return out
}

type queryClient struct {
	client  *http.Client
	base    string
	tenant  string
	headers map[string]string
}

// rangeQuery runs one query_range and returns the response size.
func (q *queryClient) rangeQuery(ctx context.Context, expr string, start, end time.Time, step time.Duration) (int64, error) {
	v := url.Values{}
	v.Set("query", expr)
	v.Set("start", strconv.FormatInt(start.Unix(), 10))
	v.Set("end", strconv.FormatInt(end.Unix(), 10))
	v.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/prometheus/api/v1/query_range", strings.NewReader(v.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Scope-OrgID", q.tenant)
	for k, val := range q.headers {
		req.Header.Set(k, val)
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, _ := io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return n, fmt.Errorf("query_range: HTTP %d", resp.StatusCode)
	}
	return n, nil
}

// queryLoop is Viewers copies of one dashboard, each re-running every panel
// every Refresh, staggered across the refresh interval. End is aligned to
// the step, as Grafana does, which makes consecutive refreshes cacheable by
// a query-frontend results cache: that is realistic, and is why fetched
// volume is compared by rank, not by value.
func queryLoop(ctx context.Context, t Tenant, store *truthStore, q *queryClient) {
	spec := t.Queries
	panels := expandPanels(t)
	viewers := max(spec.Viewers, 1)
	var wg sync.WaitGroup
	for v := 0; v < viewers; v++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			offset := spec.Refresh.D * time.Duration(v) / time.Duration(viewers)
			select {
			case <-ctx.Done():
				return
			case <-time.After(offset):
			}
			ticker := time.NewTicker(spec.Refresh.D)
			defer ticker.Stop()
			for {
				end := time.Now().Truncate(spec.Step.D)
				for _, expr := range panels {
					n, err := q.rangeQuery(ctx, expr, end.Add(-spec.Range.D), end, spec.Step.D)
					if ctx.Err() != nil {
						return
					}
					store.update(t.Name, func(tt *TenantTruth) {
						c := &tt.Counters
						c.QueryResponseBytes += n
						if err != nil {
							c.QueriesFailed++
							c.fail(err)
						} else {
							c.QueriesOK++
						}
					})
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(v)
	}
	wg.Wait()
}
