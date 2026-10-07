package promapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// slowServer answers after delay, or as soon as the client goes away.
func slowServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNew_DefaultTimeoutMatchesMimirQuerierTimeout(t *testing.T) {
	c := New(Config{BaseURL: "http://unused"})
	if c.client.Timeout != DefaultTimeout || DefaultTimeout != 2*time.Minute {
		t.Fatalf("default client timeout = %s, want %s (Mimir's -querier.timeout default)", c.client.Timeout, 2*time.Minute)
	}
	c = New(Config{BaseURL: "http://unused", Timeout: 5 * time.Second})
	if c.client.Timeout != 5*time.Second {
		t.Fatalf("configured timeout not honoured: %s", c.client.Timeout)
	}
}

// The live test on PR #44 failed with a bare "context deadline exceeded"
// that named neither the limit nor the setting. The error must name both.
func TestInstant_ClientTimeoutNamesBackendTimeout(t *testing.T) {
	srv := slowServer(t, 2*time.Second)
	c := New(Config{BaseURL: srv.URL, Timeout: 50 * time.Millisecond})

	_, err := c.Instant(context.Background(), "up")
	if err == nil {
		t.Fatal("want a timeout error")
	}
	for _, want := range []string{"backend.timeout", "50ms"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A deadline the caller set is not the client's timeout, so the hint
// would point at the wrong knob.
func TestInstant_CallerDeadlineIsNotExplainedAsClientTimeout(t *testing.T) {
	srv := slowServer(t, 2*time.Second)
	c := New(Config{BaseURL: srv.URL, Timeout: time.Minute})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Instant(ctx, "up")
	if err == nil {
		t.Fatal("want a deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want context.DeadlineExceeded, got %v", err)
	}
	if strings.Contains(err.Error(), "backend.timeout") {
		t.Errorf("caller deadline wrongly blamed on backend.timeout: %v", err)
	}
}
