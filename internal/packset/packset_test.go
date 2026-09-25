package packset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw  string
		kind Kind
		key  string
		ver  string
	}{
		{"rest", Published, "rest", ""},
		{"./steps", Local, "./steps", ""},
		{"steps/../lib", Local, "", ""}, // not a valid entry: no ./ and no dot in the host
		{"../shared/steps", Local, "../shared/steps", ""},
		{"github.com/team/axx-grpc@v1.2.0", Module, "github.com/team/axx-grpc", "v1.2.0"},
		{"example.com/steps", Module, "example.com/steps", ""},
	}
	for _, c := range cases {
		e, err := Parse(c.raw)
		if c.key == "" {
			if err == nil {
				t.Errorf("%s: want an error", c.raw)
			}
			continue
		}
		if err != nil || e.Kind != c.kind || e.Key() != c.key || e.Version != c.ver {
			t.Errorf("%s: %+v %v", c.raw, e, err)
		}
	}
}

func TestFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, found, err := Load(dir); found || err != nil {
		t.Fatalf("missing file: %v %v", found, err)
	}
	f := &File{}
	added, _ := f.Add("rest", "sql", "./steps", "github.com/x/y@v1.0.0")
	if len(added) != 4 {
		t.Fatalf("added %v", added)
	}
	if added, _ := f.Add("rest", "github.com/x/y@v1.1.0"); len(added) != 1 || f.Packs[3] != "github.com/x/y@v1.1.0" {
		t.Fatalf("re-add: %v %v", added, f.Packs)
	}
	if removed, _ := f.Remove("sql", "github.com/x/y"); len(removed) != 2 {
		t.Fatalf("removed %v", removed)
	}
	if err := f.Save(dir); err != nil {
		t.Fatal(err)
	}
	g, found, err := Load(dir)
	if err != nil || !found || strings.Join(g.Packs, ",") != "rest,./steps" {
		t.Fatalf("loaded %v %v %v", g, found, err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("packs: [rest, rest]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("duplicate: %v", err)
	}
	l := &Lock{Axx: "v0.1.0", Modules: []LockedModule{{Path: "github.com/x/y", Version: "v1.1.0"}}}
	if err := l.Save(dir); err != nil {
		t.Fatal(err)
	}
	m, err := LoadLock(dir)
	if err != nil || m.Version("github.com/x/y") != "v1.1.0" || m.Version("other") != "" {
		t.Fatalf("lock %+v %v", m, err)
	}
}
