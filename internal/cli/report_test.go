package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReport_Clean(t *testing.T) {
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/clean/rules",
		"--telemetry", "testdata/report/clean/executions.ndjson",
		"--config", "testdata/report/clean/promcost.yaml",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{"analytics", "payments", "customer_activity", "revenue"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in report, got:\n%s", want, stdout)
		}
	}
}

func TestReport_MimirlogsFormat(t *testing.T) {
	// Real-format Mimir ruler query-stats log (--telemetry-format
	// mimirlogs): two query-stats lines (one local eval with stats, one
	// remote eval with none) both matching the fixture's single rule
	// definition by expression text, plus an unrelated distributor log
	// line that must be silently ignored.
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/mimirlogs/rules",
		"--telemetry", "testdata/report/mimirlogs/ruler.log",
		"--telemetry-format", "mimirlogs",
		"--config", "testdata/report/mimirlogs/promcost.yaml",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Total executions: 2") {
		t.Errorf("expected both real query-stats lines to be parsed (and the unrelated distributor line ignored), got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "customer_activity") {
		t.Errorf("expected the query text to have been matched back to the customer_activity rule definition, got:\n%s", stdout)
	}
}

func TestReport_Unmatched(t *testing.T) {
	// An execution whose namespace doesn't match any loaded rule definition
	// is a legitimate, reportable observation — not a load/read failure.
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/unmatched/rules",
		"--telemetry", "testdata/report/unmatched/executions.ndjson",
		"--config", "testdata/report/unmatched/promcost.yaml",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (unmatched executions are reported, not fatal). stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Unmatched executions") {
		t.Errorf("expected an \"Unmatched executions\" section, got:\n%s", stdout)
	}
}

func TestReport_MalformedTelemetry_DefaultWarnsAndProceeds(t *testing.T) {
	// A telemetry read error is routine, expected output (an unmatched or
	// ambiguous mimirlogs line is a normal thing for real ruler logs to
	// contain — see internal/cli/report.go's comment on this). report is
	// informational, not a pass/fail gate like check, so by default it
	// warns and still renders whatever did parse rather than discarding a
	// mostly-good report over one bad line.
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/malformed-telemetry/rules",
		"--telemetry", "testdata/report/malformed-telemetry/executions.ndjson",
		"--config", "testdata/report/malformed-telemetry/promcost.yaml",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	// Warnings must go to stderr, never stdout — stdout carries only the
	// rendered report body (see TestReport_JSONFormat_ValidDespiteWarnings
	// for why this is a hard requirement, not a style preference).
	if strings.Contains(stdout, "warning:") {
		t.Errorf("warning text leaked into stdout, want it on stderr only. stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "warning: telemetry record skipped") {
		t.Errorf("expected a skip warning on stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stdout, "Total executions: 1") {
		t.Errorf("expected the one valid line to still be reported, got:\n%s", stdout)
	}
}

// Regression test for a real bug: warnings used to print to stdout, which
// silently corrupted --format json output (a plain-text warning line
// followed by a JSON object is not valid JSON) whenever a telemetry
// source had any read errors — exactly the routine case mimirlogs hits on
// real Mimir output (see internal/telemetry/mimirlogs's package doc).
// Found by actually piping real output through a JSON parser, not just
// checking exit codes.
func TestReport_JSONFormat_ValidDespiteWarnings(t *testing.T) {
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/malformed-telemetry/rules",
		"--telemetry", "testdata/report/malformed-telemetry/executions.ndjson",
		"--config", "testdata/report/malformed-telemetry/promcost.yaml",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "warning:") {
		t.Fatalf("test setup problem: expected this fixture to still produce a warning, got stderr:\n%s", stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON despite a warning being emitted: %v\nstdout:\n%s", err, stdout)
	}
}

func TestReport_MalformedTelemetry_Strict(t *testing.T) {
	_, stderr, code := run("report",
		"--dir", "testdata/report/malformed-telemetry/rules",
		"--telemetry", "testdata/report/malformed-telemetry/executions.ndjson",
		"--config", "testdata/report/malformed-telemetry/promcost.yaml",
		"--strict",
	)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
	}
}

func TestReport_SinceFilter(t *testing.T) {
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/since-filter/rules",
		"--telemetry", "testdata/report/since-filter/executions.ndjson",
		"--config", "testdata/report/since-filter/promcost.yaml",
		"--since", "24h",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Total executions: 0") {
		t.Errorf("expected the fixture's 2020-dated execution to be filtered out by --since 24h, got:\n%s", stdout)
	}
}

