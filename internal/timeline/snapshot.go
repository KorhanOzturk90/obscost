// Package timeline records when rule definitions changed in Mimir, by
// diffing successive snapshots of the ruler API (ADR 0004 decision 6).
//
// Nothing else records this. The ruler API shows only the rules loaded now,
// and git shows only what the files say, not when a definition reached the
// ruler (ADR 0002 decision 3). Snapshotting the ruler API sees every change
// however it was deployed: PrometheusRule CRDs, `mimirtool rules sync`, or
// direct API calls.
//
// The package has three parts:
//
//   - Snapshot: one tenant's rule definitions as the ruler API reported them,
//     with the time window in which they were observed.
//   - Diff and Build: pure functions from snapshots to typed Change events.
//   - Store and DirStore: where snapshots and their changes are kept. DirStore
//     writes JSON files under a local directory. A sink (issue #32) can
//     implement Store later.
//
// # What a change's timestamp means
//
// A snapshot is taken over a request, not at an instant: the ruler's answer
// reflects its state at some moment between RequestedAt and ObservedAt.
// Diffing two snapshots therefore can't date a change to a point. It can
// only say the change became visible in the ruler API after the previous
// request started and no later than the current response arrived, which is
// what Window holds. Both bounds come from the clock of the machine running
// promcost, not Mimir's.
//
// "Visible in the ruler API" is not quite "written to the rule store".
// Rulers pick up rule-store changes on their own poll loop
// (-ruler.poll-interval), so the ruler API can lag a write by that much.
// The ruler API is still the right reference for cost: it shows what the
// ruler is actually evaluating.
//
// Snapshots also have limits no diff can remove. A change that is made and
// reverted between two snapshots is invisible, and several changes to one
// rule between two snapshots collapse into one. Polling more often narrows
// both the windows and these blind spots.
package timeline

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// FormatVersion is the snapshot file format version. Bump it on any
// incompatible change to Snapshot's JSON encoding.
const FormatVersion = 1

// Snapshot is one tenant's rule definitions as the ruler API reported them.
// Groups and rules keep the order the API returned them in. Diff doesn't
// depend on that order.
type Snapshot struct {
	Version int    `json:"version"`
	Tenant  string `json:"tenant"`
	// Source is the Mimir base URL the snapshot was fetched from, with any
	// userinfo removed. It is informational: Diff doesn't compare it, so a
	// changed hostname for the same cluster doesn't break a timeline.
	Source string `json:"source,omitempty"`
	// RequestedAt is when the request to the ruler API was sent, and
	// ObservedAt when its response had been read. The ruler's answer
	// reflects its state at some moment in between.
	RequestedAt time.Time `json:"requested_at"`
	ObservedAt  time.Time `json:"observed_at"`
	Groups      []Group   `json:"groups"`
}

// Group is one rule group's definition. Namespace is the ruler API's "file"
// field, which is what rule.RuleID calls Namespace.
type Group struct {
	Namespace       string  `json:"namespace"`
	Name            string  `json:"name"`
	IntervalSeconds float64 `json:"interval_seconds"`
	Rules           []Rule  `json:"rules"`
}

// Rule is one rule's definition within a group. Kind is kept as the string
// the ruler API returned ("recording" or "alerting"), so a snapshot records
// what Mimir said even if a future Mimir adds a kind promcost doesn't know.
type Rule struct {
	Kind                 string            `json:"kind"`
	Name                 string            `json:"name"`
	Expr                 string            `json:"expr"`
	ForSeconds           float64           `json:"for_seconds,omitempty"`
	KeepFiringForSeconds float64           `json:"keep_firing_for_seconds,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	Annotations          map[string]string `json:"annotations,omitempty"`
}

// Validate checks the fields Diff and DirStore rely on.
func (s Snapshot) Validate() error {
	if s.Version != FormatVersion {
		return fmt.Errorf("snapshot format version %d, want %d", s.Version, FormatVersion)
	}
	if err := ValidateTenant(s.Tenant); err != nil {
		return err
	}
	if s.RequestedAt.IsZero() || s.ObservedAt.IsZero() {
		return errors.New("snapshot has no requested_at/observed_at time")
	}
	if s.ObservedAt.Before(s.RequestedAt) {
		return fmt.Errorf("snapshot observed_at %s is before requested_at %s", s.ObservedAt.Format(time.RFC3339Nano), s.RequestedAt.Format(time.RFC3339Nano))
	}
	return nil
}

// ValidateTenant rejects tenant IDs that can't safely name a directory.
// Mimir's own tenant ID rules already forbid all of these.
func ValidateTenant(tenant string) error {
	switch {
	case tenant == "":
		return errors.New("empty tenant ID")
	case tenant == "." || tenant == "..":
		return fmt.Errorf("invalid tenant ID %q", tenant)
	case strings.ContainsAny(tenant, "/\\\x00"):
		return fmt.Errorf("invalid tenant ID %q: contains a path separator or NUL", tenant)
	}
	return nil
}

// redactURL removes userinfo from a base URL, so a snapshot file never
// stores credentials that were embedded in backend.url. A URL that doesn't
// parse is dropped rather than stored as-is, for the same reason.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return u.String()
}
