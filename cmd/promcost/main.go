// Command promcost is the CLI entrypoint. All logic lives in internal/cli
// so it's testable in-process without spawning a subprocess.
package main

import (
	"os"

	"github.com/KorhanOzturk90/obscost/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
