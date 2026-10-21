package cli_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// twoClusterMimir is a metrics tenant that scrapes two Mimir clusters.
// Asked without a cluster matcher it answers with both clusters' series
// summed, as Mimir would for `sum by (user)`; asked with
// {cluster="prod-eu"} it answers with that cluster's alone.
func twoClusterMimir(t *testing.T) *httptest.Server {
	t.Helper()
	vec := func(pairs ...string) string {
		var parts []string
		for i := 0; i < len(pairs); i += 2 {
			parts = append(parts, `{"metric":{`+pairs[i]+`},"value":[1757800000,"`+pairs[i+1]+`"]}`)
		}
		return `{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(parts, ",") + `]}}`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		eu := strings.Contains(q, `cluster="prod-eu"`)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(q, "count by (cluster)"):
			_, _ = w.Write([]byte(vec(`"cluster":"prod-eu"`, "1", `"cluster":"prod-us"`, "1")))
		case strings.Contains(q, "cortex_ingester_active_series") && !strings.Contains(q, "label_replace") && eu:
			// prod-eu alone: analytics 900, infra 100 (RF 1).
			_, _ = w.Write([]byte(vec(`"user":"analytics"`, "900", `"user":"infra"`, "100")))
		case strings.Contains(q, "cortex_ingester_active_series") && !strings.Contains(q, "label_replace"):
			// prod-eu + prod-us, where prod-us holds infra 3000.
			_, _ = w.Write([]byte(vec(`"user":"analytics"`, "900", `"user":"infra"`, "3100")))
		case strings.Contains(q, "cortex_distributor_replication_factor"):
			_, _ = w.Write([]byte(vec(``, "1")))
		default:
			_, _ = w.Write([]byte(vec()))
		}
	}))
}

func TestCost_SelectorScopesToOneCluster(t *testing.T) {
	srv := twoClusterMimir(t)
	defer srv.Close()
	args := []string{"cost", "--metrics-tenant", "monitoring", "--pool", "ingester_memory"}

	// No selector: the figures span both clusters, and stderr says so.
	stdout, stderr, code := run(append(args, "--config", writeCostConfig(t, srv.URL, ""))...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "holds 2 Mimir clusters (cluster=prod-eu, prod-us)") || !strings.Contains(stderr, "--selector") {
		t.Errorf("stderr = %q, want a several-clusters warning", stderr)
	}
	if !strings.Contains(stdout, "| infra | 3,100 |") {
		t.Errorf("without a selector want both clusters summed:\n%s", stdout)
	}

	for name, extra := range map[string][]string{
		"flag":   {"--config", writeCostConfig(t, srv.URL, ""), "--selector", `{cluster="prod-eu"}`},
		"config": {"--config", writeCostConfig(t, srv.URL+"\n  selector: '{cluster=\"prod-eu\"}'", "")},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := run(append(args, extra...)...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if strings.Contains(stderr, "warning") {
				t.Errorf("stderr = %q, want no warning with a selector", stderr)
			}
			for _, want := range []string{"`analytics` uses 90.0% of ingester memory", "| infra | 100 |", "| cluster selector | `{cluster=\"prod-eu\"}"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout missing %q:\n%s", want, stdout)
				}
			}
		})
	}

	_, stderr, code = run(append(args, "--config", writeCostConfig(t, srv.URL, ""), "--selector", `up{cluster="a"}`)...)
	if code == 0 || !strings.Contains(stderr, "--selector: selector") {
		t.Errorf("code=%d stderr=%q, want a --selector parse error", code, stderr)
	}
}
