package mimirmetrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// The three fixtures below are byte-shape-accurate copies of what
// GET /prometheus/api/v1/query actually returned from a live mimir-3.2.0
// instance (X-Scope-OrgID: infra), trimmed to four tenants — the standard
// Prometheus instant-query envelope, values rendered as strings, and the
// rule_group label carrying a ruler storage path rather than a group name.

const tenantDurationFixture = `{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {"metric": {"user": "infra"}, "value": [1757800000.123, "10.528"]},
      {"metric": {"user": "analytics"}, "value": [1757800000.123, "1.204"]},
      {"metric": {"user": "payments"}, "value": [1757800000.123, "0.431"]}
    ]
  }
}`

// Deliberately missing "payments": that tenant is present in the duration
// query and absent here, which is the merge case Read must not drop.
const tenantCountFixture = `{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {"metric": {"user": "infra"}, "value": [1757800000.123, "1227"]},
      {"metric": {"user": "analytics"}, "value": [1757800000.123, "240"]}
    ]
  }
}`

const groupCountFixture = `{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {"metric": {"user": "infra", "rule_group": "/data/ruler/infra/alerts.yaml;mimir_alerts"}, "value": [1757800000.123, "600"]},
      {"metric": {"user": "infra", "rule_group": "/data/ruler/infra/recording-rules.yaml;mimir_api_1"}, "value": [1757800000.123, "627"]},
      {"metric": {"user": "analytics", "rule_group": "/data/ruler/analytics/slo.yaml;analytics_slo"}, "value": [1757800000.123, "240"]}
    ]
  }
}`

// newMockQueryServer routes on the metric name embedded in the `query`
// param, the way the real endpoint distinguishes the three calls. Keys are
// full metric names, so no key is a substring of another query.
func newMockQueryServer(t *testing.T, tenant string, byMetric map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/query" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Scope-OrgID"); got != tenant {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":"error","errorType":"unauthorized","error":"no org id"}`))
			return
		}
		query := r.URL.Query().Get("query")
		for metric, body := range byMetric {
			if strings.Contains(query, metric) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
				return
			}
		}
		t.Errorf("unexpected query %q", query)
		w.WriteHeader(http.StatusBadRequest)
	}))
}

func fullFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newMockQueryServer(t, "infra", map[string]string{
		"cortex_prometheus_rule_evaluation_duration_seconds_sum":   tenantDurationFixture,
		"cortex_prometheus_rule_evaluation_duration_seconds_count": tenantCountFixture,
		"cortex_prometheus_rule_evaluations_total":                 groupCountFixture,
	})
}

func TestRead_RealResponseShape(t *testing.T) {
	srv := fullFixtureServer(t)
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	got, err := s.Read(context.Background(), 15*time.Minute)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got.Tenants) != 3 {
		t.Fatalf("len(Tenants) = %d, want 3", len(got.Tenants))
	}

	// Sorted by DurationSeconds desc.
	wantOrder := []string{"infra", "analytics", "payments"}
	for i, want := range wantOrder {
		if got.Tenants[i].Tenant != want {
			t.Errorf("Tenants[%d].Tenant = %q, want %q (sorted by DurationSeconds desc)", i, got.Tenants[i].Tenant, want)
		}
	}

	infra := got.Tenants[0]
	if infra.DurationSeconds != 10.528 {
		t.Errorf("infra.DurationSeconds = %v, want 10.528", infra.DurationSeconds)
	}
	if infra.Evaluations != 1227 {
		t.Errorf("infra.Evaluations = %v, want 1227", infra.Evaluations)
	}
	if len(infra.Groups) != 2 {
		t.Fatalf("len(infra.Groups) = %d, want 2", len(infra.Groups))
	}
	// Groups sorted by Evaluations desc, and the rule_group label split
	// into base-name namespace + group name.
	if infra.Groups[0] != (GroupWorkload{Namespace: "recording-rules.yaml", Group: "mimir_api_1", Evaluations: 627}) {
		t.Errorf("infra.Groups[0] = %+v, want recording-rules.yaml/mimir_api_1 at 627", infra.Groups[0])
	}
	if infra.Groups[1] != (GroupWorkload{Namespace: "alerts.yaml", Group: "mimir_alerts", Evaluations: 600}) {
		t.Errorf("infra.Groups[1] = %+v, want alerts.yaml/mimir_alerts at 600", infra.Groups[1])
	}

	analytics := got.Tenants[1]
	if analytics.DurationSeconds != 1.204 || analytics.Evaluations != 240 {
		t.Errorf("analytics = %+v, want 1.204s / 240 evaluations", analytics)
	}
	if len(analytics.Groups) != 1 || analytics.Groups[0].Namespace != "slo.yaml" || analytics.Groups[0].Group != "analytics_slo" {
		t.Errorf("analytics.Groups = %+v, want one slo.yaml/analytics_slo entry", analytics.Groups)
	}
}

