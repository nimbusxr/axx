package fixtures

import (
	"testing"
)

// The $ref contract: a node holding a reference is replaced by what it
// points at; paths are relative to the referring document (a leading slash
// means the resource root), the fragment is a JSON Pointer into the target's
// RESOLVED values, siblings layer over what comes back, and a cycle fails by
// name.

func refsWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.opts = Options{Functions: []Function{last4}}
	w.write("orders/orders.factory.yaml", "factory:\n  family: json\n  schema: order.schema.json\nidentity:\n  - path: orderId\n")
	w.write("orders/order.schema.json", `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "orderId": { "type": "string" },
    "customerId": { "type": "string" },
    "displayId": { "type": "string" },
    "city": { "type": "string" },
    "firstLine": { "type": "string" },
    "borrowed": { "type": "string" },
    "from": { "type": "string" },
    "nope": { "type": "string" },
    "id": { "type": "string" },
    "tier": { "type": "string" },
    "shortId": { "type": "string" },
    "parties": { "type": "array", "items": { "type": "string" } },
    "address": { "type": "object" },
    "customer": { "type": "object" }
  }
}`)
	w.write("shared/customer.fixture.yaml", `factory: orders/orders
data:
  id: CUST-1
  tier: GOLD
  address:
    city: Springfield
    lines:
      - 100 Main St
      - Suite 4
`)
	return w
}

func (w *workspace) generated(rel string) any {
	w.t.Helper()
	w.generate()
	return jsonOf(w.t, []byte(w.read(rel)))
}

func TestRefs(t *testing.T) {
	tests := []struct {
		name, fixture, body string
		check               func(t *testing.T, doc any)
	}{
		{
			"pointer takes one value", "orders/basic", "orderId: ORD-1\n  customerId:\n    $ref: ../shared/customer.fixture.yaml#/id\n",
			func(t *testing.T, d any) { expect(t, str(at(d, "customerId")), "CUST-1") },
		},
		{
			"pointer walks objects and indices", "orders/nested",
			"orderId: ORD-2\n  city:\n    $ref: ../shared/customer.fixture.yaml#/address/city\n  firstLine:\n    $ref: ../shared/customer.fixture.yaml#/address/lines/0\n",
			func(t *testing.T, d any) {
				expect(t, str(at(d, "city")), "Springfield")
				expect(t, str(at(d, "firstLine")), "100 Main St")
			},
		},
		{
			"no pointer takes the whole document", "orders/whole", "orderId: ORD-3\n  customer:\n    $ref: ../shared/customer.fixture.yaml\n",
			func(t *testing.T, d any) {
				expect(t, str(at(d, "customer", "id")), "CUST-1")
				expect(t, str(at(d, "customer", "address", "city")), "Springfield")
			},
		},
		{
			"siblings layer over the reference", "orders/override", "orderId: ORD-4\n  customer:\n    $ref: ../shared/customer.fixture.yaml\n    tier: PLATINUM\n",
			func(t *testing.T, d any) {
				expect(t, str(at(d, "customer", "tier")), "PLATINUM")
				expect(t, str(at(d, "customer", "id")), "CUST-1")
			},
		},
		{
			"absolute paths are root-relative", "orders/absolute", "orderId: ORD-5\n  customerId:\n    $ref: /shared/customer.fixture.yaml#/id\n",
			func(t *testing.T, d any) { expect(t, str(at(d, "customerId")), "CUST-1") },
		},
		{
			"references inside arrays", "orders/list", "orderId: ORD-6\n  parties:\n    - $ref: ../shared/customer.fixture.yaml#/id\n    - LOYALTY\n",
			func(t *testing.T, d any) {
				expect(t, str(at(d, "parties", 0)), "CUST-1")
				expect(t, str(at(d, "parties", 1)), "LOYALTY")
			},
		},
		{
			"same-document reference sees the finished document", "orders/self", "orderId: ORD-7\n  displayId:\n    $ref: \"#/orderId\"\n",
			func(t *testing.T, d any) { expect(t, str(at(d, "displayId")), "ORD-7") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := refsWorkspace(t)
			w.write(tc.fixture+".fixture.yaml", "data:\n  "+tc.body)
			tc.check(t, w.generated(tc.fixture+".json"))
		})
	}
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRefErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		parts []string
	}{
		{
			"pointer leading nowhere",
			map[string]string{"orders/missing": "orderId: ORD-8\n  nope:\n    $ref: ../shared/customer.fixture.yaml#/does/not/exist\n"},
			[]string{"resolved to nothing"},
		},
		{
			"unknown document",
			map[string]string{"orders/unknown": "orderId: ORD-9\n  nope:\n    $ref: ../shared/nobody.fixture.yaml#/id\n"},
			[]string{"names no document", "customer.fixture.yaml"},
		},
		{"cycle", map[string]string{
			"orders/ping": "orderId: ORD-10\n  from:\n    $ref: ./pong.fixture.yaml#/orderId\n",
			"orders/pong": "orderId: ORD-11\n  from:\n    $ref: ./ping.fixture.yaml#/orderId\n",
		}, []string{"cycle"}},
		{
			"value with siblings",
			map[string]string{"orders/scalar": "orderId: ORD-12\n  customer:\n    $ref: ../shared/customer.fixture.yaml#/id\n    tier: X\n"},
			[]string{"resolved to a value, so it cannot carry [tier]"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := refsWorkspace(t)
			for f, body := range tc.files {
				w.write(f+".fixture.yaml", "data:\n  "+body)
			}
			mustErrContain(t, w.generateErr(), tc.parts...)
		})
	}
}

func TestRefSeesTheTargetsResolvedValues(t *testing.T) {
	w := refsWorkspace(t)
	w.write("shared/computed.fixture.yaml", "factory: orders/orders\ndata:\n  id: CUST-2\n  shortId: ${ last4(id) }\n")
	w.write("orders/resolved.fixture.yaml", "data:\n  orderId: ORD-12\n  borrowed:\n    $ref: ../shared/computed.fixture.yaml#/shortId\n")
	expect(t, str(at(w.generated("orders/resolved.json"), "borrowed")), "ST-2")
}

func TestMetadataRidesBesideTheValues(t *testing.T) {
	w := refsWorkspace(t)
	w.write("orders/withMeta.fixture.yaml", "data:\n  orderId: ORD-13\nmetadata:\n  key:\n    $ref: ../shared/customer.fixture.yaml#/id\n")
	doc := w.generated("orders/withMeta.json")
	if at(doc, "key") != nil {
		t.Error("metadata is not part of the record")
	}
}

func TestJSONPointerAndNormalize(t *testing.T) {
	doc := newObject("a/b", newObject("~x", []any{"zero", "one"}))
	expect(t, str(jsonPointer(doc, "/a~1b/~0x/1")), "one")
	if jsonPointer(doc, "/a~1b/~0x/2") != nil || jsonPointer(doc, "/missing") != nil {
		t.Error("pointers leading nowhere are nil")
	}
	for in, want := range map[[2]string]string{
		{"orders", "../shared/c.yaml"}: "shared/c.yaml",
		{"orders", "/shared/c.yaml"}:   "shared/c.yaml",
		{"", "./a/../b.yaml"}:          "b.yaml",
		{"", "../../x.yaml"}:           "../../x.yaml",
		{"a/b", "../../../x.yaml"}:     "../x.yaml",
	} {
		expect(t, normalizeRef(in[0], in[1]), want)
	}
}
