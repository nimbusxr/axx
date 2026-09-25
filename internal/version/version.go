// Package version reports the build version of axx.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Set at build time via -ldflags "-X github.com/nimbusxr/axx/internal/version.Version=...".
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Date      string `json:"date,omitempty"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
	Channel   string `json:"channel"`
}

// Get returns version information, falling back to Go build info so binaries
// built with `go install` still report a meaningful version.
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = strings.TrimPrefix(bi.Main.Version, "v")
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			}
		}
	}
	if info.Version == "" {
		info.Version = "0.0.0-dev"
	}
	info.Version = strings.TrimPrefix(info.Version, "v")
	info.Channel = channel(info.Version)
	return info
}

// channel classifies a version: everything before 1.0.0 is beta.
func channel(v string) string {
	switch {
	case strings.Contains(v, "-dev") || strings.Contains(v, "SNAPSHOT") || strings.Contains(v, "nightly") ||
		strings.HasPrefix(v, "0.0.0-"): // Go pseudo-versions of untagged builds
		return "dev"
	case strings.HasPrefix(v, "0."):
		return "beta"
	case strings.Contains(v, "-rc"):
		return "rc"
	default:
		return "stable"
	}
}

// String renders the human form, e.g. "v0.4.1 (beta)".
func (i Info) String() string {
	s := "v" + i.Version
	if i.Channel != "stable" {
		s += " (" + i.Channel + ")"
	}
	return s
}