func TestRead_TenantMissingFromCountQueryStillAppears(t *testing.T) {
	srv := fullFixtureServer(t)
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	got, err := s.Read(context.Background(), 15*time.Minute)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	var payments *TenantWorkload
	for i := range got.Tenants {
		if got.Tenants[i].Tenant == "payments" {
			payments = &got.Tenants[i]
		}
	}
	if payments == nil {
		t.Fatal("payments is absent; a tenant in the duration query but not the count query must still be reported")
	}
	if payments.DurationSeconds != 0.431 {
		t.Errorf("payments.DurationSeconds = %v, want 0.431", payments.DurationSeconds)
	}
	if payments.Evaluations != 0 {
		t.Errorf("payments.Evaluations = %v, want 0 for the query it was missing from", payments.Evaluations)
	}
	if payments.Groups != nil {
		t.Errorf("payments.Groups = %+v, want nil", payments.Groups)
	}
}

func TestRead_WindowBecomesStartEndAndPromQLRange(t *testing.T) {
	var seenQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQueries = append(seenQueries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	before := time.Now()
	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	got, err := s.Read(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	after := time.Now()

	if d := got.End.Sub(got.Start); d != 7*24*time.Hour {
		t.Errorf("End-Start = %v, want the requested 168h window", d)
	}
	if got.End.Before(before) || got.End.After(after) {
		t.Errorf("End = %v, want a timestamp taken during Read", got.End)
	}
	if len(seenQueries) != 3 {
		t.Fatalf("sent %d queries, want 3", len(seenQueries))
	}
	p := parser.NewParser(parser.Options{})
	for _, q := range seenQueries {
		if !strings.Contains(q, "[7d]") {
			t.Errorf("query %q does not carry the [7d] range", q)
		}
		if _, err := p.ParseExpr(q); err != nil {
			t.Errorf("query %q is not valid PromQL: %v", q, err)
		}
	}
}

func TestRead_SortOrderTieBreaks(t *testing.T) {
	equalDurations := `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"user":"zeta"},"value":[1757800000.123,"5"]},
		{"metric":{"user":"alpha"},"value":[1757800000.123,"5"]}]}}`
	equalGroups := `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"user":"alpha","rule_group":"/data/ruler/alpha/z.yaml;zzz"},"value":[1757800000.123,"7"]},
		{"metric":{"user":"alpha","rule_group":"/data/ruler/alpha/a.yaml;aaa"},"value":[1757800000.123,"7"]}]}}`
	srv := newMockQueryServer(t, "infra", map[string]string{
		"cortex_prometheus_rule_evaluation_duration_seconds_sum":   equalDurations,
		"cortex_prometheus_rule_evaluation_duration_seconds_count": `{"status":"success","data":{"resultType":"vector","result":[]}}`,
		"cortex_prometheus_rule_evaluations_total":                 equalGroups,
	})
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	got, err := s.Read(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Tenants) != 2 || got.Tenants[0].Tenant != "alpha" || got.Tenants[1].Tenant != "zeta" {
		t.Fatalf("tenant order = %+v, want equal durations broken by Tenant asc (alpha, zeta)", got.Tenants)
	}
	groups := got.Tenants[0].Groups
	if len(groups) != 2 || groups[0].Group != "aaa" || groups[1].Group != "zzz" {
		t.Errorf("group order = %+v, want equal evaluations broken by Group asc (aaa, zzz)", groups)
	}
}

func TestRead_MetricsTenantRequired(t *testing.T) {
	srv := fullFixtureServer(t)
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL})
	_, err := s.Read(context.Background(), time.Hour)
	if err == nil {
		t.Fatal("Read succeeded with no MetricsTenant, want an error")
	}
	if !strings.Contains(err.Error(), "MetricsTenant") {
		t.Errorf("error = %v, want it to name MetricsTenant", err)
	}
}

