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

// An RF=3 cluster: each tenant's active series, summed across ingesters,
// is three times its real count. The rig runs RF=1 and so cannot tell a
// missing divisor from a present one; this fixture can (ADR 0003 [A]).
const activeSeriesRF3 = `{"status":"success","data":{"resultType":"vector","result":[
  {"metric":{"user":"analytics"},"value":[1757800000,"45015"]},
  {"metric":{"user":"infra"},"value":[1757800000,"23229"]},
  {"metric":{"user":"payments"},"value":[1757800000,"3615"]}]}}`

const rf3 = `{"status":"success","data":{"resultType":"vector","result":[
  {"metric":{},"value":[1757800000,"3"]}]}}`

const empty = `{"status":"success","data":{"resultType":"vector","result":[]}}`

func server(t *testing.T, series, rf string, queries *[]string) *httptest.Server {
	t.Helper()
	return serverWithIngest(t, series, empty, rf, queries)
}

// serverWithIngest answers the classic and ingest-storage forms of the
// active-series query separately, the way a real cluster of one
// architecture answers one of them with data and the other with nothing.
func serverWithIngest(t *testing.T, classic, ingest, rf string, queries *[]string) *httptest.Server {
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
		switch {
		case strings.Contains(q, "cortex_ingester_active_series") && strings.Contains(q, "label_replace"):
			_, _ = w.Write([]byte(ingest))
		case strings.Contains(q, "cortex_ingester_active_series"):
			_, _ = w.Write([]byte(classic))
		case strings.Contains(q, "cortex_distributor_replication_factor"):
			_, _ = w.Write([]byte(rf))
		default:
			t.Errorf("unexpected query %q", q)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestReadIngesterMemory_RF3EndToEnd(t *testing.T) {
	var queries []string
	srv := server(t, activeSeriesRF3, rf3, &queries)
	defer srv.Close()

	m, err := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"}).ReadIngesterMemory(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("ReadIngesterMemory: %v", err)
	}
	if m.ReplicationFactor == nil || *m.ReplicationFactor != 3 {
		t.Fatalf("ReplicationFactor = %v, want 3", m.ReplicationFactor)
	}
	if m.Drivers["analytics"] != 45015 {
		t.Errorf("analytics raw driver = %v, want 45015 (undivided)", m.Drivers["analytics"])
	}

	a := cost.Allocate(m, cost.Inventory{}, nil)
	if a.Total == nil || *a.Total != 23953 {
		t.Errorf("Total = %v, want 23953 real series (71859 / RF 3)", a.Total)
	}
	if got := a.Tenants[0]; got.Tenant != "analytics" || got.Driver == nil || *got.Driver != 15005 {
		t.Errorf("top tenant = %+v, want analytics with 15005 real series", got)
	}

	if len(queries) != 2 {
		t.Fatalf("issued %d queries, want 2", len(queries))
	}
	if !strings.Contains(queries[0], "unless on (cluster, namespace, job) cortex_partition_ring_partitions") {
		t.Errorf("active series query lacks the ingest-storage guard: %s", queries[0])
	}
}

func TestReadIngesterMemory_QueriesAreValidPromQL(t *testing.T) {
	p := parser.NewParser(parser.Options{})
	for _, w := range []time.Duration{time.Hour, 24 * time.Hour, 30 * 24 * time.Hour} {
		var queries []string
		srv := server(t, activeSeriesRF3, rf3, &queries)
		if _, err := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"}).ReadIngesterMemory(context.Background(), w); err != nil {
			t.Fatalf("window %v: %v", w, err)
		}
		srv.Close()
		for _, q := range queries {
			if _, err := p.ParseExpr(q); err != nil {
				t.Errorf("window %v: %q is not valid PromQL: %v", w, q, err)
			}
		}
	}
}

func TestReadIngesterMemory_MissingReplicationFactorIsNil(t *testing.T) {
	srv := server(t, activeSeriesRF3, empty, nil)
	defer srv.Close()
	m, err := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"}).ReadIngesterMemory(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadIngesterMemory: %v", err)
	}
	if m.ReplicationFactor != nil {
		t.Errorf("ReplicationFactor = %v, want nil when Mimir returns none (not a default of 1)", *m.ReplicationFactor)
	}
}

func TestReadIngesterMemory_MetricsTenantRequired(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://unused"}).ReadIngesterMemory(context.Background(), time.Hour); err == nil {
		t.Fatal("expected an error without MetricsTenant")
	}
}

func TestSubqueryStep(t *testing.T) {
	tests := []struct{ window, want time.Duration }{
		{time.Hour, time.Minute},
		{24 * time.Hour, time.Minute},
		{7 * 24 * time.Hour, 7 * time.Minute},
		{30 * 24 * time.Hour, 30 * time.Minute},
	}
	for _, tt := range tests {
		if got := subqueryStep(tt.window); got != tt.want {
			t.Errorf("subqueryStep(%v) = %v, want %v", tt.window, got, tt.want)
		}
	}
}

// Real per-tenant values from dev/mimir-k8s running ARCH=ingest: the
// classic query returned nothing and no replication factor was exported.
const activeSeriesIngest = `{"status":"success","data":{"resultType":"vector","result":[
  {"metric":{"user":"analytics"},"value":[1757800000,"13010"]},
  {"metric":{"user":"monitoring"},"value":[1757800000,"12643"]},
  {"metric":{"user":"infra"},"value":[1757800000,"7005"]}]}}`

func TestReadIngesterMemory_FallsBackToIngestStorage(t *testing.T) {
	var queries []string
	srv := serverWithIngest(t, empty, activeSeriesIngest, empty, &queries)
	defer srv.Close()

	m, err := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"}).ReadIngesterMemory(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadIngesterMemory: %v", err)
	}
	if !m.Deduplicated || m.Architecture != "ingest storage" {
		t.Fatalf("Deduplicated=%v Architecture=%q, want true / ingest storage", m.Deduplicated, m.Architecture)
	}
	if !strings.Contains(m.DriverQuery, "label_replace") {
		t.Errorf("DriverQuery = %q, want the ingest-storage form recorded for the assumption trail", m.DriverQuery)
	}
	a := cost.Allocate(m, cost.Inventory{}, nil)
	if a.Total == nil || *a.Total != 32658 {
		t.Errorf("Total = %v, want 32658 (no divisor under ingest storage)", a.Total)
	}
}

func TestReadIngesterMemory_ClassicDataSkipsIngestQuery(t *testing.T) {
	var queries []string
	srv := serverWithIngest(t, activeSeriesRF3, activeSeriesIngest, rf3, &queries)
	defer srv.Close()
	m, err := New(Config{BaseURL: srv.URL, MetricsTenant: "monitoring"}).ReadIngesterMemory(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ReadIngesterMemory: %v", err)
	}
	if m.Deduplicated {
		t.Error("classic data present, but the measurement was marked deduplicated")
	}
	for _, q := range queries {
		if strings.Contains(q, "label_replace") {
			t.Errorf("ingest-storage query issued although the classic one returned data: %s", q)
		}
	}
}
