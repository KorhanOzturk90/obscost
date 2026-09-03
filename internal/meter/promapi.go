package meter

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"

	"github.com/KorhanOzturk90/obscost/internal/config"
)

// promAPIMeter is the real Meter implementation (spec §5): a thin wrapper
// over a Prometheus-compatible instant-query HTTP API (Mimir's query
// frontend, including Grafana Cloud's hosted Mimir). It only ever issues
// the four cheap metering primitives the Meter interface documents — never
// a range query, and never a tenant's own expression (design rule #2).
type promAPIMeter struct {
	api            v1.API
	timeout        time.Duration
	presenceWindow time.Duration
}

// New builds a Meter for backend. tenancyHeader is the header name attached
// per-request from the tenant argument each Meter method receives (empty
// means don't attach one, which fits a single-tenant test stack such as
// Grafana Cloud's free tier). presenceWindow is W in the interface's
// SeriesCount/GroupedCount formulas (spec's presence_window, default 1h) —
// it exists so cold series aren't missed by a bare instant lookback.
//
// Auth is read once here from the env vars backend.Auth names, never
// inline from YAML: basic auth (username_env+password_env) wins
// deterministically if both schemes are configured, matching
// config.BackendAuth's doc comment.
func New(backend config.BackendConfig, tenancyHeader string, presenceWindow time.Duration) (Meter, error) {
	if backend.URL == "" {
		return nil, fmt.Errorf("meter: backend.url is empty")
	}

	rt := &authRoundTripper{
		base:          api.DefaultRoundTripper,
		tenancyHeader: tenancyHeader,
	}
	switch {
	case backend.Auth.UsernameEnv != "" && backend.Auth.PasswordEnv != "":
		rt.username = os.Getenv(backend.Auth.UsernameEnv)
		rt.password = os.Getenv(backend.Auth.PasswordEnv)
	case backend.Auth.BearerTokenEnv != "":
		rt.bearerToken = os.Getenv(backend.Auth.BearerTokenEnv)
	}

	client, err := api.NewClient(api.Config{
		Address:      backend.URL,
		RoundTripper: rt,
	})
	if err != nil {
		return nil, fmt.Errorf("meter: new client: %w", err)
	}

	timeout := backend.Timeout.Duration()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &promAPIMeter{
		api:            v1.NewAPI(client),
		timeout:        timeout,
		presenceWindow: presenceWindow,
	}, nil
}

// Probe issues one cheap, fixed instant query ("vector(1)", which needs no
// tenant data to exist) to verify the backend is reachable and credentials
// are accepted. tenant may be "" — an empty tenant sends no tenancy header
// at all (see authRoundTripper), which is enough to validate connectivity
// and auth against a single-tenant stack; a backend that hard-requires a
// tenant header for every request will surface that as a probe failure too,
// which is a legitimate exit-code-3 signal.
//
// Used by check's --strict/exit-code-3 wiring at startup, since no PC-L0x
// check exists yet to exercise the Meter organically.
func (m *promAPIMeter) Probe(ctx context.Context, tenant string) error {
	ctx, cancel := context.WithTimeout(withTenant(ctx, tenant), m.timeout)
	defer cancel()

	_, _, err := m.api.Query(ctx, "vector(1)", time.Now())
	if err != nil {
		return fmt.Errorf("meter: probe query: %w", err)
	}
	return nil
}
