package packbuild

import (
	"strings"
	"testing"
)

// The build requires the running axx's version as a module version, or
// replaces axx with a source checkout.
func TestGoModAxxVersion(t *testing.T) {
	for _, c := range []struct{ version, source, want string }{
		{"0.1.0", "", "require github.com/nimbusxr/axx v0.1.0\n"},
		{"v0.2.0-rc.1", "", "require github.com/nimbusxr/axx v0.2.0-rc.1\n"},
		{"0.0.0-dev", "/src/axx", "require github.com/nimbusxr/axx v0.0.0\n"},
	} {
		mod := string(goMod(Options{AxxVersion: c.version, AxxSource: c.source}, nil))
		if !strings.Contains(mod, c.want) {
			t.Errorf("version %q: go.mod lacks %q:\n%s", c.version, c.want, mod)
		}
		if c.source != "" && !strings.Contains(mod, "replace github.com/nimbusxr/axx => /src/axx\n") {
			t.Errorf("source build has no replace:\n%s", mod)
		}
	}
	if got := ModuleVersion("0.0.0-20260924160014-d47d47ac1f7b+dirty"); got != "" {
		t.Errorf("a development version has no module version, got %q", got)
	}
}
