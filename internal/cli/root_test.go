package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/buildinfo"
	"github.com/KorhanOzturk90/obscost/internal/cli"
)

func run(args ...string) (stdout, stderr string, code int) {
	var out, err bytes.Buffer
	code = cli.Run(args, &out, &err)
	return out.String(), err.String(), code
}

func TestCheckCommandRemoved(t *testing.T) {
	_, stderr, code := run("check", "--dir", ".")
	if code != 1 || !strings.Contains(stderr, `unknown command "check"`) {
		t.Fatalf("code = %d, stderr = %q, want 1 and an unknown-command error", code, stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	info := buildinfo.Info{Version: "v0.1.0", Commit: "1a2b3c4", Date: "2026-10-27T09:00:00Z"}
	code := cli.RunWithBuildInfo([]string{"--version"}, &out, &errOut, info)
	want := "promcost v0.1.0 (commit 1a2b3c4, built 2026-10-27T09:00:00Z)\n"
	if code != 0 || out.String() != want {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; want 0 and %q", code, out.String(), errOut.String(), want)
	}
}
