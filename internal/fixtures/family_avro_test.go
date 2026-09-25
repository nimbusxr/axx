package fixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Engine behavior matrix of the avro family: layered resolution, union
// encoding, identity derivation, byte-stable determinism, the hand-edit
// refusal contract, and the committed goldens.

func avroWorkspace(t *testing.T) *workspace {
	withoutAvroOracle(t)
	return newWorkspace(t).seedKafka()
}

const orderPayments = "kafka/order-payments"

func TestAvroExpansionIsByteStable(t *testing.T) {
	w := avroWorkspace(t)
	first, second := w.expand(), w.expand()
	if len(first) != 4 { // 3 fixtures + axx-lint.generated.yaml
		t.Fatalf("files: %v", keysOf(first))
	}
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("%s differs between runs", p)
		}
	}
}

func TestAvroGeneratedBytesMatchCommittedGoldens(t *testing.T) {
	w := avroWorkspace(t)
	for path, got := range w.expand() {
		golden, err := os.ReadFile(filepath.Join(corpusDir(), "golden", filepath.Base(path)))
		if err != nil {
			t.Fatalf("missing golden for %s", path)
		}
		if !bytes.Equal(golden, got) {
			t.Errorf("%s:\n--- golden\n%s\n+++ generated\n%s", path, golden, got)
		}
	}
}

func TestAvroDefaultsPrecedence(t *testing.T) {
	w := avroWorkspace(t)
	factory := orderPayments + "/order-payments.factory.yaml"
	// A [] wildcard path is more specific than the bare-name channel: "POS".
	w.replace(factory, "defaults:", "defaults:\n  payments[].channel: \"WEB\"")
	doc := jsonOf(t, w.expand()[orderPayments+"/order-authorized.json"])
	if got := str(at(doc, "payments", 0, "channel")); got != "WEB" {
		t.Errorf("wildcard should beat the bare name: %s", got)
	}
	// An exact indexed path is more specific still.
	w.replace(factory, "defaults:", "defaults:\n  payments[0].channel: \"KIOSK\"")
	doc = jsonOf(t, w.expand()[orderPayments+"/order-authorized.json"])
	if got := str(at(doc, "payments", 0, "channel")); got != "KIOSK" {
		t.Errorf("exact index should beat the wildcard: %s", got)
	}
}

func TestAvroResolutionLayersAndUnionEncoding(t *testing.T) {
	w := avroWorkspace(t)
	files := w.expand()
	doc := jsonOf(t, files[orderPayments+"/order-authorized.json"])
	for path, want := range map[string]string{
		"order_id": "ord-order-authorized", "status": "AUTHORIZED", "amount_cents": "12999",
		"currency": "USD", "tender_type": "CREDIT_CARD", "captured_at": "null",
	} {
		if got := str(at(doc, path)); got != want {
			t.Errorf("%s = %s, want %s", path, got, want)
		}
	}
	if str(at(doc, "payments", 0, "payment_id")) != "pay-order-authorized-primary" || str(at(doc, "payments", 0, "channel")) != "POS" ||
		at(doc, "payments", 0, "loyalty_id") != nil || str(at(doc, "metadata")) != "{}" {
		t.Errorf("payment line: %s", files[orderPayments+"/order-authorized.json"])
	}
	captured := jsonOf(t, files[orderPayments+"/order-captured.json"])
	if str(at(captured, "captured_at", "string")) != "2026-07-01T12:00:00Z" || str(at(captured, "metadata", "region")) != "us-east" {
		t.Errorf("captured: %s", files[orderPayments+"/order-captured.json"])
	}
	loyalty := jsonOf(t, files[orderPayments+"/order-captured-loyalty.json"])
	if str(at(loyalty, "order_id")) != "54321000103" || str(at(loyalty, "payments", 0, "loyalty_id", "string")) != "loy-000103" {
		t.Errorf("loyalty: %s", files[orderPayments+"/order-captured-loyalty.json"])
	}
}

