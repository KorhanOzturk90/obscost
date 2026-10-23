package mimirdrivers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/promapi"
)

// Issue #54: every query a Source sends — each pool's drivers, the whole
// pool-6 chain, the replication factor, the evaluation-mode probe and the
// cluster probe — carries the selector on every vector selector. Checked
// by parsing what the server received, not by string matching.
func TestSelector_EveryQueryCarriesIt(t *testing.T) {
	sel, err := promapi.ParseSelector(`{cluster="prod-eu", namespace=~"mimir|mimir-.*"}`)
	if err != nil {
		t.Fatal(err)
	}
	var queries []string
	srv := metricServer(t, nil, &queries) // every metric empty: every fallback is walked
	defer srv.Close()
	src := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring", Selector: sel, SelectorSource: "test"})

	ctx := context.Background()
	var ms []cost.Measurement
	for _, read := range []func(context.Context, time.Duration) (cost.Measurement, error){src.ReadIngesterMemory, src.ReadQueryPath, src.ReadRulerCPU} {
		m, err := read(ctx, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		ms = append(ms, m)
	}
	if _, err := src.Clusters(ctx); err != nil {
		t.Fatal(err)
	}
	// 3 for pool 1 (classic, ingest storage, RF), 4 for the pool-6 chain, 4
	// for the shared evaluation-mode probe, 1 cluster probe.
	if len(queries) != 12 {
		t.Errorf("sent %d queries, want 12: %q", len(queries), queries)
	}
	for _, m := range ms {
		queries = append(queries, m.DriverQuery)
		if m.RFQuery != "" {
			queries = append(queries, m.RFQuery)
		}
		if !strings.Contains(assumption(m, "cluster selector"), `{cluster="prod-eu"`) {
			t.Errorf("pool %s: no cluster selector in the assumption trail", m.Pool.ID)
		}
	}
	for _, q := range queries {
		expr, err := parser.NewParser(parser.Options{}).ParseExpr(q)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		n := 0
		parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
			vs, ok := node.(*parser.VectorSelector)
			if !ok {
				return nil
			}
			n++
			var cluster, namespace bool
			for _, m := range vs.LabelMatchers {
				cluster = cluster || (m.Name == "cluster" && m.Type == labels.MatchEqual && m.Value == "prod-eu")
				namespace = namespace || (m.Name == "namespace" && m.Type == labels.MatchRegexp && m.Value == "mimir|mimir-.*")
			}
			if !cluster || !namespace {
				t.Errorf("%q: vector selector %s lacks the selector", q, vs)
			}
			return nil
		})
		if n == 0 {
			t.Errorf("%q: no vector selector found", q)
		}
	}
}
