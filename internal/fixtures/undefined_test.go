package fixtures

import (
	"strings"
	"testing"
)

// undefined: no such property (ADR 0014). A fixture writes the word bare
// where it leaves out a value a layer below sets; in quotes it is the string.

const eventSchema = `{"type": "object", "required": ["id"], "properties": {
  "id": {"type": "string"},
  "started_at": {"type": "string"},
  "kind": {"type": "string"},
  "region": {"type": "string", "default": "eu"},
  "note": {"type": ["string", "null"]},
  "actor": {"type": "object", "properties": {"name": {"type": "string"}, "role": {"type": "string"}}},
  "tags": {"type": "object"}
}}`

// eventsWorkspace is a JSON factory whose prototype, defaults: and schema
// each set a field: started_at, kind and region.
func eventsWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.write("events/event.schema.json", eventSchema)
	w.write("events/events.factory.yaml", "factory:\n  family: json\n  schema: event.schema.json\ndefaults:\n  kind: VIEW\nidentity:\n  - path: id\n")
	w.write("events/events.prototype.yaml", "data:\n  started_at: \"2026-01-01T00:00:00Z\"\n  actor:\n    name: Sophie\n    role: admin\n  tags:\n    source: seed\n    batch: one\n")
	return w
}

func TestAFixtureLeavesOutWhatItsPrototypeSets(t *testing.T) {
	w := eventsWorkspace(t)
	w.write("events/all.fixture.yaml", "data: {}\n")
	w.write("events/bare.fixture.yaml", "data:\n  started_at: undefined\n  kind: undefined\n  region: undefined\n  actor:\n    role: undefined\n  tags:\n    batch: undefined\n")
	files := w.expand()
	// Every layer fills the fixture that says nothing: the prototype, defaults: and the schema's default.
	expect(t, string(files["events/all.json"]), `{
  "id": "all",
  "started_at": "2026-01-01T00:00:00Z",
  "kind": "VIEW",
  "region": "eu",
  "actor": {
    "name": "Sophie",
    "role": "admin"
  },
  "tags": {
    "batch": "one",
    "source": "seed"
  }
}
`)
	// None fills what a fixture leaves out, at any depth, in a free-form object too.
	expect(t, string(files["events/bare.json"]), `{
  "id": "bare",
  "actor": {
    "name": "Sophie"
  },
  "tags": {
    "source": "seed"
  }
}
`)
}

// null is the value null, and the word in quotes the string: neither leaves
// anything out.
func TestNullAndTheQuotedWordAreValues(t *testing.T) {
	w := eventsWorkspace(t)
	w.write("events/said.fixture.yaml", "data:\n  note: null\n  kind: \"undefined\"\n  started_at: 'undefined'\n  actor: undefined\n  tags: undefined\n")
	expect(t, string(w.expand()["events/said.json"]), `{
  "id": "said",
  "started_at": "undefined",
  "kind": "undefined",
  "region": "eu",
  "note": null
}
`)
}

// A field the schema requires cannot be left out: the fixture is told so,
// by its name. An identity left out derives, as one never set.
func TestARequiredFieldLeftOut(t *testing.T) {
	w := eventsWorkspace(t)
	w.replace("events/event.schema.json", `"required": ["id"]`, `"required": ["id", "started_at"]`)
	w.write("events/bare.fixture.yaml", "data:\n  started_at: undefined\n")
	mustErrContain(t, w.expandErr(), "started_at", "required", "fixtures.bare")

	w = eventsWorkspace(t)
	w.write("events/named.fixture.yaml", "data:\n  id: undefined\n")
	mustContain(t, string(w.expand()["events/named.json"]), `"id": "named"`)
}

// Next to a $ref, it leaves out what the $ref brings; and a prototype
// overlay leaves out what the prototype under it sets.
func TestUndefinedOverARefAndInAPrototypeOverlay(t *testing.T) {
	w := eventsWorkspace(t)
	w.replace("events/events.prototype.yaml", "  actor:\n    name: Sophie\n    role: admin\n", "")
	w.write("events/base.fixture.yaml", "data:\n  actor:\n    name: Ada\n    role: owner\n")
	w.write("events/copy.fixture.yaml", "data:\n  actor:\n    $ref: ./base.fixture.yaml#/actor\n    role: undefined\n")
	w.write("events/night/events.prototype.yaml", "data:\n  started_at: undefined\n  tags: undefined\n")
	w.write("events/night/late.fixture.yaml", "factory: events\ndata: {}\n")
	files := w.expand()
	copied := string(files["events/copy.json"])
	mustContain(t, copied, `"name": "Ada"`)
	if strings.Contains(copied, "role") {
		t.Errorf("the role a $ref brought stays:\n%s", copied)
	}
	late := string(files["events/night/late.json"])
	if strings.Contains(late, "started_at") || strings.Contains(late, "tags") {
		t.Errorf("the overlay left them out, and they stay:\n%s", late)
	}
	mustContain(t, late, `"kind": "VIEW"`)
}