func TestRead_NonPositiveWindow(t *testing.T) {
	srv := fullFixtureServer(t)
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	if _, err := s.Read(context.Background(), 0); err == nil {
		t.Fatal("Read succeeded with a zero window, want an error")
	}
}

func TestRead_NonOKStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"execution","error":"query timed out"}`))
	}))
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	_, err := s.Read(context.Background(), time.Hour)
	if err == nil {
		t.Fatal("Read succeeded against a 422, want an error")
	}
	if !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "query timed out") {
		t.Errorf("error = %v, want the status code and the body's explanation", err)
	}
	if !strings.Contains(err.Error(), "evaluation duration") {
		t.Errorf("error = %v, want it to name which of the three queries failed", err)
	}
}

func TestRead_StatusErrorBodyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A 200 carrying status:"error" — parsing must not treat the
		// HTTP status alone as success.
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"invalid parameter \"query\""}`))
	}))
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	_, err := s.Read(context.Background(), time.Hour)
	if err == nil {
		t.Fatal("Read succeeded against status:\"error\", want an error")
	}
	if !strings.Contains(err.Error(), "invalid parameter") {
		t.Errorf("error = %v, want the API's own error text", err)
	}
}

func TestRead_MalformedJSONIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":`))
	}))
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	_, err := s.Read(context.Background(), time.Hour)
	if err == nil {
		t.Fatal("Read succeeded against truncated JSON, want an error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error = %v, want a decode failure", err)
	}
}

func TestRead_UnparseableSampleValueIsSkippedNotFatal(t *testing.T) {
	durations := `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"user":"good"},"value":[1757800000.123,"2.5"]},
		{"metric":{"user":"bad"},"value":[1757800000.123,"not-a-number"]},
		{"metric":{"user":"nan"},"value":[1757800000.123,"NaN"]},
		{"metric":{"rule_group":"x;y"},"value":[1757800000.123,"9"]}]}}`
	empty := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	srv := newMockQueryServer(t, "infra", map[string]string{
		"cortex_prometheus_rule_evaluation_duration_seconds_sum":   durations,
		"cortex_prometheus_rule_evaluation_duration_seconds_count": empty,
		"cortex_prometheus_rule_evaluations_total":                 empty,
	})
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	got, err := s.Read(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("Read: %v — one unusable sample must not fail the whole read", err)
	}
	if len(got.Tenants) != 1 || got.Tenants[0].Tenant != "good" || got.Tenants[0].DurationSeconds != 2.5 {
		t.Errorf("Tenants = %+v, want only the parseable sample", got.Tenants)
	}
}

func TestRead_TenantHeaderAndBearerToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Tenant") != "monitoring" {
			t.Errorf("X-Custom-Tenant = %q, want monitoring", r.Header.Get("X-Custom-Tenant"))
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("Authorization = %q, want a bearer token", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	s := New(Config{BaseURL: srv.URL, Header: "X-Custom-Tenant", MetricsTenant: "monitoring", BearerToken: "secret-token"})
	if _, err := s.Read(context.Background(), time.Hour); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestRead_RespectsContext(t *testing.T) {
	srv := fullFixtureServer(t)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New(Config{BaseURL: srv.URL, MetricsTenant: "infra"})
	if _, err := s.Read(ctx, time.Hour); err == nil {
		t.Fatal("Read succeeded with a cancelled context, want an error")
	}
}

func TestSplitRuleGroup(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		wantNamespace string
		wantGroup     string
	}{
		{
			name:          "real mimir-3.2.0 value: ruler storage path, semicolon, group",
			value:         "/data/ruler/infra/alerts.yaml;mimir_alerts",
			wantNamespace: "alerts.yaml",
			wantGroup:     "mimir_alerts",
		},
		{
			name:          "deep path keeps only the base name",
			value:         "/var/lib/mimir/ruler/tenant-a/team/prod/recording-rules.yaml;mimir_api_1",
			wantNamespace: "recording-rules.yaml",
			wantGroup:     "mimir_api_1",
		},
		{
			name:          "no semicolon: the whole value is the group name",
			value:         "mimir_alerts",
			wantNamespace: "",
			wantGroup:     "mimir_alerts",
		},
		{
			name:          "splits on the last semicolon, since a path may contain one",
			value:         "/data/ruler/infra/weird;name.yaml;mimir_alerts",
			wantNamespace: "weird;name.yaml",
			wantGroup:     "mimir_alerts",
		},
		{
			name:          "relative path with no directory",
			value:         "alerts.yaml;mimir_alerts",
			wantNamespace: "alerts.yaml",
			wantGroup:     "mimir_alerts",
		},
		{
			name:          "empty left half is an unknown namespace, not \".\"",
			value:         ";mimir_alerts",
			wantNamespace: "",
			wantGroup:     "mimir_alerts",
		},
		{
			name:          "trailing separator does not invent a namespace of \"/\"",
			value:         "/data/ruler/infra/;mimir_alerts",
			wantNamespace: "infra",
			wantGroup:     "mimir_alerts",
		},
		{
			name:          "empty value",
			value:         "",
			wantNamespace: "",
			wantGroup:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNamespace, gotGroup := splitRuleGroup(tt.value)
			if gotNamespace != tt.wantNamespace || gotGroup != tt.wantGroup {
				t.Errorf("splitRuleGroup(%q) = (%q, %q), want (%q, %q)", tt.value, gotNamespace, gotGroup, tt.wantNamespace, tt.wantGroup)
			}
		})
	}
}

func TestPromQLDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{15 * time.Minute, "15m"},
		{time.Hour, "1h"},
		{7 * 24 * time.Hour, "7d"},
		{24 * time.Hour, "1d"}, // an exact day renders as a day, not 24h
		{90 * time.Second, "90s"},
		{30 * 24 * time.Hour, "30d"},
		{36 * time.Hour, "36h"}, // not a whole day, so it drops one rung
		{5 * time.Minute, "5m"},
		{1500 * time.Millisecond, "1500ms"},
		{time.Millisecond, "1ms"},
		{1_500_500 * time.Nanosecond, "1ms"}, // truncated to PromQL's finest unit
	}

	p := parser.NewParser(parser.Options{})
	for _, tt := range tests {
		t.Run(tt.in.String(), func(t *testing.T) {
			got, err := promQLDuration(tt.in)
			if err != nil {
				t.Fatalf("promQLDuration(%v): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("promQLDuration(%v) = %q, want %q", tt.in, got, tt.want)
			}
			// The point of this formatter is producing something Mimir
			// will accept, so check that with the real PromQL parser
			// rather than trusting the string.
			if _, err := p.ParseExpr(fmt.Sprintf("up[%s]", got)); err != nil {
				t.Errorf("up[%s] is not valid PromQL: %v", got, err)
			}
		})
	}
}

func TestPromQLDuration_RejectsNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Hour, 500 * time.Nanosecond} {
		if _, err := promQLDuration(d); err == nil {
			t.Errorf("promQLDuration(%v) succeeded, want an error", d)
		}
	}
}

// TestDurationStringIsNotUsable records why promQLDuration exists at all:
// time.Duration.String() renders a week without ever mentioning a week,
// and pads an exact hour with zero terms. Both are the strings an operator
// would have to read back out of a failing query.
func TestDurationStringIsNotUsable(t *testing.T) {
	if got := (7 * 24 * time.Hour).String(); got != "168h0m0s" {
		t.Fatalf("(7d).String() = %q; this test's premise has changed", got)
	}
	if got := time.Hour.String(); got != "1h0m0s" {
		t.Fatalf("(1h).String() = %q; this test's premise has changed", got)
	}

	// Checked rather than assumed, because it decides what the formatter
	// is actually for: PromQL does accept the padded form, so this is a
	// legibility problem, not a correctness one...
	p := parser.NewParser(parser.Options{})
	if _, err := p.ParseExpr("up[168h0m0s]"); err != nil {
		t.Errorf("up[168h0m0s] unexpectedly failed to parse (%v); promQLDuration's doc comment says it parses but reads badly", err)
	}
	// ...except for fractions, where Duration.String() is genuinely
	// invalid PromQL — which is why promQLDuration truncates to whole
	// milliseconds instead of ever emitting a decimal point.
	if _, err := p.ParseExpr("up[1.5s]"); err == nil {
		t.Error("up[1.5s] parsed; promQLDuration truncates on the premise that fractional units are rejected")
	}
}
