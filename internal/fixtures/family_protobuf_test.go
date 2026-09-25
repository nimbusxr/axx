package fixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// The protobuf family: descriptor-set and .proto schema refs, canonical
// proto-JSON emission (JSON names, int64 strings, enum names, maps, oneofs,
// well-known passthrough), the proto-JSON oracle, conformance, lint json-name
// rebasing and adoption.

func scalarField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name: proto.String(name), Number: proto.Int32(number), Type: typ.Enum(),
		Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// ordersDescriptorSet is axx.test.OrderEvent as protoc --descriptor_set_out
// would write it.
func ordersDescriptorSet() []byte {
	str, i32, i64, dbl, enm, msg, byt := descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, descriptorpb.FieldDescriptorProto_TYPE_ENUM,
		descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, descriptorpb.FieldDescriptorProto_TYPE_BYTES
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	with := func(f *descriptorpb.FieldDescriptorProto, edit func(*descriptorpb.FieldDescriptorProto)) *descriptorpb.FieldDescriptorProto {
		edit(f)
		return f
	}
	file := &descriptorpb.FileDescriptorProto{
		Name: proto.String("orders.proto"), Package: proto.String("axx.test"), Syntax: proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Customer"), Field: []*descriptorpb.FieldDescriptorProto{scalarField("id", 1, str), scalarField("loyalty_tier", 2, str)}},
			{Name: proto.String("LineItem"), Field: []*descriptorpb.FieldDescriptorProto{scalarField("sku", 1, str), scalarField("quantity", 2, i32), scalarField("price", 3, dbl)}},
			{
				Name: proto.String("OrderEvent"),
				Field: []*descriptorpb.FieldDescriptorProto{
					scalarField("order_id", 1, str),
					with(scalarField("status", 2, enm), func(f *descriptorpb.FieldDescriptorProto) { f.TypeName = proto.String(".axx.test.Status") }),
					scalarField("total_cents", 3, i64),
					with(scalarField("customer", 4, msg), func(f *descriptorpb.FieldDescriptorProto) { f.TypeName = proto.String(".axx.test.Customer") }),
					with(scalarField("items", 5, msg), func(f *descriptorpb.FieldDescriptorProto) {
						f.TypeName, f.Label = proto.String(".axx.test.LineItem"), repeated
					}),
					with(scalarField("attributes", 6, msg), func(f *descriptorpb.FieldDescriptorProto) {
						f.TypeName, f.Label = proto.String(".axx.test.OrderEvent.AttributesEntry"), repeated
					}),
					with(scalarField("card_token", 7, str), func(f *descriptorpb.FieldDescriptorProto) { f.OneofIndex = proto.Int32(0) }),
					with(scalarField("gift_card_id", 8, str), func(f *descriptorpb.FieldDescriptorProto) { f.OneofIndex = proto.Int32(0) }),
					scalarField("signature", 9, byt),
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("tender")}},
				NestedType: []*descriptorpb.DescriptorProto{{
					Name:    proto.String("AttributesEntry"),
					Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
					Field:   []*descriptorpb.FieldDescriptorProto{scalarField("key", 1, str), scalarField("value", 2, str)},
				}},
			},
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("AUTHORIZED"), Number: proto.Int32(1)},
				{Name: proto.String("CAPTURED"), Number: proto.Int32(2)},
			},
		}},
	}
	b, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}})
	if err != nil {
		panic(err)
	}
	return b
}

// ordersProto is the same schema as a .proto file.
const ordersProto = `syntax = "proto3";
package axx.test;

message Customer {
  string id = 1;
  string loyalty_tier = 2;
}

message LineItem {
  string sku = 1;
  int32 quantity = 2;
  double price = 3;
}

enum Status {
  STATUS_UNSPECIFIED = 0;
  AUTHORIZED = 1;
  CAPTURED = 2;
}

message OrderEvent {
  string order_id = 1;
  Status status = 2;
  int64 total_cents = 3;
  Customer customer = 4;
  repeated LineItem items = 5;
  map<string, string> attributes = 6;
  oneof tender {
    string card_token = 7;
    string gift_card_id = 8;
  }
  bytes signature = 9;
}
`

func protobufWorkspace(t *testing.T) *workspace {
	t.Helper()
	return newWorkspace(t).seed("protobuf-corpus")
}

