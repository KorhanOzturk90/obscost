package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/loader"
	"github.com/KorhanOzturk90/obscost/internal/loader/dir"
	"github.com/KorhanOzturk90/obscost/internal/loader/rulerapi"
	"github.com/KorhanOzturk90/obscost/internal/report"
	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/telemetry"
	"github.com/KorhanOzturk90/obscost/internal/telemetry/mimirlogs"
	"github.com/KorhanOzturk90/obscost/internal/telemetry/mimirmetrics"
	"github.com/KorhanOzturk90/obscost/internal/telemetry/ndjson"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

// newReportCmd wires `report`: config.Load -> rule-definitions source
// (--dir, or the ruler API when --dir is omitted) -> telemetry source
// (rule executions) -> optional --since filter -> attribution.Aggregate ->
// workload reporter. Unlike `check`, this needs no live backend for
// telemetry itself — internal/meter is not involved — but the ruler-API
// definitions source does talk to Mimir's HTTP API (config's backend.url).
//
// stdout carries only the rendered report body (md or json) — nothing
// else is ever written there, specifically so `--format json | jq` (or any
// other consumer expecting stdout to be exactly one parseable document)
// keeps working even when warnings are printed. Load errors and telemetry
// read-error warnings go to stderr instead.
func newReportCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		dirPath       string
		tenants       string
		telemetryPath string
		telemetryFmt  string
		metricsTenant string
		configPath    string
		format        string
		since         string
		strict        bool
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report observed rule-execution workload by tenant, joined against rule definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReport(cmd.Context(), stdout, stderr, reportOptions{
				dir:           dirPath,
				tenants:       tenants,
				telemetryPath: telemetryPath,
				telemetryFmt:  telemetryFmt,
				metricsTenant: metricsTenant,
				configPath:    configPath,
				format:        format,
				since:         since,
				strict:        strict,
			})
		},
	}

	cmd.Flags().StringVar(&dirPath, "dir", "", "directory of rule files providing rule definitions. If omitted, rule definitions are instead fetched live from Mimir's ruler API (config's backend.url) for --tenant's tenants — no local rule-file checkout needed")
	cmd.Flags().StringVar(&tenants, "tenant", "", "comma-separated tenant IDs to fetch rule definitions for from the ruler API; required when --dir is omitted, ignored when --dir is given")
	cmd.Flags().StringVar(&telemetryPath, "telemetry", "", "path to a rule-execution telemetry file. If omitted, workload is read from Mimir's own rule metrics instead (config's backend.url) — no log capture needed, but figures then stop at rule-group granularity")
	cmd.Flags().StringVar(&telemetryFmt, "telemetry-format", "ndjson", "format of --telemetry: ndjson|mimirlogs (Mimir's -ruler.query-stats-enabled log)")
	cmd.Flags().StringVar(&metricsTenant, "metrics-tenant", "", "tenant whose TSDB holds Mimir's own scraped metrics, for the metrics workload source. NOT a tenant being reported on — see internal/telemetry/mimirmetrics")
	cmd.Flags().StringVar(&configPath, "config", "", "path to promcost.yaml")
	cmd.Flags().StringVar(&format, "format", "md", "output format: md|json|html")
	cmd.Flags().StringVar(&since, "since", "", "window to report over (e.g. 24h, 7d, 2w). With --telemetry it filters executions; with the metrics source it is the query window and defaults to 1h")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail instead of warning when telemetry records can't be read/matched (e.g. an unmatched or ambiguous mimirlogs line)")

	return cmd
}

type reportOptions struct {
	dir           string
	tenants       string
	telemetryPath string
	telemetryFmt  string
	metricsTenant string
	configPath    string
	format        string
	since         string
	strict        bool
}

// defaultMetricsWindow is the window the metrics source queries when --since
// is omitted. Unlike the telemetry path — where "no --since" honestly means
// "everything in the file" — a PromQL range query must name some window, so
// one gets chosen here rather than silently implying the report covers all
// of history.
const defaultMetricsWindow = time.Hour

