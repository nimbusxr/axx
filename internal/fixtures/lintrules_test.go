package fixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// Generated lint rules: one structural cross-file-unique rule per identity,
// wildcarded paths, directory-wide coverage, byte-stable, in the shape
// `axx lint` reads (lint.include).

func TestLintRulesMatchTheGolden(t *testing.T) {
	w := avroWorkspace(t)
	golden, err := os.ReadFile(filepath.Join(corpusDir(), "golden", DefaultLintOutput))
	if err != nil {
		t.Fatal(err)
	}
	got := w.expand()[DefaultLintOutput]
	if !bytes.Equal(got, golden) {
		t.Errorf("lint rules:\n%s\nwant:\n%s", got, golden)
	}
	if !bytes.Equal(got, w.expand()[DefaultLintOutput]) {
		t.Error("not byte-stable")
	}
}

func TestLintRulesShape(t *testing.T) {
	w := avroWorkspace(t)
	doc, err := jyaml.Unmarshal(w.expand()[DefaultLintOutput])
	if err != nil {
		t.Fatal(err)
	}
	root := doc.(*jsonx.Object)
	if strings.Join(root.Keys(), ",") != "rules" {
		t.Fatalf("a rules-only include file: %v", root.Keys())
	}
	rules := get(root, "rules").([]any)
	if len(rules) != 2 {
		t.Fatalf("rules: %d", len(rules))
	}
	first := rules[0].(*jsonx.Object)
	expect(t, strings.Join(first.Keys(), ","), "name,filePatterns,type,jsonPath,description,validation")
	expect(t, str(get(first, "name")), "kafka/order-payments/order-payments.factory.yaml: order_id uniqueness")
	expect(t, str(get(first, "type")), "jsonpath")
	expect(t, str(get(first, "validation")), "cross-file-unique")
	expect(t, str(get(first, "filePatterns")), "[kafka/order-payments/*.json]")
	expect(t, str(get(rules[1].(*jsonx.Object), "jsonPath")), "payments[*].payment_id")
}

func TestLintRulesEmitNothingWithoutIdentities(t *testing.T) {
	out, err := emitLintRules([]*Spec{{SourceName: "factories/no-identities.factory.yaml", Fixtures: newFixtureSet()}},
		func(*Spec) (Family, error) { return avroFamily{}, nil })
	if err != nil || out != nil {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestLintRulesCanBeDisabledAndMoved(t *testing.T) {
	w := avroWorkspace(t)
	w.cfg.LintEmit = false
	if w.expand()[DefaultLintOutput] != nil {
		t.Error("emit: false writes no rules")
	}
	w.cfg.LintEmit, w.cfg.LintOutput = true, "lint/generated.yaml"
	if w.expand()["lint/generated.yaml"] == nil {
		t.Error("lint.output moves the file")
	}
}

func TestWildcardIndices(t *testing.T) {
	for in, want := range map[string]string{"payments[0].lines[12].id": "payments[*].lines[*].id", "order_id": "order_id", "payments[*].id": "payments[*].id"} {
		expect(t, wildcardIndices(in), want)
	}
}

// The rules select the identity values of the generated files, so a
// hand-written neighbor reusing one is caught by the same JSONPath.
func TestLintRulesSelectTheIdentityValues(t *testing.T) {
	w := avroWorkspace(t)
	files := w.expand()
	doc, _ := jyaml.Unmarshal(files[DefaultLintOutput])
	values := map[string][]string{}
	for _, r := range get(doc.(*jsonx.Object), "rules").([]any) {
		rule := r.(*jsonx.Object)
		for _, name := range keysOf(files) {
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			parsed, err := jsonx.Parse(string(files[name]))
			if err != nil {
				t.Fatal(err)
			}
			v, _, err := jsonx.Read(parsed, str(get(rule, "jsonPath")))
			if err != nil {
				t.Fatalf("%s on %s: %v", str(get(rule, "jsonPath")), name, err)
			}
			values[str(get(rule, "jsonPath"))] = append(values[str(get(rule, "jsonPath"))], jsonx.MarshalValue(v))
		}
	}
	mustContain(t, strings.Join(values["order_id"], ","), "ord-order-authorized", "54321000103")
	mustContain(t, strings.Join(values["payments[*].payment_id"], ","), "pay-order-authorized-primary", "pay-order-captured-primary")
}

// A consumer-authored family is selected by factory.family, receives
// factory.options, claims identities and flows through generate and check
// like a built-in.

func customWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.opts = Options{Families: []Family{propertiesFamily{}}}
	w.write("factories/notes.factory.yaml", `factory:
  family: properties
  schema: none.schema
  output: { dir: notes }
  options:
    header: consumer-extension
prototype:
  audience: crew
identity:
  - path: note_id
    prefix: note-
fixtures:
  briefing: {}
  debrief: { audience: command }
`)
	return w
}

func TestCustomFamilyGeneratesThroughTheStandardEngine(t *testing.T) {
	w := customWorkspace(t)
	w.generate()
	expect(t, w.read("notes/briefing.properties"), "# consumer-extension\naudience=crew\nnote_id=note-briefing\n")
	mustContain(t, w.read("notes/debrief.properties"), "audience=command", "note_id=note-debrief")
}

func TestCustomFamilyIsDriftChecked(t *testing.T) {
	w := customWorkspace(t)
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("notes/briefing.properties", "crew", "everyone")
	f := w.check()
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "briefing.properties", "FIXTURE DRIFT")
}

func TestNonJSONFamilyOptsOutOfLintRules(t *testing.T) {
	files := customWorkspace(t).expand()
	if len(files) != 2 || files[DefaultLintOutput] != nil {
		t.Errorf("files: %v", keysOf(files))
	}
}

func TestDuplicateFamilyNamesFail(t *testing.T) {
	w := newWorkspace(t)
	w.opts = Options{Families: []Family{avroFamily{}}}
	_, err := w.generator()
	mustErrContain(t, err, "two fixture families claim the name 'avro'")
}
