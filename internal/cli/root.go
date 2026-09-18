// Package cli wires promcost's cobra command tree: `report` (observed
// workload attribution, internal/attribution).
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// exitError lets a subcommand's RunE communicate a specific process exit
// code (spec §2's exit code table) without cobra's own error-printing
// machinery getting in the way; Run() unwraps it.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "promcost",
		Short:         "Cost attribution for multi-tenant Grafana Mimir",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newReportCmd(stdout, stderr))
	return root
}

// Run executes the CLI and returns a process exit code (spec §2):
// 0 success, 1 config/usage error.
func Run(args []string, stdout, stderr io.Writer) int {
	root := newRootCmd(stdout, stderr)
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		return 0
	}

	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", ee.err)
		}
		return ee.code
	}

	_, _ = fmt.Fprintln(stderr, "error:", err)
	return 1
}
