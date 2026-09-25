package mcp

import (
	"os"
	"testing"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/all"
)

// TestMain builds every pack into the test binary, as axx builds itself
// with a project's packs.
func TestMain(m *testing.M) {
	for k, p := range all.Packs() {
		engine.Register(k, p)
	}
	os.Exit(m.Run())
}
