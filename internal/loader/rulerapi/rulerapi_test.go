package rulerapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// realResponseFixture is a trimmed but byte-shape-accurate copy of what
// GET /prometheus/api/v1/rules actually returned from a live mimir-3.2.0
// instance (X-Scope-OrgID: infra) — not invented.
const realResponseFixture = `{
  "status": "success",
  "data": {
    "groups": [
      {
        "name": "alertmanager_alerts",
        "file": "alerts.yaml",
        "interval": 60,
        "rules": [
          {
            "state": "inactive",
            "name": "MimirAlertmanagerSyncConfigsFailing",
            "query": "rate(cortex_alertmanager_sync_configs_failed_total[5m]) > 0",
            "duration": 1800,
            "labels": {"severity": "critical"},
            "health": "ok",
            "type": "alerting"
          }
        ]
      },
      {
        "name": "mimir_api_1",
        "file": "recording-rules.yaml",
        "interval": 60,
        "rules": [
          {
            "name": "cluster_job_pod:cortex_alertmanager_alerts:sum",
            "query": "sum by (cluster, job, pod) (cortex_alertmanager_alerts)",
            "labels": {},
            "health": "ok",
            "type": "recording"
          }
        ]
      }
    ]
  }
}`

func newMockRulerServer(t *testing.T, byTenant map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/rules" {
			http.NotFound(w, r)
			return
		}
		tenant := r.Header.Get("X-Scope-OrgID")
		body, ok := byTenant[tenant]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":"error","error":"no org id or unknown tenant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestLoad_RealResponseShape(t *testing.T) {
	srv := newMockRulerServer(t, map[string]string{"infra": realResponseFixture})
	defer srv.Close()

	l := New(Config{BaseURL: srv.URL, Tenants: []string{"infra"}})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) != 0 {
		t.Fatalf("loadErrs = %+v, want none", loadErrs)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(rules))
	}

	alert := rules[0]
	if alert.Kind.String() != "alerting" || alert.Alert != "MimirAlertmanagerSyncConfigsFailing" {
		t.Errorf("rules[0] = %+v, want the alerting rule", alert)
	}
	if alert.Location.File != "alerts.yaml" || alert.Location.Group != "alertmanager_alerts" {
		t.Errorf("rules[0].Location = %+v, want file=alerts.yaml group=alertmanager_alerts", alert.Location)
	}
	if alert.Tenant != "infra" {
		t.Errorf("rules[0].Tenant = %q, want infra", alert.Tenant)
	}
	if alert.AST == nil {
		t.Error("rules[0].AST is nil, want a parsed expression")
	}

	rec := rules[1]
	if rec.Kind.String() != "recording" || rec.Record != "cluster_job_pod:cortex_alertmanager_alerts:sum" {
		t.Errorf("rules[1] = %+v, want the recording rule", rec)
	}
	if rec.Group.Interval.Seconds() != 60 {
		t.Errorf("rules[1].Group.Interval = %v, want 60s", rec.Group.Interval)
	}
}

func TestLoad_MultipleTenants(t *testing.T) {
	tenantB := `{"status":"success","data":{"groups":[{"name":"g","file":"f.yaml","interval":30,
		"rules":[{"name":"other_rule","query":"up","type":"recording"}]}]}}`
	srv := newMockRulerServer(t, map[string]string{
		"infra":   realResponseFixture,
		"tenantb": tenantB,
	})
	defer srv.Close()

	l := New(Config{BaseURL: srv.URL, Tenants: []string{"infra", "tenantb"}})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) != 0 {
		t.Fatalf("loadErrs = %+v, want none", loadErrs)
	}
	if len(rules) != 3 {
		t.Fatalf("len(rules) = %d, want 3 (2 from infra + 1 from tenantb)", len(rules))
	}
	tenants := map[string]bool{}
	for _, r := range rules {
		tenants[r.Tenant] = true
	}
	if !tenants["infra"] || !tenants["tenantb"] {
		t.Errorf("tenants seen = %v, want both infra and tenantb", tenants)
	}
}

func TestLoad_TenantWithNoRuleGroups(t *testing.T) {
	empty := `{"status":"success","data":{"groups":[]}}`
	srv := newMockRulerServer(t, map[string]string{"empty-tenant": empty})
	defer srv.Close()

	l := New(Config{BaseURL: srv.URL, Tenants: []string{"empty-tenant"}})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) != 0 {
		t.Fatalf("loadErrs = %+v, want none — zero rule groups is not an error", loadErrs)
	}
	if len(rules) != 0 {
		t.Fatalf("len(rules) = %d, want 0", len(rules))
	}
}

func TestLoad_OneTenantFailureDoesNotBlockOthers(t *testing.T) {
	srv := newMockRulerServer(t, map[string]string{"infra": realResponseFixture})
	defer srv.Close()

	l := New(Config{BaseURL: srv.URL, Tenants: []string{"infra", "does-not-exist"}})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2 (from the tenant that succeeded)", len(rules))
	}
	if len(loadErrs) != 1 {
		t.Fatalf("loadErrs = %+v, want exactly 1 (for the failing tenant)", loadErrs)
	}
	if !strings.Contains(loadErrs[0].File, "does-not-exist") {
		t.Errorf("LoadError.File = %q, want it to identify the failing tenant", loadErrs[0].File)
	}
}

func TestLoad_TenantHeaderIsConfigurable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Tenant") != "infra" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(realResponseFixture))
	}))
	defer srv.Close()

	l := New(Config{BaseURL: srv.URL, Header: "X-Custom-Tenant", Tenants: []string{"infra"}})
	rules, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) != 0 || len(rules) != 2 {
		t.Fatalf("rules=%d loadErrs=%+v, want 2 rules and no errors", len(rules), loadErrs)
	}
}

func TestLoad_BearerTokenSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(realResponseFixture))
	}))
	defer srv.Close()

	l := New(Config{BaseURL: srv.URL, Tenants: []string{"infra"}, BearerToken: "secret-token"})
	_, loadErrs, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadErrs) != 0 {
		t.Fatalf("loadErrs = %+v, want none (bearer token should have been accepted)", loadErrs)
	}
}
