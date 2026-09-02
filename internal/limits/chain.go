package limits

import (
	"context"
	"errors"
	"fmt"

	"github.com/KorhanOzturk90/obscost/internal/config"
)

// ErrUnsupportedSource is returned when a configured limits source type has
// no implementation yet (only "file" is implemented this milestone).
var ErrUnsupportedSource = errors.New("limits source type not implemented in this milestone")

// NewChainProvider tries each configured source in order, returning the
// first that loads successfully (spec §3: "sources tried in order; first
// success wins"). With no sources configured, it returns an empty provider
// rather than an error — checks.disable/promcost.yaml files that don't
// touch limits at all must still work.
func NewChainProvider(ctx context.Context, sources []config.LimitsSource) (Provider, error) {
	if len(sources) == 0 {
		return &mapProvider{tenants: map[string]Tenant{}}, nil
	}

	var errs []error
	for _, src := range sources {
		s, err := newSource(src)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, err := s.Load(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return p, nil
	}
	return nil, fmt.Errorf("no limits source could be loaded: %w", errors.Join(errs...))
}

func newSource(cfg config.LimitsSource) (Source, error) {
	switch cfg.Type {
	case "file":
		return newFileSource(cfg), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedSource, cfg.Type)
	}
}
