package fixtures

import (
	"strings"
	"testing"
)

// Explain names the source a generated value comes from, the way there
// included: $refs, expressions and identities, then what the family fills.

func (w *workspace) explain(file, path string) *Explanation {
	w.t.Helper()
	ex, err := w.mustGenerator().Explain(w.path(file), path)
	if err != nil {
		w.t.Fatalf("explain %s %s: %v", file, path, err)
	}
	return ex
}

// origins renders an explanation's origins as kind | file | at | detail,
// outermost first.
func origins(ex *Explanation) string {
	var out []string
	for _, o := range ex.Origins {
		parts := []string{o.Kind}
		for _, p := range []string{o.File, o.At, o.Detail} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		out = append(out, strings.Join(parts, " | "))
	}
	return strings.Join(out, " > ")
}

type explainCase struct{ file, path, value, origins string }

func runExplain(t *testing.T, w *workspace, tests []explainCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.file+" "+tc.path, func(t *testing.T) {
			w.t = t
			ex := w.explain(tc.file, tc.path)
			expect(t, string(ex.Value), tc.value)
			expect(t, origins(ex), tc.origins)
		})
	}
}

func TestExplainAvroLayersDefaultsIdentitiesAndSchemaDefaults(t *testing.T) {
	const factory = orderPayments + "/order-payments.factory.yaml"
	runExplain(t, avroWorkspace(t), []explainCase{
		{orderPayments + "/order-captured.json", "status", `"CAPTURED"`, "fixture | " + orderPayments + "/order-captured.fixture.yaml | data.status"},
		{orderPayments + "/order-captured.json", "amount_cents", "12999", "prototype | " + orderPayments + "/order-payments.prototype.yaml | data.amount_cents"},
		{orderPayments + "/order-captured.json", "payments[0].method", `"CREDIT_CARD"`, "prototype | " + orderPayments + "/order-payments.prototype.yaml | data.payments[0].method"},
		{orderPayments + "/order-captured.json", "payments[0].channel", `"POS"`, "defaults | " + factory + " | channel"},
		{orderPayments + "/order-captured.json", "tender_type", `"CREDIT_CARD"`, "defaults | " + factory + " | tender_type"},
		{orderPayments + "/order-captured.json", "currency", `"USD"`, "schema | schemas/order-payment.avsc | the schema's default"},
		{orderPayments + "/order-captured.json", "captured_at", `"2026-07-01T12:00:00Z"`, "fixture | " + orderPayments + "/order-captured.fixture.yaml | data.captured_at"},
		{orderPayments + "/order-captured.json", "metadata.region", `"us-east"`, "fixture | " + orderPayments + "/order-captured.fixture.yaml | data.metadata.region"},
		{orderPayments + "/order-captured.json", "$.order_id", `"ord-order-captured"`, "identity | " + factory + ` | order_id | derived from the fixture key "order-captured" with the prefix "ord-"`},
		{orderPayments + "/order-captured.json", "payments[0].payment_id", `"pay-order-captured-primary"`, "identity | " + factory + ` | payments[0].payment_id | derived from the fixture key "order-captured" with the prefix "pay-" and the qualifier "primary"`},
	})
}

func TestExplainOverlaysAndSchemaDefaults(t *testing.T) {
	w := yamlWorkspace(t)
	w.write("configs/regional/service-configs.prototype.yaml", "data:\n  mode: shadow\n")
	w.write("configs/regional/eu-config.fixture.yaml", "factory: configs/service-configs\ndata:\n  timeouts:\n    read_ms: 700\n")
	runExplain(t, w, []explainCase{
		{"configs/regional/eu-config.yaml", "mode", `"shadow"`, "prototype-overlay | configs/regional/service-configs.prototype.yaml | data.mode"},
		{"configs/regional/eu-config.yaml", "port", "8080", "prototype | configs/service-configs.prototype.yaml | data.port"},
		{"configs/regional/eu-config.yaml", "timeouts.read_ms", "700", "fixture | configs/regional/eu-config.fixture.yaml | data.timeouts.read_ms"},
		{"configs/regional/eu-config.yaml", "timeouts.connect_ms", "250", "schema | schemas/app-config.schema.json | the schema's default"},
		{"configs/checkout-config.yaml", "tags[1]", `"tier-1"`, "fixture | configs/checkout-config.fixture.yaml | data.tags[1]"},
		{"configs/checkout-config.yaml", "mode", `"live"`, "prototype | configs/service-configs.prototype.yaml | data.mode"},
		{"configs/checkout-config.yaml", "service", `"svc-checkout-config"`, `identity | configs/service-configs.factory.yaml | service | derived from the fixture key "checkout-config" with the prefix "svc-"`},
	})
}