func TestAvroGenerateIsIdempotentAndRefusesHandEdits(t *testing.T) {
	w := avroWorkspace(t)
	if first := w.generate(); len(first.Written) != 4 {
		t.Fatalf("written: %v", first.Written)
	}
	second := w.generate()
	if len(second.Written) != 0 || len(second.Unchanged) != 4 {
		t.Fatalf("second run: %+v", second)
	}
	w.replace(orderPayments+"/order-authorized.json", "POS", "WEB")
	mustErrContain(t, w.generateErr(), "hand-edited", "order-authorized.json")
	if codeOf(w.generateErr()) != CodeHandEdit {
		t.Error("hand-edits carry their own code")
	}
}

func TestAvroErrors(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(w *workspace)
		parts []string
	}{
		{"unresolved required field names field and every fixture", func(w *workspace) {
			w.replace("schemas/order-payment.avsc", `{"name": "tender_type", "type": "string"},`,
				"{\"name\": \"tender_type\", \"type\": \"string\"},\n    {\"name\": \"channel_code\", \"type\": \"string\"},")
		}, []string{"channel_code", "order-authorized", "order-captured", "defaults:"}},
		{"unknown field is a typo", func(w *workspace) {
			w.write(orderPayments+"/order-authorized.fixture.yaml", "data: { statuz: \"CAPTURED\" }\n")
		}, []string{"statuz", "OrderPayment"}},
		{"invalid enum symbol lists the symbols", func(w *workspace) {
			w.replace(orderPayments+"/order-payments.prototype.yaml", `status: "AUTHORIZED"`, `status: "PENDING"`)
		}, []string{"PENDING", "AUTHORIZED"}},
		{"identity collision names both owners", func(w *workspace) {
			w.replace(orderPayments+"/order-captured-loyalty.fixture.yaml", `order_id: "54321000103"`, `order_id: "ord-order-authorized"`)
		}, []string{"identity collision", "order-authorized", "order-captured-loyalty"}},
		{"classpath refs need a JVM", func(w *workspace) {
			w.replace(orderPayments+"/order-payments.factory.yaml", "schema: ../../schemas/order-payment.avsc", "schema: classpath:nowhere/nope.avsc")
		}, []string{"nowhere/nope.avsc", "classpath:"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := avroWorkspace(t)
			tc.edit(w)
			mustErrContain(t, w.expandErr(), tc.parts...)
		})
	}
}

func TestAvroZeroAdoptionIsZeroBehavior(t *testing.T) {
	w := avroWorkspace(t)
	if err := os.RemoveAll(w.path("kafka")); err != nil {
		t.Fatal(err)
	}
	res := w.generate()
	if len(res.Written) != 0 || len(res.Unchanged) != 0 {
		t.Fatalf("result: %+v", res)
	}
	if w.exists(ManifestFile) {
		t.Error("no factories must leave no trace, not even a manifest")
	}
}

func TestAvroManifestRecordsOwnershipSorted(t *testing.T) {
	w := avroWorkspace(t)
	x, err := w.mustGenerator().Expand(ModeVerify)
	if err != nil {
		t.Fatal(err)
	}
	entries := x.Manifest.Entries()
	if len(entries) != 4 {
		t.Fatalf("entries: %+v", entries)
	}
	e, ok := x.Manifest.Get(orderPayments + "/order-authorized.json")
	if !ok || e.Factory != orderPayments+"/order-payments.factory.yaml" || e.Fixture != "order-authorized" || len(e.SHA256) != 64 {
		t.Errorf("entry: %+v", e)
	}
	for i := 1; i < len(entries); i++ {
		if javaCompare(entries[i-1].Path, entries[i].Path) >= 0 {
			t.Errorf("not sorted: %s before %s", entries[i-1].Path, entries[i].Path)
		}
	}
}

