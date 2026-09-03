package meter

import (
	"context"
	"net/http"
)

type tenantKey struct{}

// withTenant attaches the tenant a Meter call is scoped to so authRoundTripper
// can read it back per-request. Meter methods take tenant as an argument
// (not at construction), since one Meter serves every tenant a check run
// touches.
func withTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

func tenantFromContext(ctx context.Context) (string, bool) {
	tenant, ok := ctx.Value(tenantKey{}).(string)
	return tenant, ok && tenant != ""
}

// authRoundTripper attaches the backend's configured credentials and the
// tenancy header to every outgoing request. At most one auth scheme is
// active: basic wins over bearer if both were somehow configured (see
// config.BackendAuth's doc comment).
type authRoundTripper struct {
	base http.RoundTripper

	username    string
	password    string
	bearerToken string

	// tenancyHeader is the header name to attach the per-call tenant under
	// (e.g. "X-Scope-OrgID"). Empty means don't attach a tenant header at
	// all, which fits a single-tenant test stack.
	tenancyHeader string
}

func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	switch {
	case rt.username != "" || rt.password != "":
		req.SetBasicAuth(rt.username, rt.password)
	case rt.bearerToken != "":
		req.Header.Set("Authorization", "Bearer "+rt.bearerToken)
	}

	if rt.tenancyHeader != "" {
		if tenant, ok := tenantFromContext(req.Context()); ok {
			req.Header.Set(rt.tenancyHeader, tenant)
		}
	}

	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
