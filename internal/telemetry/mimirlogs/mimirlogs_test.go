package mimirlogs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// def builds a test rule.AnnotatedRule with AST populated by actually
// parsing expr — matching what every real loader (internal/loader/dir)
// guarantees. A hand-built fixture that skipped this would panic in
// buildExprIndex, same as production code would if a loader ever produced
// an AnnotatedRule with a nil AST: that's meant to be an impossible state,
// not one this package defends against.
func def(t *testing.T, tenant, namespace, group, name, expr string) rule.AnnotatedRule {
	t.Helper()
	ast, err := promqlParser.ParseExpr(expr)
	if err != nil {
		t.Fatalf("def: parse expr %q: %v", expr, err)
	}
	return rule.AnnotatedRule{
		Rule:     rule.Rule{Kind: rule.KindRecording, Record: name, Expr: expr},
		AST:      ast,
		Tenant:   tenant,
		Group:    rule.RuleGroupMeta{Name: group},
		Location: rule.SourceLocation{File: namespace, Group: group, Rule: name},
	}
}

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ruler.log")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write log fixture: %v", err)
	}
	return path
}

// Real, unmodified example lines as actually rendered by Mimir's ruler
// (pkg/ruler/compat.go's RecordAndReportRuleQueryMetrics, logged via
// pkg/util/log's logfmt logger) — confirmed against grafana/mimir source
// at tag mimir-3.2.0, not invented.
const (
	localEvalLine = `ts=2026-01-15T10:30:00.123456789Z caller=compat.go:394 level=info user=analytics msg="query stats" component=ruler query="sum(rate(http_requests_total[5m]))" query_wall_time_seconds=0.523 fetched_series_count=1200 fetched_chunk_bytes=45231 fetched_chunks_count=340 sharded_queries=0 result_series_count=1`

	remoteEvalLine = `ts=2026-01-15T10:31:00.987654321Z caller=compat.go:394 level=info user=payments msg="query stats" component=ruler query="sum(rate(payment_total[5m]))" result_series_count=1`

	unrelatedLine = `ts=2026-01-15T10:29:00.000000000Z caller=distributor.go:210 level=info user=analytics msg="push request" component=distributor samples=42`
)

func TestRead_LocalEvaluation_FullStats(t *testing.T) {
	defs := []rule.AnnotatedRule{
		def(t, "analytics", "team-a/rules.yaml", "g", "customer_activity", "sum(rate(http_requests_total[5m]))"),
	}
	path := writeLog(t, localEvalLine)

	execs, readErrs, err := New(Config{Path: path, Definitions: defs}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 0 {
		t.Fatalf("readErrs = %+v, want none", readErrs)
	}
	if len(execs) != 1 {
		t.Fatalf("len(execs) = %d, want 1", len(execs))
	}

	e := execs[0]
	if e.Tenant != "analytics" || e.Namespace != "team-a/rules.yaml" || e.Group != "g" || e.RuleName != "customer_activity" {
		t.Errorf("identity = %+v, want tenant=analytics namespace=team-a/rules.yaml group=g rule=customer_activity", e.RuleID())
	}
	wantTS := time.Date(2026, 1, 15, 10, 30, 0, 123456789, time.UTC)
	if !e.Timestamp.Equal(wantTS) {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, wantTS)
	}
	if e.QueryText != "sum(rate(http_requests_total[5m]))" {
		t.Errorf("QueryText = %q", e.QueryText)
	}
	if e.SamplesProcessed != nil {
		t.Errorf("SamplesProcessed = %v, want nil (this stat does not exist on this log line, ever)", *e.SamplesProcessed)
	}
	switch {
	case e.DurationSeconds == nil || *e.DurationSeconds != 0.523:
		t.Errorf("DurationSeconds = %v, want 0.523", e.DurationSeconds)
	case e.FetchedSeries == nil || *e.FetchedSeries != 1200:
		t.Errorf("FetchedSeries = %v, want 1200", e.FetchedSeries)
	case e.FetchedChunks == nil || *e.FetchedChunks != 340:
		t.Errorf("FetchedChunks = %v, want 340", e.FetchedChunks)
	case e.FetchedBytes == nil || *e.FetchedBytes != 45231:
		t.Errorf("FetchedBytes = %v, want 45231", e.FetchedBytes)
	}
}

