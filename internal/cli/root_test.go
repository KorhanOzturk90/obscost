package cli_test

import (
	"bytes"
	"strings"
	"testing"

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
