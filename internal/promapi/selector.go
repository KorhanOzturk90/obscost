package promapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// Selector is a set of label matchers, e.g. {cluster="prod-eu"}, added to
// every vector selector of every query a Source runs (#54). It exists
// because one metrics tenant commonly scrapes several Mimir clusters, told
// apart by a label; without it every driver, total and the replication
// factor span all of them, and "N of your M ingesters" multiplies a
// multi-cluster share by one cluster's replica count.
//
// The zero Selector selects nothing extra.
type Selector struct {
	text     string
	matchers []*labels.Matcher
}

var promqlParser = parser.NewParser(parser.Options{})

// ParseSelector parses s with the PromQL parser and accepts only a plain
// matcher set in braces: no metric name, no __name__ matcher, and no
// matcher on user, which is the tenant label and would silently drop
// tenants from every pool's denominator (--tenant is how to name tenants).
func ParseSelector(s string) (Selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Selector{}, nil
	}
	if !strings.HasPrefix(s, "{") {
		return Selector{}, fmt.Errorf("selector %q must be a label-matcher set in braces, e.g. {cluster=\"prod-eu\"}, with no metric name", s)
	}
	ms, err := promqlParser.ParseMetricSelector(s)
	if err != nil {
		return Selector{}, fmt.Errorf("selector %q: %w", s, err)
	}
	if len(ms) == 0 {
		return Selector{}, fmt.Errorf("selector %q has no matchers: omit it to select every cluster", s)
	}
	for _, m := range ms {
		switch m.Name {
		case model.MetricNameLabel:
			return Selector{}, fmt.Errorf("selector %q matches on %s: it selects a Mimir cluster, not a metric", s, model.MetricNameLabel)
		case "user":
			return Selector{}, fmt.Errorf("selector %q matches on user, the tenant label: it selects a Mimir cluster, not tenants (use --tenant to name tenants)", s)
		}
	}
	return Selector{text: s, matchers: ms}, nil
}

func (s Selector) String() string { return s.text }

// IsZero is true for a Selector that adds nothing.
func (s Selector) IsZero() bool { return len(s.matchers) == 0 }

// Apply adds the selector's matchers to every vector selector in query,
// through the AST rather than by string splicing, so a selector reaches
// subqueries, both sides of a binary operator, and label_replace's
// argument alike. With no selector the query is returned unchanged,
// keeping its original text in the assumption trail.
func (s Selector) Apply(query string) (string, error) {
	if s.IsZero() {
		return query, nil
	}
	expr, err := promqlParser.ParseExpr(query)
	if err != nil {
		return "", fmt.Errorf("adding selector to %q: %w", query, err)
	}
	parser.Inspect(expr, func(n parser.Node, _ []parser.Node) error {
		if vs, ok := n.(*parser.VectorSelector); ok {
			vs.LabelMatchers = append(slices.Clone(vs.LabelMatchers), s.matchers...)
		}
		return nil
	})
	return expr.String(), nil
}

// ClusterLabel is the label the Mimir mixin, and so most meta-monitoring
// setups, tell clusters apart by.
const ClusterLabel = "cluster"

// Clusters returns the distinct non-empty values of ClusterLabel on the
// given metrics, now, with sel applied. More than one, with no selector,
// means every figure read from those metrics sums several Mimir clusters.
func (c *Client) Clusters(ctx context.Context, sel Selector, metrics ...string) ([]string, error) {
	q, err := sel.Apply(`count by (` + ClusterLabel + `) ({__name__=~"` + strings.Join(metrics, "|") + `"})`)
	if err != nil {
		return nil, err
	}
	samples, err := c.Instant(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, smp := range samples {
		if v := smp.Metric[ClusterLabel]; v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out, nil
}
