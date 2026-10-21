// Package buildinfo resolves the version, commit and build date that
// `--version` prints.
//
// Release builds (GoReleaser, `make build`) inject all three through
// -ldflags into package main, which passes them to Resolve. A plain
// `go install module@vX.Y.Z` has no ldflags, so Resolve falls back to what
// the Go toolchain records in the binary: the module version, and the VCS
// revision and time when built from a checkout.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

// Unknown is printed for any field neither ldflags nor the toolchain
// supplied, rather than an empty string.
const Unknown = "unknown"

// Info is what `--version` prints.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// String renders Info as one line, e.g.
// "v0.1.0 (commit 1a2b3c4, built 2026-10-27T09:00:00Z)".
func (i Info) String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", i.Version, i.Commit, i.Date)
}

// Resolve fills each of version, commit and date that is empty from read
// (debug.ReadBuildInfo in production), and any still empty with Unknown.
// Values injected through ldflags always win.
func Resolve(version, commit, date string, read func() (*debug.BuildInfo, bool)) Info {
	info := Info{Version: version, Commit: commit, Date: date}
	if bi, ok := read(); ok && bi != nil {
		// "(devel)" is what the toolchain records for a build from a local
		// checkout; it says nothing a VCS revision doesn't say better.
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		var revision, vcsTime string
		var modified bool
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.time":
				vcsTime = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if info.Commit == "" && revision != "" {
			info.Commit = revision
			if modified {
				info.Commit += "-dirty"
			}
		}
		if info.Date == "" {
			info.Date = vcsTime
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = Unknown
	}
	if info.Date == "" {
		info.Date = Unknown
	}
	return info
}
