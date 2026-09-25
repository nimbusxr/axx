package fixtures

import (
	"strings"
	"testing"
)

// The colocated layout: same-named factories nest without collision,
// outputs follow their fixture, and every ambiguity is a loud error.

const noteSchema = `{
  "type": "record",
  "name": "Note",
  "namespace": "us.nimbusxr.space",
  "fields": [
    {"name": "note_id", "type": "string"},
    {"name": "text", "type": "string"}
  ]
}`

func notesWorkspace(t *testing.T) *workspace {
	t.Helper()
	withoutAvroOracle(t)
	w := newWorkspace(t)
	w.write("schemas/note.avsc", noteSchema)
	return w
}

// factory writes <dir>/<name>.factory.yaml with a sibling prototype and a
// pure-prototype briefing fixture.
func (w *workspace) factory(dir, name, prefix string) {
	ups := strings.Repeat("../", len(strings.Split(dir, "/")))
	w.write(dir+"/"+name+".factory.yaml", "factory:\n  family: avro\n  schema: "+ups+"schemas/note.avsc\nidentity:\n  - path: note_id\n    prefix: "+prefix+"\n")
	w.write(dir+"/"+name+".prototype.yaml", "data:\n  text: \"hello\"\n")
	w.write(dir+"/briefing.fixture.yaml", "# pure prototype\n")
}

func TestSameNamedFactoriesColocateTheirOutputs(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("checkout/events", "event", "chk-")
	w.factory("order/events", "event", "ord-")
	files := w.expand()
	if files["checkout/events/briefing.json"] == nil || files["order/events/briefing.json"] == nil {
		t.Fatalf("files: %v", keysOf(files))
	}
	mustContain(t, string(files["checkout/events/briefing.json"]), "chk-briefing")
}

func TestInlinePrototypeAndPrototypeFileTogetherAreRefused(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.write("notes/note.factory.yaml", w.read("notes/note.factory.yaml")+"prototype:\n  text: \"inline\"\n")
	_, err := w.generator()
	mustErrContain(t, err, "note.prototype.yaml", "keep exactly one")
}

func TestInlineAndFileFixturesCoexistButKeyCollisionsAreNamed(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.write("notes/note.factory.yaml", w.read("notes/note.factory.yaml")+"fixtures:\n  inline-one:\n    text: \"inline\"\n")
	files := w.expand()
	if files["notes/briefing.json"] == nil || files["notes/inline-one.json"] == nil {
		t.Fatalf("files: %v", keysOf(files))
	}
	w.write("notes/inline-one.fixture.yaml", "# collides\n")
	_, err := w.generator()
	mustErrContain(t, err, "two fixtures claim the key 'inline-one'", "inline in notes/note.factory.yaml", "notes/inline-one.fixture.yaml")
}

func TestMultiFactoryDirectoryNeedsExplicitBinding(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.write("notes/memo.factory.yaml", "factory:\n  family: avro\n  schema: ../schemas/note.avsc\nidentity:\n  - path: note_id\n    prefix: m-\n")
	_, err := w.generator()
	mustErrContain(t, err, "holds 2 factories", "memo", "note")
	w.write("notes/briefing.fixture.yaml", "factory: note\n# pure prototype\n")
	w.write("notes/memo-1.fixture.yaml", "factory: memo\ndata:\n  text: \"memo body\"\n")
	files := w.expand()
	mustContain(t, string(files["notes/briefing.json"]), "n-briefing")
	mustContain(t, string(files["notes/memo-1.json"]), "m-memo-1")
}

func TestNestedFixturesBindUpwardAndOutputsFollowTheFixture(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("checkout/events", "event", "chk-")
	w.write("checkout/events/launches/launch-1.fixture.yaml", "data:\n  text: \"liftoff\"\n")
	files := w.expand()
	mustContain(t, string(files["checkout/events/launches/launch-1.json"]), "chk-launch-1")
	mustContain(t, string(files[DefaultLintOutput]), "checkout/events/*.json", "checkout/events/launches/*.json")
}

func TestPathBindingReachesFactoriesOutsideTheAncestorChain(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("checkout/events", "event", "chk-")
	w.write("elsewhere/stray.fixture.yaml", "factory: checkout/events/event\ndata:\n  text: \"far away\"\n")
	mustContain(t, string(w.expand()["elsewhere/stray.json"]), "chk-stray")
}

func TestUnboundFixtureFileFailsListingKnownFactories(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.write("orphans/lost.fixture.yaml", "data:\n  text: x\n")
	_, err := w.generator()
	mustErrContain(t, err, "orphans/lost.fixture.yaml", "no factory in this directory or any ancestor", "notes/note.factory.yaml")
	w.write("orphans/lost.fixture.yaml", "factory: nowhere/nothing\ndata:\n  text: x\n")
	_, err = w.generator()
	mustErrContain(t, err, "matches no factory")
}