// The word stands for a field's value where fixtures' values are: nowhere
// else, and never for a list's element.
func TestUndefinedWhereItHasNoMeaning(t *testing.T) {
	for name, tc := range map[string]struct{ file, content, at string }{
		"a list's element":    {"events/x.fixture.yaml", "data:\n  tags:\n    list: [a, undefined]\n", "data.tags.list[1]"},
		"the data itself":     {"events/x.fixture.yaml", "data: undefined\n", "data"},
		"a fixture's factory": {"events/x.fixture.yaml", "factory: undefined\ndata: {}\n", "factory"},
		"defaults":            {"events/events.factory.yaml", "factory:\n  family: json\n  schema: event.schema.json\ndefaults:\n  kind: undefined\n", "defaults.kind"},
		"a setting":           {"events/events.factory.yaml", "factory:\n  family: json\n  schema: undefined\n", "factory.schema"},
		"a row":               {"events/x.fixture.yaml", "data:\n  rows:\n    - undefined\n", "data.rows[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			w := eventsWorkspace(t)
			w.write(tc.file, tc.content)
			_, err := w.generator()
			if err == nil {
				err = w.expandErr()
			}
			mustErrContain(t, err, tc.file, tc.at+" is undefined", `"undefined"`)
		})
	}
}

func TestUndefinedInAnOpenAPIComponent(t *testing.T) {
	w := newWorkspace(t)
	w.write("api/api.yaml", `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Event:
      type: object
      required: [id]
      properties:
        id: {type: string}
        kind: {type: string}
        region: {type: string, default: eu}
        started_at: {type: string}
`)
	w.write("api/events.factory.yaml", "factory:\n  family: json\n  schema: api.yaml#/components/schemas/Event\ndefaults:\n  kind: VIEW\nprototype:\n  started_at: \"2026-01-01T00:00:00Z\"\nidentity:\n  - path: id\n")
	w.write("api/bare.fixture.yaml", "data:\n  started_at: undefined\n  kind: undefined\n  region: undefined\n")
	expect(t, string(w.expand()["api/bare.json"]), "{\n  \"id\": \"bare\"\n}\n")
}

// An Avro record has every field: one left out takes the schema's default,
// never the factory's, and without a default it is unresolved.
func TestUndefinedInAnAvroRecord(t *testing.T) {
	withoutAvroOracle(t)
	w := newWorkspace(t)
	w.write("kafka/event.avsc", `{"type": "record", "name": "Event", "fields": [
  {"name": "id", "type": "string"},
  {"name": "kind", "type": "string", "default": "VIEW"},
  {"name": "region", "type": "string"}
]}`)
	w.write("kafka/events.factory.yaml", "factory:\n  family: avro\n  schema: event.avsc\ndefaults:\n  kind: CLICK\n  region: eu\nidentity:\n  - path: id\n")
	w.write("kafka/usual.fixture.yaml", "data: {}\n")
	w.write("kafka/bare.fixture.yaml", "data:\n  kind: undefined\n")
	files := w.expand()
	mustContain(t, string(files["kafka/usual.json"]), `"kind": "CLICK"`)
	mustContain(t, string(files["kafka/bare.json"]), `"kind": "VIEW"`, `"region": "eu"`)

	w.write("kafka/bare.fixture.yaml", "data:\n  region: undefined\n")
	mustErrContain(t, w.expandErr(), "region", "fixtures.bare")
}

func TestUndefinedInAProtobufMessage(t *testing.T) {
	w := newWorkspace(t)
	w.write("proto/event.proto", "syntax = \"proto3\";\npackage axx.test;\nmessage Event {\n  string id = 1;\n  string started_at = 2;\n  string kind = 3;\n}\n")
	w.write("proto/events.factory.yaml", "factory:\n  family: protobuf\n  schema: event.proto#axx.test.Event\ndefaults:\n  kind: VIEW\n  started_at: \"2026-01-01T00:00:00Z\"\nidentity:\n  - path: id\n")
	// By the name the .proto gives the field, and by its JSON name.
	w.write("proto/bare.fixture.yaml", "data:\n  started_at: undefined\n")
	w.write("proto/camel.fixture.yaml", "data:\n  startedAt: undefined\n  kind: undefined\n")
	files := w.expand()
	expect(t, string(files["proto/bare.json"]), "{\n  \"id\": \"bare\",\n  \"kind\": \"VIEW\"\n}\n")
	expect(t, string(files["proto/camel.json"]), "{\n  \"id\": \"camel\"\n}\n")
}

