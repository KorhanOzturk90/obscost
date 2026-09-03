// Package analyzer runs the registered Checks over a loaded rule set. It is
// pure: all network I/O a check might need goes through CheckContext.Meter,
// never through the analyzer itself.
package analyzer

import (
	"context"

	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/limits"
	"github.com/KorhanOzturk90/obscost/internal/meter"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// Tier discriminates checks by what they're allowed to touch.
type Tier int

const (
	TierStatic Tier = iota
	TierLive
	TierFleet
)

func (t Tier) String() string {
	switch t {
	case TierStatic:
		return "static"
	case TierLive:
		return "live"
	case TierFleet:
		return "fleet"
	default:
		return "unknown"
	}
}

// CheckContext bundles everything a check may read. Limits and Meter may be
// nil — Meter is nil under --offline, when no backend.url is configured, or
// when the configured backend was unreachable and check degraded rather
// than failing (see internal/cli/check.go's buildMeter). No PC-L0x check is
// registered yet regardless (checks.All() is still static-tier only), so
// checks must treat a nil Meter as "no live data available" rather than
// panic.
type CheckContext struct {
	Config config.Config
	Limits limits.Provider
	Meter  meter.Meter
}

// Check receives the whole loaded rule set, not one rule at a time. This is
// required by checks that reason across rules (e.g. PC-S05's per-group rule
// counts, future PC-F04 cross-tenant duplicate detection) — giving every
// check this shape means static, live, and fleet tiers share one interface
// with no rework later. Single-rule checks simply iterate internally.
type Check interface {
	ID() string
	Tier() Tier
	Run(ctx context.Context, rules []rule.AnnotatedRule, cc CheckContext) ([]rule.Finding, error)
}
