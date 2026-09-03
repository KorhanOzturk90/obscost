//go:build integration

// This file only builds under `go test -tags=integration`, so plain
// `go test ./...` (what CI runs) never touches a real backend. It exercises
// the real Meter (internal/meter/promapi.go) against an actual
// Mimir/Prometheus-compatible backend — Grafana Cloud's free tier is the
// intended target — to prove the HTTP/auth/tenancy-header plumbing works,
// not to assert on specific formula outputs (that belongs to golden-corpus
// FakeMeter tests once a PC-L0x check exists to drive them).
package meter_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/meter"
)

// selector targets the eventual self-instrumentation series
// (promcost_run_*, a separate follow-up pass) rather than
// testdata/mimir-rec-rules, since that mixin's rules query Mimir's own
// internal cortex_* engine metrics, which a Grafana Cloud tenant does not
// expose to itself. Until that follow-up lands, this test only asserts "no
// error, count >= 0" — a connectivity/auth smoke test, not a correctness
// test.
const selector = `promcost_run_duration_seconds`

func testConfig(t *testing.T) (config.BackendConfig, string) {
	t.Helper()

	url := os.Getenv("PROMCOST_TEST_BACKEND_URL")
	username := os.Getenv("PROMCOST_TEST_BACKEND_USERNAME")
	password := os.Getenv("PROMCOST_TEST_BACKEND_PASSWORD")
	if url == "" || username == "" || password == "" {
		t.Skip("PROMCOST_TEST_BACKEND_URL/USERNAME/PASSWORD not set, skipping live Grafana Cloud smoke test")
	}

	t.Setenv("PROMCOST_TEST_USERNAME", username)
	t.Setenv("PROMCOST_TEST_PASSWORD", password)

	return config.BackendConfig{
		Type: "mimir",
		URL:  url,
		Auth: config.BackendAuth{
			UsernameEnv: "PROMCOST_TEST_USERNAME",
			PasswordEnv: "PROMCOST_TEST_PASSWORD",
		},
		Timeout: config.Duration(10 * time.Second),
	}, "" // Grafana Cloud's free tier is single-tenant; no tenancy header needed.
}

func newTestMeter(t *testing.T) meter.Meter {
	t.Helper()
	backend, tenancyHeader := testConfig(t)
	m, err := meter.New(backend, tenancyHeader, time.Hour)
	if err != nil {
		t.Fatalf("meter.New: %v", err)
	}
	return m
}

func TestIntegration_Probe(t *testing.T) {
	m := newTestMeter(t)
	prober, ok := m.(meter.Prober)
	if !ok {
		t.Fatal("real Meter does not implement Prober")
	}
	if err := prober.Probe(context.Background(), ""); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

func TestIntegration_SeriesCount(t *testing.T) {
	m := newTestMeter(t)
	n, err := m.SeriesCount("", selector)
	if err != nil {
		t.Fatalf("SeriesCount: %v", err)
	}
	if n < 0 {
		t.Errorf("SeriesCount = %d, want >= 0", n)
	}
}

func TestIntegration_GroupedCount(t *testing.T) {
	m := newTestMeter(t)
	n, err := m.GroupedCount("", selector, []string{"job"})
	if err != nil {
		t.Fatalf("GroupedCount: %v", err)
	}
	if n < 0 {
		t.Errorf("GroupedCount = %d, want >= 0", n)
	}
}

func TestIntegration_SampleSeries(t *testing.T) {
	m := newTestMeter(t)
	series, err := m.SampleSeries("", selector, 5)
	if err != nil {
		t.Fatalf("SampleSeries: %v", err)
	}
	if len(series) > 5 {
		t.Errorf("SampleSeries returned %d series, want <= 5", len(series))
	}
}

func TestIntegration_RangeSampleCount(t *testing.T) {
	m := newTestMeter(t)
	exact := labels.FromStrings("__name__", selector)
	n, err := m.RangeSampleCount("", exact, time.Hour)
	if err != nil {
		t.Fatalf("RangeSampleCount: %v", err)
	}
	if n < 0 {
		t.Errorf("RangeSampleCount = %d, want >= 0", n)
	}
}