func TestExplainFollowsReferencesAndExpressions(t *testing.T) {
	w := refsWorkspace(t)
	w.write("orders/whole.fixture.yaml", "data:\n  orderId: ORD-3\n  customer:\n    $ref: ../shared/customer.fixture.yaml\n    tier: PLATINUM\n")
	w.write("orders/list.fixture.yaml", "data:\n  orderId: ORD-6\n  parties:\n    - $ref: ../shared/customer.fixture.yaml#/id\n    - LOYALTY\n  firstLine:\n    $ref: ../shared/customer.fixture.yaml#/address/lines/0\n")
	w.write("orders/self.fixture.yaml", "data:\n  orderId: ORD-7\n  displayId:\n    $ref: \"#/orderId\"\n")
	w.write("orders/computed.fixture.yaml", "data:\n  orderId: ORD-12345678\n  shortId: ${ last4(orderId) }\n")
	w.write("billing/invoices.factory.yaml", "factory:\n  family: json\n  schema: ../orders/order.schema.json\nfixtures:\n  inv-1:\n    orderId: INV-1\n    borrowed:\n      $ref: ../orders/orders.factory.yaml#/whole/customer/id\n")
	w.write("orders/derived.fixture.yaml", "data:\n  city: Leipzig\n")
	const shared = "fixture | shared/customer.fixture.yaml | "
	runExplain(t, w, []explainCase{
		{"orders/whole.json", "customer.tier", `"PLATINUM"`, "fixture | orders/whole.fixture.yaml | data.customer.tier"},
		{"orders/whole.json", "customer.address.city", `"Springfield"`, "reference | orders/whole.fixture.yaml | data.customer.$ref | ../shared/customer.fixture.yaml > " + shared + "data.address.city"},
		{"orders/list.json", "parties[0]", `"CUST-1"`, "reference | orders/list.fixture.yaml | data.parties[0].$ref | ../shared/customer.fixture.yaml#/id > " + shared + "data.id"},
		{"orders/list.json", "parties[1]", `"LOYALTY"`, "fixture | orders/list.fixture.yaml | data.parties[1]"},
		{"orders/list.json", "firstLine", `"100 Main St"`, "reference | orders/list.fixture.yaml | data.firstLine.$ref | ../shared/customer.fixture.yaml#/address/lines/0 > " + shared + "data.address.lines[0]"},
		{"orders/self.json", "displayId", `"ORD-7"`, "reference | orders/self.fixture.yaml | data.displayId.$ref | #/orderId > fixture | orders/self.fixture.yaml | data.orderId"},
		{"orders/computed.json", "shortId", `"5678"`, "expression | ${ last4(orderId) } > fixture | orders/computed.fixture.yaml | data.shortId"},
		{"billing/inv-1.json", "borrowed", `"CUST-1"`, "reference | billing/invoices.factory.yaml | fixtures.inv-1.borrowed.$ref | ../orders/orders.factory.yaml#/whole/customer/id > reference | orders/whole.fixture.yaml | data.customer.$ref | ../shared/customer.fixture.yaml > " + shared + "data.id"},
		{"orders/derived.json", "orderId", `"derived"`, `identity | orders/orders.factory.yaml | orderId | derived from the fixture key "derived"`},
		{"orders/whole.fixture.yaml", "orderId", `"ORD-3"`, "identity | orders/orders.factory.yaml | orderId | the value its sources set > fixture | orders/whole.fixture.yaml | data.orderId"},
	})
}

func TestExplainProtobufAnswersToBothNames(t *testing.T) {
	const protos = "events/order-events.prototype.yaml"
	runExplain(t, protobufWorkspace(t), []explainCase{
		{"events/order-authorized.json", "cardToken", `"tok-abc"`, "fixture | events/order-authorized.fixture.yaml | data.card_token"},
		{"events/order-authorized.json", "total_cents", `"12999"`, "fixture | events/order-authorized.fixture.yaml | data.total_cents"},
		{"events/order-authorized.json", "customer.loyaltyTier", `"gold"`, "prototype | " + protos + " | data.customer.loyalty_tier"},
		{"events/order-authorized.json", "items[0].sku", `"SKU-1"`, "prototype | " + protos + " | data.items[0].sku"},
		{"events/order-authorized.json", "orderId", `"ord-order-authorized"`, `identity | events/order-events.factory.yaml | order_id | derived from the fixture key "order-authorized" with the prefix "ord-"`},
	})
}

