package controlsasvs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	controlscore "github.com/nimbusxr/axx/packs/controls/core"
)

// The pack carries the standard as OWASP publishes it: the file's hash is the
// one pinned, which internal/tools/controlsdata fetched it by.
func TestTheStandardIsOWASPsFile(t *testing.T) {
	raw, err := Raw()
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != SourceSHA256 {
		t.Fatalf("the embedded file's SHA-256 is %s, and the pack pins %s: run internal/tools/controlsdata", hex.EncodeToString(sum[:]), SourceSHA256)
	}
}

// The standard's 345 requirements, each with its text and where it is in the
// standard, and its three levels: a level has its own requirements and those
// of the levels under it.
func TestTheStandardsRequirementsAndLevels(t *testing.T) {
	f := Framework()
	if f.Key != "asvs" || f.Version != "5.0.0" || len(f.Controls) != 345 || !strings.Contains(f.Notice, "CC BY-SA 4.0") {
		t.Fatalf("the framework: %s %s, %d requirements", f.Key, f.Version, len(f.Controls))
	}
	seen := map[string]bool{}
	for _, c := range f.Controls {
		if seen[c.ID] || c.Text == "" || c.Title == "" || c.Group == "" || strings.HasPrefix(c.ID, "V") {
			t.Errorf("the requirement %q: %+v", c.ID, c)
		}
		seen[c.ID] = true
	}
	var password controlscore.Control
	for _, c := range f.Controls {
		if c.ID == "6.2.1" {
			password = c
		}
	}
	if !strings.HasPrefix(password.Text, "Verify that user set passwords are at least 8 characters in length") ||
		password.Group != "V6 Authentication, V6.2 Password Security" || password.Title != "Password Security" {
		t.Errorf("the requirement 6.2.1: %+v", password)
	}
	want := map[string]int{"asvs-5.0-level-1": 70, "asvs-5.0-level-2": 253, "asvs-5.0-level-3": 345}
	if len(f.Lists) != 3 {
		t.Fatalf("the lists: %d", len(f.Lists))
	}
	for _, l := range f.Lists {
		if len(l.Controls) != want[l.Name] {
			t.Errorf("%s has %d requirements, want %d", l.Name, len(l.Controls), want[l.Name])
		}
	}
	// A requirement's ID is written with the V of the standard's files in
	// front of it, or without.
	for _, same := range [][2]string{{"6.2.1", "V6.2.1"}, {"v6.2.1", "6.2.1"}} {
		if !Same(same[0], same[1]) {
			t.Errorf("%s and %s are the same requirement", same[0], same[1])
		}
	}
	if Same("6.2.1", "6.2.10") {
		t.Error("6.2.1 and 6.2.10 are two requirements")
	}
}

// A project that has the pack can require a level and find a requirement by a
// word of it: the pack registers the standard with controls-core.
func TestAProjectRequiresALevel(t *testing.T) {
	var commands []core.Command
	for _, c := range controlscore.Pack().Manifest().Commands {
		commands = c.Commands
	}
	run := func(name string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		call := &core.CommandCall{
			Context: context.Background(), Args: args, Flags: map[string]string{"format": "text"}, Stderr: io.Discard, ProjectDir: t.TempDir(),
			Settings: func(v any) error { return json.Unmarshal([]byte(`{"require": ["asvs-5.0-level-1"]}`), v) },
			Emit:     func(_ any, human func(w io.Writer) error) error { return human(&out) },
		}
		for _, c := range commands {
			if c.Name == name {
				if err := c.Run(call); err != nil {
					t.Fatalf("axx controls %s: %v", name, err)
				}
			}
		}
		return out.String()
	}
	if out := run("list"); !strings.Contains(out, "asvs         OWASP ASVS 5.0.0, 345 controls") ||
		!strings.Contains(out, "asvs-5.0-level-1             OWASP ASVS 5.0, level 1, 70 controls  (required)") {
		t.Errorf("the lists:\n%s", out)
	}
	if out := run("list", "passwords", "8", "characters"); !strings.HasPrefix(out, "asvs:6.2.1  Password Security\n") {
		t.Errorf("the requirements that mention the words:\n%s", out)
	}
	out := run("show", "asvs:V6.2.1")
	for _, want := range []string{
		"asvs:6.2.1  Password Security", "in OWASP ASVS 5.0.0, V6 Authentication, V6.2 Password Security",
		"Verify that user set passwords are at least 8 characters in length", "CC BY-SA 4.0", "required by asvs-5.0-level-1", "the tag @asvs:6.2.1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if out := run("report"); !strings.Contains(out, "controls: 0 of 70 required are covered, 70 not covered") || !strings.Contains(out, "CC BY-SA 4.0") {
		t.Errorf("the report before any run:\n%s", out[:min(len(out), 400)])
	}
}

func TestThePack(t *testing.T) {
	m := Pack().Manifest()
	if m.Name != "controls-asvs" || !m.Experimental || len(m.Requires) != 1 || m.Requires[0] != "controls-core" || len(m.Steps) != 0 {
		t.Errorf("the manifest: %+v", m)
	}
}

// A control's ID as a name, for the formats that take only names (OSCAL): as ASVS writes it.
// Every control has one such a format accepts.
func TestAControlsIDAsAName(t *testing.T) {
	f := Framework()
	for id, want := range map[string]string{"6.2.1": "V6.2.1", "17.3.2": "V17.3.2"} {
		if got := f.Token(id); got != want {
			t.Errorf("%s as a name: %q, want %q", id, got, want)
		}
	}
	name := regexp.MustCompile(`^(\p{L}|_)(\p{L}|\p{N}|[.\-_])*$`)
	for _, c := range f.Controls {
		if got := f.Token(c.ID); !name.MatchString(got) {
			t.Errorf("%s as a name is %q, which is none", c.ID, got)
		}
	}
}
