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
type RuleExecution struct {
	Tenant           string    `json:"tenant"`
	Namespace        string    `json:"namespace"`
	Group            string    `json:"group"`
	RuleName         string    `json:"rule_name"`
	Timestamp        time.Time `json:"timestamp"`
	DurationSeconds  float64   `json:"duration_seconds"`
	SamplesProcessed uint64    `json:"samples_processed"`
	FetchedSeries    uint64    `json:"fetched_series"`
	FetchedChunks    uint64    `json:"fetched_chunks"`
	FetchedBytes     uint64    `json:"fetched_bytes"`
	QueryText        string    `json:"query_text,omitempty"`
	QueryHash        string    `json:"query_hash,omitempty"`
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
