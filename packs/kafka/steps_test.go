package kafka

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

func TestServiceAndClientRegistration(t *testing.T) {
	h := newHarness(t, "")
	h.fails("the order-events kafka topic client", "No kafka service set")
	h.fails("the svc kafka service with the following properties:", `Property "brokers" is required`, []string{"url", "x"})
	h.offline("space-kafka")
	h.fails("the space-kafka kafka service with the following properties:", "already set", []string{"brokers", "x:1"})
	h.must("the order-events kafka topic client")
	h.fails("the order-events kafka topic client", "Kafka topic client already exists: order-events")
	h.fails("an audit kafka topic client on the nowhere kafka service with the following properties:", `Kafka service "nowhere" not set`,
		[]string{"producer.acks", "1"})
	h.must("an audit kafka topic client on the space-kafka kafka service with the following properties:",
		[]string{"producer.acks", "1"},
		[]string{"consumer.group.id", "legacy-group"},
		[]string{"schema.registry.url", "http://localhost:8081"},
		[]string{"producer.linger.msec", "5"})
	logs := h.sink.all()
	for _, want := range []string{
		`property "schema.registry.url" is ignored: prefix it with producer. or consumer.`,
		"note: consumer.group.id has no effect in axx",
		`warning: unknown producer property "linger.msec" is ignored`,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs should mention %q:\n%s", want, logs)
		}
	}
	tc := h.client("space-kafka", "audit")
	if tc.producer.Acks != "1" || tc.producer.Idempotent == nil || *tc.producer.Idempotent {
		t.Errorf("acks=1 must disable idempotence like Java: %+v", tc.producer)
	}
	h.fails("a bad kafka topic client with the following properties:", "cannot run in axx",
		[]string{"producer.value.serializer", "io.confluent.kafka.serializers.protobuf.KafkaProtobufSerializer"})
	h.fails("a bad kafka topic client with the following properties:", "needs consumer.schema.registry.url",
		[]string{"consumer.value.deserializer", avroDeserializer})
}

func TestDraftsAndOrdinals(t *testing.T) {
	h := newHarness(t, "")
	h.offline("space-kafka")
	h.must("the orders kafka topic client")
	h.fails("the orders kafka event key is k1", "Kafka event not found: orders")
	h.fails("a 2nd ordered orders kafka event", "Ordinals must be sequential: 2")
	h.must("a 1st ordered orders kafka event")
	h.must("a 2nd ordered orders kafka event")
	h.fails("a 1st ordered orders kafka event", "Ordinal Kafka event already exists: 1")
	h.fails("a 4th ordered orders kafka event", "Ordinals must be sequential: 4")

	// The ordinal loophole: an ordinal equal to the number of events
	// appends one more, with a warning.
	h.must("a 2nd ordered orders kafka event")
	if !strings.Contains(h.sink.all(), "a 2nd ordered orders kafka event already exists, so this creates the 3rd; axx 1.0 will reject this") {
		t.Errorf("the ordinal loophole must be logged as a warning:\n%s", h.sink.all())
	}
	tc := h.client("space-kafka", "orders")
	if len(tc.events) != 3 {
		t.Fatalf("events: %d", len(tc.events))
	}
	h.must("an orders kafka event") // non-ordinal events always append
	if len(tc.events) != 4 {
		t.Fatalf("events: %d", len(tc.events))
	}

	h.must("the orders kafka event key is first-key")
	h.must("the 2nd ordered orders kafka event key is second-key on the space-kafka kafka service")
	h.fails("the 5th ordered orders kafka event key is x", "Kafka event not found: 5")
	if *tc.events[0].Key != "first-key" || *tc.events[1].Key != "second-key" || tc.events[2].Key != nil {
		t.Errorf("keys: %v %v %v", tc.events[0].Key, tc.events[1].Key, tc.events[2].Key)
	}

	h.must("the orders kafka event headers are:", []string{"trace", "t1"}, []string{"empty", ""})
	h.must("the orders kafka event headers on the space-kafka kafka service are:", []string{"trace", "t2"}, []string{"source", "svc"})
	var got []string
	for _, hd := range tc.events[0].Headers {
		got = append(got, hd.Key+"="+*hd.Value)
	}
	if strings.Join(got, ",") != "trace=t2,empty=null,source=svc" {
		t.Errorf("headers: %v", got)
	}
}

