// Command promcost is the CLI entrypoint. All logic lives in internal/cli
// so it's testable in-process without spawning a subprocess.
package main

import (
	"os"
	"runtime/debug"

	"github.com/KorhanOzturk90/obscost/internal/buildinfo"
	"github.com/KorhanOzturk90/obscost/internal/cli"
)

// Set at link time by GoReleaser and `make build`:
//
//	-ldflags "-X main.version=v0.1.0 -X main.commit=<sha> -X main.date=<RFC 3339>"
//
// They live in package main so the -X paths don't change when the module
// path does. Any left empty is filled from debug.ReadBuildInfo.
var (
	version string
	commit  string
	date    string
)

func main() {
	info := buildinfo.Resolve(version, commit, date, debug.ReadBuildInfo)
	os.Exit(cli.RunWithBuildInfo(os.Args[1:], os.Stdout, os.Stderr, info))
}
