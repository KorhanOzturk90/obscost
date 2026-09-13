// Package mimirmetrics reads rule-evaluation workload straight out of
// Mimir's own self-monitoring metrics, over its PromQL HTTP API. It is the
// source that needs no log capture at all: given a Mimir URL and nothing
// else, it answers "what share of ruler time does each tenant use" and
// "which rule groups run most often" — exactly, and for the past — which
// is what docs/adr/0002-where-workload-evidence-comes-from.md assigns to
// metrics rather than to the ruler query-stats log.
//
// # MetricsTenant is not a tenant you are reporting on
//
// This is the single most confusing thing about this source, so it comes
// first. The cortex_prometheus_rule_* series are Mimir's metrics about
// itself, and they land in whichever tenant scrapes Mimir's own /metrics
// endpoints — commonly a monitoring or "infra" tenant. Inside that one
// tenant's TSDB, every other tenant appears only as the value of a `user`
// label.
//
// So Config.MetricsTenant is the tenant these queries run *as* (the value
// of the X-Scope-OrgID header), while the tenants in the returned Workload
// come from the `user` label and are usually completely different names.
// Setting MetricsTenant to "payments" does not scope the result to
// payments; it asks payments' own TSDB for Mimir's self-monitoring series,
// which are almost certainly not there, and yields an empty report with no
// error — which is why MetricsTenant is required rather than defaulted.
//
// # What this source deliberately cannot do
//
// It does not implement telemetry.Source, and that is not an oversight.
// telemetry.Source produces per-rule rule.RuleExecution records; no
// cortex_prometheus_rule_* metric carries a rule name, and none measures
// fetched data volume at all (ADR 0002, "Source 1" — every rule metric and
// its labels are tabulated there). The depth is not in the data, so the
// type returned here stops at the rule group and reports only time and
// evaluation counts, instead of pretending to a shape it cannot fill.
// Per-rule attribution and the fetched_* resource dimensions stay the
// query-stats log's job (internal/telemetry/mimirlogs).
//
// The three queries below, and the surprising shape of the `rule_group`
// label value (see splitRuleGroup), were run against a live mimir-3.2.0
// instance before this parser was written rather than guessed.
package mimirmetrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config configures a Source. Header/BearerToken/HTTPClient/Timeout mirror
// rulerapi.Config, since both talk to the same Mimir HTTP API with the same
// tenancy and auth mechanics; MetricsTenant is the one field with no
// rulerapi analogue, and the package doc explains why it is not just
// "the tenant to report on".
type Config struct {
	BaseURL       string // Mimir's HTTP API address, e.g. http://localhost:8080
	Header        string // tenancy header name; defaults to X-Scope-OrgID
	MetricsTenant string // required; the tenant whose TSDB holds Mimir's own cortex_prometheus_rule_* series
	BearerToken   string // optional; sent as "Authorization: Bearer <token>" if set
	Timeout       time.Duration
	HTTPClient    *http.Client // optional; a default client with Timeout is used if nil
}

// GroupWorkload is one rule group's share of a tenant's evaluations.
// Namespace/Group are recovered from the single `rule_group` label — see
// splitRuleGroup. There is deliberately no duration field: Mimir publishes
// per-group duration only as "the last run took this long"
// (cortex_prometheus_rule_group_last_duration_seconds), which cannot be
// summed over a window, so reporting it beside a windowed count would put
// two incomparable numbers in the same row.
type GroupWorkload struct {
	Namespace   string
	Group       string
	Evaluations float64
}

// TenantWorkload is one tenant's rule workload over the queried window.
//
// The figures are float64 rather than integers because they come from
// increase() over a counter: PromQL extrapolates at the window edges, so
// even an evaluation *count* comes back fractional (e.g. 1227.4). Rounding
// here would hide that these are estimates over a window, not exact tallies.
type TenantWorkload struct {
	Tenant          string
	DurationSeconds float64
	Evaluations     float64
	Groups          []GroupWorkload
}

// Workload is one Read's worth of observations. Start/End describe the
// window the caller asked for, measured against the local clock at Read
// time — Mimir evaluates increase() against its own clock, so the two can
// differ by whatever skew exists between this process and the cluster.
type Workload struct {
	Start, End time.Time
	Tenants    []TenantWorkload
}

type Source struct {
	cfg    Config
	client *http.Client
}

