package fixtures

import (
	"testing"
)

// A factory's JSON Schema may reference definitions in local files: a path
// relative to the schema holding the $ref, with or without a fragment.
// Remote schemas are not fetched.

func localRefsWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.write("schemas/shipment.schema.json", `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Shipment",
  "type": "object",
  "required": ["parcelId", "to", "price"],
  "properties": {
    "parcelId": {"type": "string"},
    "to": {"$ref": "address.schema.json"},
    "price": {"$ref": "common/money.schema.json#/$defs/Money"}
  }
}`)
	w.write("schemas/address.schema.json", `{"type": "object", "required": ["city"], "properties": {"city": {"type": "string"}, "postcode": {"type": "string"}}}`)
	w.write("schemas/common/money.schema.json", `{
  "$defs": {
    "Money": {"type": "object", "required": ["amount", "currency"], "properties": {"amount": {"type": "number"}, "currency": {"$ref": "#/$defs/Currency"}}},
    "Currency": {"type": "string", "enum": ["EUR", "USD"]}
  }
}`)
	w.write("shipments/shipment.factory.yaml", "factory:\n  family: json\n  schema: ../schemas/shipment.schema.json\n")
	w.write("shipments/express-berlin.fixture.yaml", "data:\n  parcelId: PX-4101\n  to: {city: Berlin, postcode: \"10115\"}\n  price: {amount: 19.9, currency: EUR}\n")
	return w
}

func TestSchemaRefsToLocalFiles(t *testing.T) {
	w := localRefsWorkspace(t)
	doc := jsonOf(t, w.expand()["shipments/express-berlin.json"])
	expect(t, str(at(doc, "to", "city")), "Berlin")
	expect(t, str(at(doc, "price", "currency")), "EUR")
}

func TestSchemaRefsToLocalFilesValidate(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(w *workspace)
		parts []string
	}{
		{"a value a referenced definition refuses", func(w *workspace) {
			w.replace("shipments/express-berlin.fixture.yaml", "currency: EUR", "currency: GBP")
		}, []string{"express-berlin", "GBP", "money.schema.json"}},
		{"a field a referenced file requires", func(w *workspace) {
			w.replace("shipments/express-berlin.fixture.yaml", "to: {city: Berlin, postcode: \"10115\"}", "to: {postcode: \"10115\"}")
		}, []string{"express-berlin", "city"}},
		{"a file that is not there", func(w *workspace) {
			w.replace("schemas/shipment.schema.json", `"address.schema.json"`, `"adress.schema.json"`)
		}, []string{"shipment.schema.json", "adress.schema.json", "schemas/adress.schema.json"}},
		{"a remote schema", func(w *workspace) {
			w.replace("schemas/shipment.schema.json", `"address.schema.json"`, `"https://example.com/address.schema.json"`)
		}, []string{"shipment.schema.json", "https://example.com/address.schema.json", "not fetched"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := localRefsWorkspace(t)
			tc.edit(w)
			mustErrContain(t, w.expandErr(), tc.parts...)
		})
	}
}

func TestSchemaRefsBetweenLocalFilesThatReferenceEachOther(t *testing.T) {
	w := localRefsWorkspace(t)
	w.write("schemas/address.schema.json", `{"type": "object", "required": ["city"], "properties": {"city": {"type": "string"}, "postcode": {"type": "string"}, "returnTo": {"$ref": "address.schema.json"}}}`)
	doc := jsonOf(t, w.expand()["shipments/express-berlin.json"])
	expect(t, str(at(doc, "to", "city")), "Berlin")
}
