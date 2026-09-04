package rule

import (
	"fmt"
	"time"
)

// RuleID is the stable join key between a rule's static definition
// (AnnotatedRule) and its runtime execution observations (RuleExecution).
// It is a plain comparable struct (no slices/maps) so it works directly as
// a Go map key.
type RuleID struct {
	Tenant    string `json:"tenant"`
	Namespace string `json:"namespace"`
	Group     string `json:"group"`
	Name      string `json:"name"`
}

func (id RuleID) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", id.Tenant, id.Namespace, id.Group, id.Name)
}

// RuleExecution is one raw, uninterpreted observation of a rule actually
// running — the runtime counterpart to AnnotatedRule's static definition.
// A telemetry.Source populates whatever fields it can measure; sources
// never aggregate or interpret these records, that's internal/attribution's
// job. Flat fields, no nested RuleID, matching PRODUCT-DIRECTION.md's
// metering-model sketch, plus QueryText/QueryHash: docs/runtime-telemetry-
// notes.md calls for retaining both when a source can provide them, for
// future explanation/correlation use.
//
// The five workload-stat fields are pointers, not bare numbers: a nil
// pointer means "this source didn't measure this stat," which is a
// different, non-zero fact. This matters concretely for Mimir's own ruler
// query-stats log (internal/telemetry/mimirlogs) — when the ruler delegates
// to a remote query-frontend rather than evaluating locally, that log line
// carries no fetched-series/chunk/byte or wall-time fields at all. Treating
// an absent stat as a real 0 would silently understate every remote-
// evaluated rule's workload. internal/attribution sums only the non-nil
// observations it sees; it never invents a 0 for a stat a source didn't
// report.
type RuleExecution struct {
	Tenant           string    `json:"tenant"`
	Namespace        string    `json:"namespace"`
	Group            string    `json:"group"`
	RuleName         string    `json:"rule_name"`
	Timestamp        time.Time `json:"timestamp"`
	DurationSeconds  *float64  `json:"duration_seconds,omitempty"`
	SamplesProcessed *uint64   `json:"samples_processed,omitempty"`
	FetchedSeries    *uint64   `json:"fetched_series,omitempty"`
	FetchedChunks    *uint64   `json:"fetched_chunks,omitempty"`
	FetchedBytes     *uint64   `json:"fetched_bytes,omitempty"`
	QueryText        string    `json:"query_text,omitempty"`
	QueryHash        string    `json:"query_hash,omitempty"`
}

// Ptr returns a pointer to v — a small ergonomic helper for constructing
// RuleExecution's optional stat fields (e.g. rule.Ptr(uint64(1200))),
// needed by every telemetry.Source implementation and by tests.
func Ptr[T any](v T) *T {
	return &v
}

// RuleID derives this execution's join key from its own flat fields.
func (e RuleExecution) RuleID() RuleID {
	return RuleID{
		Tenant:    e.Tenant,
		Namespace: e.Namespace,
		Group:     e.Group,
		Name:      e.RuleName,
	}
}

// RuleID derives a loaded rule's join key: Tenant, Location.File as
// Namespace, Group.Name, Name(). Location.File is the loader's full
// forward-slash-normalized relative path (e.g. "team-payments/rules.yaml"
// for directory-mode loading) — NOT the tenancy-discovery first-path-
// segment convention internal/loader/dir uses privately to resolve a
// tenant. A telemetry source's "namespace" field must string-match this
// exactly (case-sensitive, no fuzzy matching) or the execution lands in
// attribution's unmatched bucket instead of joining.
func (a AnnotatedRule) RuleID() RuleID {
	return RuleID{
		Tenant:    a.Tenant,
		Namespace: a.Location.File,
		Group:     a.Group.Name,
		Name:      a.Name(),
	}
}
