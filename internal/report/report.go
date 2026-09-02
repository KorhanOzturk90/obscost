// Package report renders analyzer findings for humans (md) and machines
// (json). Spec §7: md is designed to be pasted into a tenant's ticket
// verbatim; json is a stable schema for CI annotation and future UI.
package report

import (
	"fmt"
	"io"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

type Format string

const (
	FormatMD   Format = "md"
	FormatJSON Format = "json"
)

// Result is everything a Reporter needs to render one check run.
type Result struct {
	Findings     []rule.Finding `json:"findings"`
	RulesScanned int            `json:"rules_scanned"`
	TenantsSeen  []string       `json:"tenants_seen,omitempty"`
	GeneratedAt  time.Time      `json:"generated_at"`
}

type Reporter interface {
	Render(w io.Writer, result Result) error
}

func New(format Format) (Reporter, error) {
	switch format {
	case FormatMD, "":
		return mdReporter{}, nil
	case FormatJSON:
		return jsonReporter{}, nil
	default:
		return nil, fmt.Errorf("unknown report format %q", format)
	}
}
