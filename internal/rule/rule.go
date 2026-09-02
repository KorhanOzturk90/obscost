// Package rule defines the shared vocabulary every other promcost package
// builds on: the annotated rule representation loaders produce and the
// analyzer consumes, and the Finding type every check emits.
package rule

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// Kind distinguishes a recording rule from an alerting rule.
type Kind int

const (
	KindRecording Kind = iota
	KindAlerting
)

func (k Kind) String() string {
	switch k {
	case KindRecording:
		return "recording"
	case KindAlerting:
		return "alerting"
	default:
		return "unknown"
	}
}

// SourceLocation identifies where a rule came from. File is a loader-defined
// opaque string: a filesystem path for directory loading today, a
// tenant/namespace/crdname reference for future CRD/ruler-API loaders.
type SourceLocation struct {
	File  string `json:"file,omitempty"`
	Group string `json:"group,omitempty"`
	Rule  string `json:"rule,omitempty"`
	Line  int    `json:"line,omitempty"`
}

func (l SourceLocation) String() string {
	if l.Line > 0 {
		return fmt.Sprintf("%s:%s:%s:%d", l.File, l.Group, l.Rule, l.Line)
	}
	return fmt.Sprintf("%s:%s:%s", l.File, l.Group, l.Rule)
}

// Rule is a single recording or alerting rule as read from its source,
// before AST parsing or tenant/location annotation.
type Rule struct {
	Kind        Kind
	Record      string
	Alert       string
	Expr        string
	For         time.Duration
	Labels      map[string]string
	Annotations map[string]string
}

// Name returns the rule's record or alert name, whichever is set.
func (r Rule) Name() string {
	if r.Kind == KindRecording {
		return r.Record
	}
	return r.Alert
}

// RuleGroupMeta carries the rule-group-level metadata a rule was loaded
// from, needed by checks that reason about evaluation cadence.
type RuleGroupMeta struct {
	Name     string
	Interval time.Duration
}

// AnnotatedRule is what every Loader hands the analyzer: the same shape
// regardless of source (a directory today; CRD/ruler-API sources later).
type AnnotatedRule struct {
	Rule
	AST      parser.Expr
	Group    RuleGroupMeta
	Tenant   string
	Location SourceLocation
}
