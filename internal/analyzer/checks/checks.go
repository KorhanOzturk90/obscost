// Package checks implements the v0 static-tier checks (PC-S01..PC-S06).
package checks

import "github.com/KorhanOzturk90/obscost/internal/analyzer"

// All returns every static-tier check this milestone implements, in check
// ID order.
func All() []analyzer.Check {
	return []analyzer.Check{
		PCS01{},
		PCS02{},
		PCS03{},
		PCS04{},
		PCS05{},
		PCS06{},
	}
}
