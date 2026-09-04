package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KorhanOzturk90/obscost/internal/attribution"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/loader/dir"
	"github.com/KorhanOzturk90/obscost/internal/report"
	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/telemetry"
	"github.com/KorhanOzturk90/obscost/internal/telemetry/mimirlogs"
	"github.com/KorhanOzturk90/obscost/internal/telemetry/ndjson"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

// newReportCmd wires `report`: config.Load -> tenancy resolver -> dir.Loader
// (rule definitions) -> ndjson.Source (rule executions) -> optional --since
// filter -> attribution.Aggregate -> workload reporter. Unlike `check`, this
// needs no live backend at all — internal/meter is not involved.
func newReportCmd(stdout io.Writer) *cobra.Command {
	var (
		dirPath       string
		telemetryPath string
		telemetryFmt  string
		configPath    string
		format        string
		since         string
		strict        bool
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report observed rule-execution workload by tenant, joined against rule definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReport(cmd.Context(), stdout, reportOptions{
				dir:           dirPath,
				telemetryPath: telemetryPath,
				telemetryFmt:  telemetryFmt,
				configPath:    configPath,
				format:        format,
				since:         since,
				strict:        strict,
			})
		},
	}

	cmd.Flags().StringVar(&dirPath, "dir", "", "directory of rule files providing rule definitions (required)")
	cmd.Flags().StringVar(&telemetryPath, "telemetry", "", "path to a rule-execution telemetry file (required)")
	cmd.Flags().StringVar(&telemetryFmt, "telemetry-format", "ndjson", "format of --telemetry: ndjson|mimirlogs (Mimir's -ruler.query-stats-enabled log)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to promcost.yaml")
	cmd.Flags().StringVar(&format, "format", "md", "output format: md|json")
	cmd.Flags().StringVar(&since, "since", "", "only include executions at or after this long ago (e.g. 24h, 7d, 2w); empty means no filtering")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail instead of warning when telemetry records can't be read/matched (e.g. an unmatched or ambiguous mimirlogs line)")
	if err := cmd.MarkFlagRequired("dir"); err != nil {
		panic(err) // programmer error: flag name typo
	}
	if err := cmd.MarkFlagRequired("telemetry"); err != nil {
		panic(err) // programmer error: flag name typo
	}

	return cmd
}

type reportOptions struct {
	dir           string
	telemetryPath string
	telemetryFmt  string
	configPath    string
	format        string
	since         string
	strict        bool
}

func runReport(ctx context.Context, stdout io.Writer, opts reportOptions) error {
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

	policy, err := tenancy.ParseUnmappedPolicy(cfg.Tenancy.Unmapped)
	if err != nil {
		return err
	}
	resolver := tenancy.NewResolver(cfg.Tenancy)

	l := dir.New(dir.Config{
		Dir:      opts.dir,
		Resolver: resolver,
		Policy:   policy,
	})
	definitions, loadErrs, err := l.Load(ctx)
	if err != nil {
		return err
	}
	if len(loadErrs) > 0 {
		for _, le := range loadErrs {
			_, _ = fmt.Fprintln(stdout, "load error:", le.Error())
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
			_, _ = fmt.Fprintln(stdout, "warning: telemetry record skipped:", re.Error())
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

	agg := attribution.Aggregate(executions, definitions)
	return rep.Render(stdout, report.WorkloadResult{
		Window:          windowLabel(opts.since),
		Tenants:         agg.Tenants,
		Unmatched:       agg.Unmatched,
		TotalExecutions: agg.TotalExecutions,
		TotalSamples:    agg.TotalSamples,
		RuleDefinitions: agg.RuleDefinitions,
		GeneratedAt:     time.Now(),
	})
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
