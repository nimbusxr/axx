package fixtures

import (
	"bytes"
	"strings"
	"testing"
)

// The json family across both backends: standalone JSON Schema documents
// (draft detection, $defs/$ref, allOf, type arrays, additionalProperties,
// composed-subtree passthrough, the validator oracle) and OpenAPI 3.1
// components, plus conformance, drift, lint rules and adoption.

func jsonWorkspace(t *testing.T) *workspace {
	t.Helper()
	return newWorkspace(t).seed("json-corpus")
}

func TestJSONWildcardDefaultsGiveSameNamedFieldsDifferentValues(t *testing.T) {
	w := jsonWorkspace(t)
	w.write("wallet/wallet.schema.json", `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "cards": {"type": "array", "items": {"type": "object", "required": ["code"],
      "properties": {"code": {"type": "string"}, "label": {"type": "string"}}}},
    "coupons": {"type": "array", "items": {"type": "object", "required": ["code"],
      "properties": {"code": {"type": "string"}}}}
  }
}`)
	w.write("wallet/wallet.factory.yaml", `factory:
  family: json
  schema: wallet.schema.json
defaults:
  cards[].code: "CARD-DEFAULT"
  coupons[].code: "COUPON-DEFAULT"
fixtures:
  basic:
    cards: [ { label: "visa" } ]
    coupons: [ { }, { } ]
`)
	doc := jsonOf(t, w.expand()["wallet/basic.json"])
	expect(t, str(at(doc, "cards", 0, "code")), "CARD-DEFAULT")
	expect(t, str(at(doc, "coupons", 0, "code")), "COUPON-DEFAULT")
	expect(t, str(at(doc, "coupons", 1, "code")), "COUPON-DEFAULT")
}

func TestJSONResolutionLayers(t *testing.T) {
	files := jsonWorkspace(t).expand()
	online := jsonOf(t, files["devices/device-online.json"])
	for path, want := range map[string]string{
		"device_id": "dev-device-online", "event_type": "device-online", "firmware": "1.4.2", "sample_rate_hz": "60", "power": "battery",
	} {
		expect(t, str(at(online, path)), want)
	}
	expect(t, str(at(online, "device", "serial")), "SN-100")
	expect(t, str(at(online, "tags", "zone")), "b")
	if at(online, "device", "site") != nil {
		t.Error("optional unresolved fields are omitted")
	}
	offline := jsonOf(t, files["devices/device-offline.json"])
	if at(offline, "region") != nil || !offline.Has("region") {
		t.Error("explicit null on a [string, null] field")
	}
	expect(t, str(at(offline, "power", "source")), "solar")
	expect(t, str(at(offline, "readings", 0, "unit")), "s")
}

func TestJSONFieldOrderFollowsTheSchemaAndOutputIsByteStable(t *testing.T) {
	w := jsonWorkspace(t)
	first, second := w.expand(), w.expand()
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("%s not byte-stable", p)
		}
	}
	body := string(first["devices/device-online.json"])
	if strings.Index(body, "device_id") > strings.Index(body, "event_type") || strings.Index(body, "event_type") > strings.Index(body, "readings") {
		t.Errorf("schema order:\n%s", body)
	}
}

func TestJSONDraft07YamlSchemaAndDefinitionsRefs(t *testing.T) {
	files := jsonWorkspace(t).expand()
	gold := jsonOf(t, files["limits/gold-tier.json"])
	expect(t, str(at(gold, "tier")), "gold")
	expect(t, str(at(gold, "ceiling", "currency")), "USD")
	expect(t, str(at(gold, "ceiling", "amount")), "5000.0")
	if jsonOf(t, files["limits/base-tier.json"]).Has("ceiling") {
		t.Error("optional unresolved: omitted")
	}
}

func TestJSONOpenAPI31Component(t *testing.T) {
	files := jsonWorkspace(t).expand()
	inStock := jsonOf(t, files["inventory/item-in-stock.json"])
	expect(t, str(at(inStock, "sku")), "sku-item-in-stock")
	expect(t, str(at(inStock, "quantity")), "12")
	back := jsonOf(t, files["inventory/item-backordered.json"])
	if !back.Has("location") || at(back, "location") != nil || back.Has("notes") {
		t.Errorf("backordered: %s", files["inventory/item-backordered.json"])
	}
}

func TestJSONErrors(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(w *workspace)
		parts []string
	}{
		{"typo when additionalProperties forbids it", func(w *workspace) {
			w.replace("devices/device-online.fixture.yaml", "event_type:", "event_typo:")
		}, []string{"event_typo", "Telemetry"}},
		{"enum violation", func(w *workspace) {
			w.replace("devices/device-online.fixture.yaml", "event_type: device-online", "event_type: exploded")
		}, []string{"exploded", "device-online"}},
		{"unresolved required field", func(w *workspace) {
			w.replace("devices/device-events.prototype.yaml", "  event_type: heartbeat\n", "")
			w.replace("devices/device-offline.fixture.yaml", "  event_type: device-offline\n", "")
		}, []string{"event_type", "required by Telemetry", "device-offline", "defaults:"}},
		{"null on a non-nullable field", func(w *workspace) {
			w.replace("devices/device-online.fixture.yaml", "event_type: device-online", "firmware: null")
		}, []string{"firmware", "null"}},
		{"composed subtree still validated by the oracle", func(w *workspace) {
			w.replace("devices/device-offline.fixture.yaml", "    watts: 240.5\n", "")
		}, []string{"does not conform to Telemetry"}},
		{"fragments into standalone schemas", func(w *workspace) {
			w.replace("devices/device-events.factory.yaml", "schema: ../schemas/telemetry.schema.json", "schema: ../schemas/telemetry.schema.json#/$defs/Reading")
		}, []string{"fragments", "#/components/schemas/"}},
		{"classpath refs", func(w *workspace) {
			w.replace("devices/device-events.factory.yaml", "schema: ../schemas/telemetry.schema.json", "schema: classpath:json-corpus/schemas/telemetry.schema.json")
		}, []string{"classpath:", "JVM"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := jsonWorkspace(t)
			tc.edit(w)
			mustErrContain(t, w.expandErr(), tc.parts...)
		})
	}
}

