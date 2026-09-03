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
		configPath    string
		format        string
		since         string
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report observed rule-execution workload by tenant, joined against rule definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReport(cmd.Context(), stdout, reportOptions{
				dir:           dirPath,
				telemetryPath: telemetryPath,
				configPath:    configPath,
				format:        format,
				since:         since,
			})
		},
	}

	cmd.Flags().StringVar(&dirPath, "dir", "", "directory of rule files providing rule definitions (required)")
	cmd.Flags().StringVar(&telemetryPath, "telemetry", "", "path to a newline-delimited JSON rule-execution log (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to promcost.yaml")
	cmd.Flags().StringVar(&format, "format", "md", "output format: md|json")
	cmd.Flags().StringVar(&since, "since", "", "only include executions at or after this long ago (e.g. 24h, 7d, 2w); empty means no filtering")
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
	configPath    string
	format        string
	since         string
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

	src := ndjson.New(ndjson.Config{Path: opts.telemetryPath})
	executions, readErrs, err := src.Read(ctx)
	if err != nil {
		return err
	}
	if len(readErrs) > 0 {
		for _, re := range readErrs {
			_, _ = fmt.Fprintln(stdout, "telemetry read error:", re.Error())
		}
		return fmt.Errorf("%d telemetry record(s) failed to read", len(readErrs))
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
