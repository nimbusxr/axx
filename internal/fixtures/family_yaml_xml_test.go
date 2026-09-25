package fixtures

import (
	"bytes"
	"strings"
	"testing"
)

// The yaml family: JSON-Schema-governed record YAML (the json family's
// machinery, YAML rendering).

func yamlWorkspace(t *testing.T) *workspace {
	t.Helper()
	return newWorkspace(t).seed("yaml-corpus")
}

func TestYAMLRendersCanonicalSchemaOrderedYAML(t *testing.T) {
	files := yamlWorkspace(t).expand()
	expect(t, string(files["configs/checkout-config.yaml"]), `service: svc-checkout-config
port: 8080
mode: live
timeouts:
  connect_ms: 250
  read_ms: 900
features:
  gift_wrap: false
  split_tender: true
tags:
- checkout
- tier-1
`)
	returns := string(files["configs/returns-config.yaml"])
	mustContain(t, returns, "mode: shadow", "read_ms: 1500")
	if strings.Contains(returns, "features") {
		t.Error("optional unresolved: omitted")
	}
}

func TestYAMLStabilityDriftAndNoLintRules(t *testing.T) {
	w := yamlWorkspace(t)
	first, second := w.expand(), w.expand()
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("%s not byte-stable", p)
		}
	}
	if first[DefaultLintOutput] != nil {
		t.Error("YAML output emits no structural lint rules")
	}
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("configs/returns-config.yaml", "shadow", "live")
	if f := w.check(); len(f) != 1 || !strings.Contains(f[0], "returns-config.yaml") {
		t.Fatalf("failures: %v", f)
	}
}

func TestYAMLEnumAndTypoGuards(t *testing.T) {
	w := yamlWorkspace(t)
	w.replace("configs/returns-config.fixture.yaml", "mode: shadow", "mode: dark")
	mustErrContain(t, w.expandErr(), "dark", "shadow")
	w.replace("configs/returns-config.fixture.yaml", "mode: dark", "moed: shadow")
	mustErrContain(t, w.expandErr(), "moed", "AppConfig")
}

func TestYAMLConformanceLintsHandWrittenYAML(t *testing.T) {
	w := yamlWorkspace(t)
	w.write("legacy/broken-config.yaml", "service: svc-broken\nport: not-a-number\nmode: live\n")
	w.conformance(ConformanceRule{Name: "legacy configs", FilePatterns: []string{"legacy/*.yaml"}, SchemaType: "yaml", SchemaRef: "schemas/app-config.schema.json"})
	f := filtered(w.check(), "conformance")
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "broken-config.yaml", "AppConfig")
}

func TestYAMLAdoptionRoundTrips(t *testing.T) {
	w := yamlWorkspace(t)
	res, err := w.adopter().Adopt("yaml", "schemas/app-config.schema.json", "legacy/*.yaml", "legacy-configs", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "decoded 2/2 files (family: yaml)", "deep-equal")
	if !w.exists("legacy/legacy-configs.factory.yaml") || !w.exists("legacy/payments-config.fixture.yaml") {
		t.Fatal("adopted files missing")
	}
	mustContain(t, w.read("legacy/legacy-configs.prototype.yaml"), "mode: live")
}

// The xml family: XSD-governed record documents, @attribute/#text
// conventions, root derivation, the XSD oracle, dotted defaults.

func xmlWorkspace(t *testing.T) *workspace {
	t.Helper()
	return newWorkspace(t).seed("xml-corpus")
}

func TestXMLRendersAttributesTextRepeatsAndIdentity(t *testing.T) {
	files := xmlWorkspace(t).expand()
	expect(t, string(files["orders/order-single.xml"]), `<?xml version="1.0" encoding="UTF-8"?>
<order channel="WEB" id="ord-order-single">
  <customer id="cust-1">
    <name>Josh</name>
  </customer>
  <line sku="SKU-1" qty="2">19.98</line>
  <note>rush</note>
</order>
`)
	multi := string(files["orders/order-multi.xml"])
	mustContain(t, multi, `id="ord-order-multi"`, "<tier>gold</tier>", `<line sku="SKU-1" qty="1">9.99</line>`, `<line sku="SKU-2" qty="3">29.97</line>`)
}

