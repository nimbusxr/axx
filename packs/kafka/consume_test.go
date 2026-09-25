package kafka

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/avrojson"
)

// fakeTopic swaps a topic client's tailer for an in-memory one.
func fakeTopic(tc *TopicClient) *tailer {
	t := &tailer{topic: tc.Topic, wake: make(chan struct{})}
	tc.tailer = t
	return t
}

func rec(key, value string, headers ...string) *kgo.Record {
	r := &kgo.Record{Topic: "t", Value: []byte(value), Timestamp: time.Unix(1700000000, 0)}
	if key != "" {
		r.Key = []byte(key)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Headers = append(r.Headers, kgo.RecordHeader{Key: headers[i], Value: []byte(headers[i+1])})
	}
	return r
}

func TestConsumerAssertions(t *testing.T) {
	h := newHarness(t, `{"timeout": "300ms"}`)
	h.offline("space-kafka")
	h.must("the orders kafka topic client")
	tc := h.client("space-kafka", "orders")
	topic := fakeTopic(tc)
	for i, r := range []*kgo.Record{
		rec("k1", `{"id":"o-1","count":3,"ratio":0.5,"ok":true,"none":null,"tags":["a"]}`, "source", "svc", "trace", "t-1"),
		rec("k2", `{"id":"o-2","count":4}`, "source", "svc", "source", "svc"),
		rec("k3", `not json`, "source", "other"),
	} {
		r.Offset = int64(i)
		topic.add([]*kgo.Record{r}, nil)
	}

	h.must("the orders kafka event named first key is k1")
	h.must("the orders kafka event named first payload properties are:",
		[]string{"$.id", "o-1"}, []string{"count", "3"}, []string{"$.ratio", ".5"}, []string{"$.ok", "TRUE"},
		[]string{"$.none", "null"}, []string{"$.tags", `["a"]`})
	h.must("the orders kafka event named first headers are:", []string{"source", "svc"}, []string{"trace", "t-1"})
	h.must("the orders kafka event named first headers match:", []string{"trace", `t-\d`})

	// Matchers accumulate under a label: the key alone matches k2, the
	// payload alone matches o-1, together nothing does.
	h.must("the orders kafka event named mixed key is k2")
	err := h.run("the orders kafka event named mixed payload properties are:", []string{"$.id", "o-1"})
	var ae *core.AssertionError
	if !errors.As(err, &ae) || !strings.Contains(ae.Message, "No records found in topic orders matching the kafka event named mixed") {
		t.Fatalf("accumulated matchers must all hold on one record: %v", err)
	}
	if got := strings.Join(ae.Expected.([]string), "; "); got != `key == "k2"; $.id == o-1` {
		t.Errorf("expected: %s", got)
	}

	// Types must match: 3 is an Integer, 3.0 a Double.
	h.fails("the orders kafka event named typed payload properties are:", "No records found", []string{"$.count", "3.0"})
	// A value that cannot be coerced fails the step at once (beyond 32 bits axx reads a Long, see jvalue; beyond 64 it fails).
	h.fails("the orders kafka event named big payload properties are:", `NumberFormatException: For input string: "99999999999999999999"`,
		[]string{"$.count", "99999999999999999999"})

	// "headers are" needs exactly one value; "headers match" ignores repeats.
	h.fails("the orders kafka event named dup headers are:", "No records found", []string{"source", "svc"}, []string{"trace", "nope"})
	h.must("the orders kafka event named twice key is k2")
	h.fails("the orders kafka event named twice headers are:", "No records found", []string{"source", "svc"})
	h.must("the orders kafka event named twice2 key is k2")
	h.must("the orders kafka event named twice2 headers match:", []string{"source", "s.c"})
	h.fails("the orders kafka event named badre headers match:", "invalid regular expression", []string{"source", "(unclosed"})

	desc := stateKey.Of(h.sc).describe().(map[string]any)
	last := desc["lastAssertion"].(*checkReport)
	if last.Label != "badre" && last.Label != "twice2" {
		t.Errorf("last assertion: %+v", last)
	}
	b, _ := json.Marshal(desc)
	if !strings.Contains(string(b), `"orders"`) || !strings.Contains(string(b), `"matchers"`) {
		t.Errorf("describe: %s", b)
	}

	// The context is public: custom packs see the same matched record.
	svc, err := Context(h.sc).Service()
	if err != nil {
		t.Fatal(err)
	}
	tc, ok := svc.Topic("orders")
	if !ok || len(svc.Topics()) != 1 {
		t.Fatalf("topic client: %v %d", ok, len(svc.Topics()))
	}
	_ = tc.Events()
}

