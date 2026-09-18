package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mutableRuler serves /prometheus/api/v1/rules from a per-tenant body that
// the test can swap between captures, standing in for rules being changed
// in Mimir by any means.
type mutableRuler struct {
	mu     sync.Mutex
	bodies map[string]string
}

func (m *mutableRuler) set(tenant, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bodies[tenant] = body
}

func (m *mutableRuler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	body, ok := m.bodies[r.Header.Get("X-Scope-OrgID")]
	m.mu.Unlock()
	if r.URL.Path != "/prometheus/api/v1/rules" || !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"error","error":"no such tenant"}`))
		return
	}
	_, _ = w.Write([]byte(body))
}

func rulesBody(expr string) string {
	return `{"status":"success","data":{"groups":[{"name":"api","file":"rules.yaml","interval":60,
		"rules":[{"name":"job:req:rate5m","query":"` + expr + `","type":"recording","health":"ok"}]}]}}`
}

func TestTimeline_CaptureThenDiff(t *testing.T) {
	ruler := &mutableRuler{bodies: map[string]string{
		"team-a": rulesBody("sum(rate(req_total[5m]))"),
		"team-b": `{"status":"success","data":{"groups":[]}}`,
	}}
	srv := httptest.NewServer(ruler)
	defer srv.Close()
	cfg := writeReportConfig(t, srv.URL)
	snapDir := filepath.Join(t.TempDir(), "snapshots")

	stdout, stderr, code := run("timeline", "capture", "--tenant", "team-a,team-b", "--snapshot-dir", snapDir, "--config", cfg)
	if code != 0 {
		t.Fatalf("first capture: exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "tenant team-a: 1 rule(s) in 1 group(s), 0 change(s)") ||
		!strings.Contains(stdout, "tenant team-b: 0 rule(s) in 0 group(s), 0 change(s)") {
		t.Errorf("first capture stdout:\n%s", stdout)
	}

	ruler.set("team-a", rulesBody("sum by (job) (rate(req_total[5m]))"))
	stdout, stderr, code = run("timeline", "capture", "--tenant", "team-a", "--snapshot-dir", snapDir, "--config", cfg)
	if code != 0 {
		t.Fatalf("second capture: exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "tenant team-a: 1 rule(s) in 1 group(s), 1 change(s)") {
		t.Errorf("second capture stdout:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(snapDir, "team-a", "changes.ndjson")); err != nil {
		t.Errorf("changes.ndjson not written: %v", err)
	}

	stdout, stderr, code = run("timeline", "diff", "--snapshot-dir", snapDir, "--format", "json")
	if code != 0 {
		t.Fatalf("diff: exit %d, stderr=%s", code, stderr)
	}
	var tl struct {
		Tenants []struct {
			Tenant    string `json:"tenant"`
			Snapshots int    `json:"snapshots"`
			Changes   []struct {
				Type   string   `json:"type"`
				Fields []string `json:"fields"`
				Rule   struct {
					Tenant, Namespace, Group, Name string
				} `json:"rule"`
				Window struct {
					After    time.Time `json:"after"`
					NotAfter time.Time `json:"not_after"`
				} `json:"window"`
			} `json:"changes"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal([]byte(stdout), &tl); err != nil {
		t.Fatalf("diff --format json is not JSON: %v\n%s", err, stdout)
	}
	if len(tl.Tenants) != 2 {
		t.Fatalf("tenants = %+v, want team-a and team-b", tl.Tenants)
	}
	a := tl.Tenants[0]
	if a.Tenant != "team-a" || a.Snapshots != 2 || len(a.Changes) != 1 {
		t.Fatalf("team-a = %+v, want 2 snapshots and 1 change", a)
	}
	c := a.Changes[0]
	if c.Type != "changed" || len(c.Fields) != 1 || c.Fields[0] != "expr" || c.Rule.Name != "job:req:rate5m" || c.Rule.Namespace != "rules.yaml" {
		t.Errorf("change = %+v, want an expr change to rules.yaml/api/job:req:rate5m", c)
	}
	if c.Window.After.IsZero() || !c.Window.After.Before(c.Window.NotAfter) {
		t.Errorf("window = %+v, want a non-empty interval", c.Window)
	}

	// The same two snapshots given as files produce the same change.
	files, _ := filepath.Glob(filepath.Join(snapDir, "team-a", "*.json"))
	if len(files) != 2 {
		t.Fatalf("team-a snapshot files = %v, want 2", files)
	}
	stdout, stderr, code = run(append([]string{"timeline", "diff"}, files...)...)
	if code != 0 {
		t.Fatalf("diff files: exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "| changed | rules.yaml / api / job:req:rate5m | expr |") {
		t.Errorf("markdown diff:\n%s", stdout)
	}
}

func TestTimeline_CaptureFailedTenantWritesNothing(t *testing.T) {
	ruler := &mutableRuler{bodies: map[string]string{"team-a": rulesBody("up")}}
	srv := httptest.NewServer(ruler)
	defer srv.Close()
	cfg := writeReportConfig(t, srv.URL)
	snapDir := t.TempDir()

	stdout, stderr, code := run("timeline", "capture", "--tenant", "team-a,ghost", "--snapshot-dir", snapDir, "--config", cfg)
	if code == 0 {
		t.Fatalf("exit 0 with a failing tenant; stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "tenant ghost:") {
		t.Errorf("stderr does not name the failed tenant:\n%s", stderr)
	}
	if !strings.Contains(stdout, "tenant team-a:") {
		t.Errorf("the healthy tenant was not captured:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(snapDir, "ghost")); !os.IsNotExist(err) {
		t.Errorf("a snapshot directory exists for the failed tenant (err=%v)", err)
	}
}

func TestTimeline_Usage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"capture needs a backend", []string{"timeline", "capture", "--tenant", "a", "--snapshot-dir", "x"}, "backend.url"},
		{"capture needs tenants", []string{"timeline", "capture", "--snapshot-dir", "x"}, "tenant"},
		{"diff needs input", []string{"timeline", "diff"}, "--snapshot-dir"},
		{"diff rejects both inputs", []string{"timeline", "diff", "--snapshot-dir", "x", "a.json"}, "not both"},
		{"diff rejects unknown format", []string{"timeline", "diff", "--format", "html", "a.json"}, "--format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := run(tc.args...)
			if code == 0 || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stderr=%q; want failure mentioning %q", code, stderr, tc.want)
			}
		})
	}
}
