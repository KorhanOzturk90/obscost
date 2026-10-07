package timeline

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/loader/rulerapi"
)

// rulerFixture has the shape of a live mimir-3.2.0 /prometheus/api/v1/rules
// response (see internal/loader/rulerapi's realResponseFixture), including
// the runtime-only fields a snapshot must not record.
const rulerFixture = `{
  "status": "success",
  "data": {
    "groups": [
      {
        "name": "alertmanager_alerts",
        "file": "alerts.yaml",
        "interval": 60,
        "evaluationTime": 0.0012,
        "lastEvaluation": "2026-09-19T03:00:00Z",
        "rules": [
          {
            "state": "inactive",
            "name": "MimirAlertmanagerSyncConfigsFailing",
            "query": "rate(cortex_alertmanager_sync_configs_failed_total[5m]) > 0",
            "duration": 1800,
            "labels": {"severity": "critical"},
            "annotations": {"summary": "sync failing"},
            "alerts": [],
            "health": "ok",
            "lastEvaluation": "2026-09-19T03:00:00Z",
            "type": "alerting"
          }
        ]
      },
      {
        "name": "mimir_api_1",
        "file": "recording-rules.yaml",
        "interval": 30,
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

func rulerServer(t *testing.T, byTenant map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/rules" {
			http.NotFound(w, r)
			return
		}
		body, ok := byTenant[r.Header.Get("X-Scope-OrgID")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":"error","error":"unknown tenant"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stepClock returns t0, t0+1s, t0+2s, ... on successive calls.
func stepClock() func() time.Time {
	n := 0
	return func() time.Time {
		n++
		return t0.Add(time.Duration(n-1) * time.Second)
	}
}

func TestCapture(t *testing.T) {
	srv := rulerServer(t, map[string]string{"infra": rulerFixture})
	client := rulerapi.New(rulerapi.Config{BaseURL: srv.URL})

	snap, err := Capture(context.Background(), client, srv.URL, "infra", stepClock())
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	want := Snapshot{
		Version:     FormatVersion,
		Tenant:      "infra",
		Source:      srv.URL,
		RequestedAt: t0,
		ObservedAt:  t0.Add(time.Second),
		Groups: []Group{
			{Namespace: "alerts.yaml", Name: "alertmanager_alerts", IntervalSeconds: 60, Rules: []Rule{{
				Kind:        "alerting",
				Name:        "MimirAlertmanagerSyncConfigsFailing",
				Expr:        "rate(cortex_alertmanager_sync_configs_failed_total[5m]) > 0",
				ForSeconds:  1800,
				Labels:      map[string]string{"severity": "critical"},
				Annotations: map[string]string{"summary": "sync failing"},
			}}},
			{Namespace: "recording-rules.yaml", Name: "mimir_api_1", IntervalSeconds: 30, Rules: []Rule{{
				Kind: "recording",
				Name: "cluster_job_pod:cortex_alertmanager_alerts:sum",
				Expr: "sum by (cluster, job, pod) (cortex_alertmanager_alerts)",
			}}},
		},
	}
	if !reflect.DeepEqual(snap, want) {
		t.Errorf("Capture =\n %+v\nwant\n %+v", snap, want)
	}
}

// A fetch that fails must not produce a snapshot: an empty one would diff
// as every rule removed.
func TestCapture_FetchFailureIsAnError(t *testing.T) {
	srv := rulerServer(t, map[string]string{"infra": rulerFixture})
	client := rulerapi.New(rulerapi.Config{BaseURL: srv.URL})

	if _, err := Capture(context.Background(), client, srv.URL, "nobody", stepClock()); err == nil {
		t.Error("Capture for a tenant the ruler rejects: want error")
	}

	broken := rulerServer(t, map[string]string{"infra": `{"status":"success","data":{"groups":[`})
	client = rulerapi.New(rulerapi.Config{BaseURL: broken.URL})
	if _, err := Capture(context.Background(), client, broken.URL, "infra", stepClock()); err == nil {
		t.Error("Capture of a truncated response: want error")
	}
}

func TestCapture_EmptyTenantIsASnapshot(t *testing.T) {
	srv := rulerServer(t, map[string]string{"empty": `{"status":"success","data":{"groups":[]}}`})
	client := rulerapi.New(rulerapi.Config{BaseURL: srv.URL})
	snap, err := Capture(context.Background(), client, srv.URL, "empty", stepClock())
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if snap.Groups == nil || len(snap.Groups) != 0 {
		t.Errorf("Groups = %#v, want empty and non-nil", snap.Groups)
	}
}

func TestCapture_SourceDropsCredentials(t *testing.T) {
	srv := rulerServer(t, map[string]string{"infra": rulerFixture})
	client := rulerapi.New(rulerapi.Config{BaseURL: srv.URL})
	withCreds := strings.Replace(srv.URL, "http://", "http://user:hunter2@", 1)

	snap, err := Capture(context.Background(), client, withCreds, "infra", stepClock())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snap.Source, "hunter2") || snap.Source != srv.URL {
		t.Errorf("Source = %q, want %q", snap.Source, srv.URL)
	}
}

func TestWriteMarkdown(t *testing.T) {
	prev := snapAt(0,
		group("a.yaml", "g", 60, rec("x", "up"), alert("A", "up == 0", 60, nil)),
		group("a.yaml", "old", 60, rec("m", "sum(up)")),
	)
	curr := snapAt(5,
		group("a.yaml", "g", 30, rec("x", "up"), alert("A", "up == 0", 300, nil), rec("y|z", "up")),
		group("b.yaml", "new", 60, rec("m", "sum(up)")),
	)
	lone := snapAt(0)
	lone.Tenant = "team-b"
	tl, err := Build([]Snapshot{curr, prev, lone})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, tl); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"## Tenant `team-a`",
		"2 snapshots, first observed 2026-09-19T03:00:02Z, last observed 2026-09-19T03:05:02Z.",
		"| 2026-09-19T03:00:00Z | 2026-09-19T03:05:02Z | changed | a.yaml / g / A | for 1m0s → 5m0s; interval 1m0s → 30s |",
		"| changed | a.yaml / g / x | interval 1m0s → 30s |",
		"| added | a.yaml / g / y\\|z | recording |",
		"| changed | b.yaml / new / m | moved from a.yaml / old / m |",
		"## Tenant `team-b`",
		"1 snapshot, observed 2026-09-19T03:00:02Z. That is a baseline",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}