func TestXMLStabilityAndDrift(t *testing.T) {
	w := xmlWorkspace(t)
	first, second := w.expand(), w.expand()
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("%s not byte-stable", p)
		}
	}
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("orders/order-single.xml", "rush", "slow")
	if f := w.check(); len(f) != 1 || !strings.Contains(f[0], "order-single.xml") {
		t.Fatalf("failures: %v", f)
	}
}

func TestXMLOracleRefusals(t *testing.T) {
	w := xmlWorkspace(t)
	w.replace("orders/order-single.fixture.yaml", "      \"@qty\": 2\n", "")
	mustErrContain(t, w.expandErr(), "does not conform to the XSD", "qty")
	w = xmlWorkspace(t)
	w.replace("orders/order-single.fixture.yaml", "note: rush", "note: rush\n  bogus: x")
	mustErrContain(t, w.expandErr(), "does not conform to the XSD", "bogus")
}

func TestXMLDottedDefaultsAndBareNameRefusal(t *testing.T) {
	w := xmlWorkspace(t)
	f := "orders/orders.factory.yaml"
	original := w.read(f)
	w.write(f, strings.Replace(original, "identity:", "defaults:\n  customer.tier: silver\nidentity:", 1))
	files := w.expand()
	mustContain(t, string(files["orders/order-single.xml"]), "<tier>silver</tier>")
	mustContain(t, string(files["orders/order-multi.xml"]), "<tier>gold</tier>")
	w.write(f, strings.Replace(original, "identity:", "defaults:\n  tier: silver\nidentity:", 1))
	mustErrContain(t, w.expandErr(), "bare-name default 'tier'")
}

func TestXMLMultipleGlobalElementsDemandOptionsRoot(t *testing.T) {
	w := xmlWorkspace(t)
	w.replace("schemas/order.xsd", "</xs:schema>", "  <xs:element name=\"refund\" type=\"xs:string\"/>\n</xs:schema>")
	mustErrContain(t, w.expandErr(), "options.root", "refund")
	w.replace("orders/orders.factory.yaml", "family: xml", "family: xml\n  options: { root: order }")
	if w.expand()["orders/order-single.xml"] == nil {
		t.Error("options.root picks the root")
	}
}

func TestXMLConformanceLintsHandWrittenXML(t *testing.T) {
	w := xmlWorkspace(t)
	w.write("legacy/good.xml", `<?xml version="1.0" encoding="UTF-8"?>
<order id="legacy-1">
  <customer id="c-1"><name>Legacy</name></customer>
  <line sku="S" qty="1">1.00</line>
</order>
`)
	w.write("legacy/bad.xml", `<?xml version="1.0" encoding="UTF-8"?>
<order id="legacy-2">
  <line sku="S" qty="NOT_A_NUMBER">1.00</line>
</order>
`)
	w.conformance(ConformanceRule{Name: "legacy orders", FilePatterns: []string{"legacy/*.xml"}, SchemaType: "xml", SchemaRef: "schemas/order.xsd"})
	f := filtered(w.check(), "conformance")
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "bad.xml")
}

func TestXMLMixedContentAndAdoptionAreRefused(t *testing.T) {
	w := xmlWorkspace(t)
	w.write("orders/order-single.fixture.yaml", "data:\n  customer:\n    \"#text\": inline\n  line:\n    - \"@sku\": S\n      \"@qty\": 1\n      \"#text\": \"1.00\"\n")
	mustErrContain(t, w.expandErr(), "mixes #text with child elements")
	w.write("legacy/x.xml", "<order/>\n")
	_, err := w.adopter().Adopt("xml", "schemas/order.xsd", "legacy/*.xml", "adopted-orders", false)
	mustErrContain(t, err, "adopt")
}
