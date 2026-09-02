package analyzer

// Registry holds the set of checks a run considers.
type Registry struct {
	checks []Check
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(c Check) {
	r.checks = append(r.checks, c)
}

// Enabled returns the registered checks whose ID is not in disabled.
func (r *Registry) Enabled(disabled []string) []Check {
	skip := make(map[string]struct{}, len(disabled))
	for _, id := range disabled {
		skip[id] = struct{}{}
	}
	out := make([]Check, 0, len(r.checks))
	for _, c := range r.checks {
		if _, ok := skip[c.ID()]; ok {
			continue
		}
		out = append(out, c)
	}
	return out
}