func TestUndefinedInAnXMLDocument(t *testing.T) {
	w := newWorkspace(t).seed("xml-corpus")
	w.replace("orders/orders.factory.yaml", "identity:", "defaults:\n  customer.tier: gold\nidentity:")
	before := string(w.expand()["orders/order-single.xml"])
	mustContain(t, before, "<tier>gold</tier>", "channel=\"WEB\"")
	w.write("orders/order-single.fixture.yaml", "data:\n  \"@channel\": undefined\n  customer:\n    tier: undefined\n  line:\n    - \"@sku\": SKU-1\n      \"@qty\": 2\n      \"#text\": \"19.98\"\n  note: rush\n")
	after := string(w.expand()["orders/order-single.xml"])
	mustContain(t, after, "<name>Josh</name>")
	for _, gone := range []string{"<tier>", "channel="} {
		if strings.Contains(after, gone) {
			t.Errorf("%s stays:\n%s", gone, after)
		}
	}
}

// A dataset's row leaves a column out: the database's default fills it, and
// neither the row template nor defaults: does. null is SQL's NULL.
func TestUndefinedInADatasetRow(t *testing.T) {
	w := sessionsWorkspace(t)
	w.replace("seeds/sessions.factory.yaml", "identity:", "defaults:\n  sessions.plan: team\nidentity:")
	w.write("seeds/sessions.prototype.yaml", "data:\n  sessions:\n    token_hash: abc\n  events:\n    kind: VIEW\n")
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - {}\n    - id: SES-2\n      plan: undefined\n    - id: SES-3\n      plan: null\n")
	expect(t, string(w.expand()["seeds/alpha.yaml"]), `sessions:
- id: SES-alpha
  token_hash: abc
  plan: team
- id: SES-2
  token_hash: abc
- id: SES-3
  token_hash: abc
  plan: null
`)
	// A column the table needs cannot be left out.
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - token_hash: undefined\n")
	mustErrContain(t, w.expandErr(), "sessions.token_hash", "NOT NULL")
	// Without a schema, the row has the columns it says.
	w.replace("seeds/sessions.factory.yaml", "  schema: schema.sql\n", "")
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - plan: undefined\n      token_hash: undefined\n")
	expect(t, string(w.expand()["seeds/alpha.yaml"]), "sessions:\n- id: SES-alpha\n")
}

// A generated YAML file and an adopted spec keep the string in quotes, so
// that reading either back gives the string.
func TestTheStringUndefinedStaysQuoted(t *testing.T) {
	w := sessionsWorkspace(t)
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - plan: \"undefined\"\n")
	mustContain(t, string(w.expand()["seeds/alpha.yaml"]), `plan: "undefined"`)
}

// Adoption writes the string in quotes into the specs it makes, so that
// generating from them gives the files it adopted.
func TestAdoptKeepsTheStringUndefined(t *testing.T) {
	w := newWorkspace(t)
	w.write("events/event.schema.json", `{"type": "object", "properties": {"id": {"type": "string"}, "kind": {"type": "string"}, "started_at": {"type": "string"}}}`)
	w.write("legacy/one.json", `{"id": "one", "kind": "undefined", "started_at": "undefined"}`+"\n")
	w.write("legacy/two.json", `{"id": "two", "kind": "VIEW", "started_at": "undefined"}`+"\n")
	if _, err := w.adopter().Adopt("json", "events/event.schema.json", "legacy/*.json", "adopted", false); err != nil {
		t.Fatal(err)
	}
	mustContain(t, w.read("legacy/adopted.prototype.yaml"), `started_at: "undefined"`)
	mustContain(t, w.read("legacy/one.fixture.yaml"), `kind: "undefined"`)
	mustContain(t, string(w.expand()["legacy/one.json"]), `"kind": "undefined"`, `"started_at": "undefined"`)
}

// Explain says the fixture leaves the value out.
func TestExplainAValueLeftOut(t *testing.T) {
	w := eventsWorkspace(t)
	w.write("events/bare.fixture.yaml", "data:\n  started_at: undefined\n")
	w.generate()
	_, err := w.mustGenerator().Explain(w.path("events/bare.json"), "started_at")
	mustErrContain(t, err, "started_at", "the fixture leaves it out", "events/bare.fixture.yaml", "data.started_at")
}

// An Avro field a fixture leaves out has the schema's default: explain says
// so, not the fixture, nor the defaults: entry it kept from filling it.
func TestExplainAnAvroFieldLeftOut(t *testing.T) {
	withoutAvroOracle(t)
	w := newWorkspace(t)
	w.write("kafka/event.avsc", `{"type": "record", "name": "Event", "fields": [{"name": "id", "type": "string"}, {"name": "kind", "type": "string", "default": "VIEW"}]}`)
	w.write("kafka/events.factory.yaml", "factory:\n  family: avro\n  schema: event.avsc\ndefaults:\n  kind: CLICK\nidentity:\n  - path: id\n")
	w.write("kafka/bare.fixture.yaml", "data:\n  kind: undefined\n")
	w.generate()
	runExplain(t, w, []explainCase{{"kafka/bare.json", "kind", `"VIEW"`, "schema | kafka/event.avsc | the schema's default"}})
}
