package fixtures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// Adoption: originals are reproduced exactly (the adopt/generate property),
// the factory, prototype and fixture envelopes land next to the adopted data,
// and every refusal is explicit and writes nothing. The avro suites need the
// Avro JSON oracle; the json and dataset suites exercise the same engine.

const avroSchemaRef = "schemas/order-payment.avsc"

var avroSources = []string{
	"order-payments.factory.yaml", "order-payments.prototype.yaml",
	"order-authorized.fixture.yaml", "order-captured.fixture.yaml", "order-captured-loyalty.fixture.yaml",
}

// unmanagedAvroWorkspace is day 0 of a consumer: generated fixture files
// with every factory source, the manifest and the lint rules removed.
func unmanagedAvroWorkspace(t *testing.T) *workspace {
	t.Helper()
	skipWithoutAvroOracle(t)
	w := newWorkspace(t).seedKafka()
	w.generate()
	for _, s := range avroSources {
		w.remove(orderPayments + "/" + s)
	}
	w.remove(ManifestFile)
	w.remove(DefaultLintOutput)
	return w
}

func snapshotJSON(t *testing.T, w *workspace, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(w.path(dir))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			out[e.Name()] = w.read(dir + "/" + e.Name())
		}
	}
	return out
}

func TestAdoptThenGenerateReproducesOriginalsByteForByte(t *testing.T) {
	w := unmanagedAvroWorkspace(t)
	before := snapshotJSON(t, w, orderPayments)
	res, err := w.adopter().Adopt("avro", avroSchemaRef, orderPayments+"/*.json", "adopted", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "3/3 deep-equal", "0 file(s) will be reformatted")
	for _, p := range []string{"adopted.factory.yaml", "adopted.prototype.yaml", "order-authorized.fixture.yaml"} {
		if !w.exists(orderPayments + "/" + p) {
			t.Errorf("missing %s", p)
		}
	}
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	after := snapshotJSON(t, w, orderPayments)
	for name, b := range before {
		if after[name] != b {
			t.Errorf("%s changed", name)
		}
	}
}

