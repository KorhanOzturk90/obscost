package cli_test

import (
	"bytes"
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
