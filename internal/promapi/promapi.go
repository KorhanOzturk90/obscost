// Package promapi is the small Prometheus-compatible instant-query client
// shared by every source in this repo that reads Mimir's own
// self-monitoring metrics over its PromQL HTTP API: the rule-workload
// source (internal/telemetry/mimirmetrics) and the cost-driver source
// (internal/cost/mimirdrivers).
//
// It deliberately knows nothing about what is being queried. The one piece
// of Mimir-specific behaviour it carries is the tenancy header, because
// every query against Mimir runs *as* some tenant — see
// mimirmetrics' package doc for why that tenant is usually not one being
// reported on.
package promapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config configures a Client. It mirrors rulerapi.Config, since both talk
// to the same Mimir HTTP API with the same tenancy and auth mechanics.
type Config struct {
	BaseURL     string // Mimir's HTTP API address, e.g. http://localhost:8080
	Header      string // tenancy header name; defaults to X-Scope-OrgID
	Tenant      string // the value sent in Header — the tenant the query runs as
	BearerToken string // optional; sent as "Authorization: Bearer <token>" if set
	Timeout     time.Duration
	HTTPClient  *http.Client // optional; a default client with Timeout is used if nil
}

type Client struct {
	cfg    Config
	client *http.Client
}

// New builds a Client. The default timeout is 30s rather than rulerapi's
// 10s: listing rule definitions is a cheap metadata read, whereas a
// *_over_time or increase() over a 7d or 30d window fans out across every
// matching series in the tenant and is a genuinely heavy query on a large
// cluster.
func New(cfg Config) *Client {
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
	return &Client{cfg: cfg, client: client}
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
		ResultType string   `json:"resultType"`
		Result     []Sample `json:"result"`
	} `json:"data"`
}

// Sample is one element of a vector result. `value` is a two-element
// heterogeneous array — [<unix seconds as a JSON number>, "<the sample
// value as a JSON *string*>"] — which is why it is held as raw messages
// rather than a typed pair. The value is a string because Prometheus needs
// to round-trip NaN and ±Inf, which JSON numbers cannot express.
type Sample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

// Float returns the sample's value, reporting ok=false for anything a
// caller cannot use: a malformed pair, an unparseable string, or a
// non-finite value (NaN, ±Inf). Non-finite values are refused rather than
// passed through because every caller either sums or sorts these, and
// both go quietly wrong on NaN.
func (s Sample) Float() (float64, bool) {
	if len(s.Value) != 2 {
		return 0, false
	}
	var raw string
	if err := json.Unmarshal(s.Value[1], &raw); err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// Instant runs one instant query at the server's current time.
func (c *Client) Instant(ctx context.Context, promql string) ([]Sample, error) {
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/prometheus/api/v1/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = url.Values{"query": []string{promql}}.Encode()
	req.Header.Set(c.cfg.Header, c.cfg.Tenant)
	if c.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
	}

	resp, err := c.client.Do(req)
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

// durationUnits is the ladder Duration renders against, largest first.
// It stops at "d" on purpose. PromQL also accepts "w" and "y", but "y" is
// a flat 365 days that quietly disagrees with a calendar year, and a
// window the operator asked for as 7d should come back as 7d, not 1w.
var durationUnits = []struct {
	suffix string
	size   time.Duration
}{
	{"d", 24 * time.Hour},
	{"h", time.Hour},
	{"m", time.Minute},
	{"s", time.Second},
	{"ms", time.Millisecond},
}

// Duration renders a Go duration as a PromQL range string.
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
func Duration(d time.Duration) (string, error) {
	if d <= 0 {
		return "", fmt.Errorf("window must be positive, got %s", d)
	}
	if d < time.Millisecond {
		return "", fmt.Errorf("window %s is below PromQL's smallest unit (1ms)", d)
	}
	for _, u := range durationUnits {
		if d%u.size == 0 {
			return strconv.FormatInt(int64(d/u.size), 10) + u.suffix, nil
		}
	}
	return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms", nil
}
