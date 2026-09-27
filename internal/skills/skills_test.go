package skills

import (
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
