package rule

import (
	"encoding/json"
	"fmt"
)

// Severity orders as info < warn < error so numeric comparison works
// directly for --fail-on threshold checks.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarn:
		return "warn"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseSeverity parses "info", "warn", or "error".
func ParseSeverity(s string) (Severity, error) {
	switch s {
	case "info":
		return SeverityInfo, nil
	case "warn":
		return SeverityWarn, nil
	case "error":
		return SeverityError, nil
	default:
		return 0, fmt.Errorf("unknown severity %q, want info|warn|error", s)
	}
}

func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *Severity) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	parsed, err := ParseSeverity(str)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// Finding is what every check emits. Every finding carries a check ID,
// severity, tenant, source location, the numbers behind it, and where
// possible a remediation.
type Finding struct {
	CheckID     string         `json:"check_id"`
	Severity    Severity       `json:"severity"`
	Tenant      string         `json:"tenant,omitempty"`
	Location    SourceLocation `json:"location"`
	Message     string         `json:"message"`
	Values      map[string]any `json:"values,omitempty"`
	Remediation string         `json:"remediation,omitempty"`
	// Assumptions carries the estimate's assumption trail, e.g.
	// "N=count(sel) @ scan time; sps=median of 5". Reserved for live-tier
	// checks; static-tier checks need no measured assumptions.
	Assumptions string `json:"assumptions,omitempty"`
}