func runReport(ctx context.Context, stdout, stderr io.Writer, opts reportOptions) error {
	var sinceDuration time.Duration
	if opts.since != "" {
		d, err := parseSinceDuration(opts.since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", opts.since, err)
		}
		sinceDuration = d
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}

	// No --telemetry means no execution-level data exists to join against
	// rule definitions, so the whole definitions/matching pipeline below is
	// skipped: Mimir's own metrics already carry tenant and rule-group
	// identity, and they cannot be drilled past that no matter what
	// definitions we load (ADR 0002).
	if opts.telemetryPath == "" {
		return runMetricsReport(ctx, stdout, opts, cfg, sinceDuration)
	}

	defsLoader, err := newDefinitionsSource(opts, cfg)
	if err != nil {
		return err
	}
	definitions, loadErrs, err := defsLoader.Load(ctx)
	if err != nil {
		return err
	}
	if len(loadErrs) > 0 {
		for _, le := range loadErrs {
			_, _ = fmt.Fprintln(stderr, "load error:", le.Error())
		}
		return fmt.Errorf("%d rule file(s) failed to load", len(loadErrs))
	}

	src, err := newTelemetrySource(opts, definitions)
	if err != nil {
		return err
	}
	executions, readErrs, err := src.Read(ctx)
	if err != nil {
		return err
	}
	// Unlike a --dir load error (a rule file that's actually broken, which
	// stays fatal below), a telemetry read error is routine, expected
	// output: an unmatched or ambiguous mimirlogs line (see that package's
	// doc comment) is a normal thing for real ruler logs to contain, not a
	// sign something is broken. report is an informational command, not a
	// pass/fail gate the way check is — by default it warns and renders
	// whatever did parse, rather than discarding a mostly-good report over
	// a handful of expected skips. --strict opts back into the stricter,
	// check-like "any read problem is fatal" behavior.
	if len(readErrs) > 0 {
		for _, re := range readErrs {
			_, _ = fmt.Fprintln(stderr, "warning: telemetry record skipped:", re.Error())
		}
		if opts.strict {
			return fmt.Errorf("%d telemetry record(s) failed to read (--strict)", len(readErrs))
		}
	}

	if sinceDuration > 0 {
		cutoff := time.Now().Add(-sinceDuration)
		filtered := executions[:0]
		for _, e := range executions {
			if !e.Timestamp.Before(cutoff) {
				filtered = append(filtered, e)
			}
		}
		executions = filtered
	}

	rep, err := report.NewWorkload(report.Format(opts.format))
	if err != nil {
		return err
	}

	var observedStart, observedEnd *time.Time
	if start, end, ok := observedRange(executions); ok {
		observedStart, observedEnd = &start, &end
	}

	agg := attribution.Aggregate(executions, definitions)
	return rep.Render(stdout, report.WorkloadResult{
		Window:           windowLabel(opts.since),
		Tenants:          agg.Tenants,
		Unmatched:        agg.Unmatched,
		TotalExecutions:  agg.TotalExecutions,
		TotalSamples:     agg.TotalSamples,
		RuleDefinitions:  agg.RuleDefinitions,
		RankMetric:       agg.RankMetric,
		ObservedStart:    observedStart,
		ObservedEnd:      observedEnd,
		SkippedTelemetry: len(readErrs),
		GeneratedAt:      time.Now(),
	})
}

// observedRange returns the earliest and latest Timestamp across
// executions (after --since filtering), or ok=false if there are none —
// the ground truth of what window a report actually covers, since
// windowLabel only echoes the --since flag and says nothing when it was
// omitted (see report.WorkloadResult.ObservedStart's doc comment).
func observedRange(executions []rule.RuleExecution) (start, end time.Time, ok bool) {
	if len(executions) == 0 {
		return time.Time{}, time.Time{}, false
	}
	start, end = executions[0].Timestamp, executions[0].Timestamp
	for _, e := range executions[1:] {
		if e.Timestamp.Before(start) {
			start = e.Timestamp
		}
		if e.Timestamp.After(end) {
			end = e.Timestamp
		}
	}
	return start, end, true
}

// runMetricsReport renders a workload report from Mimir's own rule metrics,
// with no log capture and no rule definitions involved at all. This is the
// metrics path of ADR 0002: per-tenant evaluation time and per-group
// evaluation counts are already published by the ruler, exactly and with
// history, so deriving them by parsing logs would be more work for a worse
// answer. The trade is granularity — these metrics carry no rule name, so
// the report stops at the rule group and says so rather than presenting an
// empty rule tier as "nothing ran".
func runMetricsReport(ctx context.Context, stdout io.Writer, opts reportOptions, cfg config.Config, since time.Duration) error {
	if cfg.Backend.URL == "" {
		return fmt.Errorf("--telemetry was omitted, so workload comes from Mimir's rule metrics — but config's backend.url is empty, so there is nowhere to query")
	}
	if opts.metricsTenant == "" {
		return fmt.Errorf("--metrics-tenant is required when --telemetry is omitted: Mimir's own metrics live in whichever tenant scrapes them, which is usually not a tenant you are reporting on")
	}

	window := since
	if window == 0 {
		window = defaultMetricsWindow
	}

	header := cfg.Tenancy.Header
	if header == "" {
		header = "X-Scope-OrgID"
	}
	var bearerToken string
	if cfg.Backend.Auth.BearerTokenEnv != "" {
		bearerToken = os.Getenv(cfg.Backend.Auth.BearerTokenEnv)
	}

	src := mimirmetrics.New(mimirmetrics.Config{
		BaseURL:       cfg.Backend.URL,
		Header:        header,
		MetricsTenant: opts.metricsTenant,
		BearerToken:   bearerToken,
		Timeout:       cfg.Backend.Timeout.Duration(),
	})
	workload, err := src.Read(ctx, window)
	if err != nil {
		return err
	}

	tenantObs := make([]attribution.TenantObservation, 0, len(workload.Tenants))
	var groupObs []attribution.GroupObservation
	for _, t := range workload.Tenants {
		tenantObs = append(tenantObs, attribution.TenantObservation{
			Tenant:          t.Tenant,
			Executions:      t.Evaluations,
			DurationSeconds: t.DurationSeconds,
			// Mimir always publishes this histogram for a ruler that ran at
			// all, so reaching here means it was genuinely measured.
			DurationObserved: true,
		})
		for _, g := range t.Groups {
			groupObs = append(groupObs, attribution.GroupObservation{
				Tenant:     t.Tenant,
				Namespace:  g.Namespace,
				Group:      g.Group,
				Executions: g.Evaluations,
			})
		}
	}

	agg := attribution.AggregateObservations(tenantObs, groupObs)

	rep, err := report.NewWorkload(report.Format(opts.format))
	if err != nil {
		return err
	}
	start, end := workload.Start, workload.End
	return rep.Render(stdout, report.WorkloadResult{
		Window:          windowLabel(opts.since),
		Tenants:         agg.Tenants,
		TotalExecutions: agg.TotalExecutions,
		RankMetric:      agg.RankMetric,
		GroupRankMetric: agg.GroupRankMetric,
		Granularity:     agg.Granularity,
		SourceLabel:     "Mimir rule metrics (cortex_prometheus_rule_*)",
		SourceNote:      "counts are PromQL increase() figures, extrapolated at window edges",
		ObservedStart:   &start,
		ObservedEnd:     &end,
		GeneratedAt:     time.Now(),
	})
}