func TestReport_SinceFilter_DayWeekSuffix(t *testing.T) {
	// parseSinceDuration's d/w suffix handling (stdlib time.ParseDuration
	// alone can't parse "7d") — exercised via the CLI, not just unit-level.
	for _, since := range []string{"1d", "1w"} {
		stdout, stderr, code := run("report",
			"--dir", "testdata/report/since-filter/rules",
			"--telemetry", "testdata/report/since-filter/executions.ndjson",
			"--config", "testdata/report/since-filter/promcost.yaml",
			"--since", since,
		)
		if code != 0 {
			t.Fatalf("--since %s: exit code = %d, want 0. stdout=%s stderr=%s", since, code, stdout, stderr)
		}
		if !strings.Contains(stdout, "Total executions: 0") {
			t.Errorf("--since %s: expected the fixture's 2020-dated execution to be filtered out, got:\n%s", since, stdout)
		}
	}
}

func TestReport_SinceFilter_InvalidValue(t *testing.T) {
	_, stderr, code := run("report",
		"--dir", "testdata/report/since-filter/rules",
		"--telemetry", "testdata/report/since-filter/executions.ndjson",
		"--config", "testdata/report/since-filter/promcost.yaml",
		"--since", "not-a-duration",
	)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
	}
}

func TestReport_JSONFormat(t *testing.T) {
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/clean/rules",
		"--telemetry", "testdata/report/clean/executions.ndjson",
		"--config", "testdata/report/clean/promcost.yaml",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"tenant": "analytics"`) {
		t.Errorf("expected snake_case JSON output, got:\n%s", stdout)
	}
}

func TestReport_JSONFormat_ZeroExecutions(t *testing.T) {
	// Regression coverage for the NaN-guard: an unmatched-only fixture with
	// --since filtering everything out must still marshal valid JSON.
	stdout, stderr, code := run("report",
		"--dir", "testdata/report/since-filter/rules",
		"--telemetry", "testdata/report/since-filter/executions.ndjson",
		"--config", "testdata/report/since-filter/promcost.yaml",
		"--since", "24h",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"total_executions": 0`) {
		t.Errorf("expected a zero-executions JSON report, got:\n%s", stdout)
	}
}

func TestReport_MissingRequiredFlags(t *testing.T) {
	if _, _, code := run("report", "--telemetry", "testdata/report/clean/executions.ndjson"); code != 1 {
		t.Errorf("missing both --dir and --tenant: exit code = %d, want 1", code)
	}
	if _, _, code := run("report", "--dir", "testdata/report/clean/rules"); code != 1 {
		t.Errorf("missing --telemetry: exit code = %d, want 1", code)
	}
}

// realRulerRulesFixture is the same byte-shape-accurate copy of a live
// mimir-3.2.0 GET /prometheus/api/v1/rules response used by
// internal/loader/rulerapi's own tests.
const realRulerRulesFixture = `{
  "status": "success",
  "data": {
    "groups": [
      {
        "name": "mimir_api_1",
        "file": "recording-rules.yaml",
        "interval": 60,
        "rules": [
          {
            "name": "cluster_job_pod:cortex_alertmanager_alerts:sum",
            "query": "sum by (cluster, job, pod) (cortex_alertmanager_alerts)",
            "labels": {},
            "health": "ok",
            "type": "recording"
          }
        ]
      }
    ]
  }
}`

// writeReportConfig writes a promcost.yaml pointing backend.url at a fake
// ruler (an httptest.Server), for exercising --tenant without --dir.
func writeReportConfig(t *testing.T, backendURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promcost.yaml")
	content := "backend:\n  url: " + backendURL + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestReport_RulerAPISource_NoDirNeeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "infra" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(realRulerRulesFixture))
	}))
	defer srv.Close()
	configPath := writeReportConfig(t, srv.URL)

	// Telemetry for the exact rule the fake ruler serves — no --dir at all.
	stdout, stderr, code := run("report",
		"--tenant", "infra",
		"--telemetry", "testdata/report/clean/executions.ndjson",
		"--config", configPath,
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout:\n%s", err, stdout)
	}
	if !strings.Contains(stdout, `"rule_definitions": 1`) {
		t.Errorf("expected exactly 1 rule definition fetched from the fake ruler API, got:\n%s", stdout)
	}
}