// New builds a Source. The default timeout is 30s rather than rulerapi's
// 10s: listing rule definitions is a cheap metadata read, whereas
// increase() over a 7d or 30d window fans out across every ruler series in
// the metrics tenant and is a genuinely heavy query on a large cluster.
func New(cfg Config) *Source {
	if cfg.Header == "" {
		cfg.Header = "X-Scope-OrgID"
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Source{cfg: cfg, client: client}
}

// The three queries, each verified against a live mimir-3.2.0. The %s is
// the window as a PromQL range string (see promQLDuration).
//
// Evaluation count is taken from the histogram's _count rather than from
// cortex_prometheus_rule_evaluations_total, even though the latter also
// counts evaluations: _sum and _count come from the same observations, so
// dividing one by the other gives a mean evaluation time that is actually
// self-consistent. cortex_prometheus_rule_evaluations_total is used for
// the per-group breakdown because it is the only one of the two carrying a
// rule_group label.
const (
	tenantDurationQuery = `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[%s]))`
	tenantCountQuery    = `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_count[%s]))`
	groupCountQuery     = `sum by (user, rule_group) (increase(cortex_prometheus_rule_evaluations_total[%s]))`
)

// Read queries Mimir for rule workload over [now-window, now].
//
// Postcondition on ordering — callers may rely on it, and the report layer
// does: Tenants is sorted by DurationSeconds descending, ties broken by
// Tenant ascending; each Groups slice is sorted by Evaluations descending,
// ties broken by Group then Namespace ascending. Nothing here is fed by an
// ordered source (both map iteration and Mimir's result order are
// arbitrary), so this sort is the only thing making two identical Reads
// produce identical output.
//
// The three queries are merged by tenant, not intersected: a tenant that
// appears in one query and not another still appears in the result, with
// zero for whatever was missing. Dropping it would be the worse failure —
// a tenant whose rules all errored has real evaluation time and no
// successful evaluations, and silently vanishing from the report is
// exactly the wrong answer for it.
func (s *Source) Read(ctx context.Context, window time.Duration) (Workload, error) {
	if s.cfg.MetricsTenant == "" {
		return Workload{}, errors.New("MetricsTenant is required: it names the tenant whose TSDB holds Mimir's own cortex_prometheus_rule_* series (commonly a monitoring tenant), which is not one of the tenants being reported on")
	}
	rangeStr, err := promQLDuration(window)
	if err != nil {
		return Workload{}, err
	}

	end := time.Now()
	start := end.Add(-window)

	// One accumulator per tenant, created by whichever of the three
	// queries mentions that tenant first — this is what makes the merge a
	// union rather than an intersection. Values accumulate with += rather
	// than assignment: `sum by (user)` should return one sample per tenant
	// and duplicates are not expected, but adding is the behaviour that
	// stays correct if one ever arrives, whereas assignment would keep an
	// arbitrary one of them.
	accums := map[string]*tenantAccum{}
	accum := func(tenant string) *tenantAccum {
		a, ok := accums[tenant]
		if !ok {
			a = &tenantAccum{groups: map[string]*GroupWorkload{}}
			accums[tenant] = a
		}
		return a
	}

	durations, err := s.vector(ctx, "per-tenant evaluation duration", fmt.Sprintf(tenantDurationQuery, rangeStr))
	if err != nil {
		return Workload{}, err
	}
	for _, smp := range durations {
		tenant, value, ok := smp.userValue()
		if !ok {
			continue
		}
		accum(tenant).durationSeconds += value
	}

	counts, err := s.vector(ctx, "per-tenant evaluation count", fmt.Sprintf(tenantCountQuery, rangeStr))
	if err != nil {
		return Workload{}, err
	}
	for _, smp := range counts {
		tenant, value, ok := smp.userValue()
		if !ok {
			continue
		}
		accum(tenant).evaluations += value
	}

	groups, err := s.vector(ctx, "per-group evaluation count", fmt.Sprintf(groupCountQuery, rangeStr))
	if err != nil {
		return Workload{}, err
	}
	for _, smp := range groups {
		tenant, value, ok := smp.userValue()
		if !ok {
			continue
		}
		namespace, group := splitRuleGroup(smp.Metric["rule_group"])
		accum(tenant).group(namespace, group).Evaluations += value
	}

	out := Workload{Start: start, End: end, Tenants: make([]TenantWorkload, 0, len(accums))}
	for tenant, a := range accums {
		out.Tenants = append(out.Tenants, a.workload(tenant))
	}
	sort.Slice(out.Tenants, func(i, j int) bool {
		if out.Tenants[i].DurationSeconds != out.Tenants[j].DurationSeconds {
			return out.Tenants[i].DurationSeconds > out.Tenants[j].DurationSeconds
		}
		return out.Tenants[i].Tenant < out.Tenants[j].Tenant
	})
	return out, nil
}

type tenantAccum struct {
	durationSeconds float64
	evaluations     float64
	groups          map[string]*GroupWorkload
}

func (a *tenantAccum) group(namespace, group string) *GroupWorkload {
	// Namespace is part of the key, not merely carried alongside the name:
	// one tenant can legitimately define the same group name in two
	// namespaces (alerts.yaml and recording-rules.yaml can both hold a
	// "mimir_api_1"), and keying on the name alone would silently add two
	// unrelated groups' evaluations together. The NUL separator keeps a
	// namespace that itself contains the separator from colliding.
	key := namespace + "\x00" + group
	g, ok := a.groups[key]
	if !ok {
		g = &GroupWorkload{Namespace: namespace, Group: group}
		a.groups[key] = g
	}
	return g
}

func (a *tenantAccum) workload(tenant string) TenantWorkload {
	tw := TenantWorkload{
		Tenant:          tenant,
		DurationSeconds: a.durationSeconds,
		Evaluations:     a.evaluations,
	}
	if len(a.groups) == 0 {
		return tw
	}
	tw.Groups = make([]GroupWorkload, 0, len(a.groups))
	for _, g := range a.groups {
		tw.Groups = append(tw.Groups, *g)
	}
	sort.Slice(tw.Groups, func(i, j int) bool {
		if tw.Groups[i].Evaluations != tw.Groups[j].Evaluations {
			return tw.Groups[i].Evaluations > tw.Groups[j].Evaluations
		}
		if tw.Groups[i].Group != tw.Groups[j].Group {
			return tw.Groups[i].Group < tw.Groups[j].Group
		}
		return tw.Groups[i].Namespace < tw.Groups[j].Namespace
	})
	return tw
}

// vectorResponse mirrors Mimir's actual /prometheus/api/v1/query response
// for an instant query, e.g.:
//
//	{"status":"success","data":{"resultType":"vector","result":[
//	  {"metric":{"user":"infra"},"value":[1757800000.123,"10.528"]}]}}
//
// This is the standard Prometheus envelope; Mimir adds nothing to it.
type vectorResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
	Data      struct {
		ResultType string         `json:"resultType"`
		Result     []vectorSample `json:"result"`
	} `json:"data"`
}

