package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/packs/all"
)

func TestPackSelectionFromFile(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "axx-packs.yaml"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	names := func(e *Engine) string {
		var out []string
		for _, p := range e.Packs {
			out = append(out, p.Name)
		}
		return strings.Join(out, ",")
	}
	compiled := all.Packs()
	opts := Options{Config: &config.Config{Dir: dir}, Compiled: compiled}
	// Without the file, the project uses no packs.
	e, err := New(opts)
	if err != nil || names(e) != "core" || e.Declared {
		t.Fatalf("default: %s %v", names(e), err)
	}
	write("packs: [rest]\n")
	e, err = New(opts)
	if err != nil || names(e) != "core,rest" || !e.Declared {
		t.Fatalf("selected: %s %v", names(e), err)
	}
	if len(e.Registry.Match("the selection has 1 row")) != 0 {
		t.Error("sql steps must not load when sql is not listed")
	}
	write("packs: [rest, ./steps]\n")
	if _, err := New(opts); err == nil || !strings.Contains(err.Error(), "not built into this axx") {
		t.Fatalf("missing custom pack: %v", err)
	}
	withCustom := all.Packs()
	withCustom["./steps"] = customPack{}
	e, err = New(Options{Config: &config.Config{Dir: dir}, Compiled: withCustom})
	if err != nil || names(e) != "core,rest,custom" {
		t.Fatalf("custom: %s %v", names(e), err)
	}
	// A pack this axx was not built with is reported, not skipped.
	write("packs: [rest, sql]\n")
	if _, err := New(Options{Config: &config.Config{Dir: dir}, Compiled: map[string]core.Pack{"rest": compiled["rest"]}}); err == nil ||
		!strings.Contains(err.Error(), "pack sql is not built into this axx") {
		t.Fatalf("not compiled: %v", err)
	}
	// A cloud service pack brings the core it builds on, once.
	write("packs: [rest, gcp-storage, gcp-pubsub]\n")
	e, err = New(opts)
	if err != nil || names(e) != "core,rest,gcp-core,gcp-storage,gcp-pubsub" {
		t.Fatalf("requires: %s %v", names(e), err)
	}
	write("packs: [aws-core, aws-s3]\n")
	e, err = New(opts)
	if err != nil || names(e) != "core,aws-core,aws-s3" {
		t.Fatalf("listed core: %s %v", names(e), err)
	}
	write("packs: [nope]\n")
	if _, err := New(opts); err == nil || !strings.Contains(err.Error(), "not a pack axx publishes") {
		t.Fatalf("unknown: %v", err)
	}
}

func TestCompiledPacksOrder(t *testing.T) {
	var got []string
	for _, p := range Ordered(all.Packs()) {
		got = append(got, p.Name)
	}
	if got[0] != "core" || got[1] != "rest" || len(got) != 1+len(all.Packs()) {
		t.Fatalf("compiled packs: %v", got)
	}
}
