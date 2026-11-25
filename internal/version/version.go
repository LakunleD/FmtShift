// Package version reports the FmtShift build version.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is set at release build time with
//
//	-ldflags "-X github.com/lakunled/fmtshift/internal/version.Version=v0.1.0"
var Version = ""

// String returns the release version, or for development builds "dev" plus
// the VCS revision Go embedded at build time, if any.
func String() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v // installed with go install module@version
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return "dev"
	}
	return "dev-" + strings.TrimSpace(rev[:min(len(rev), 12)]) + dirty
}