func TestRead_RemoteEvaluation_NoStatsFields(t *testing.T) {
	defs := []rule.AnnotatedRule{
		def(t, "payments", "team-b/rules.yaml", "g", "revenue", "sum(rate(payment_total[5m]))"),
	}
	path := writeLog(t, remoteEvalLine)

	execs, readErrs, err := New(Config{Path: path, Definitions: defs}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 0 {
		t.Fatalf("readErrs = %+v, want none", readErrs)
	}
	if len(execs) != 1 {
		t.Fatalf("len(execs) = %d, want 1", len(execs))
	}

	e := execs[0]
	if e.DurationSeconds != nil || e.SamplesProcessed != nil || e.FetchedSeries != nil || e.FetchedChunks != nil || e.FetchedBytes != nil {
		t.Errorf("stats = %+v, want every stat pointer nil (remote evaluation carries none of them — never a fabricated 0)", e)
	}
	if e.Tenant != "payments" || e.RuleName != "revenue" {
		t.Errorf("identity = %+v, want tenant=payments rule=revenue", e.RuleID())
	}
}

func TestRead_NonRulerLine_SkippedSilently(t *testing.T) {
	defs := []rule.AnnotatedRule{
		def(t, "analytics", "team-a/rules.yaml", "g", "customer_activity", "sum(rate(http_requests_total[5m]))"),
	}
	path := writeLog(t, unrelatedLine, localEvalLine)

	execs, readErrs, err := New(Config{Path: path, Definitions: defs}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 0 {
		t.Fatalf("readErrs = %+v, want none (a non-ruler line is not a parse error)", readErrs)
	}
	if len(execs) != 1 {
		t.Fatalf("len(execs) = %d, want 1 (only the real ruler query-stats line)", len(execs))
	}
}

func TestRead_NoMatchingDefinition(t *testing.T) {
	// No definitions loaded at all -> the query text can't be resolved to
	// any rule identity.
	path := writeLog(t, localEvalLine)

	execs, readErrs, err := New(Config{Path: path, Definitions: nil}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("execs = %+v, want none", execs)
	}
	if len(readErrs) != 1 {
		t.Fatalf("readErrs = %+v, want exactly 1", readErrs)
	}
	if !strings.Contains(readErrs[0].Error(), "no match") {
		t.Errorf("ReadError = %q, want it to mention \"no match\"", readErrs[0].Error())
	}
}

func TestRead_AmbiguousDefinition(t *testing.T) {
	// Two definitions in the same tenant with byte-identical Expr text —
	// refuse to guess which one produced the execution.
	defs := []rule.AnnotatedRule{
		def(t, "analytics", "team-a/rules.yaml", "g1", "rule_one", "sum(rate(http_requests_total[5m]))"),
		def(t, "analytics", "team-a/rules2.yaml", "g2", "rule_two", "sum(rate(http_requests_total[5m]))"),
	}
	path := writeLog(t, localEvalLine)

	execs, readErrs, err := New(Config{Path: path, Definitions: defs}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("execs = %+v, want none", execs)
	}
	if len(readErrs) != 1 {
		t.Fatalf("readErrs = %+v, want exactly 1", readErrs)
	}
	if !strings.Contains(readErrs[0].Error(), "ambiguous") {
		t.Errorf("ReadError = %q, want it to mention \"ambiguous\" (distinct from the no-match case)", readErrs[0].Error())
	}
}

func TestRead_MalformedApparentQueryStatsLine(t *testing.T) {
	// Looks like it was meant to be a ruler query-stats line (has
	// component=ruler and msg="query...) but the logfmt itself is a genuine
	// syntax error (a bare "=" where a key was expected) — confirmed via
	// go-logfmt/logfmt directly that this trips Decoder.Err(), unlike some
	// other malformations (e.g. an unterminated quote) that the decoder
	// tolerates leniently. Must report a ReadError with the right line
	// number and keep parsing the valid line that follows.
	malformed := `ts=2026-01-15T10:32:00.000000000Z level=info user=analytics msg="query stats" component=ruler ="badvalue"`
	defs := []rule.AnnotatedRule{
		def(t, "analytics", "team-a/rules.yaml", "g", "customer_activity", "sum(rate(http_requests_total[5m]))"),
	}
	path := writeLog(t, malformed, localEvalLine)

	execs, readErrs, err := New(Config{Path: path, Definitions: defs}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 1 {
		t.Fatalf("readErrs = %+v, want exactly 1", readErrs)
	}
	if readErrs[0].Line != 1 {
		t.Errorf("ReadError.Line = %d, want 1", readErrs[0].Line)
	}
	if len(execs) != 1 {
		t.Fatalf("len(execs) = %d, want 1 (the valid line after the malformed one must still parse)", len(execs))
	}
}

func TestRead_NonexistentFile(t *testing.T) {
	_, _, err := New(Config{Path: "/nonexistent/path/ruler.log"}).Read(context.Background())
	if err == nil {
		t.Fatal("expected a top-level error for a missing file")
	}
}

// Regression test for a real bug found running this package against a live
// Mimir instance (not a hypothetical): Prometheus's rule engine queries
// using its parsed expression's own String() form, not the rule file's
// original source text. A mixin author writing the aggregation modifier
// after the expression (`sum(rate(m[1m])) by (cluster, job)`) shows up in
// the ruler's actual query-stats log with the modifier reprinted before it
// (`sum by (cluster, job) (rate(m[1m]))`) — semantically identical PromQL,
// byte-for-byte different source text. Comparing raw text here matched
// almost nothing against real ruler output; this asserts the fix (compare
// each side's canonical AST string instead) actually holds.
func TestRead_MatchesDespiteAggregationModifierReprinting(t *testing.T) {
	defs := []rule.AnnotatedRule{
		def(t, "analytics", "team-a/rules.yaml", "g", "avg_latency",
			"sum(rate(request_duration_seconds_sum[1m])) by (cluster, job) / sum(rate(request_duration_seconds_count[1m])) by (cluster, job)"),
	}
	// This is exactly the reprinted form the real ruler logs for the
	// AnnotatedRule.Expr above — confirmed by actually parsing both with
	// promql/parser and comparing their String() output.
	line := `ts=2026-01-15T10:33:00.000000000Z level=info user=analytics msg="query stats" component=ruler query="sum by (cluster, job) (rate(request_duration_seconds_sum[1m])) / sum by (cluster, job) (rate(request_duration_seconds_count[1m]))" query_wall_time_seconds=0.01 fetched_series_count=10 fetched_chunk_bytes=100 fetched_chunks_count=10 result_series_count=1`
	path := writeLog(t, line)

	execs, readErrs, err := New(Config{Path: path, Definitions: defs}).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 0 {
		t.Fatalf("readErrs = %+v, want none — raw-text mismatch must not cause a no-match error here", readErrs)
	}
	if len(execs) != 1 {
		t.Fatalf("len(execs) = %d, want 1", len(execs))
	}
	if execs[0].RuleName != "avg_latency" {
		t.Errorf("RuleName = %q, want avg_latency (the reprinted query text should still resolve to the original rule)", execs[0].RuleName)
	}
}
