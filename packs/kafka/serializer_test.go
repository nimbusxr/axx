package kafka

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/iskorotkov/avro/v2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"

	"github.com/nimbusxr/axx/internal/avrojson"
)

// registering is a Schema Registry that registers every schema it is sent:
// a subject's schema gets the next ID, and the same schema again the same
// one. registered lists what was sent, as "<subject> <schema>".
func registering(t *testing.T) (ts *httptest.Server, registered func() []string) {
	t.Helper()
	var mu sync.Mutex
	var sent []string
	ids := map[string]int{}
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/subjects/"), "/versions")
		var body struct {
			Schema string `json:"schema"`
		}
		if r.Method != http.MethodPost || !ok || json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error_code":404,"message":"not found"}`))
			return
		}
		mu.Lock()
		key := subject + " " + body.Schema
		id, seen := ids[key]
		if !seen {
			id = 40 + len(ids)
			ids[key] = id
		}
		sent = append(sent, key)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.schemaregistry.v1+json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	}))
	t.Cleanup(ts.Close)
	return ts, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
}

// unwire splits the Confluent wire format into its schema ID and the value
// it decodes to with schema.
func unwire(t *testing.T, b []byte, schema string) (int, any) {
	t.Helper()
	if len(b) < 5 || b[0] != 0 {
		t.Fatalf("not the wire format: %v", b)
	}
	v, err := avrojson.DecodeBinary(avro.MustParse(schema), b[5:])
	if err != nil {
		t.Fatal(err)
	}
	return int(binary.BigEndian.Uint32(b[1:5])), v
}

func TestTheSerializerWritesAsThePublishStepsDo(t *testing.T) {
	const schema = `{"type": "record", "name": "Order", "namespace": "ex", "fields": [
	  {"name": "id", "type": "string"}, {"name": "count", "type": "int"}, {"name": "note", "type": ["null", "string"]}]}`
	const compact = `{"type":"record","name":"Order","namespace":"ex","fields":[{"name":"id","type":"string"},{"name":"count","type":"int"},{"name":"note","type":["null","string"]}]}`
	ts, registered := registering(t)
	h := newHarness(t, "")
	h.offline("space-kafka")
	h.must("an orders kafka topic client with the following properties:",
		[]string{"producer.key.serializer", avroSerializer},
		[]string{"producer.value.serializer", avroSerializer},
		[]string{"producer.schema.registry.url", ts.URL})
	svc, err := Context(h.sc).Service("space-kafka")
	if err != nil {
		t.Fatal(err)
	}
	z, err := svc.Serializer(h.sc, "orders")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	value, err := z.AvroValue(ctx, schema, map[string]any{"id": "o-1", "count": 3, "note": map[string]any{"string": "fragile"}})
	if err != nil {
		t.Fatal(err)
	}
	id, decoded := unwire(t, value, schema)
	want := map[string]any{"id": "o-1", "count": 3, "note": map[string]any{"string": "fragile"}}
	if id != 40 || !reflect.DeepEqual(decoded, want) {
		t.Errorf("value: schema %d, %#v", id, decoded)
	}
	fromJSON, err := z.AvroJSON(ctx, schema, []byte(`{"id": "o-1", "count": 3, "note": {"string": "fragile"}}`))
	if err != nil || !bytes.Equal(fromJSON, value) {
		t.Errorf("from JSON: %v %v, want %v", fromJSON, err, value)
	}
	if _, err := z.AvroJSON(ctx, schema, []byte(`{"id": "o-1", "count": 3, "note": "fragile"}`)); err == nil ||
		!strings.Contains(err.Error(), "the orders kafka event payload does not match schema ex.Order") || !strings.Contains(err.Error(), "set packs.kafka.lenientUnions") {
		t.Errorf("a bare union value: %v", err)
	}
	key, err := z.Key(ctx, "o-1")
	if err != nil {
		t.Fatal(err)
	}
	if id, k := unwire(t, key, `"string"`); id != 41 || k != "o-1" {
		t.Errorf("key: schema %d, %#v", id, k)
	}
	// Each schema is registered once, as the publish steps register it: its
	// JSON compacted, under the subjects TopicNameStrategy names.
	if got := registered(); !reflect.DeepEqual(got, []string{"orders-value " + compact, `orders-key "string"`}) {
		t.Errorf("registered: %q", got)
	}
	if _, err := z.AvroValue(ctx, `{"type": "nope"}`, nil); err == nil || !strings.Contains(err.Error(), "invalid Avro schema") {
		t.Errorf("a bad schema: %v", err)
	}
}