func TestAdoptPrototypeTakesModalValuesAndFixturesKeepDeltas(t *testing.T) {
	w := unmanagedAvroWorkspace(t)
	if _, err := w.adopter().Adopt("avro", avroSchemaRef, orderPayments+"/*.json", "adopted", false); err != nil {
		t.Fatal(err)
	}
	factory, err := jyaml.Unmarshal([]byte(w.read(orderPayments + "/adopted.factory.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := parseFactory(factory, "adopted.factory.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Family != "avro" || spec.Schema != "../../"+avroSchemaRef || spec.Prototype.Len() != 0 || len(spec.Identity) != 1 || spec.Identity[0].Path != "order_id" {
		t.Errorf("spec: %+v", spec)
	}
	mustContain(t, w.read(orderPayments+"/adopted.prototype.yaml"), "status: CAPTURED", "currency: USD")
	mustContain(t, w.read(orderPayments+"/order-authorized.fixture.yaml"), "status: AUTHORIZED")
	if strings.Contains(w.read(orderPayments+"/order-captured.fixture.yaml"), "status:") {
		t.Error("modal values are not repeated")
	}
	for key, want := range map[string]string{"order-authorized": "ord-order-authorized", "order-captured": "ord-order-captured", "order-captured-loyalty": "54321000103"} {
		doc, _ := jyaml.Unmarshal([]byte(w.read(orderPayments + "/" + key + ".fixture.yaml")))
		if str(at(doc, "factory")) != "adopted" || str(at(doc, "data", "order_id")) != want {
			t.Errorf("%s: %s", key, w.read(orderPayments+"/"+key+".fixture.yaml"))
		}
	}
}

func TestAdoptHandFormattedFilesAreReformatOnly(t *testing.T) {
	w := unmanagedAvroWorkspace(t)
	doc, _ := parseJSON([]byte(w.read(orderPayments+"/order-authorized.json")), "x")
	w.write(orderPayments+"/order-authorized.json", string(compactJSON(doc)))
	res, err := w.adopter().Adopt("avro", avroSchemaRef, orderPayments+"/*.json", "adopted", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "1 file(s) will be reformatted", "0 semantic changes")
}

func TestAdoptMultiDirectoryPlacesTheFactoryAtTheCommonAncestor(t *testing.T) {
	w := unmanagedAvroWorkspace(t)
	w.write("kafka/other-payments/other-authorized.json", w.read(orderPayments+"/order-authorized.json"))
	res, err := w.adopter().Adopt("avro", avroSchemaRef, "kafka/**/*.json", "all-payments", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "4/4 deep-equal")
	if !w.exists("kafka/all-payments.factory.yaml") || !w.exists("kafka/other-payments/other-authorized.fixture.yaml") {
		t.Fatal("layout")
	}
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Errorf("check: %v", f)
	}
}

func TestAdoptRefusals(t *testing.T) {
	withoutAvroOracle(t)
	tests := []struct {
		name  string
		setup func(w *workspace)
		glob  string
		fac   string
		parts []string
	}{
		{"existing factory", func(w *workspace) { w.write("ingest/adopted.factory.yaml", "# here\n") }, "ingest/*.json", "adopted", []string{"never overwrites"}},
		{"path-like name", nil, "ingest/*.json", "factories/adopted.factory.yaml", []string{"NAME"}},
		{"empty glob", nil, "nowhere/*.json", "adopted", []string{"matches no files"}},
		{"duplicate keys across directories", func(w *workspace) {
			w.write("ingest/more/sensor-a.json", w.read("ingest/sensor-a.json"))
		}, "ingest/**.json", "adopted", []string{"sensor-a", "unique within a factory"}},
		{"undecodable file", func(w *workspace) { w.write("ingest/broken.json", `{"device_id": "x"}`) }, "ingest/*.json", "adopted", []string{"broken.json"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := jsonWorkspace(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			_, err := w.adopter().Adopt("json", "schemas/telemetry.schema.json", tc.glob, tc.fac, false)
			mustErrContain(t, err, tc.parts...)
			if tc.name != "existing factory" && w.exists("ingest/adopted.factory.yaml") {
				t.Error("refusals write nothing")
			}
		})
	}
}

func TestAdoptRefusesManagedFiles(t *testing.T) {
	w := jsonWorkspace(t)
	w.generate()
	_, err := w.adopter().Adopt("json", "schemas/telemetry.schema.json", "devices/*.json", "again", false)
	mustErrContain(t, err, "already managed", ManifestFile)
}

func TestAdoptSingleFileProposesNoIdentities(t *testing.T) {
	w := jsonWorkspace(t)
	w.remove("ingest/sensor-b.json")
	w.remove("ingest/sensor-c.json")
	res, err := w.adopter().Adopt("json", "schemas/telemetry.schema.json", "ingest/*.json", "adopted", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "identity candidates: none")
	mustContain(t, w.read("ingest/sensor-a.fixture.yaml"), "factory: adopted", "Pure prototype")
	if strings.Contains(w.read("ingest/adopted.factory.yaml"), "identity:") {
		t.Error("no identities with one file")
	}
}

func TestAdoptDryRunWritesNothing(t *testing.T) {
	w := jsonWorkspace(t)
	res, err := w.adopter().Adopt("json", "schemas/telemetry.schema.json", "ingest/*.json", "adopted", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 5 || w.exists("ingest/adopted.factory.yaml") {
		t.Errorf("dry run: %+v", res)
	}
	mustContain(t, joinLines(res.Report), "would write")
}

// Incremental adoption (--into).

func writeNewEvent(t *testing.T, w *workspace, dir, name, orderID string) {
	t.Helper()
	w.generate()
	s := w.read(orderPayments + "/order-authorized.json")
	s = strings.ReplaceAll(s, "ord-order-authorized", orderID)
	s = strings.ReplaceAll(s, "pay-order-authorized", "pay-"+orderID)
	w.write(dir+"/"+name, s)
}

func TestAdoptIntoJoinsTheExistingFactory(t *testing.T) {
	skipWithoutAvroOracle(t)
	w := newWorkspace(t).seedKafka()
	writeNewEvent(t, w, "features/extra", "order-adjusted.json", "ord-adjusted-1")
	res, err := w.adopter().AdoptInto("order-payments", "features/extra/*.json", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "bound 1 new fixture(s) into", "deep-equal")
	fixture := w.read("features/extra/order-adjusted.fixture.yaml")
	mustContain(t, fixture, "factory: kafka/order-payments/order-payments", "ord-adjusted-1")
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Errorf("check: %v", f)
	}
}

func TestAdoptIntoRefusals(t *testing.T) {
	withoutAvroOracle(t)
	w := newWorkspace(t).seedKafka()
	writeNewEvent(t, w, "features/extra", "order-authorized.json", "ord-dupe-1")
	_, err := w.adopter().AdoptInto("order-payments", "features/extra/*.json", false)
	mustErrContain(t, err, "order-authorized", "already exists")

	_, err = w.adopter().AdoptInto("order-payments", orderPayments+"/*.json", false)
	mustErrContain(t, err, "already managed")

	_, err = w.adopter().AdoptInto("no-such-factory", "kafka/*.json", false)
	mustErrContain(t, err, "matches no factory", "order-payments.factory.yaml")

	w.write("elsewhere/order-payments.factory.yaml", "factory:\n  family: avro\n  schema: ../schemas/order-payment.avsc\nfixtures:\n  elsewhere-event: {}\n")
	_, err = w.adopter().AdoptInto("order-payments", "features/**/*.json", false)
	mustErrContain(t, err, "ambiguous", "kafka/order-payments/order-payments")
}

func TestAdoptIntoDatasetRowTemplatesSubtractPerRow(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("features/return/full-refund-seed.yaml", "space.missions:\n- id: ret-msn-1\n  name: Refund Mission\n  metadata: '{\"priority\": \"low\"}'\n")
	res, err := w.adopter().AdoptInto("mission-seeds", "features/return/*.yaml", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "deep-equal")
	fixture := w.read("features/return/full-refund-seed.fixture.yaml")
	mustContain(t, fixture, "Refund Mission", "id: ret-msn-1")
	if strings.Contains(fixture, "metadata") {
		t.Errorf("the row template is subtracted per row:\n%s", fixture)
	}
}

func TestSubtractAndModal(t *testing.T) {
	proto := newObject("a", int64Num(1), "m", newObject("x", "1", "y", "2"), "rows", newObject("k", "v"))
	tree := newObject("a", int64Num(1), "b", "new", "m", newObject("x", "1", "y", "3"), "rows", []any{newObject("k", "v", "z", "1"), "scalar"})
	got := str(subtract(tree, proto))
	expect(t, got, "{b=new, m={y=3}, rows=[{z=1}, scalar]}")
	best, n := modal([]any{"a", "b", "b", nil, nil})
	if best != "b" || n != 2 {
		t.Errorf("modal: %v %d", best, n)
	}
	if best, _ := modal([]any{newObject("q", "1"), newObject("q", "1"), "x"}); str(best) != "{q=1}" {
		t.Errorf("modal of maps: %v", best)
	}
}

func int64Num(v int64) any { return jsonx.LongNumber(v) }

func TestFactoryRelative(t *testing.T) {
	a := &Adopter{baseDir: filepath.FromSlash("/r")}
	for in, want := range map[[2]string]string{
		{"schemas/x.avsc", "kafka/orders"}:                "../../schemas/x.avsc",
		{"openapi/a.yaml#/components/schemas/X", "mocks"}: "../openapi/a.yaml#/components/schemas/X",
		{"x.avsc", ""}: "x.avsc",
	} {
		expect(t, a.factoryRelative(in[0], in[1]), want)
	}
}
