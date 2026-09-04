// Package telemetry defines the Source interface every rule-execution
// ingestion adapter implements. Sources only ever parse and normalize raw
// observations into rule.RuleExecution — they never aggregate or interpret
// them (that's internal/attribution's job; see PRODUCT-DIRECTION.md's
// "keep the meter responsible for raw observations" principle).
//
// internal/telemetry/ndjson is the first concrete Source: a portable,
// self-contained newline-delimited JSON file. A real Mimir-log-format
// adapter (e.g. internal/telemetry/mimirlogs) is a natural sibling package
// to add later — this interface shape doesn't need to change for it.
package telemetry

import (
	"context"
	"fmt"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// Source produces raw RuleExecution observations from some origin.
type Source interface {
	Read(ctx context.Context) ([]rule.RuleExecution, []ReadError, error)
}

// ReadError is a per-record problem within a source (bad JSON, missing a
// required field) — mirrors loader.LoadError exactly: the source collects
// and continues past a bad record; the caller decides whether a non-empty
// list is fatal.
type ReadError struct {
	Source string
	Line   int
	Err    error
}

func (e ReadError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.Source, e.Line, e.Err.Error())
}

func (e ReadError) Unwrap() error { return e.Err }
