package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ruleOutputFixture serves costFixture's RF=3 pool-1 data to the metrics
// tenant, plus a ruler API and the tenant's own series. infra has 23,229
// raw active series, 7,743 once divided by RF 3, and three rules: one
// recording rule with 774 output series, one whose output has no series,
// and one alerting rule.
func ruleOutputFixture(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var countTenants []string
	poolSrv := costFixture(t)
	t.Cleanup(poolSrv.Close)
	pool := poolSrv.Config.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.Header.Get("X-Scope-OrgID")
		if tenant == "monitoring" {
			pool.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case tenant == "infra" && r.URL.Path == "/prometheus/api/v1/rules":
			_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[{"name":"node","file":"infra-ns","interval":60,"rules":[
				{"name":"instance:node_cpu:rate5m","query":"sum by (instance) (rate(node_cpu_seconds_total[5m]))","type":"recording"},
				{"name":"cluster:never:sum","query":"sum(nothing_matches)","type":"recording"},
				{"name":"InstanceDown","query":"up == 0","type":"alerting"}]}]}}`))
		case tenant == "infra" && r.URL.Path == "/prometheus/api/v1/query":
			countTenants = append(countTenants, tenant)
			q := r.URL.Query().Get("query")
			if !strings.HasPrefix(q, "count by (__name__) ({__name__=~") {
				t.Errorf("unexpected count query %q", q)
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"__name__":"instance:node_cpu:rate5m"},"value":[1757800000,"774"]}]}}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &countTenants
}

func TestCost_RuleOutputs(t *testing.T) {
	srv, countTenants := ruleOutputFixture(t)

	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, ""),
		"--tenant", "infra",
		"--rule-outputs",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"## Rule output series (point-in-time)",
		"(1 batched query)",
		// 774 / (23,229 / RF 3) = 10.0%
		"2 recording rule(s) writing 2 distinct metric(s): 774 series now, 10.0% of its 7,743 active series.",
		"| infra-ns / node / instance:node_cpu:rate5m | `instance:node_cpu:rate5m` | 774 | 10.0% |",
		"| infra-ns / node / cluster:never:sum | `cluster:never:sum` | 0 (no series now) | 0.0% |",
		"1 alerting rule(s) write no series",
		"That is not zero cost",
		"| replication factor | `3` |",
		"It is not a breakdown of the tenant's ingestion",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "InstanceDown |") {
		t.Errorf("alerting rule must not appear as a table row with a figure:\n%s", stdout)
	}
	if len(*countTenants) != 1 {
		t.Errorf("count queries = %d, want 1 batched query", len(*countTenants))
	}
	// Generated line still closes the document.
	if !strings.HasSuffix(strings.TrimSpace(stdout), ".") || !strings.Contains(stdout, "\nGenerated ") {
		t.Errorf("missing trailing Generated line:\n%s", stdout)
	}
}

func TestCost_RuleOutputsJSON(t *testing.T) {
	srv, _ := ruleOutputFixture(t)
	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", writeCostConfig(t, srv.URL, ""),
		"--tenant", "infra",
		"--rule-outputs",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d. stderr=%s", code, stderr)
	}
	var doc struct {
		RuleOutputs struct {
			Tenants []struct {
				Tenant string `json:"tenant"`
				Rules  []struct {
					Rule   string `json:"rule"`
					Status string `json:"status"`
					Series *int64 `json:"series"`
				} `json:"rules"`
			} `json:"tenants"`
		} `json:"rule_outputs"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, stdout)
	}
	got := map[string]string{}
	for _, r := range doc.RuleOutputs.Tenants[0].Rules {
		got[r.Rule] = r.Status
		if r.Status == "no_output" && r.Series != nil {
			t.Errorf("alerting rule %s carries a series figure %d; it must be absent, not zero", r.Rule, *r.Series)
		}
		if r.Status == "no_series" && (r.Series == nil || *r.Series != 0) {
			t.Errorf("%s: no_series must be a measured zero", r.Rule)
		}
	}
	want := map[string]string{
		"instance:node_cpu:rate5m": "series",
		"cluster:never:sum":        "no_series",
		"InstanceDown":             "no_output",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s status = %q, want %q", k, got[k], v)
		}
	}
}

func TestCost_RuleOutputsUsageErrors(t *testing.T) {
	srv, _ := ruleOutputFixture(t)
	cfg := writeCostConfig(t, srv.URL, "")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no rule source", []string{"--rule-outputs"}, "needs rule definitions"},
		{"dir without rule-outputs", []string{"--dir", "x"}, "--dir only supplies"},
		{"zero concurrency", []string{"--rule-outputs", "--tenant", "infra", "--rule-output-concurrency", "0"}, "at least 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"cost", "--metrics-tenant", "monitoring", "--config", cfg}, tt.args...)
			_, stderr, code := run(args...)
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Errorf("code=%d stderr=%q, want 1 and %q", code, stderr, tt.want)
			}
		})
	}
}

// TestCost_RuleOutputsFromDir loads rules from a directory, resolving
// tenants through promcost.yaml's tenancy block, and has one tenant's count
// query fail: that tenant's rule renders as not measured, the run still
// succeeds, and stderr says so.
func TestCost_RuleOutputsFromDir(t *testing.T) {
	poolSrv := costFixture(t)
	defer poolSrv.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Scope-OrgID") {
		case "monitoring":
			poolSrv.Config.Handler.ServeHTTP(w, r)
		case "analytics":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"__name__":"customer_activity"},"value":[1757800000,"30"]}]}}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}
	}))
	defer srv.Close()

	cfg := writeCostConfig(t, srv.URL, `tenancy:
  discovery:
    - source: static
      map:
        team-a: analytics
        team-b: payments
  unmapped: error
`)
	stdout, stderr, code := run("cost",
		"--metrics-tenant", "monitoring",
		"--config", cfg,
		"--rule-outputs",
		"--dir", "testdata/report/clean/rules",
	)
	if code != 0 {
		t.Fatalf("exit code = %d. stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"| team-a/rules.yaml / g / customer_activity | `customer_activity` | 30 |",
		"| team-b/rules.yaml / g / revenue | `revenue` | not measured | — |",
		"1 recording rule(s) writing 1 distinct metric(s); none could be counted.",
		"unexpected status 500: boom",
		"lower bounds",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "1 output metric(s) could not be counted") {
		t.Errorf("stderr should warn about the failed count, got %q", stderr)
	}
}
