// Package cli wires promcost's cobra command tree: `report` (observed
// workload attribution, internal/attribution).
package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/KorhanOzturk90/obscost/internal/buildinfo"
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

func newRootCmd(stdout, stderr io.Writer, info buildinfo.Info) *cobra.Command {
	root := &cobra.Command{
		Use:           "promcost",
		Short:         "Cost attribution for multi-tenant Grafana Mimir",
		Version:       info.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// cobra's --version prints only Version by default; a bug report needs
	// the commit and build date too.
	root.SetVersionTemplate("{{.Name}} " + info.String() + "\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newReportCmd(stdout, stderr))
	root.AddCommand(newCostCmd(stdout, stderr))
	return root
}

// Run executes the CLI and returns a process exit code (spec §2):
// 0 success, 1 config/usage error. Its --version reports only what the Go
// toolchain recorded in the binary; RunWithBuildInfo takes ldflags values.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWithBuildInfo(args, stdout, stderr, buildinfo.Resolve("", "", "", debug.ReadBuildInfo))
}

// RunWithBuildInfo is Run with the version, commit and date --version
// prints.
func RunWithBuildInfo(args []string, stdout, stderr io.Writer, info buildinfo.Info) int {
	root := newRootCmd(stdout, stderr, info)
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
