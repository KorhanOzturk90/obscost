package cli_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testPassword = "s3cr3t-pa55word"

// authMimir is costFixture behind a gateway that requires basic auth,
// counting requests so a test can show a credential error fails before
// any query is sent.
func authMimir(t *testing.T, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	inner := costFixture(t)
	t.Cleanup(inner.Close)
	target, err := url.Parse(inner.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if u, p, ok := r.BasicAuth(); !ok || u != "promcost" || p != testPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
}

func authConfig(t *testing.T, backendURL, auth string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promcost.yaml")
	content := "backend:\n  url: " + backendURL + "\n  auth:\n" + auth
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func costArgs(cfg string) []string {
	return []string{"cost", "--metrics-tenant", "monitoring", "--config", cfg, "--pool", "ingester_memory"}
}

// Issue #55 (a): a bearer_token_env that resolves to nothing fails at
// startup, naming the variable, before any request is sent.
func TestCost_UnsetBearerTokenEnvFailsLoudly(t *testing.T) {
	var requests atomic.Int32
	srv := authMimir(t, &requests)
	defer srv.Close()
	t.Setenv("PROMCOST_TEST_TOKEN", "")

	_, stderr, code := run(costArgs(authConfig(t, srv.URL, "    bearer_token_env: PROMCOST_TEST_TOKEN\n"))...)
	if code == 0 {
		t.Fatal("exit code 0, want an error for an empty bearer_token_env")
	}
	if !strings.Contains(stderr, "bearer_token_env names PROMCOST_TEST_TOKEN, which is unset or empty") {
		t.Errorf("stderr = %q, want it to name the variable", stderr)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) sent, want none", n)
	}
}

// Issue #55 (b): basic auth, with the password from an env var or a file,
// and the password never in output or errors.
func TestCost_BasicAuth(t *testing.T) {
	var requests atomic.Int32
	srv := authMimir(t, &requests)
	defer srv.Close()

	pwFile := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pwFile, []byte(testPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROMCOST_TEST_PW", testPassword)
	t.Setenv("PROMCOST_TEST_WRONG_PW", "wrong-"+testPassword)

	for name, auth := range map[string]string{
		"password_env":  "    basic_auth:\n      username: promcost\n      password_env: PROMCOST_TEST_PW\n",
		"password_file": "    basic_auth:\n      username: promcost\n      password_file: " + pwFile + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			for _, format := range []string{"md", "json"} {
				stdout, stderr, code := run(append(costArgs(authConfig(t, srv.URL, auth)), "--format", format)...)
				if code != 0 {
					t.Fatalf("exit code %d, stderr=%s", code, stderr)
				}
				if !strings.Contains(stdout, "analytics") {
					t.Errorf("want a report, got:\n%s", stdout)
				}
				if strings.Contains(stdout+stderr, testPassword) {
					t.Errorf("the password appears in the %s output", format)
				}
			}
		})
	}

	t.Run("rejected password is not echoed", func(t *testing.T) {
		auth := "    basic_auth:\n      username: promcost\n      password_env: PROMCOST_TEST_WRONG_PW\n"
		stdout, stderr, code := run(costArgs(authConfig(t, srv.URL, auth))...)
		if code == 0 || !strings.Contains(stderr, "401") {
			t.Fatalf("code=%d stderr=%q, want a 401 error", code, stderr)
		}
		if strings.Contains(stdout+stderr, testPassword) {
			t.Errorf("the password appears in the error: %q", stderr)
		}
	})
}

func TestCost_BasicAuthConfigErrors(t *testing.T) {
	t.Setenv("PROMCOST_TEST_EMPTY", "")
	t.Setenv("PROMCOST_TEST_PW", testPassword)
	for name, tc := range map[string]struct{ auth, want string }{
		"inline password": {"    basic_auth:\n      username: u\n      password: " + testPassword + "\n", "never put a password in promcost.yaml"},
		"no username":     {"    basic_auth:\n      password_env: PROMCOST_TEST_PW\n", "username is empty"},
		"no password":     {"    basic_auth:\n      username: u\n", "needs password_env or password_file"},
		"empty env":       {"    basic_auth:\n      username: u\n      password_env: PROMCOST_TEST_EMPTY\n", "password_env names PROMCOST_TEST_EMPTY, which is unset or empty"},
		"missing file":    {"    basic_auth:\n      username: u\n      password_file: /nonexistent/pw\n", "password_file: open /nonexistent/pw"},
		"both methods":    {"    bearer_token_env: PROMCOST_TEST_PW\n    basic_auth:\n      username: u\n      password_env: PROMCOST_TEST_PW\n", "use one"},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, code := run(costArgs(authConfig(t, "http://127.0.0.1:1", tc.auth))...)
			if code == 0 || !strings.Contains(stderr, tc.want) {
				t.Errorf("code=%d stderr=%q, want an error containing %q", code, stderr, tc.want)
			}
			if strings.Contains(stderr, testPassword) {
				t.Errorf("the password appears in the error: %q", stderr)
			}
		})
	}
}