func TestConsumerFailureReport(t *testing.T) {
	h := newHarness(t, `{"timeout": "200ms"}`)
	h.offline("space-kafka")
	h.must("the orders kafka topic client")
	tc := h.client("space-kafka", "orders")
	topic := fakeTopic(tc)
	topic.add([]*kgo.Record{rec("k1", `{"id":"o-1"}`), rec("", `{"id":"o-2"}`), {Topic: "t", Key: []byte("k3")}}, nil)
	err := h.run("the orders kafka event named x payload properties are:", []string{"$.id", "o-9"})
	var ae *core.AssertionError
	if !errors.As(err, &ae) {
		t.Fatalf("want an assertion failure, got %v", err)
	}
	last := stateKey.Of(h.sc).last
	if last.Checked != 3 || last.Passed || len(last.Recent) != 3 {
		t.Fatalf("report: %+v", last)
	}
	reasons := []string{last.Recent[0].Mismatch, last.Recent[1].Mismatch, last.Recent[2].Mismatch}
	want := []string{`$.id is "o-1", expected o-9`, `$.id is "o-2", expected o-9`, "payload is not JSON: the record has no value (tombstone)"}
	for i := range want {
		if reasons[i] != want[i] {
			t.Errorf("record %d: %q, want %q", i, reasons[i], want[i])
		}
	}
	if !strings.Contains(ae.Actual.(string), "tombstone") {
		t.Errorf("actual: %v", ae.Actual)
	}
}

func TestConsumerWaitsForLateRecords(t *testing.T) {
	h := newHarness(t, `{"timeout": "5s"}`)
	h.offline("space-kafka")
	h.must("the orders kafka topic client")
	topic := fakeTopic(h.client("space-kafka", "orders"))
	topic.add([]*kgo.Record{rec("early", `{}`)}, nil)
	go func() {
		time.Sleep(150 * time.Millisecond)
		topic.add([]*kgo.Record{rec("other", `{}`)}, nil)
		time.Sleep(150 * time.Millisecond)
		topic.add([]*kgo.Record{rec("late", `{"n":1}`)}, nil)
	}()
	start := time.Now()
	h.must("the orders kafka event named late key is late")
	if time.Since(start) < 250*time.Millisecond {
		t.Error("the assertion should have waited for the late record")
	}
	if got := stateKey.Of(h.sc).last.Checked; got != 3 {
		t.Errorf("records checked: %d (each record is evaluated once)", got)
	}
}

// fakeRegistry serves schemas by ID like the Confluent Schema Registry.
func fakeRegistry(t *testing.T, schemas map[int]string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i, s := range schemas {
			if r.URL.Path == "/schemas/ids/"+itoa(i) {
				w.Header().Set("Content-Type", "application/vnd.schemaregistry.v1+json")
				_ = json.NewEncoder(w).Encode(map[string]any{"schema": s})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":40403,"message":"Schema not found"}`))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestAvroRecordsAreCheckedAsJavaPrintedThem(t *testing.T) {
	const schema = `{"type":"record","name":"MissionEvent","namespace":"ex","fields":[
	  {"name":"mission_id","type":"string"},{"name":"speed","type":"double"},
	  {"name":"metadata","type":["null","string"]},{"name":"tags","type":{"type":"map","values":"int"}}]}`
	ts := fakeRegistry(t, map[int]string{7: schema})
	h := newHarness(t, `{"timeout": "300ms"}`)
	h.offline("space-kafka")
	h.must("a missions kafka topic client with the following properties:",
		[]string{"consumer.value.deserializer", avroDeserializer},
		[]string{"consumer.schema.registry.url", ts.URL})
	tc := h.client("space-kafka", "missions")
	topic := fakeTopic(tc)
	s, _ := parseSchema(schema)
	v, err := avrojson.Decode(s, []byte(`{"mission_id":"m-1","speed":2,"metadata":{"string":"{\"p\": 1}"},"tags":{"b":2,"a":1}}`), avrojson.Options{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := avrojson.Marshal(s, v)
	value := make([]byte, 5, 5+len(body))
	binary.BigEndian.PutUint32(value[1:], 7)
	value = append(value, body...)
	topic.add([]*kgo.Record{{Topic: "missions", Value: []byte("not avro")}, {Topic: "missions", Value: value}}, nil)

	h.must("the missions kafka event named m payload properties are:",
		[]string{"$.mission_id", "m-1"}, []string{"$.speed", "2.0"}, []string{"$.metadata", `"{"p": 1}"`}, []string{"$.tags.a", "1"})
	got := tc.label("m").Matched
	if got.SchemaID != 7 || !strings.Contains(string(got.Value.(json.RawMessage)), `"metadata": "{\"p\": 1}"`) {
		t.Errorf("matched: %+v", got)
	}
	h.fails("the missions kafka event named none payload properties are:", "No records found", []string{"$.mission_id", "m-2"})
	recent := stateKey.Of(h.sc).last.Recent
	if len(recent) != 2 || !strings.Contains(recent[0].Mismatch, "Confluent Avro wire format") {
		t.Errorf("a record that is not Avro is reported, not fatal: %+v", recent)
	}
}