// The corpus carries the descriptor set as protoc would write it, so the
// expected outputs come from the very same bytes.
func TestOrdersDescriptorSetIsCommitted(t *testing.T) {
	path := filepath.Join(corpusDir(), "protobuf-corpus", "schemas", "orders.desc")
	if *update {
		if err := os.WriteFile(path, ordersDescriptorSet(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var a, b descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(committed, &a); err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(ordersDescriptorSet(), &b); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&a, &b) {
		t.Error("testdata orders.desc is stale; run go test -run TestOrdersDescriptorSetIsCommitted -update")
	}
}

func TestProtobufCanonicalProtoJSON(t *testing.T) {
	files := protobufWorkspace(t).expand()
	a := jsonOf(t, files["events/order-authorized.json"])
	for path, want := range map[string]string{"orderId": "ord-order-authorized", "status": "AUTHORIZED", "totalCents": "12999", "cardToken": "tok-abc"} {
		expect(t, str(at(a, path)), want)
	}
	if _, isString := at(a, "totalCents").(string); !isString {
		t.Error("int64 must be a JSON string")
	}
	expect(t, str(at(a, "customer", "loyaltyTier")), "gold")
	expect(t, str(at(a, "items", 0, "sku")), "SKU-1")
	expect(t, str(at(a, "items", 0, "price")), "9.99")
	expect(t, str(at(a, "attributes", "channel")), "WEB")
	if a.Has("giftCardId") || a.Has("signature") {
		t.Error("unset oneof members and unresolved fields are omitted")
	}
	c := jsonOf(t, files["events/order-captured.json"])
	expect(t, str(at(c, "status")), "CAPTURED")
	expect(t, str(at(c, "signature")), "c2lnbmVk")
	body := string(files["events/order-authorized.json"])
	if strings.Index(body, "orderId") > strings.Index(body, "status") || strings.Index(body, "totalCents") > strings.Index(body, "customer") {
		t.Errorf("descriptor order:\n%s", body)
	}
}

func TestProtobufProtoSourceGovernsLikeTheDescriptorSet(t *testing.T) {
	w := protobufWorkspace(t)
	fromDesc := w.expand()
	w.write("schemas/orders.proto", ordersProto)
	w.replace("events/order-events.factory.yaml", "../schemas/orders.desc#axx.test.OrderEvent", "../schemas/orders.proto#axx.test.OrderEvent")
	fromProto := w.expand()
	for _, p := range []string{"events/order-authorized.json", "events/order-captured.json"} {
		if !bytes.Equal(fromDesc[p], fromProto[p]) {
			t.Errorf("%s differs:\n%s\n%s", p, fromDesc[p], fromProto[p])
		}
	}
}

func TestProtobufWellKnownTypesFromStandardImports(t *testing.T) {
	w := newWorkspace(t)
	w.write("types/api-types.factory.yaml", `factory:
  family: protobuf
  schema: ../google/protobuf/type.proto#google.protobuf.Type
fixtures:
  string-type:
    name: axx.StringType
    syntax: SYNTAX_PROTO3
`)
	doc := jsonOf(t, w.expand()["types/string-type.json"])
	expect(t, str(at(doc, "name")), "axx.StringType")
	expect(t, str(at(doc, "syntax")), "SYNTAX_PROTO3")
}

func TestProtobufWildcardDefaultsFillRepeatedMessages(t *testing.T) {
	w := protobufWorkspace(t)
	w.replace("events/order-events.prototype.yaml", "      price: 9.99\n", "")
	w.replace("events/order-events.factory.yaml", "identity:", "defaults:\n  items[].price: 4.99\nidentity:")
	expect(t, str(at(jsonOf(t, w.expand()["events/order-authorized.json"]), "items", 0, "price")), "4.99")
}

func TestProtobufErrors(t *testing.T) {
	tests := []struct {
		name, old, new string
		parts          []string
	}{
		{"enum", "status: CAPTURED", "status: EXPLODED", []string{"EXPLODED", "axx.test.Status", "AUTHORIZED"}},
		{"typo", "total_cents:", "totale_cents:", []string{"totale_cents", "axx.test.OrderEvent"}},
		{"oneof", "gift_card_id: gc-9", "gift_card_id: gc-9\n  card_token: t-1", []string{"oneof 'tender'"}},
		{"null", "signature: c2lnbmVk", "signature: null", []string{"omit the field"}},
		{"both name forms", "total_cents: 500", "total_cents: 500\n  totalCents: 6", []string{"same protobuf field"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := protobufWorkspace(t)
			w.replace("events/order-captured.fixture.yaml", tc.old, tc.new)
			mustErrContain(t, w.expandErr(), tc.parts...)
		})
	}
	w := protobufWorkspace(t)
	w.replace("events/order-events.factory.yaml", "../schemas/orders.desc#axx.test.OrderEvent", "class:com.example.OrderEvent")
	mustErrContain(t, w.expandErr(), "class:", ".proto")
}

func TestProtobufLifecycleConformanceAndLintRules(t *testing.T) {
	w := protobufWorkspace(t)
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("events/order-captured.json", "CAPTURED", "AUTHORIZED")
	if f := w.check(); len(f) != 1 || !strings.Contains(f[0], "FIXTURE DRIFT") {
		t.Fatalf("failures: %v", f)
	}
	rules := string(w.expand()[DefaultLintOutput])
	mustContain(t, rules, "jsonPath: orderId", "events/*.json")
	if strings.Contains(rules, "jsonPath: order_id") {
		t.Error("identity paths are rebased to JSON names")
	}

	w = protobufWorkspace(t)
	w.write("legacy/good.json", `{"orderId": "legacy-1", "status": "CAPTURED", "totalCents": "100"}`)
	w.write("legacy/bad.json", `{"orderId": "legacy-2", "status": "NOT_A_STATUS"}`)
	w.conformance(ConformanceRule{Name: "legacy order events", FilePatterns: []string{"legacy/*.json"}, SchemaType: "protobuf", SchemaRef: "schemas/orders.desc#axx.test.OrderEvent"})
	f := filtered(w.check(), "conformance")
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "bad.json", "axx.test.OrderEvent")
}

func TestProtobufAdoptionRoundTrips(t *testing.T) {
	w := protobufWorkspace(t)
	w.write("ingest/evt-a.json", `{"orderId": "evt-a-1", "status": "CAPTURED", "totalCents": "100",
 "customer": {"id": "c-9", "loyaltyTier": "gold"}}`)
	w.write("ingest/evt-b.json", `{"orderId": "evt-b-7", "status": "CAPTURED", "totalCents": "250",
 "customer": {"id": "c-9", "loyaltyTier": "gold"}}`)
	res, err := w.adopter().Adopt("protobuf", "schemas/orders.desc#axx.test.OrderEvent", "ingest/*.json", "ingested-events", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "decoded 2/2 files (family: protobuf)", "deep-equal")
	a, _ := parseJSON([]byte(w.read("ingest/evt-a.json")), "a")
	b, _ := parseJSON(w.expand()["ingest/evt-a.json"], "b")
	if !deepEquals(a, b) {
		t.Errorf("regenerated evt-a differs")
	}
}
