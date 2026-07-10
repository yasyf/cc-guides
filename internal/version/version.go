// Package version exposes the build version. The release pipeline stamps it in
// with -ldflags; a `go install`ed binary falls back to module build info.
package version

import (
	"runtime/debug"
	"strings"
)

// Set at build time via -ldflags (see Taskfile.yml and .goreleaser.yaml), e.g.
//
//	-X github.com/yasyf/cc-guides/internal/version.Version=v1.2.3
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// String returns the build version, preferring the ldflags-injected value and
// falling back to the module version recorded by `go install`.
func String() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

// Bare returns the ldflags-injected Version with any leading "v" stripped. This
// is the token recorded in the cc-guides lock and printed by `--version`; it reads
// Version directly (not String) so an unstamped build cleanly reports "dev" — the
// lock's signal that the render is not release-pinned.
func Bare() string {
	return strings.TrimPrefix(Version, "v")
}
