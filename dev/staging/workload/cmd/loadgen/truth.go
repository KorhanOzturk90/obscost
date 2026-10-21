package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// GroundTruth is the machine-readable record of what the generator actually
// produced. It holds facts, not expectations: when each tenant's samples
// really started and stopped arriving, how many were accepted, which
// queries ran. Turning those facts into "what Mimir should report" needs
// Mimir's own semantics (the active-series idle timeout, rule lookback), and
// that model lives in the harness (dev/staging/validate), where it is
// stated next to the checks that rely on it.
type GroundTruth struct {
	Schema    int                     `json:"schema"`
	Profile   Profile                 `json:"profile"`
	Gateway   string                  `json:"gateway"`
	StartedAt time.Time               `json:"started_at"` // first start of this run; start_after/stop_after count from here
	UpdatedAt time.Time               `json:"updated_at"`
	Tenants   map[string]*TenantTruth `json:"tenants"`
}

type TenantTruth struct {
	Shape string `json:"shape"`

	Series SeriesTruth `json:"series"`
	// ChurnPeriodSeconds is set for a churny tenant: every series is
	// replaced at each multiple of it, in unix time.
	ChurnPeriodSeconds *float64 `json:"churn_period_s,omitempty"`

	PlannedStartAfterSeconds float64  `json:"planned_start_after_s"`
	PlannedStopAfterSeconds  *float64 `json:"planned_stop_after_s,omitempty"`

	// Segments are the periods samples were actually accepted by Mimir,
	// one per generator process lifetime (a restart opens a new one).
	Segments []Segment `json:"segments"`

	Queries *QueryTruth `json:"queries,omitempty"`
	Rules   *RuleTruth  `json:"rules,omitempty"`
	// RulesPushedAt is when the ruler accepted this tenant's rules.
	RulesPushedAt *time.Time `json:"rules_pushed_at,omitempty"`

	Counters Counters `json:"counters"`
}

type SeriesTruth struct {
	Metrics          int     `json:"metrics"`
	PerMetric        int     `json:"per_metric"`
	Shards           int     `json:"shards"`
	Live             int     `json:"live"` // series written at any one moment
	IntervalSeconds  float64 `json:"interval_s"`
	SamplesPerSecond float64 `json:"samples_per_second"`
	ExtraLabels      int     `json:"extra_labels"`
}

type Segment struct {
	FirstSampleAt *time.Time `json:"first_sample_at,omitempty"`
	LastSampleAt  *time.Time `json:"last_sample_at,omitempty"`
	// StoppedAt is set when the tenant stopped on purpose (stop_after, or
	// a clean shutdown). Unset with a LastSampleAt means the process died.
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
}

type QueryTruth struct {
	Panels         []string `json:"panels"`
	Viewers        int      `json:"viewers"`
	PerMinute      float64  `json:"per_minute"`
	RangeSeconds   float64  `json:"range_s"`
	StepSeconds    float64  `json:"step_s"`
	RefreshSeconds float64  `json:"refresh_s"`
	// SeriesPerQuery is how many raw series each panel's selector reads
	// (one metric's worth), so fetched volume per query is predictable up
	// to chunk encoding.
	SeriesPerQuery int `json:"series_per_query"`
}

type Counters struct {
	SamplesSent         int64      `json:"samples_sent"`
	SamplesFailed       int64      `json:"samples_failed"`
	WriteRequestsFailed int64      `json:"write_requests_failed"`
	QueriesOK           int64      `json:"queries_ok"`
	QueriesFailed       int64      `json:"queries_failed"`
	QueryResponseBytes  int64      `json:"query_response_bytes"`
	LastError           string     `json:"last_error,omitempty"`
	LastErrorAt         *time.Time `json:"last_error_at,omitempty"`
}

func planTruth(p Profile, gateway string, now time.Time) *GroundTruth {
	gt := &GroundTruth{Schema: 1, Profile: p, Gateway: gateway, StartedAt: now, Tenants: map[string]*TenantTruth{}}
	for _, t := range p.Tenants {
		tt := &TenantTruth{
			Shape: t.Shape,
			Series: SeriesTruth{
				Metrics: t.Series.Metrics, PerMetric: t.Series.PerMetric, Shards: t.Series.Shards,
				Live: t.Series.Live(), IntervalSeconds: t.Series.Interval.Seconds(),
				SamplesPerSecond: float64(t.Series.Live()) / t.Series.Interval.Seconds(),
				ExtraLabels:      t.Series.ExtraLabels,
			},
			PlannedStartAfterSeconds: t.Timing.StartAfter.Seconds(),
		}
		if t.Churn != nil {
			s := t.Churn.Period.Seconds()
			tt.ChurnPeriodSeconds = &s
		}
		if t.Timing.StopAfter.D > 0 {
			s := t.Timing.StopAfter.Seconds()
			tt.PlannedStopAfterSeconds = &s
		}
		if q := t.Queries; q != nil {
			v := q.Viewers
			if v < 1 {
				v = 1
			}
			tt.Queries = &QueryTruth{
				Panels: expandPanels(t), Viewers: v, PerMinute: q.PerMinute(),
				RangeSeconds: q.Range.Seconds(), StepSeconds: q.Step.Seconds(), RefreshSeconds: q.Refresh.Seconds(),
				SeriesPerQuery: t.Series.PerMetric,
			}
		}
		if t.Rules != nil {
			_, rt := generateRules(t)
			tt.Rules = &rt
		}
		gt.Tenants[t.Name] = tt
	}
	return gt
}

// truthStore guards the ground truth, which every tenant goroutine updates,
// and persists it so a restarted generator continues the same run.
type truthStore struct {
	mu   sync.Mutex
	gt   *GroundTruth
	path string
}

// resume merges facts from a previous run's file into a freshly planned
// truth, when the file is for the same profile: the run keeps its original
// start time (so start_after/stop_after mean the same thing) and every
// tenant keeps its earlier segments and counters.
func (s *truthStore) resume() (bool, error) {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var old GroundTruth
	if err := json.Unmarshal(raw, &old); err != nil {
		return false, fmt.Errorf("%s: %w", s.path, err)
	}
	if old.Profile.Name != s.gt.Profile.Name {
		return false, fmt.Errorf("%s holds a run of profile %q, not %q: move it away to start a new run", s.path, old.Profile.Name, s.gt.Profile.Name)
	}
	s.gt.StartedAt = old.StartedAt
	for name, tt := range s.gt.Tenants {
		if o, ok := old.Tenants[name]; ok {
			tt.Segments = o.Segments
			tt.Counters = o.Counters
			tt.RulesPushedAt = o.RulesPushedAt
		}
	}
	return true, nil
}

func (s *truthStore) update(tenant string, f func(*TenantTruth)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s.gt.Tenants[tenant])
}

func (s *truthStore) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gt.UpdatedAt = time.Now().UTC()
	out, _ := json.MarshalIndent(s.gt, "", "  ")
	return out
}

func (s *truthStore) save() error {
	if s.path == "" {
		return nil
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, s.snapshot(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (c *Counters) fail(err error) {
	now := time.Now().UTC()
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	c.LastError, c.LastErrorAt = msg, &now
}
