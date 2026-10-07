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
// On the dev/mimir-k8s rig (Mimir 3.2.0) that lag measured a consistent
// ~20s from a config-API write to the rule appearing in the ruler API, so
// in practice a change's window is about as wide as the capture interval.
// The ruler API is still the right reference for cost: it shows what the
// ruler is actually evaluating.
//
// # Unchanged captures
//
// A capture whose definitions are identical to the tenant's latest snapshot
// doesn't store a new snapshot: an unchanged 120-rule tenant would
// otherwise write a ~37 KB copy on every poll. It records a LastSeen
// instead, so the store still knows the definitions were observed unchanged
// up to that capture. Diff uses it to start the next change's window at the
// last capture that saw the old definitions, not at the first one. A failed
// fetch records neither, so it can't read as "unchanged" any more than as
// "every rule removed".
//
// Snapshots also have limits no diff can remove. A change that is made and
// reverted between two snapshots is invisible, and several changes to one
// rule between two snapshots collapse into one. Polling more often narrows
// both the windows and these blind spots.
package timeline

import (
	"bytes"
	"encoding/json"
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
	// LastSeen, if set, is the latest later capture that found exactly
	// these definitions again. It isn't part of the snapshot file: a Store
	// keeps it separately (DirStore in a sidecar file) and attaches it when
	// reading the snapshot back.
	LastSeen *LastSeen `json:"-"`
}

// LastSeen records that captures after a snapshot found the tenant's
// definitions unchanged, so no new snapshot was stored for them. It holds
// the times of the latest such capture. Snapshot identifies the snapshot it
// belongs to by that snapshot's ObservedAt, which is unique per tenant.
type LastSeen struct {
	Version  int       `json:"version"`
	Tenant   string    `json:"tenant"`
	Snapshot time.Time `json:"snapshot_observed_at"`
	// Captures counts every capture that found these definitions, the one
	// that stored the snapshot included, so it is at least 2.
	Captures    int       `json:"captures"`
	RequestedAt time.Time `json:"requested_at"`
	ObservedAt  time.Time `json:"observed_at"`
}

// Validate checks a LastSeen on its own. Whether it fits its snapshot is
// checked by attach.
func (l LastSeen) Validate() error {
	if l.Version != FormatVersion {
		return fmt.Errorf("last-seen format version %d, want %d", l.Version, FormatVersion)
	}
	if err := ValidateTenant(l.Tenant); err != nil {
		return err
	}
	if l.Snapshot.IsZero() || l.RequestedAt.IsZero() || l.ObservedAt.IsZero() {
		return errors.New("last-seen record is missing a time")
	}
	if l.ObservedAt.Before(l.RequestedAt) {
		return fmt.Errorf("last-seen observed_at %s is before requested_at %s", l.ObservedAt.Format(time.RFC3339Nano), l.RequestedAt.Format(time.RFC3339Nano))
	}
	if l.Captures < 2 {
		return fmt.Errorf("last-seen record counts %d capture(s), want at least 2", l.Captures)
	}
	return nil
}

// attach sets l as snap's LastSeen after checking it belongs to snap and
// comes after it.
func (s *Snapshot) attach(l LastSeen) error {
	if l.Tenant != s.Tenant || !l.Snapshot.Equal(s.ObservedAt) {
		return fmt.Errorf("last-seen record is for tenant %s's snapshot observed at %s, not tenant %s's observed at %s",
			l.Tenant, l.Snapshot.Format(time.RFC3339Nano), s.Tenant, s.ObservedAt.Format(time.RFC3339Nano))
	}
	if l.RequestedAt.Before(s.ObservedAt) {
		return fmt.Errorf("tenant %s: last-seen capture requested at %s precedes its snapshot, observed at %s",
			s.Tenant, l.RequestedAt.Format(time.RFC3339Nano), s.ObservedAt.Format(time.RFC3339Nano))
	}
	s.LastSeen = &l
	return nil
}

// lastRequestedAt and lastObservedAt are the times of the latest capture
// that saw these definitions: the LastSeen capture if there is one,
// otherwise the snapshot's own.
func (s Snapshot) lastRequestedAt() time.Time {
	if s.LastSeen != nil {
		return s.LastSeen.RequestedAt
	}
	return s.RequestedAt
}

func (s Snapshot) lastObservedAt() time.Time {
	if s.LastSeen != nil {
		return s.LastSeen.ObservedAt
	}
	return s.ObservedAt
}

// captures is how many captures saw these definitions.
func (s Snapshot) captures() int {
	if s.LastSeen != nil {
		return s.LastSeen.Captures
	}
	return 1
}

// sameDefinitions reports whether a and b would be stored as the same
// snapshot file apart from their times: same tenant, source, and groups and
// rules in the same order. It is stricter than Diff finding no changes
// (Diff ignores order and source), so a reordered or re-sourced rule set
// still gets a snapshot of its own.
func sameDefinitions(a, b Snapshot) (bool, error) {
	strip := func(s Snapshot) ([]byte, error) {
		s.RequestedAt, s.ObservedAt, s.LastSeen = time.Time{}, time.Time{}, nil
		return json.Marshal(s)
	}
	ja, err := strip(a)
	if err != nil {
		return false, err
	}
	jb, err := strip(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ja, jb), nil
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
