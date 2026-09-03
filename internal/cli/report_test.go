package cli_test

import (
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

func TestReport_MalformedTelemetry(t *testing.T) {
	_, stderr, code := run("report",
		"--dir", "testdata/report/malformed-telemetry/rules",
		"--telemetry", "testdata/report/malformed-telemetry/executions.ndjson",
		"--config", "testdata/report/malformed-telemetry/promcost.yaml",
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
		t.Errorf("missing --dir: exit code = %d, want 1", code)
	}
	if _, _, code := run("report", "--dir", "testdata/report/clean/rules"); code != 1 {
		t.Errorf("missing --telemetry: exit code = %d, want 1", code)
	}
}