func TestReport_NeitherDirNorTenant(t *testing.T) {
	_, stderr, code := run("report",
		"--telemetry", "testdata/report/clean/executions.ndjson",
		"--config", writeReportConfig(t, "http://unused"),
	)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--dir") || !strings.Contains(stderr, "--tenant") {
		t.Errorf("expected the error to mention both --dir and --tenant, got:\n%s", stderr)
	}
}

func TestReport_TenantWithoutBackendURL(t *testing.T) {
	_, stderr, code := run("report",
		"--tenant", "infra",
		"--telemetry", "testdata/report/clean/executions.ndjson",
		// no --config at all -> config.Default() has an empty backend.url
	)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "backend.url") {
		t.Errorf("expected the error to mention the missing backend.url, got:\n%s", stderr)
	}
}

// metricsFixture is the real instant-query response shape Mimir returns for
// the three cortex_prometheus_rule_* queries the metrics source issues.
func metricsFixture(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "monitoring" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q, "rule_evaluation_duration_seconds_sum"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"user":"infra"},"value":[1757800000,"10.5"]},
				{"metric":{"user":"analytics"},"value":[1757800000,"1.04"]}]}}`))
		case strings.Contains(q, "rule_evaluation_duration_seconds_count"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"user":"infra"},"value":[1757800000,"3540"]},
				{"metric":{"user":"analytics"},"value":[1757800000,"270"]}]}}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"user":"infra","rule_group":"/data/ruler/infra/alerts.yaml;mimir_alerts"},"value":[1757800000,"305"]},
				{"metric":{"user":"analytics","rule_group":"/data/ruler/analytics/workload.yaml;analytics_alerts"},"value":[1757800000,"270"]}]}}`))
		}
	}))
}

// The headline of ADR 0002's first step: a useful report with no --telemetry,
// no --dir, and no log capture anywhere — just a Mimir URL.
func TestReport_MetricsSource_NoTelemetryNeeded(t *testing.T) {
	srv := metricsFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("report",
		"--metrics-tenant", "monitoring",
		"--config", writeReportConfig(t, srv.URL),
		"--since", "1h",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{"infra", "analytics", "mimir_alerts", "Mimir rule metrics"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in report, got:\n%s", want, stdout)
		}
	}
	// The rule tier is absent because the source cannot see it — saying
	// "no matched rule executions" here would report a limit of the source
	// as a finding about the workload (ADR 0002).
	if strings.Contains(stdout, "No matched rule executions") {
		t.Errorf("group-granularity report claimed no rule executions matched:\n%s", stdout)
	}
	if !strings.Contains(stdout, "rule-group granularity") {
		t.Errorf("expected the granularity limit to be stated, got:\n%s", stdout)
	}
	// Tenants rank by measured wall time, not by evaluation count.
	if !strings.Contains(stdout, "query wall time") {
		t.Errorf("expected ranking by wall time, got:\n%s", stdout)
	}
}

func TestReport_MetricsSource_JSONIsValid(t *testing.T) {
	srv := metricsFixture(t)
	defer srv.Close()

	stdout, stderr, code := run("report",
		"--metrics-tenant", "monitoring",
		"--config", writeReportConfig(t, srv.URL),
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout:\n%s", err, stdout)
	}
	if parsed["granularity"] != "group" {
		t.Errorf("granularity = %v, want group", parsed["granularity"])
	}
}

// --metrics-tenant names the tenant that SCRAPED Mimir, which is usually not
// a tenant being reported on. Defaulting it would silently query the wrong
// TSDB and produce an empty report that looks like a real finding.
func TestReport_MetricsSource_RequiresMetricsTenant(t *testing.T) {
	srv := metricsFixture(t)
	defer srv.Close()

	_, stderr, code := run("report", "--config", writeReportConfig(t, srv.URL))
	if code != 1 {
		t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--metrics-tenant") {
		t.Errorf("expected the error to name --metrics-tenant, got:\n%s", stderr)
	}
}

func TestReport_MetricsSource_RequiresBackendURL(t *testing.T) {
	_, stderr, code := run("report", "--metrics-tenant", "monitoring")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1. stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "backend.url") {
		t.Errorf("expected the error to mention backend.url, got:\n%s", stderr)
	}
}
