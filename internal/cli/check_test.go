package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/cli"
)

func run(args ...string) (stdout, stderr string, code int) {
	var out, err bytes.Buffer
	code = cli.Run(args, &out, &err)
	return out.String(), err.String(), code
}

func TestCheck_Clean(t *testing.T) {
	stdout, stderr, code := run("check", "--dir", "testdata/clean/rules", "--config", "testdata/clean/promcost.yaml")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestCheck_WarnFindingsBelowDefaultFailOn(t *testing.T) {
	stdout, stderr, code := run("check", "--dir", "testdata/warnonly/rules", "--config", "testdata/warnonly/promcost.yaml")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (warn findings below default --fail-on=error). stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("PC-S02")) {
		t.Errorf("expected PC-S02 in report, got:\n%s", stdout)
	}
}

func TestCheck_ErrorFindingsAtOrAboveFailOn(t *testing.T) {
	stdout, stderr, code := run("check", "--dir", "testdata/errorfindings/rules", "--config", "testdata/errorfindings/promcost.yaml")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2. stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestCheck_BadConfigPath(t *testing.T) {
	_, stderr, code := run("check", "--dir", "testdata/clean/rules", "--config", "testdata/does-not-exist.yaml")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Error("expected an error message on stderr")
	}
}

func TestCheck_MalformedRuleFile(t *testing.T) {
	_, _, code := run("check", "--dir", "testdata/malformed/rules")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestCheck_MissingRequiredDirFlag(t *testing.T) {
	_, _, code := run("check")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (missing required --dir)", code)
	}
}

// writeBackendConfig writes a promcost.yaml with backend.url pointing at an
// unreachable/erroring server, so buildMeter's construction-or-probe
// failure path is exercised without needing a real backend.
func writeBackendConfig(t *testing.T, backendURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promcost.yaml")
	contents := "checks:\n  disable: [PC-S06]\nbackend:\n  type: mimir\n  url: " + backendURL + "\n  timeout: 1s\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestCheck_OfflineSkipsMeterEvenWithBackendConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	configPath := writeBackendConfig(t, srv.URL)
	stdout, _, code := run("check", "--dir", "testdata/clean/rules", "--config", configPath, "--offline")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0. stdout=%s", code, stdout)
	}
	if strings.Contains(stdout, "backend unreachable") {
		t.Errorf("--offline should never contact backend.url, got warning in stdout:\n%s", stdout)
	}
}

func TestCheck_StrictBackendUnreachableExitsThree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	configPath := writeBackendConfig(t, srv.URL)
	_, stderr, code := run("check", "--dir", "testdata/clean/rules", "--config", configPath, "--strict")
	if code != 3 {
		t.Fatalf("exit code = %d, want 3. stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "backend unreachable") {
		t.Errorf("expected \"backend unreachable\" on stderr, got:\n%s", stderr)
	}
}

func TestCheck_NonStrictBackendUnreachableDegrades(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	configPath := writeBackendConfig(t, srv.URL)
	stdout, _, code := run("check", "--dir", "testdata/clean/rules", "--config", configPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (degrade to static-only, not fail). stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "backend unreachable") {
		t.Errorf("expected \"backend unreachable\" warning on stdout, got:\n%s", stdout)
	}
}