// newDefinitionsSource selects where rule definitions come from: a local
// directory (--dir, the original behavior — reads real files, resolves
// tenants via config's tenancy block, exactly like `check` does), or, when
// --dir is omitted, Mimir's own ruler API for an explicit --tenant list.
// The ruler-API path exists because in a real deployment each tenant's
// rules typically live in a separate repository promcost has no access
// to; asking the ruler directly what it's currently evaluating needs no
// local checkout at all, for any tenant. Tenants are given explicitly
// (not auto-discovered) so a report run stays predictable and auditable —
// see internal/loader/rulerapi's package doc for the exact API this
// fetches from.
func newDefinitionsSource(opts reportOptions, cfg config.Config) (loader.Loader, error) {
	if opts.dir != "" {
		policy, err := tenancy.ParseUnmappedPolicy(cfg.Tenancy.Unmapped)
		if err != nil {
			return nil, err
		}
		return dir.New(dir.Config{
			Dir:      opts.dir,
			Resolver: tenancy.NewResolver(cfg.Tenancy),
			Policy:   policy,
		}), nil
	}

	if opts.tenants == "" {
		return nil, fmt.Errorf("either --dir or --tenant is required (rule definitions must come from somewhere)")
	}
	if cfg.Backend.URL == "" {
		return nil, fmt.Errorf("--tenant given without --dir, but config's backend.url is empty — nowhere to fetch rule definitions from")
	}

	var tenants []string
	for _, t := range strings.Split(opts.tenants, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tenants = append(tenants, t)
		}
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("--tenant was given but contained no tenant IDs")
	}

	header := cfg.Tenancy.Header
	if header == "" {
		header = "X-Scope-OrgID"
	}
	var bearerToken string
	if cfg.Backend.Auth.BearerTokenEnv != "" {
		bearerToken = os.Getenv(cfg.Backend.Auth.BearerTokenEnv)
	}

	return rulerapi.New(rulerapi.Config{
		BaseURL:     cfg.Backend.URL,
		Header:      header,
		Tenants:     tenants,
		BearerToken: bearerToken,
		Timeout:     cfg.Backend.Timeout.Duration(),
	}), nil
}

// newTelemetrySource selects the telemetry.Source implementation for
// --telemetry-format. mimirlogs additionally needs the already-loaded rule
// definitions (its log lines carry no rule group/name, only tenant + raw
// query text — see internal/telemetry/mimirlogs's package doc); ndjson
// (the default, preserving today's behavior) does not.
func newTelemetrySource(opts reportOptions, definitions []rule.AnnotatedRule) (telemetry.Source, error) {
	switch opts.telemetryFmt {
	case "", "ndjson":
		return ndjson.New(ndjson.Config{Path: opts.telemetryPath}), nil
	case "mimirlogs":
		return mimirlogs.New(mimirlogs.Config{Path: opts.telemetryPath, Definitions: definitions}), nil
	default:
		return nil, fmt.Errorf("unknown --telemetry-format %q, want ndjson|mimirlogs", opts.telemetryFmt)
	}
}

// parseSinceDuration accepts stdlib duration syntax plus a trailing d/w
// suffix (7d -> 7*24h, 2w -> 14*24h) — time.ParseDuration alone can't parse
// day/week units, but PRODUCT-DIRECTION.md's own example command is
// literally `promcost report --since 7d`.
func parseSinceDuration(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("invalid day count %q", n)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		weeks, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("invalid week count %q", n)
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// windowLabel renders the raw --since flag value for display (e.g. "7d" ->
// "last 7d"); report.WorkloadResult.Window is display-only, the actual
// filtering already happened above via parseSinceDuration.
func windowLabel(since string) string {
	if since == "" {
		return ""
	}
	return "last " + since
}
