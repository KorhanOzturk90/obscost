// Package loader defines the Loader interface every rule source implements.
// internal/loader/dir is the only implementation this milestone
// (--dir PATH); future CRD/ruler-API loaders populate the same
// []rule.AnnotatedRule shape.
package loader

import (
	"context"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// LoadError is a per-file/per-rule problem (bad YAML, unparseable PromQL).
// The CLI treats any non-empty LoadError list as a hard failure (exit 1):
// a rule that doesn't parse, or can't be assigned a tenant, can't be
// meaningfully attributed.
type LoadError struct {
	File string
	Err  error
}

func (e LoadError) Error() string {
	return e.File + ": " + e.Err.Error()
}

func (e LoadError) Unwrap() error { return e.Err }

type Loader interface {
	Load(ctx context.Context) ([]rule.AnnotatedRule, []LoadError, error)
}