func TestAvroHandAuthoredWrapperSelectsTheExactBranch(t *testing.T) {
	w := newWorkspace(t)
	withoutAvroOracle(t)
	w.write("schemas/order-line.avsc", orderLineSchema)
	w.write("kafka/manual/manual.factory.yaml", `factory:
  family: avro
  schema: ../../schemas/order-line.avsc
fixtures:
  manual-return:
    line_id: "line-9"
    activity:
      us.nimbusxr.space.Return: { exchangeLineNumber: 3 }
`)
	files := w.expand()
	doc := jsonOf(t, files["kafka/manual/manual-return.json"])
	if str(at(doc, "activity", "us.nimbusxr.space.Return", "exchangeLineNumber")) != "3" ||
		str(at(doc, "activity", "us.nimbusxr.space.Return", "quantity")) != "1" {
		t.Errorf("wrapper: %s", files["kafka/manual/manual-return.json"])
	}
	if !bytes.Equal(files["kafka/manual/manual-return.json"], w.expand()["kafka/manual/manual-return.json"]) {
		t.Error("not byte-stable")
	}
}

// orderLineSchema has a union with two record branches (ambiguous once
// unwrapped).
const orderLineSchema = `{
  "type": "record",
  "name": "OrderLine",
  "namespace": "us.nimbusxr.space",
  "fields": [
    {"name": "line_id", "type": "string"},
    {"name": "note", "type": ["null", "string"], "default": null},
    {"name": "activity", "type": [
      "null",
      {"type": "record", "name": "Purchase", "fields": [
        {"name": "quantity", "type": "int", "default": 1}
      ]},
      {"type": "record", "name": "Return", "fields": [
        {"name": "quantity", "type": "int", "default": 1},
        {"name": "exchangeLineNumber", "type": "int"}
      ]}
    ], "default": null}
  ]
}
`

func TestAvroAdoptionRetainsBranchWrappersAndRoundTrips(t *testing.T) {
	skipWithoutAvroOracle(t)
	w := newWorkspace(t)
	w.write("schemas/order-line.avsc", orderLineSchema)
	w.write("kafka/lines/purchase-line.json", `{"line_id": "line-1", "note": null, "activity": {"us.nimbusxr.space.Purchase": {"quantity": 2}}}`+"\n")
	w.write("kafka/lines/return-line.json", `{"line_id": "line-2", "note": {"string": "damaged"}, "activity": {"us.nimbusxr.space.Return": {"quantity": 1, "exchangeLineNumber": 7}}}`+"\n")
	w.write("kafka/lines/bare-line.json", `{"line_id": "line-3", "note": null, "activity": null}`+"\n")
	res, err := w.adopter().Adopt("avro", "schemas/order-line.avsc", "kafka/lines/*.json", "lines", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "3/3 deep-equal")
	mustContain(t, w.read("kafka/lines/return-line.fixture.yaml"), "us.nimbusxr.space.Return", "note: damaged")
	w.generate()
	doc := jsonOf(t, []byte(w.read("kafka/lines/return-line.json")))
	if str(at(doc, "activity", "us.nimbusxr.space.Return", "exchangeLineNumber")) != "7" {
		t.Errorf("branch lost: %s", w.read("kafka/lines/return-line.json"))
	}
}

func TestAvroConformanceOracle(t *testing.T) {
	skipWithoutAvroOracle(t)
	w := newWorkspace(t).seedKafka()
	w.generate()
	w.write("kafka/legacy/good.json", `{"order_id": "legacy-1", "status": "CAPTURED", "amount_cents": 1, "currency": "USD", "tender_type": "GIFT_CARD", "captured_at": null, "payments": [], "metadata": {}}`+"\n")
	w.write("kafka/legacy/bad.json", `{"order_id": "legacy-2"}`)
	w.conformance(ConformanceRule{Name: "legacy order payloads", FilePatterns: []string{"kafka/legacy/*.json"}, SchemaType: "avro", SchemaRef: "schemas/order-payment.avsc"})
	failures := w.check()
	if len(failures) != 1 {
		t.Fatalf("failures: %v", failures)
	}
	mustContain(t, failures[0], "bad.json", "OrderPayment")
}
