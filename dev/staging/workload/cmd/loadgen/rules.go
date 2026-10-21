package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rule generation. Every rule reads only its own tenant's generated
// metrics, so its output is fully determined by the profile:
//
//   - recording rule k, k even: sum by (shard) (rate(m[range]))  → Shards series
//   - recording rule k, k odd:  sum (rate(m[range]))             → 1 series
//   - alerting rule j:          sum by (shard) (rate(m[range])) < 0, which
//     never fires (a rate is never negative), so it writes no ALERTS series
//     and costs only its evaluation
//
// Every record: name is unique, so promcost --rule-outputs (which counts
// series per record: name) can be checked rule by rule.

type ruleGroup struct {
	Name     string `yaml:"name"`
	Interval string `yaml:"interval"`
	Rules    []rule `yaml:"rules"`
}

type rule struct {
	Record string            `yaml:"record,omitempty"`
	Alert  string            `yaml:"alert,omitempty"`
	Expr   string            `yaml:"expr"`
	For    string            `yaml:"for,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

// RuleTruth is what a tenant's rules are expected to produce.
type RuleTruth struct {
	Groups    int `json:"groups"`
	Recording int `json:"recording"`
	Alerting  int `json:"alerting"`
	// IntervalSeconds and RangeSeconds describe each evaluation's cost.
	IntervalSeconds float64 `json:"interval_s"`
	RangeSeconds    float64 `json:"range_s"`
	// EvaluationsPerMinute is rules × evaluations per minute: the figure
	// pool 7's share should broadly follow when rules cost the same each.
	EvaluationsPerMinute float64 `json:"evaluations_per_minute"`
	// RecordSeries maps each record: name to the series it writes while its
	// source metric has data.
	RecordSeries      map[string]int `json:"record_series"`
	RecordSeriesTotal int            `json:"record_series_total"`
}

func generateRules(t Tenant) ([]ruleGroup, RuleTruth) {
	spec := t.Rules
	if spec == nil {
		return nil, RuleTruth{}
	}
	rng := promDuration(spec.Range.D)
	truth := RuleTruth{
		Groups:          spec.Groups,
		Recording:       spec.Groups * spec.Recording,
		Alerting:        spec.Groups * spec.Alerting,
		IntervalSeconds: spec.Interval.Seconds(),
		RangeSeconds:    spec.Range.Seconds(),
		RecordSeries:    map[string]int{},
	}
	truth.EvaluationsPerMinute = float64(truth.Recording+truth.Alerting) * 60 / spec.Interval.Seconds()
	prefix := sanitize(t.Name)
	var groups []ruleGroup
	k, j := 0, 0
	for g := 0; g < spec.Groups; g++ {
		grp := ruleGroup{Name: fmt.Sprintf("%s-load-%02d", t.Name, g), Interval: promDuration(spec.Interval.D)}
		for i := 0; i < spec.Recording; i++ {
			m := metricName(t.Name, k%t.Series.Metrics)
			var r rule
			if k%2 == 0 {
				r = rule{
					Record: fmt.Sprintf("%s:load_r%d:rate_by_shard", prefix, k),
					Expr:   fmt.Sprintf("sum by (shard) (rate(%s[%s]))", m, rng),
				}
				truth.RecordSeries[r.Record] = t.Series.Shards
			} else {
				r = rule{
					Record: fmt.Sprintf("%s:load_r%d:rate_sum", prefix, k),
					Expr:   fmt.Sprintf("sum(rate(%s[%s]))", m, rng),
				}
				truth.RecordSeries[r.Record] = 1
			}
			grp.Rules = append(grp.Rules, r)
			k++
		}
		for i := 0; i < spec.Alerting; i++ {
			m := metricName(t.Name, j%t.Series.Metrics)
			grp.Rules = append(grp.Rules, rule{
				Alert:  fmt.Sprintf("%sLoadNeverFires%d", camel(t.Name), j),
				Expr:   fmt.Sprintf("sum by (shard) (rate(%s[%s])) < 0", m, rng),
				For:    "5m",
				Labels: map[string]string{"severity": "none"},
			})
			j++
		}
		groups = append(groups, grp)
	}
	for _, n := range truth.RecordSeries {
		truth.RecordSeriesTotal += n
	}
	return groups, truth
}

func camel(s string) string {
	var b strings.Builder
	up := true
	for _, c := range s {
		if c == '-' || c == '_' {
			up = true
			continue
		}
		if up && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		up = false
		b.WriteRune(c)
	}
	return b.String()
}

// promDuration renders d the way PromQL and rule files write durations
// ("5m", "1h30m"), which time.Duration.String ("5m0s") is not.
func promDuration(d interface{ Seconds() float64 }) string {
	s := int64(d.Seconds())
	var b strings.Builder
	for _, u := range []struct {
		n int64
		s string
	}{{3600, "h"}, {60, "m"}, {1, "s"}} {
		if s >= u.n {
			fmt.Fprintf(&b, "%d%s", s/u.n, u.s)
			s %= u.n
		}
	}
	if b.Len() == 0 {
		return "0s"
	}
	return b.String()
}

// writeRuleFiles writes one Prometheus-format rule file per tenant under
// dir/<tenant>/, the layout promcost --dir and Mimir's local ruler storage
// both read.
func writeRuleFiles(p Profile, dir string) error {
	for _, t := range p.Tenants {
		groups, _ := generateRules(t)
		if len(groups) == 0 {
			continue
		}
		out, err := yaml.Marshal(map[string]any{"groups": groups})
		if err != nil {
			return err
		}
		td := filepath.Join(dir, t.Name)
		if err := os.MkdirAll(td, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(td, p.RuleNamespace+".yaml"), out, 0o644); err != nil {
			return err
		}
	}
	return nil
}

type rulerClient struct {
	client  *http.Client
	base    string // gateway URL; the ruler config API is under /prometheus/config/v1/rules
	headers map[string]string
}

// replace deletes the tenant's namespace (so groups a previous profile
// pushed do not linger) and pushes every group.
func (r *rulerClient) replace(ctx context.Context, tenant, namespace string, groups []ruleGroup) error {
	nsURL := r.base + "/prometheus/config/v1/rules/" + url.PathEscape(namespace)
	if err := r.do(ctx, http.MethodDelete, nsURL, tenant, nil, http.StatusAccepted, http.StatusNotFound); err != nil {
		return fmt.Errorf("delete namespace %s: %w", namespace, err)
	}
	for _, g := range groups {
		body, err := yaml.Marshal(g)
		if err != nil {
			return err
		}
		if err := r.do(ctx, http.MethodPost, nsURL, tenant, body, http.StatusAccepted); err != nil {
			return fmt.Errorf("push group %s: %w", g.Name, err)
		}
	}
	return nil
}

func (r *rulerClient) do(ctx context.Context, method, u, tenant string, body []byte, ok ...int) error {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Scope-OrgID", tenant)
	if body != nil {
		req.Header.Set("Content-Type", "application/yaml")
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	for _, s := range ok {
		if resp.StatusCode == s {
			return nil
		}
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
}