func TestATopicWithoutAvroSerializesText(t *testing.T) {
	h := newHarness(t, "")
	h.offline("space-kafka")
	svc, err := Context(h.sc).Service()
	if err != nil {
		t.Fatal(err)
	}
	z, err := svc.Serializer(h.sc, "audit")
	if err != nil {
		t.Fatal(err)
	}
	if k, err := z.Key(context.Background(), "a-1"); err != nil || string(k) != "a-1" {
		t.Errorf("key: %q %v", k, err)
	}
	if _, err := z.AvroValue(context.Background(), `"string"`, "x"); err == nil || !strings.Contains(err.Error(), "serializes values with StringSerializer") {
		t.Errorf("Avro without KafkaAvroSerializer: %v", err)
	}
}

func TestPacksProduceWithTheTopicClientsProducer(t *testing.T) {
	h := newHarness(t, "")
	h.offline("space-kafka")
	h.must("the orders kafka topic client")
	h.must("an audit kafka topic client with the following properties:",
		[]string{"producer.client.id", "stream"},
		[]string{"producer.acks", "1"},
		[]string{"producer.security.protocol", "SASL_PLAINTEXT"},
		[]string{"producer.sasl.jaas.config", `org.apache.kafka.common.security.plain.PlainLoginModule required username="u" password="p";`})
	svc, err := Context(h.sc).Service("space-kafka")
	if err != nil {
		t.Fatal(err)
	}
	orders := h.client("space-kafka", "orders")
	for _, topic := range []string{"orders", "no-client"} {
		cl, err := svc.Producer(h.sc, topic)
		if err != nil || cl != orders.prod {
			t.Errorf("%s: %p %v, want the orders client's producer %p", topic, cl, err, orders.prod)
		}
	}
	if cl, _ := svc.Producer(h.sc, "audit"); cl != h.client("space-kafka", "audit").prod {
		t.Error("audit: not its topic client's producer")
	}
	opts, err := svc.ProducerOpts(h.sc, "audit")
	if err != nil {
		t.Fatal(err)
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	mechanisms, _ := cl.OptValue(kgo.SASL).([]sasl.Mechanism)
	if cl.OptValue(kgo.ClientID) != "stream" || !reflect.DeepEqual(cl.OptValue(kgo.SeedBrokers), []string{"127.0.0.1:1"}) ||
		cl.OptValue(kgo.RequiredAcks) != kgo.LeaderAck() || len(mechanisms) != 1 || mechanisms[0].Name() != "PLAIN" {
		t.Errorf("options: client.id %v, brokers %v, acks %v, SASL %v", cl.OptValue(kgo.ClientID), cl.OptValue(kgo.SeedBrokers), cl.OptValue(kgo.RequiredAcks), mechanisms)
	}
}

func TestRecordHeadersSendNullAsText(t *testing.T) {
	v := "ParcelScanned"
	got := RecordHeaders([]Header{{Key: "X-Event-Type", Value: &v}, {Key: "X-Empty"}})
	want := []kgo.RecordHeader{{Key: "X-Event-Type", Value: []byte("ParcelScanned")}, {Key: "X-Empty", Value: []byte("null")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headers: %+v", got)
	}
	if RecordHeaders(nil) != nil {
		t.Error("no headers are nil")
	}
}
