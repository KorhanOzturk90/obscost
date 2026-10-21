package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Profile is the whole workload: every tenant's shape, read from a YAML file
// under profiles/. It is the *intended* workload; what was actually produced
// (start and stop times, samples sent, queries run) is recorded separately
// in the ground truth, because the harness must compare promcost against
// what happened, not against what was planned.
type Profile struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Tenants     []Tenant `yaml:"tenants" json:"-"`
	// RuleNamespace is the ruler namespace every generated group is pushed
	// under, so a re-run can replace exactly what an earlier run pushed.
	RuleNamespace string `yaml:"rule_namespace" json:"rule_namespace"`
}

type Tenant struct {
	Name  string `yaml:"name"`
	Shape string `yaml:"shape"`

	Series  SeriesSpec `yaml:"series"`
	Churn   *ChurnSpec `yaml:"churn,omitempty"`
	Queries *QuerySpec `yaml:"queries,omitempty"`
	Rules   *RuleSpec  `yaml:"rules,omitempty"`
	Timing  TimingSpec `yaml:"timing,omitempty"`
}

// SeriesSpec is Metrics metric names × PerMetric series each, one sample per
// series every Interval. Every series carries a `shard` label (its index mod
// Shards), which is what recording rules aggregate by, so a rule's output
// series count is known before it runs.
type SeriesSpec struct {
	Metrics   int      `yaml:"metrics"`
	PerMetric int      `yaml:"per_metric"`
	Shards    int      `yaml:"shards"`
	Interval  Duration `yaml:"interval"`
	// ExtraLabels pads every series with N constant labels, which makes
	// series bigger (more bytes per series) without making more of them.
	ExtraLabels int `yaml:"extra_labels"`
}

func (s SeriesSpec) Live() int { return s.Metrics * s.PerMetric }

// ChurnSpec replaces every series once per Period, the way a deploy replaces
// every pod: the `generation` label changes, so the old series stop and new
// ones start. Generations are aligned to the wall clock (unix time / Period),
// so the ground truth does not depend on when the generator started.
type ChurnSpec struct {
	Period Duration `yaml:"period"`
}

// QuerySpec is a dashboard: Panels range queries, all re-run every Refresh,
// each over the last Range at Step — what Grafana sends for one open
// dashboard with auto-refresh. Viewers multiplies it.
type QuerySpec struct {
	Panels  []string `yaml:"panels"`
	Refresh Duration `yaml:"refresh"`
	Range   Duration `yaml:"range"`
	Step    Duration `yaml:"step"`
	Viewers int      `yaml:"viewers"`
}

func (q QuerySpec) PerMinute() float64 {
	if q.Refresh.D <= 0 {
		return 0
	}
	v := q.Viewers
	if v < 1 {
		v = 1
	}
	return float64(len(q.Panels)*v) * float64(time.Minute) / float64(q.Refresh.D)
}

// RuleSpec generates Groups rule groups per tenant, each with Recording
// recording rules and Alerting alerting rules, evaluated every Interval.
// See rules.go for exactly what each rule is and how many series it writes.
type RuleSpec struct {
	Groups    int      `yaml:"groups"`
	Recording int      `yaml:"recording_per_group"`
	Alerting  int      `yaml:"alerting_per_group"`
	Interval  Duration `yaml:"interval"`
	// Range is the rate() range every generated rule uses. Longer ranges
	// make each evaluation read more samples, i.e. cost more ruler CPU.
	Range Duration `yaml:"range"`
}

// TimingSpec makes a tenant start late or stop early, relative to when the
// generator starts. Zero StartAfter starts at once; zero StopAfter never
// stops.
type TimingSpec struct {
	StartAfter Duration `yaml:"start_after"`
	StopAfter  Duration `yaml:"stop_after"`
}

// Duration is a time.Duration that reads "15s"/"6h" from YAML and writes
// seconds to JSON, which is what the Python harness wants.
type Duration struct{ D time.Duration }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	d.D = v
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return d.D.String(), nil }

func (d Duration) Seconds() float64 { return d.D.Seconds() }

var tenantName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func LoadProfile(path string) (Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}
	if p.RuleNamespace == "" {
		p.RuleNamespace = "loadgen"
	}
	return p, p.validate()
}

func (p Profile) validate() error {
	seen := map[string]bool{}
	for _, t := range p.Tenants {
		if !tenantName.MatchString(t.Name) {
			return fmt.Errorf("tenant %q: name must match %s (it becomes a metric-name prefix)", t.Name, tenantName)
		}
		if seen[t.Name] {
			return fmt.Errorf("tenant %q listed twice", t.Name)
		}
		seen[t.Name] = true
		s := t.Series
		if s.Metrics < 1 || s.PerMetric < 1 || s.Interval.D <= 0 {
			return fmt.Errorf("tenant %s: series needs metrics, per_metric and interval", t.Name)
		}
		if s.Shards < 1 || s.Shards > s.PerMetric {
			// Fewer series than shards would leave some shard values unused,
			// and the expected rule-output count would be wrong.
			return fmt.Errorf("tenant %s: series.shards must be between 1 and per_metric (%d)", t.Name, s.PerMetric)
		}
		if t.Churn != nil && t.Churn.Period.D < time.Minute {
			return fmt.Errorf("tenant %s: churn.period must be at least 1m", t.Name)
		}
		if q := t.Queries; q != nil && (len(q.Panels) == 0 || q.Refresh.D <= 0 || q.Range.D <= 0 || q.Step.D <= 0) {
			return fmt.Errorf("tenant %s: queries needs panels, refresh, range and step", t.Name)
		}
		if r := t.Rules; r != nil && (r.Groups < 1 || r.Interval.D <= 0 || r.Range.D <= 0) {
			return fmt.Errorf("tenant %s: rules needs groups, interval and range", t.Name)
		}
		if t.Timing.StopAfter.D > 0 && t.Timing.StopAfter.D <= t.Timing.StartAfter.D {
			return fmt.Errorf("tenant %s: timing.stop_after must be after start_after", t.Name)
		}
	}
	return nil
}

// metricName is the i-th metric of tenant t. Tenant names may contain '-',
// which a metric name may not.
func metricName(t string, i int) string {
	return fmt.Sprintf("%s_load_m%d_total", sanitize(t), i)
}

func sanitize(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c == '-' {
			b[i] = '_'
		}
	}
	return string(b)
}
