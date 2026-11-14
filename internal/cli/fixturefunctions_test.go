package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// functionPack adds a fixture function of a project's own: the check digit
// of a parcel's reference.
type functionPack struct{}

func (functionPack) Manifest() core.Manifest {
	return core.Manifest{Name: "function-probe", FixtureFunctions: []core.FixtureFunction{{
		Name: "parcelCheckDigit", Doc: "the check digit of a parcel's reference",
		Apply: func(call *core.FixtureCall) (string, error) {
			sum := 0
			for _, c := range call.Args[0] {
				sum += int(c)
			}
			return string(rune('0' + sum%10)), nil
		},
	}}}
}

func init() { engine.Register("./function-probe", functionPack{}) }

// `axx fixtures` computes a spec's expressions with axx's own functions and
// with those the project's packs add.
func TestFixturesUseAxxsFunctionsAndThePacks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{
		"axx.yaml":                     "version: 1\n",
		"labels/label.schema.json":     `{"type": "object", "properties": {"id": {"type": "string"}, "reference": {"type": "string"}, "check": {"type": "string"}, "digest": {"type": "string"}}}`,
		"labels/labels.factory.yaml":   "factory:\n  family: json\n  schema: label.schema.json\nidentity:\n  - path: id\n",
		"labels/labels.prototype.yaml": "data:\n  check: ${ parcelCheckDigit(reference) }\n  digest: ${ sha256(\"parcel-key\") }\n",
		"labels/express.fixture.yaml":  "data:\n  reference: PX-4101\n",
	})
	if out, errOut, code := run(t, "fixtures", "generate"); code != int(exitcode.OK) {
		t.Fatalf("generate: exit %d:\n%s\n%s", code, out, errOut)
	}
	b, err := os.ReadFile(filepath.Join(dir, "labels", "express.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"check": "1"`, `"digest": "b159b218218d8f225ff054163fe85ee4c85be82f8c9b53144b54056a357e1cef"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in:\n%s", want, b)
		}
	}
	if out, errOut, code := run(t, "fixtures", "check"); code != int(exitcode.OK) {
		t.Errorf("check: exit %d:\n%s\n%s", code, out, errOut)
	}
	out, _, code := run(t, "fixtures", "explain", "labels/express.json", "check")
	if code != int(exitcode.OK) || !strings.Contains(out, "parcelCheckDigit(reference)") {
		t.Errorf("explain: exit %d:\n%s", code, out)
	}
	if out, _, _ := run(t, "fixtures", "--help"); !strings.Contains(out, "sha256") || !strings.Contains(out, "the project's packs") {
		t.Errorf("the help says nothing of functions:\n%s", out)
	}
}
