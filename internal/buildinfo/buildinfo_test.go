package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	noBuildInfo := func() (*debug.BuildInfo, bool) { return nil, false }
	fromToolchain := func(version string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: version}, Settings: settings}, true
		}
	}
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.time", Value: "2026-10-20T10:00:00Z"},
	}

	tests := []struct {
		name                  string
		version, commit, date string
		read                  func() (*debug.BuildInfo, bool)
		want                  Info
	}{
		{
			name:    "ldflags win over the toolchain",
			version: "v0.1.0", commit: "1a2b3c4", date: "2026-10-27T09:00:00Z",
			read: fromToolchain("v0.0.9", vcs...),
			want: Info{Version: "v0.1.0", Commit: "1a2b3c4", Date: "2026-10-27T09:00:00Z"},
		},
		{
			name: "go install module@version: module version, no VCS stamp",
			read: fromToolchain("v0.1.0"),
			want: Info{Version: "v0.1.0", Commit: Unknown, Date: Unknown},
		},
		{
			name: "go build from a checkout: (devel) is dropped, VCS stamp used",
			read: fromToolchain("(devel)", vcs...),
			want: Info{Version: "dev", Commit: "abc123", Date: "2026-10-20T10:00:00Z"},
		},
		{
			name: "uncommitted changes mark the commit dirty",
			read: fromToolchain("(devel)", append(vcs, debug.BuildSetting{Key: "vcs.modified", Value: "true"})...),
			want: Info{Version: "dev", Commit: "abc123-dirty", Date: "2026-10-20T10:00:00Z"},
		},
		{
			name: "nothing known",
			read: noBuildInfo,
			want: Info{Version: "dev", Commit: Unknown, Date: Unknown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.version, tt.commit, tt.date, tt.read); got != tt.want {
				t.Errorf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
