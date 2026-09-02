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
	"github.com/KorhanOzturk90/obscost/internal/report"
	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

// newCheckCmd wires `check`: config.Load -> tenancy resolver -> limits
// provider -> dir.Loader -> analyzer.Analyzer (checks.All()) -> md
// reporter -> exit code. `scan` (not built this milestone) will reuse the
// same Analyzer and check Registry with a CRD/ruler-API Loader and a
// different Reporter/flags in its place.
func newCheckCmd(stdout io.Writer) *cobra.Command {
	var (
		dirPath    string
		configPath string
		offline    bool
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
				failOn:     failOn,
			})
		},
	}

	cmd.Flags().StringVar(&dirPath, "dir", "", "directory of rule files to check (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to promcost.yaml")
	cmd.Flags().BoolVar(&offline, "offline", false, "skip live/fleet-tier checks (reserved; a no-op until a live tier ships)")
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
