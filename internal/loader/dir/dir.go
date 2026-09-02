// Package dir implements the --dir PATH rule source: a plain directory of
// Prometheus/Mimir ruler-compatible rule-group YAML files.
package dir

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	commonmodel "github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/loader"
	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/tenancy"
)

type Config struct {
	Dir      string
	Resolver tenancy.Resolver
	Policy   tenancy.UnmappedPolicy
	// DefaultGroupInterval is used when a rule group omits interval,
	// matching the ruler's own default-interval fallback.
	DefaultGroupInterval time.Duration
}

type Loader struct {
	cfg Config
}

func New(cfg Config) *Loader {
	if cfg.DefaultGroupInterval <= 0 {
		cfg.DefaultGroupInterval = time.Minute
	}
	return &Loader{cfg: cfg}
}

// discardLogger silences rulefmt.ParseFile's only logging call (a warning
// about multi-document YAML files), which isn't promcost's concern here.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func (l *Loader) Load(_ context.Context) ([]rule.AnnotatedRule, []loader.LoadError, error) {
	var files []string
	err := filepath.WalkDir(l.cfg.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".yaml", ".yml":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("walk %s: %w", l.cfg.Dir, err)
	}
	sort.Strings(files)

	p := parser.NewParser(parser.Options{})

	var (
		rules    []rule.AnnotatedRule
		loadErrs []loader.LoadError
	)

	for _, path := range files {
		groups, errs := rulefmt.ParseFile(path, false, commonmodel.LegacyValidation, p, discardLogger)
		if len(errs) > 0 {
			for _, e := range errs {
				loadErrs = append(loadErrs, loader.LoadError{File: path, Err: e})
			}
			continue
		}

		relPath, relErr := filepath.Rel(l.cfg.Dir, path)
		if relErr != nil {
			relPath = path
		}
		namespace := syntheticNamespace(relPath)
		resolvedTenant, resolved := l.cfg.Resolver.Resolve(tenancy.Facts{Namespace: namespace})
		finalTenant, keep := l.cfg.Policy.Apply(resolvedTenant, resolved)
		if !keep {
			continue
		}

		for _, g := range groups.Groups {
			interval := time.Duration(g.Interval)
			if interval <= 0 {
				interval = l.cfg.DefaultGroupInterval
			}

			for _, rn := range g.Rules {
				ast, perr := p.ParseExpr(rn.Expr)
				if perr != nil {
					loadErrs = append(loadErrs, loader.LoadError{
						File: path,
						Err:  fmt.Errorf("group %s: could not parse expr: %w", g.Name, perr),
					})
					continue
				}

				kind := rule.KindRecording
				name := rn.Record
				if rn.Alert != "" {
					kind = rule.KindAlerting
					name = rn.Alert
				}

				rules = append(rules, rule.AnnotatedRule{
					Rule: rule.Rule{
						Kind:        kind,
						Record:      rn.Record,
						Alert:       rn.Alert,
						Expr:        rn.Expr,
						For:         time.Duration(rn.For),
						Labels:      rn.Labels,
						Annotations: rn.Annotations,
					},
					AST: ast,
					Group: rule.RuleGroupMeta{
						Name:     g.Name,
						Interval: interval,
					},
					Tenant: finalTenant,
					Location: rule.SourceLocation{
						File:  filepath.ToSlash(relPath),
						Group: g.Name,
						Rule:  name,
					},
				})
			}
		}
	}

	return rules, loadErrs, nil
}

// syntheticNamespace treats the first path segment under the load root as
// the tenancy-discovery namespace, approximating a Kubernetes namespace for
// directory-mode loading. This is a documented convention for this
// milestone, not a spec-stated one: a flat directory (no subfolders) yields
// "", which resolves to unmapped unless tenancy.discovery maps "" itself.
func syntheticNamespace(relPath string) string {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}
