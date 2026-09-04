package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
	"github.com/KorhanOzturk90/obscost/internal/analyzer/checks"
	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/limits"
	"github.com/KorhanOzturk90/obscost/internal/loader/dir"
	"github.com/KorhanOzturk90/obscost/internal/meter"
	"github.com/KorhanOzturk90/obscost/internal/report"
	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

// newCheckCmd wires `check`: config.Load -> tenancy resolver -> limits
// provider -> meter.New (unless --offline or no backend.url) -> dir.Loader
// -> analyzer.Analyzer (checks.All()) -> md reporter -> exit code. `scan`
// (not built this milestone) will reuse the same Analyzer and check
// Registry with a CRD/ruler-API Loader and a different Reporter/flags in
// its place.
//
// No PC-L0x check is registered yet (checks.All() is still static-tier
// only), so a constructed Meter has no functional effect on findings this
// milestone — its only visible effect today is the --strict/exit-code-3
// backend-reachability contract below.
func newCheckCmd(stdout io.Writer) *cobra.Command {
	var (
		dirPath    string
		configPath string
		offline    bool
		strict     bool
		failOn     string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Analyze rule files in a directory and report findings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheck(cmd.Context(), stdout, checkOptions{
				dir:        dirPath,
				configPath: configPath,
				offline:    offline,
				strict:     strict,
				failOn:     failOn,
			})
		},
	}

	cmd.Flags().StringVar(&dirPath, "dir", "", "directory of rule files to check (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to promcost.yaml")
	cmd.Flags().BoolVar(&offline, "offline", false, "skip live-tier checks and never contact backend.url")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 3 if backend.url is configured but unreachable, instead of degrading to static-only")
	cmd.Flags().StringVar(&failOn, "fail-on", "error", "minimum severity that causes a non-zero exit: warn|error")
	if err := cmd.MarkFlagRequired("dir"); err != nil {
		panic(err) // programmer error: flag name typo
	}

	return cmd
}

type checkOptions struct {
	dir        string
	configPath string
	offline    bool
	strict     bool
	failOn     string
}

func runCheck(ctx context.Context, stdout io.Writer, opts checkOptions) error {
	failOnSeverity, err := rule.ParseSeverity(opts.failOn)
	if err != nil {
		return fmt.Errorf("invalid --fail-on value %q: %w", opts.failOn, err)
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

	limitsProvider, err := limits.NewChainProvider(ctx, cfg.Limits.Sources)
	if err != nil {
		return err
	}

	m, err := buildMeter(ctx, stdout, cfg, opts)
	if err != nil {
		return err
	}

	l := dir.New(dir.Config{
		Dir:      opts.dir,
		Resolver: resolver,
		Policy:   policy,
	})
	rules, loadErrs, err := l.Load(ctx)
	if err != nil {
		return err
	}
	if len(loadErrs) > 0 {
		for _, le := range loadErrs {
			_, _ = fmt.Fprintln(stdout, "load error:", le.Error())
		}
		return fmt.Errorf("%d rule file(s) failed to load", len(loadErrs))
	}

	reg := analyzer.NewRegistry()
	for _, c := range checks.All() {
		reg.Register(c)
	}

	findings, err := analyzer.New(reg).Run(ctx, rules, analyzer.CheckContext{
		Config: cfg,
		Limits: limitsProvider,
		Meter:  m,
	})
	if err != nil {
		return err
	}

	rep, err := report.New(report.FormatMD)
	if err != nil {
		return err
	}
	if err := rep.Render(stdout, report.Result{
		Findings:     findings,
		RulesScanned: len(rules),
		TenantsSeen:  tenantsOf(rules),
		GeneratedAt:  time.Now(),
	}); err != nil {
		return err
	}

	if maxSev, ok := maxSeverityOf(findings); ok && maxSev >= failOnSeverity {
		return &exitError{code: 2, err: fmt.Errorf("findings at or above severity %s", failOnSeverity)}
	}
	return nil
}

// buildMeter constructs and probes the real Meter when a backend is
// configured and --offline wasn't passed. A construction or probe failure
// degrades to static-only (Meter: nil) with a warning printed to stdout,
// unless --strict was passed, in which case it's exit code 3 (spec §2:
// "backend unreachable... check mode degrades to static-only... unless
// --strict").
func buildMeter(ctx context.Context, stdout io.Writer, cfg config.Config, opts checkOptions) (meter.Meter, error) {
	if opts.offline || cfg.Backend.URL == "" {
		return nil, nil
	}

	m, err := meter.New(cfg.Backend, cfg.Tenancy.Header, cfg.Checks.Thresholds.PresenceWindow.Duration())
	if err == nil {
		if prober, ok := m.(meter.Prober); ok {
			err = prober.Probe(ctx, "")
		}
	}
	if err != nil {
		if opts.strict {
			return nil, &exitError{code: 3, err: fmt.Errorf("backend unreachable: %w", err)}
		}
		_, _ = fmt.Fprintln(stdout, "warning: backend unreachable, continuing with static-only checks:", err)
		return nil, nil
	}
	return m, nil
}

func tenantsOf(rules []rule.AnnotatedRule) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range rules {
		if r.Tenant == "" {
			continue
		}
		if _, ok := seen[r.Tenant]; ok {
			continue
		}
		seen[r.Tenant] = struct{}{}
		out = append(out, r.Tenant)
	}
	return out
}

func maxSeverityOf(findings []rule.Finding) (rule.Severity, bool) {
	if len(findings) == 0 {
		return 0, false
	}
	max := findings[0].Severity
	for _, f := range findings[1:] {
		if f.Severity > max {
			max = f.Severity
		}
	}
	return max, true
}
