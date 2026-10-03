package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/fixtures"
	"github.com/nimbusxr/axx/packs/all"
)

func build(t *testing.T) map[string]map[string]string {
	t.Helper()
	e, err := engine.New(engine.Options{Config: &config.Config{}, Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	sks, err := Build(e)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	for _, sk := range sks {
		out[sk.Name] = map[string]string{}
		for _, f := range sk.Files {
			out[sk.Name][f.Path] = string(f.Content)
		}
	}
	return out
}

func TestTheTestDataSkillCarriesTheSpecFileSchemasAndErrorCodes(t *testing.T) {
	files := build(t)["axx-test-data"]
	spec := files["references/spec-files.md"]
	for _, kind := range fixtures.SchemaKinds {
		if !strings.Contains(spec, fixtures.SchemaID(kind)) {
			t.Errorf("references/spec-files.md lacks the %s schema", kind)
		}
	}
	if !strings.Contains(files["references/error-codes.md"], "AXX-E0903") {
		t.Error("references/error-codes.md lacks the fixture codes")
	}
}

func TestEverySkillIsNamedAfterItsDirectory(t *testing.T) {
	name := regexp.MustCompile(`(?m)^name: (\S+)$`)
	description := regexp.MustCompile(`(?m)^description: \S`)
	for skill, files := range build(t) {
		md := files["SKILL.md"]
		if m := name.FindStringSubmatch(md); m == nil || m[1] != skill {
			t.Errorf("%s/SKILL.md is named %v", skill, m)
		}
		if !description.MatchString(md) {
			t.Errorf("%s/SKILL.md has no description", skill)
		}
	}
}

// The skills carry no step pages: `axx steps` and `axx steps show` tell the
// steps, at a tenth of the tokens agents spent reading pages in chunks.
// axx-custom-steps has the parameter types it needs.
func TestTheSkillsCarryNoStepPages(t *testing.T) {
	sks := build(t)
	for name, files := range sks {
		for p := range files {
			if strings.Contains(p, "steps-") || strings.Contains(p, "step-index") {
				t.Errorf("%s carries %s", name, p)
			}
		}
	}
	if _, ok := sks["axx-acceptance-tests"]["references/parameter-types.md"]; !ok {
		t.Error("axx-acceptance-tests lacks the parameter types")
	}
	for p := range sks["axx-custom-steps"] {
		if strings.HasPrefix(p, "references/") && p != "references/parameter-types.md" {
			t.Errorf("axx-custom-steps carries %s", p)
		}
	}
}

// An install removes the files an earlier one wrote that the skills no
// longer have, and keeps one the user edited.
func TestInstallRemovesFilesTheSkillsDropped(t *testing.T) {
	root := t.TempDir()
	v1 := []Skill{{Name: "s", Files: []File{{Path: "SKILL.md", Content: []byte("a")}, {Path: "references/old.md", Content: []byte("o")}, {Path: "references/edited.md", Content: []byte("e")}}}}
	if _, err := Install(v1, InstallOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(root, ".agents", "skills", "s", "references", "edited.md")
	if err := os.WriteFile(edited, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	v2 := []Skill{{Name: "s", Files: []File{{Path: "SKILL.md", Content: []byte("b")}}}}
	res, err := Install(v2, InstallOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, []string{"s/references/old.md"}) || !reflect.DeepEqual(res.Kept, []string{"s/references/edited.md"}) {
		t.Errorf("removed %v, kept %v", res.Removed, res.Kept)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "s", "references", "old.md")); !os.IsNotExist(err) {
		t.Errorf("old.md is still there: %v", err)
	}
	if b, _ := os.ReadFile(edited); string(b) != "mine" {
		t.Errorf("the edited file changed: %s", b)
	}
}