func TestFactoryWithoutFixturesIsRefused(t *testing.T) {
	w := notesWorkspace(t)
	w.write("notes/note.factory.yaml", "factory:\n  family: avro\n  schema: ../schemas/note.avsc\n")
	_, err := w.generator()
	mustErrContain(t, err, "no fixtures")
	if codeOf(err) != CodeSpec {
		t.Errorf("code: %s", codeOf(err))
	}
}

func TestPrototypeOverlaysLayerNearestWins(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("checkout/events", "event", "chk-")
	w.write("checkout/events/launches/event.prototype.yaml", "data:\n  text: \"liftoff\"\n")
	w.write("checkout/events/launches/launch-1.fixture.yaml", "# pure\n")
	files := w.expand()
	mustContain(t, string(files["checkout/events/briefing.json"]), "hello")
	mustContain(t, string(files["checkout/events/launches/launch-1.json"]), "liftoff")
}

func TestUnboundAndUselessOverlaysAreLoudErrors(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.write("elsewhere/nobody.prototype.yaml", "data:\n  text: x\n")
	_, err := w.generator()
	mustErrContain(t, err, "no factory named 'nobody'")
	w.remove("elsewhere/nobody.prototype.yaml")
	w.write("elsewhere/note.prototype.yaml", "data:\n  text: x\n")
	_, err = w.generator()
	mustErrContain(t, err, "covers no fixtures")
}

func TestDeclaredOutputDirOverridesColocation(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.replace("notes/note.factory.yaml", "  schema: ../schemas/note.avsc", "  schema: ../schemas/note.avsc\n  output: { dir: generated/notes }")
	files := w.expand()
	if files["generated/notes/briefing.json"] == nil || len(files) != 2 { // fixture + lint rules
		t.Errorf("files: %v", keysOf(files))
	}
}

func TestSpecFileErrors(t *testing.T) {
	tests := []struct {
		name, file, content string
		parts               []string
	}{
		{"unknown top-level field", "notes/note.factory.yaml", "factory:\n  family: avro\nfixturez: {}\n", []string{"unknown field \"fixturez\"", "known: defaults, factory"}},
		{"unknown factory field", "notes/note.factory.yaml", "factory:\n  family: avro\n  schemz: x\n", []string{"unknown field \"schemz\" in factory"}},
		{"missing family", "notes/note.factory.yaml", "factory:\n  schema: x.avsc\nfixtures:\n  a: {}\n", []string{"factory.family is required"}},
		{"bad derive", "notes/note.factory.yaml", "factory:\n  family: avro\nidentity:\n  - path: a\n    derive: random\nfixtures:\n  a: {}\n", []string{"derive must be fixture-key or authored"}},
		{"bad format", "notes/note.factory.yaml", "factory:\n  family: avro\nidentity:\n  - path: a\n    format: uuid4\nfixtures:\n  a: {}\n", []string{"format must be literal or uuid-name-based"}},
		{"identity without path", "notes/note.factory.yaml", "factory:\n  family: avro\nidentity:\n  - prefix: a\nfixtures:\n  a: {}\n", []string{"identity entries need a path"}},
		{"fixture envelope", "notes/a.fixture.yaml", "datum: {}\n", []string{"unknown field \"datum\"", "factory:, data: and metadata:"}},
		{"empty factory", "notes/note.factory.yaml", "# nothing\n", []string{"the file is empty"}},
		{"yaml syntax", "notes/note.factory.yaml", "factory: [\n", []string{"cannot parse notes/note.factory.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := notesWorkspace(t)
			if tc.file != "notes/note.factory.yaml" {
				w.factory("notes", "note", "n-")
			}
			w.write(tc.file, tc.content)
			_, err := w.generator()
			mustErrContain(t, err, tc.parts...)
		})
	}
}

func TestUnsupportedFamilyNamesTheSupportedSet(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.replace("notes/note.factory.yaml", "family: avro", "family: nope")
	mustErrContain(t, w.expandErr(), "unsupported family 'nope'", "[avro, dataset, json, protobuf, xml, yaml]")
}

func TestUUIDNameBasedIdentities(t *testing.T) {
	w := notesWorkspace(t)
	w.factory("notes", "note", "n-")
	w.replace("notes/note.factory.yaml", "    prefix: n-\n", "    prefix: n-\n    format: uuid-name-based\n")
	doc := jsonOf(t, w.expand()["notes/briefing.json"])
	// RFC 4122 v5 of "n-briefing" in the factory namespace.
	if got := str(at(doc, "note_id")); got != "37d98738-cbd9-5698-8510-175c88df4fe2" {
		t.Errorf("note_id: %s", got)
	}
	for name, want := range map[string]string{"ord-x": "570c8339-d53a-5e17-b166-6575bbd83749", "é-ü": "a9eee789-cc53-5eb2-8796-bd914ec60a53"} {
		if got := nameBasedUUID(name); got != want {
			t.Errorf("nameBasedUUID(%q) = %s, want %s", name, got, want)
		}
	}
}