func TestPayloadSteps(t *testing.T) {
	h := newHarness(t, "")
	h.offline("space-kafka")
	h.must("the first kafka topic client")
	h.must("the second kafka topic client")
	h.must("a first kafka event")
	h.must("a second kafka event")
	h.must("a 2nd ordered second kafka event")
	h.file("kafka/order.json", `{"id": "o-1", "count": 3, "customer": {"name": "Ann", "email": "a@x"}, "tags": []}`)

	first, second := h.client("space-kafka", "first"), h.client("space-kafka", "second")
	if first.events[0].Payload != "{}" {
		t.Fatalf("a new event's payload is {}: %q", first.events[0].Payload)
	}
	// A property must already exist in the payload.
	h.fails("the first kafka event payload properties are:", "Property not found in Kafka event payload: $.name does not exist",
		[]string{"$.name", "x"})
	h.fails("the first kafka event payload is a kafka/missing.json resource", "Error reading file")
	h.must("the first kafka event payload is a kafka/order.json resource")
	h.must("the 2nd ordered second kafka event payload is a kafka/order.json resource on the space-kafka kafka service")

	// Values are always set as strings; an empty cell sets null.
	h.must("the first kafka event payload properties are:",
		[]string{"$.id", "o-2"}, []string{"$.count", "5"}, []string{"customer.name", ""})
	doc, _ := jsonx.Parse(first.events[0].Payload)
	for path, want := range map[string]any{"$.id": "o-2", "$.count": "5", "$.customer.name": nil} {
		v, _, err := jsonx.Read(doc, path)
		if err != nil || !jsonx.JavaEquals(v, want) {
			t.Errorf("%s = %#v (%v), want %#v", path, v, err, want)
		}
	}
	h.must("the first kafka event payload property $.customer.email is null")
	if !strings.Contains(first.events[0].Payload, `"email":null`) {
		t.Errorf("payload: %s", first.events[0].Payload)
	}
	h.fails("the first kafka event payload property $.nope is null", "Property not found in Kafka event payload")

	// "the kafka event payload properties" use the service's first topic client.
	h.must("the kafka event payload properties are:", []string{"$.id", "via-first"})
	if !strings.Contains(first.events[0].Payload, `"id":"via-first"`) {
		t.Errorf("the first topic client's first event: %s", first.events[0].Payload)
	}
	h.fails("the 2nd ordered kafka event payload properties are:", "Kafka event not found: 2", []string{"$.id", "x"})
	h.must("the 2nd ordered second kafka event payload properties are:", []string{"$.id", "second-2"})
	if !strings.Contains(second.events[1].Payload, `"id":"second-2"`) || second.events[0].Payload != "{}" {
		t.Errorf("second topic: %q / %q", second.events[0].Payload, second.events[1].Payload)
	}

	h.file("plain.txt", "plain text")
	h.must("the second kafka event payload is a plain.txt resource")
	if second.events[0].Payload != "plain text" {
		t.Errorf("resource payload: %q", second.events[0].Payload)
	}
	// json-smart reads plain text as a JSON string, as Jayway did.
	h.fails("the second kafka event payload properties are:", "This is not a json object", []string{"$.a", "b"})
}

func TestPublishValidation(t *testing.T) {
	h := newHarness(t, "")
	h.offline("space-kafka")
	h.must("an avro kafka topic client with the following properties:",
		[]string{"producer.value.serializer", avroSerializer},
		[]string{"producer.schema.registry.url", "http://127.0.0.1:1"})
	h.must("the plain kafka topic client")
	h.must("an avro kafka event")
	h.must("a plain kafka event")
	h.fails("the plain kafka event is published using schema schemas/x.json", "Supported schema types are .avsc")
	h.fails("the plain kafka event is published using schema schemas/x.avsc", "publishing with a schema needs producer.value.serializer")
	h.fails("the avro kafka event is published using schema schemas/x.avsc", "Schema file not found: schemas/x.avsc")
	h.file("schemas/m.avsc", `{"type":"record","name":"M","namespace":"ex","fields":[{"name":"id","type":"string"},{"name":"note","type":["null","string"]}]}`)
	h.file("m5.json", `{"id": 5, "note": null}`)
	h.must("the avro kafka event payload is a m5.json resource")
	h.fails("the avro kafka event is published using schema schemas/m.avsc", "does not match schema schemas/m.avsc: $.id: expected string, got number 5")
	h.file("ma.json", `{"id": "a", "note": "n"}`)
	h.must("the avro kafka event payload is a ma.json resource")
	h.fails("the avro kafka event is published using schema schemas/m.avsc", "set packs.kafka.lenientUnions")
	h.fails("the avro kafka event is published", "publish with \"is published using schema")
}
