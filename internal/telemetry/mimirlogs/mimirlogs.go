// Package mimirlogs implements telemetry.Source for Mimir's own ruler
// query-stats log (-ruler.query-stats-enabled), Mimir's default logfmt
// output. Verified against grafana/mimir's actual source at tag
// mimir-3.2.0 (pkg/ruler/compat.go's RecordAndReportRuleQueryMetrics,
// pkg/util/log/log.go and wrappers.go) rather than guessed — see the field
// mapping notes on executionFromFields.
//
// Two hard limits of this real log line, both load-bearing for how this
// package works:
//
//  1. It has no rule group/name field at all — only the raw PromQL
//     expression text and the tenant. Group/RuleName/Namespace are
//     recovered by matching that expression text against already-loaded
//     rule definitions (see exprindex.go), not read directly off the log.
//  2. It has no samples_processed stat, ever — that measurement simply
//     doesn't exist in Mimir's ruler telemetry. rule.RuleExecution's
//     SamplesProcessed is always nil for every record this source
//     produces. And when the ruler delegates evaluation to a remote
//     query-frontend rather than evaluating locally, the log line carries
//     none of the four other stat fields either — those come back nil
//     too, never a fabricated 0 (see rule.RuleExecution's doc comment).
package mimirlogs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-logfmt/logfmt"

	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/telemetry"
)

// Config configures a Source. Unlike ndjson.Config, this also needs the
// already-loaded rule Definitions the log's expression text will be
// matched against — see the package doc's point 1.
type Config struct {
	Path        string
	Definitions []rule.AnnotatedRule
}

type Source struct {
	cfg Config
}

func New(cfg Config) *Source {
	return &Source{cfg: cfg}
}

// initialBufSize/maxBufSize mirror ndjson's: room for a line carrying a
// long query text without failing on an ordinary long line.
const (
	initialBufSize = 64 * 1024
	maxBufSize     = 8 * 1024 * 1024
)

func (s *Source) Read(_ context.Context) ([]rule.RuleExecution, []telemetry.ReadError, error) {
	f, err := os.Open(s.cfg.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("open telemetry file %s: %w", s.cfg.Path, err)
	}
	defer func() { _ = f.Close() }()

	index := buildExprIndex(s.cfg.Definitions)

	var (
		executions []rule.RuleExecution
		readErrs   []telemetry.ReadError
	)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields, err := decodeLogfmtLine(line)
		if err != nil {
			// Mimir's log stream mixes every component's output together;
			// most lines are never meant to be ruler query-stats records.
			// Only surface a ReadError for a line that looks like it was
			// trying to be one (an unrelated malformed line elsewhere in
			// the stream is not this source's problem to report).
			if looksLikeRulerQueryStatsLine(line) {
				readErrs = append(readErrs, telemetry.ReadError{Source: s.cfg.Path, Line: lineNo, Err: fmt.Errorf("malformed logfmt: %w", err)})
			}
			continue
		}

		if fields["msg"] != "query stats" || fields["component"] != "ruler" {
			continue // some other Mimir component/message — not a rule evaluation, ignore silently
		}

		exec, err := executionFromFields(fields, index)
		if err != nil {
			readErrs = append(readErrs, telemetry.ReadError{Source: s.cfg.Path, Line: lineNo, Err: err})
			continue
		}
		executions = append(executions, exec)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read telemetry file %s: %w", s.cfg.Path, err)
	}

	return executions, readErrs, nil
}

func looksLikeRulerQueryStatsLine(line string) bool {
	return strings.Contains(line, "component=ruler") && strings.Contains(line, `msg="query`)
}

func decodeLogfmtLine(line string) (map[string]string, error) {
	dec := logfmt.NewDecoder(strings.NewReader(line))
	fields := make(map[string]string)
	for dec.ScanRecord() {
		for dec.ScanKeyval() {
			fields[string(dec.Key())] = string(dec.Value())
		}
	}
	if err := dec.Err(); err != nil {
		return nil, err
	}
	return fields, nil
}

// executionFromFields builds a RuleExecution from one decoded ruler
// query-stats log line's fields.
//
// Field mapping, confirmed against grafana/mimir @ mimir-3.2.0:
//   - ts               -> Timestamp (RFC3339Nano — go-kit's DefaultTimestampUTC)
//   - user              -> Tenant (injected by pkg/util/log.WithContext,
//     not part of compat.go's own logMessage — every request-scoped Mimir
//     log line carries this)
//   - query             -> QueryText, and the input to the expression-match
//     recovering Namespace/Group/RuleName (see exprindex.go)
//   - query_wall_time_seconds -> DurationSeconds (local evaluation only)
//   - fetched_series_count    -> FetchedSeries    (local evaluation only)
//   - fetched_chunks_count    -> FetchedChunks    (local evaluation only)
//   - fetched_chunk_bytes     -> FetchedBytes     (local evaluation only;
//     note singular "chunk" in the real field name)
//   - (no field)        -> SamplesProcessed always nil: this statistic does
//     not exist anywhere in Mimir's ruler query-stats telemetry.
func executionFromFields(fields map[string]string, index *exprIndex) (rule.RuleExecution, error) {
	tenant := fields["user"]
	if tenant == "" {
		return rule.RuleExecution{}, fmt.Errorf("query-stats line missing required field \"user\" (tenant)")
	}
	query := fields["query"]
	if query == "" {
		return rule.RuleExecution{}, fmt.Errorf("query-stats line missing required field \"query\"")
	}
	tsRaw := fields["ts"]
	if tsRaw == "" {
		return rule.RuleExecution{}, fmt.Errorf("query-stats line missing required field \"ts\"")
	}
	ts, err := time.Parse(time.RFC3339Nano, tsRaw)
	if err != nil {
		return rule.RuleExecution{}, fmt.Errorf("parse ts %q: %w", tsRaw, err)
	}

	def, err := index.lookup(tenant, query)
	if err != nil {
		return rule.RuleExecution{}, err
	}

	exec := rule.RuleExecution{
		Tenant:    tenant,
		Namespace: def.Location.File,
		Group:     def.Group.Name,
		RuleName:  def.Name(),
		Timestamp: ts,
		QueryText: query,
	}

	if v, ok := parseFloatField(fields, "query_wall_time_seconds"); ok {
		exec.DurationSeconds = rule.Ptr(v)
	}
	if v, ok := parseUintField(fields, "fetched_series_count"); ok {
		exec.FetchedSeries = rule.Ptr(v)
	}
	if v, ok := parseUintField(fields, "fetched_chunks_count"); ok {
		exec.FetchedChunks = rule.Ptr(v)
	}
	if v, ok := parseUintField(fields, "fetched_chunk_bytes"); ok {
		exec.FetchedBytes = rule.Ptr(v)
	}

	return exec, nil
}

// parseFloatField/parseUintField return ok=false both when the field is
// absent (the common, expected case for remote-evaluated queries) and when
// it's present but unparseable (unexpected, but treated the same as
// "unknown" for that one stat rather than failing the whole record).
func parseFloatField(fields map[string]string, key string) (float64, bool) {
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseUintField(fields map[string]string, key string) (uint64, bool) {
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