func TestJSONGenerateCheckLifecycle(t *testing.T) {
	w := jsonWorkspace(t)
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("limits/gold-tier.json", "USD", "EUR")
	f := w.check()
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "gold-tier.json", "FIXTURE DRIFT")
}

func TestJSONConformanceRulesLintUnmanagedFiles(t *testing.T) {
	w := jsonWorkspace(t)
	w.conformance(ConformanceRule{Name: "ingested telemetry", FilePatterns: []string{"ingest/*.json"}, SchemaType: "json", SchemaRef: "schemas/telemetry.schema.json"})
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("ingest/sensor-a.json", `"heartbeat"`, `"exploded"`)
	f := w.check()
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "sensor-a.json", "Telemetry")
}

func TestJSONLintRulesCoverEveryOutputDirectory(t *testing.T) {
	rules := string(jsonWorkspace(t).expand()[DefaultLintOutput])
	mustContain(t, rules, "jsonPath: device_id", "devices/*.json", "jsonPath: sku", "inventory/*.json")
}

func TestJSONAdoptionRoundTrips(t *testing.T) {
	w := jsonWorkspace(t)
	res, err := w.adopter().Adopt("json", "schemas/telemetry.schema.json", "ingest/*.json", "ingested-telemetry", false)
	if err != nil {
		t.Fatal(err)
	}
	report := joinLines(res.Report)
	mustContain(t, report, "decoded 3/3 files (family: json)", "deep-equal", "device_id")
	if !w.exists("ingest/ingested-telemetry.factory.yaml") || !w.exists("ingest/sensor-a.fixture.yaml") {
		t.Fatal("adopted files missing")
	}
	regenerated := w.expand()["ingest/sensor-c.json"]
	a, _ := parseJSON([]byte(w.read("ingest/sensor-c.json")), "original")
	b, _ := parseJSON(regenerated, "regenerated")
	if !deepEquals(a, b) {
		t.Errorf("regenerated sensor-c differs:\n%s", regenerated)
	}
}

func TestJSONAdoptionEmitsTheJavaFileSet(t *testing.T) {
	w := jsonWorkspace(t)
	if _, err := w.adopter().Adopt("json", "schemas/telemetry.schema.json", "ingest/*.json", "ingested-telemetry", false); err != nil {
		t.Fatal(err)
	}
	// The exact bytes of this adoption.
	expect(t, w.read("ingest/ingested-telemetry.factory.yaml"), `# Adopted by axx fixtures. Values are preserved verbatim from the
# original files; review the proposed identity candidates below.
factory:
  family: json
  schema: ../schemas/telemetry.schema.json
# review: every value of these string fields is distinct across the
# adopted fixtures, so they are declared as identities. Remove any that
# are not identities; values stay pinned in the fixture data either way.
identity:
- path: device_id
  prefix: sensor-
- path: device.serial
  prefix: SN-
`)
	expect(t, w.read("ingest/ingested-telemetry.prototype.yaml"), `# Shared shape of every ingested-telemetry fixture (modal values from adoption);
# fixture files carry only their deltas.
data:
  event_type: heartbeat
  firmware: 2.0.1
  region: us-east
  sample_rate_hz: 60
  readings:
  - metric: temp_c
    value: 20.0
  device:
    model: T2000
  power: mains
`)
	// Identity values are pinned after the deltas.
	expect(t, w.read("ingest/sensor-c.fixture.yaml"), `factory: ingested-telemetry
data:
  event_type: device-offline
  region: eu-west
  readings:
  - metric: temp_c
    value: 19.0
  power: battery
  tags:
    decommissioned: "true"
  device_id: sensor-c-however
  device:
    serial: SN-C-9
`)
}

func TestJSONOverlappingIdentityCandidatesKeepOnlyTheFirst(t *testing.T) {
	w := jsonWorkspace(t)
	w.write("schemas/echo.schema.json", `{"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "Echo",
 "type": "object",
 "properties": {"txn_id": {"type": "string"}, "correlated_id": {"type": "string"}, "note": {"type": "string"}},
 "additionalProperties": false}`)
	w.write("echo/self-correlated.json", `{"txn_id": "T-1", "correlated_id": "T-1", "note": "reversal of itself"}`)
	w.write("echo/cross-correlated.json", `{"txn_id": "T-2", "correlated_id": "T-9", "note": "reversal of another"}`)
	res, err := w.adopter().Adopt("json", "schemas/echo.schema.json", "echo/*.json", "echo-events", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "deep-equal")
	factory := w.read("echo/echo-events.factory.yaml")
	mustContain(t, factory, "path: txn_id")
	if strings.Contains(factory, "correlated_id") {
		t.Errorf("overlapping candidate kept:\n%s", factory)
	}
}