// vectorSample is one element of a vector result. `value` is a two-element
// heterogeneous array — [<unix seconds as a JSON number>, "<the sample
// value as a JSON *string*>"] — which is why it is held as raw messages
// rather than a typed pair. The value is a string because Prometheus needs
// to round-trip NaN and ±Inf, which JSON numbers cannot express.
type vectorSample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

// userValue pulls the tenant and the sample value out of one sample,
// reporting ok=false for anything it cannot use.
//
// Unusable samples are skipped rather than failing the whole Read, and
// deliberately are not surfaced: Read's signature stays (Workload, error)
// with no per-sample error list because there is no known way for Mimir to
// produce one of these — the PromQL API always emits a `user` label for a
// `sum by (user)` result and always renders values as parseable strings —
// so this is a guard against a proxy or a future response shape, not a
// case a caller could act on. Non-finite values (NaN, ±Inf) are skipped
// for the same reason plus a concrete one: they would make Read's
// documented sort order non-transitive.
func (s vectorSample) userValue() (tenant string, value float64, ok bool) {
	tenant = s.Metric["user"]
	if tenant == "" || len(s.Value) != 2 {
		return "", 0, false
	}
	var raw string
	if err := json.Unmarshal(s.Value[1], &raw); err != nil {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return "", 0, false
	}
	return tenant, v, true
}

// vector runs one instant query, labelling any failure with which of the
// three queries it was — all three hit the same endpoint with the same
// headers, so without the label a failure is unattributable.
func (s *Source) vector(ctx context.Context, name, promql string) ([]vectorSample, error) {
	samples, err := s.instant(ctx, promql)
	if err != nil {
		return nil, fmt.Errorf("%s query: %w", name, err)
	}
	return samples, nil
}

