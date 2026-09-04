package cli_test

import (
	"encoding/json"
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
		t.Errorf("missing --dir: exit code = %d, want 1", code)
	}
	if _, _, code := run("report", "--dir", "testdata/report/clean/rules"); code != 1 {
		t.Errorf("missing --telemetry: exit code = %d, want 1", code)
	}
}