func TestExplainXMLAttributesAndDottedDefaults(t *testing.T) {
	w := xmlWorkspace(t)
	w.replace("orders/orders.factory.yaml", "identity:", "defaults:\n  customer.tier: silver\nidentity:")
	runExplain(t, w, []explainCase{
		{"orders/order-single.xml", "@channel", `"WEB"`, "prototype | orders/orders.prototype.yaml | data.@channel"},
		{"orders/order-single.xml", "line[0].@sku", `"SKU-1"`, "fixture | orders/order-single.fixture.yaml | data.line[0].@sku"},
		{"orders/order-single.xml", "customer.tier", `"silver"`, "defaults | orders/orders.factory.yaml | customer.tier"},
		{"orders/order-multi.xml", "customer.tier", `"gold"`, "fixture | orders/order-multi.fixture.yaml | data.customer.tier"},
		{"orders/order-single.xml", "@id", `"ord-order-single"`, `identity | orders/orders.factory.yaml | @id | derived from the fixture key "order-single" with the prefix "ord-"`},
	})
}

func TestExplainDatasetRows(t *testing.T) {
	w := datasetWorkspace(t)
	w.replace(missions+"/mission-seeds.factory.yaml", "identity:", "defaults:\n  space.missions.status: planned\nidentity:")
	const factory = missions + "/mission-seeds.factory.yaml"
	runExplain(t, w, []explainCase{
		{missions + "/mission-beta.yaml", "space.missions[0].status", `"launched"`, "fixture | " + missions + "/mission-beta.fixture.yaml | data.space.missions[0].status"},
		{missions + "/mission-beta.yaml", "space.missions[1].metadata", `"{\"priority\": \"low\"}"`, "prototype | " + missions + "/mission-seeds.prototype.yaml | data.space.missions.metadata"},
		{missions + "/mission-alpha.yaml", "space.missions[0].status", `"planned"`, "defaults | " + factory + " | space.missions.status"},
		{missions + "/mission-beta.yaml", "space.missions[0].id", `"msn-mission-beta"`, "identity | " + factory + ` | space.missions.id | derived from the fixture key "mission-beta" with the prefix "msn-"`},
		{missions + "/mission-beta.yaml", "space.missions[1].id", `"msn-mission-beta-2"`, "identity | " + factory + ` | space.missions.id | derived from the fixture key and row number "mission-beta-2" with the prefix "msn-"`},
		{missions + "/mission-beta.yaml", "SPACE.spacecraft[0].capacity", "4", "fixture | " + missions + "/mission-beta.fixture.yaml | data.space.spacecraft[0].capacity"},
	})
}

func TestExplainRefusesWhatHoldsNoSingleValue(t *testing.T) {
	w := yamlWorkspace(t)
	g := w.mustGenerator()
	for _, tc := range []struct{ file, path, want string }{
		{"configs/checkout-config.yaml", "timeouts", "timeouts is an object in configs/checkout-config.yaml: explain one of its fields (connect_ms, read_ms)"},
		{"configs/checkout-config.yaml", "timeouts.write_ms", "configs/checkout-config.yaml has no value at timeouts.write_ms (its fields: service, port, mode, timeouts, features, tags)"},
		{"configs/checkout-config.yaml", "tags[..", "'tags[..' is not a path"},
		{"configs/service-configs.factory.yaml", "port", "configs/service-configs.factory.yaml is not a file the fixture factory generates"},
		{"legacy/payments-config.yaml", "port", "legacy/payments-config.yaml is not a file the fixture factory generates"},
	} {
		_, err := g.Explain(w.path(tc.file), tc.path)
		if err == nil || codeOf(err) != CodeExplain {
			t.Fatalf("%s %s: %v", tc.file, tc.path, err)
		}
		mustContain(t, err.Error(), tc.want)
	}
	dw := datasetWorkspace(t)
	_, err := dw.mustGenerator().Explain(dw.path(missions+"/mission-beta.yaml"), "status")
	mustErrContain(t, err, "fixture mission-beta is a dataset: give <table>[<row>].<column>, like space.missions[0].<column>")
}
