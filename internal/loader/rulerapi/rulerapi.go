// Package rulerapi implements a rule.AnnotatedRule source that fetches
// currently-loaded rule definitions directly from a Mimir ruler's own
// Prometheus-compatible rules API (GET /prometheus/api/v1/rules,
// tenant-scoped via X-Scope-OrgID), instead of requiring a local checkout
// of each tenant's rule files.
//
// This exists because in a real multi-tenant Mimir deployment, each
// tenant's rules typically live in a separate repository promcost has no
// access to — asking Mimir directly "what does the ruler currently think
// this tenant's rules are" is the source that doesn't require checking out
// N repositories, and it's authoritative (a local checkout can be stale;
// the ruler can't lie about what it's actually evaluating).
//
// The exact response shape below was confirmed against a live mimir-3.2.0
// instance before writing this parser, not guessed — see rulesResponse's
// doc comment.
package rulerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/KorhanOzturk90/obscost/internal/loader"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// Config configures a Loader. Unlike dir.Config, this needs no tenancy
// resolver — the tenant for each fetched rule is simply whichever tenant
// it was fetched for (Tenants), read directly from Mimir rather than
// inferred from a file path.
type Config struct {
	BaseURL     string       // Mimir's HTTP API address, e.g. http://localhost:8080
	Header      string       // tenancy header name; defaults to X-Scope-OrgID
	Tenants     []string     // explicit tenant list to fetch rules for
	BearerToken string       // optional; sent as "Authorization: Bearer <token>" if set
	HTTPClient  *http.Client // optional; a default client with Timeout is used if nil
	Timeout     time.Duration
}

type Loader struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) *Loader {
	if cfg.Header == "" {
		cfg.Header = "X-Scope-OrgID"
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Loader{cfg: cfg, client: client}
}

// rulesResponse mirrors Mimir's actual /prometheus/api/v1/rules response
// (Prometheus-compatible rules API), e.g.:
//
//	{"status":"success","data":{"groups":[{"name":"g","file":"alerts.yaml","interval":30,
//	  "rules":[{"name":"MyAlert","query":"up == 0","type":"alerting"}]}]}}
//
// "file" is the ruler's namespace for that group — the same concept
// internal/loader/dir populates SourceLocation.File with for directory
// loading (there, a relative file path), so it maps directly onto
// RuleID.Namespace with no translation needed.
type rulesResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		Groups []ruleGroup `json:"groups"`
	} `json:"data"`
}

type ruleGroup struct {
	Name     string     `json:"name"`
	File     string     `json:"file"`
	Interval float64    `json:"interval"` // seconds
	Rules    []ruleJSON `json:"rules"`
}

type ruleJSON struct {
	Name  string `json:"name"`
	Query string `json:"query"`
	Type  string `json:"type"` // "recording" | "alerting"
}

// Load fetches rules for every configured tenant. A per-tenant fetch
// failure (network error, non-2xx, bad auth) becomes a LoadError for that
// tenant and Load continues with the rest — matching how a per-record
// problem is handled elsewhere in this codebase (internal/telemetry's
// sources, internal/loader/dir's per-file errors): one bad tenant
// shouldn't take down an otherwise-good multi-tenant fetch. A tenant with
// zero configured rule groups is not an error — Mimir returns
// {"status":"success","data":{"groups":[]}} for that, which just yields
// zero rules for that tenant.
func (l *Loader) Load(ctx context.Context) ([]rule.AnnotatedRule, []loader.LoadError, error) {
	p := parser.NewParser(parser.Options{})

	var (
		rules    []rule.AnnotatedRule
		loadErrs []loader.LoadError
	)

	for _, tenant := range l.cfg.Tenants {
		source := fmt.Sprintf("%s (tenant %s)", l.cfg.BaseURL, tenant)

		resp, err := l.fetch(ctx, tenant)
		if err != nil {
			loadErrs = append(loadErrs, loader.LoadError{File: source, Err: err})
			continue
		}

		for _, g := range resp.Data.Groups {
			interval := time.Duration(g.Interval * float64(time.Second))

			for _, rn := range g.Rules {
				kind, err := rule.ParseKind(rn.Type)
				if err != nil {
					loadErrs = append(loadErrs, loader.LoadError{File: source, Err: fmt.Errorf("group %s rule %s: %w", g.Name, rn.Name, err)})
					continue
				}
				ast, err := p.ParseExpr(rn.Query)
				if err != nil {
					loadErrs = append(loadErrs, loader.LoadError{File: source, Err: fmt.Errorf("group %s rule %s: could not parse query: %w", g.Name, rn.Name, err)})
					continue
				}

				var record, alert string
				if kind == rule.KindRecording {
					record = rn.Name
				} else {
					alert = rn.Name
				}

				rules = append(rules, rule.AnnotatedRule{
					Rule: rule.Rule{
						Kind:   kind,
						Record: record,
						Alert:  alert,
						Expr:   rn.Query,
					},
					AST: ast,
					Group: rule.RuleGroupMeta{
						Name:     g.Name,
						Interval: interval,
					},
					Tenant: tenant,
					Location: rule.SourceLocation{
						File:  g.File,
						Group: g.Name,
						Rule:  rn.Name,
					},
				})
			}
		}
	}

	return rules, loadErrs, nil
}

func (l *Loader) fetch(ctx context.Context, tenant string) (*rulesResponse, error) {
	url := strings.TrimRight(l.cfg.BaseURL, "/") + "/prometheus/api/v1/rules"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(l.cfg.Header, tenant)
	if l.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+l.cfg.BearerToken)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed rulesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("ruler API returned status %q: %s", parsed.Status, parsed.Error)
	}
	return &parsed, nil
}