func (s *Source) instant(ctx context.Context, promql string) ([]vectorSample, error) {
	endpoint := strings.TrimRight(s.cfg.BaseURL, "/") + "/prometheus/api/v1/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = url.Values{"query": []string{promql}}.Encode()
	req.Header.Set(s.cfg.Header, s.cfg.MetricsTenant)
	if s.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.BearerToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Prometheus/Mimir return a JSON error body alongside a 4xx for a
		// bad query, and it names the actual problem, so echo it rather
		// than reporting a bare status code.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed vectorResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("query API returned status %q (%s): %s", parsed.Status, parsed.ErrorType, parsed.Error)
	}
	// A non-vector resultType would decode without error into an empty
	// Result (a matrix carries "values", not "value"), turning a
	// misdirected request — an interposed proxy, a range endpoint — into a
	// silently empty report. Fail instead. An absent resultType is
	// tolerated because only a non-Prometheus responder omits it.
	if parsed.Data.ResultType != "" && parsed.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected resultType %q, want vector", parsed.Data.ResultType)
	}
	return parsed.Data.Result, nil
}

// splitRuleGroup recovers a namespace and a group name from Mimir's
// `rule_group` label, whose value is not the group name: it is the ruler's
// *storage path* for the namespace file, then a semicolon, then the group
// name. Observed verbatim on mimir-3.2.0:
//
//	/data/ruler/infra/alerts.yaml;mimir_alerts
//
// The left half is reduced to its base name so the namespace matches what
// every other source in this repo calls one — rulerapi reads it from the
// rules API's "file" field ("alerts.yaml"), and loader/dir from a rule
// file's path — meaning a group identified here can be joined against one
// identified there without translation.
//
// Splitting happens on the last semicolon, not the first: a group name
// cannot contain one, but a file path on disk legally can, and the split
// that survives a weird path is the one anchored at the end. A value with
// no semicolon at all is treated as a bare group name in an unknown
// namespace rather than guessed at.
func splitRuleGroup(value string) (namespace, group string) {
	i := strings.LastIndex(value, ";")
	if i < 0 {
		return "", value
	}
	file, group := value[:i], value[i+1:]
	if file == "" {
		return "", group
	}
	// path.Base, not filepath.Base: this is a path on the ruler's
	// filesystem, always slash-separated, and must not be reinterpreted
	// against the separator of whichever OS promcost happens to run on.
	return path.Base(file), group
}

// promQLUnits is the ladder promQLDuration renders against, largest first.
// It stops at "d" on purpose. PromQL also accepts "w" and "y", but "y" is
// a flat 365 days that quietly disagrees with a calendar year, and a
// window the operator asked for as 7d should come back as 7d, not 1w.
var promQLUnits = []struct {
	suffix string
	size   time.Duration
}{
	{"d", 24 * time.Hour},
	{"h", time.Hour},
	{"m", time.Minute},
	{"s", time.Second},
	{"ms", time.Millisecond},
}

// promQLDuration renders a Go duration as a PromQL range string.
//
// time.Duration.String() cannot be used for this. It has no day unit, so
// 7 days prints as "168h0m0s", and it pads out lower units even when they
// are zero, so an exact hour prints as "1h0m0s". Both of those do parse —
// checked against the real parser, see TestDurationStringIsNotUsable — but
// they land in a query an operator may have to read back out of an error
// message, and "168h0m0s" hides the one fact the reader wants, that this
// is a week. What Duration.String() genuinely gets *wrong* is fractions:
// "1.5s" is rejected outright, PromQL's grammar being integer terms only.
//
// The rule here is: emit a single term in the largest unit that divides
// the duration exactly. Sub-millisecond remainders are truncated rather
// than rendered as a decimal, both because a millisecond is the floor of
// PromQL's grammar and because a decimal would not parse at all.
func promQLDuration(d time.Duration) (string, error) {
	if d <= 0 {
		return "", fmt.Errorf("window must be positive, got %s", d)
	}
	if d < time.Millisecond {
		return "", fmt.Errorf("window %s is below PromQL's smallest unit (1ms)", d)
	}
	for _, u := range promQLUnits {
		if d%u.size == 0 {
			return strconv.FormatInt(int64(d/u.size), 10) + u.suffix, nil
		}
	}
	return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms", nil
}
