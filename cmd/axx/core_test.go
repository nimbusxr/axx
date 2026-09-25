package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCoreOnly keeps packs out of the axx binary: every pack gets into axx
// through a project's axx-packs.yaml.
func TestCoreOnly(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.HasPrefix(p, "github.com/nimbusxr/axx/packs/") {
			t.Errorf("axx imports %s; packs are listed in axx-packs.yaml, not compiled into the core", p)
		}
	}
}
