package version

import (
	"runtime/debug"
	"testing"
)

func TestChannel(t *testing.T) {
	for v, want := range map[string]string{
		"0.1.0":                  "beta",
		"0.2.3":                  "beta",
		"1.0.0":                  "stable",
		"1.1.0-rc.1":             "rc",
		"0.0.0-dev":              "dev",
		"0.0.0-SNAPSHOT-59db4db": "dev",
		"nightly":                "dev",
		// Builds of a commit (go build in a checkout, go install ...@main):
		// Go pseudo-versions, before and after the first tag, and local changes.
		"0.0.0-20260924120000-abcdefabcdef":         "dev",
		"0.1.1-0.20260925184804-3bf0dd25927c":       "dev",
		"0.1.1-0.20260925184804-3bf0dd25927c+dirty": "dev",
		"0.2.0-rc.1.0.20260925184804-3bf0dd25927c":  "dev",
		"0.1.0+dirty": "dev",
	} {
		if got := channel(v); got != want {
			t.Errorf("channel(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestTheVersionIsTheAxxModules(t *testing.T) {
	axx := func(v string) debug.Module { return debug.Module{Path: Module, Version: v} }
	for _, c := range []struct {
		name string
		bi   debug.BuildInfo
		want string
	}{
		{"axx itself", debug.BuildInfo{Main: axx("v0.1.1")}, "0.1.1"},
		{"axx built from its checkout", debug.BuildInfo{Main: axx("(devel)")}, ""},
		{"a project's build", debug.BuildInfo{Main: debug.Module{Path: "axx.local/build", Version: "(devel)"}, Deps: []*debug.Module{{Path: "rsc.io/quote/v3", Version: "v3.1.0"}, {Path: Module, Version: "v0.1.1"}}}, "0.1.1"},
		{"a project's build of axx from source", debug.BuildInfo{Main: debug.Module{Path: "axx.local/build"}, Deps: []*debug.Module{{Path: Module, Version: "v0.0.0", Replace: &debug.Module{Path: "/src/axx"}}}}, ""},
		{"a project's build of axx from source, as Go records it", debug.BuildInfo{Main: debug.Module{Path: "axx.local/build"}, Deps: []*debug.Module{{Path: Module, Version: "v0.0.0", Replace: &debug.Module{Path: "/src/axx", Version: "(devel)"}}}}, ""},
	} {
		if got := moduleVersion(&c.bi); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFromModuleProxy(t *testing.T) {
	for v, want := range map[string]bool{
		"0.1.12": true,
		"0.1.13-0.20261003225805-0a9d0a74590b":       true,
		"0.1.13-0.20261003225805-0a9d0a74590b+dirty": false,
		"0.0.0-dev": false,
		"":          false,
	} {
		if got := FromModuleProxy(v); got != want {
			t.Errorf("FromModuleProxy(%q) = %v, want %v", v, got, want)
		}
	}
}
