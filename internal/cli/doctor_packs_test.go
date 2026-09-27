package cli

import (
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/nimbusxr/axx/internal/version"
)

func TestPacksSayWhereTheyComeFrom(t *testing.T) {
	project := t.TempDir()
	mods := []*debug.Module{
		{Path: "axx.local/build", Version: "(devel)"},
		{Path: version.Module, Version: "v0.1.1"},
		{Path: "axx.local/steps", Version: "v0.0.0", Replace: &debug.Module{Path: filepath.Join(project, "steps"), Version: "(devel)"}},
		{Path: "example.com/shipping", Version: "v1.4.0"},
	}
	for _, c := range []struct{ name, pkg, want string }{
		{"rest", "github.com/nimbusxr/axx/packs/rest", "rest (axx 0.1.1)"},
		{"steps", "axx.local/steps", "steps (./steps)"},
		{"shipping", "example.com/shipping/axxpack", "shipping (example.com/shipping 1.4.0)"},
	} {
		if got := packSource(c.name, c.pkg, mods, project).String(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
