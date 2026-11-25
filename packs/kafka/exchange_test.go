package kafka

import (
	"context"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nimbusxr/axx/core"
)

// The steps that register a kafka service and a topic client say that the
// scenario has them, and a step that finds a record on the topic says the
// record, once: the steps that expect more of the same record say no more.
func TestAConsumedEventSaysItsExchange(t *testing.T) {
	h := newHarness(t, `{"timeout": "300ms"}`)
	h.offline("events")
	h.must("the parcel-events kafka topic client")
	topic := fakeTopic(h.client("events", "parcel-events"))
	first, second, plain := rec("PX-1", `{"reference":"PX-1","status":"REGISTERED"}`, "X-Api-Key", "example-key"), rec("PX-2", `{"reference":"PX-2"}`), rec("", "not json", "kind", "text")
	first.Partition, first.Offset = 0, 12
	second.Partition, second.Offset = 1, 3
	plain.Offset = 13
	topic.add([]*kgo.Record{first, second, plain}, nil)

	h.must("the parcel-events kafka event named registered key is PX-1")
	h.must("the parcel-events kafka event named registered payload properties are:", []string{"$.status", "REGISTERED"})
	h.must("the parcel-events kafka event named registered headers are:", []string{"X-Api-Key", "example-key"})
	h.must("the parcel-events kafka event named again payload properties are:", []string{"$.reference", "PX-1"})
	h.fails("the parcel-events kafka event named missing key is PX-9", "No records found")
	h.must("the parcel-events kafka event named other key is PX-2")
	h.must("the parcel-events kafka event named text headers are:", []string{"kind", "text"})

	got := h.sc.Exchanges()
	if len(got) != 5 {
		t.Fatalf("exchanges: %+v", got)
	}
	if svc := got[0]; svc != (core.Exchange{Peer: "events", Kind: "broker", At: svc.At}) || svc.At.IsZero() {
		t.Errorf("the kafka service: %+v", svc)
	}
	if client := got[1]; client != (core.Exchange{Peer: "parcel-events", Kind: "topic", At: client.At}) {
		t.Errorf("the topic client: %+v", client)
	}
	x := got[2]
	if x.Peer != "parcel-events" || x.Kind != "topic" || x.From != "" || x.Sent != nil || x.Seen == nil {
		t.Fatalf("the consumed event: %+v", x)
	}
	if x.Seen.Title != "key PX-1, partition 0, offset 12" || x.Seen.MediaType != "application/json" || x.Seen.Body != `{"reference":"PX-1","status":"REGISTERED"}` {
		t.Errorf("seen: %+v", x.Seen)
	}
	if other := got[3].Seen; other == nil || other.Title != "key PX-2, partition 1, offset 3" {
		t.Errorf("the second event: %+v", other)
	}
	if text := got[4].Seen; text == nil || text.Title != "no key, partition 0, offset 13" || text.MediaType != "text/plain" || text.Body != "not json" {
		t.Errorf("an event with no key, of text: %+v", text)
	}
	for _, x := range got {
		if x.Seen != nil && strings.Contains(x.Seen.Title+x.Seen.Body, "example-key") {
			t.Errorf("an exchange should have no header: %+v", x.Seen)
		}
	}
}

// A published event says its key and its payload, cut where it is long. The
// publish steps say it once the broker has the record (the integration test
// checks that): here, what they say.
func TestAPublishedEventSaysItsExchange(t *testing.T) {
	h := newHarness(t, "")
	h.offline("events")
	h.must("the parcel-events kafka topic client")
	h.must("a parcel-events kafka event")
	h.must("the parcel-events kafka event key is PX-1")
	h.file("registered.json", `{"reference": "PX-1", "status": "REGISTERED"}`)
	h.must("the parcel-events kafka event payload is a registered.json resource")
	h.must("the parcel-events kafka event headers are:", []string{"X-Api-Key", "example-key"})
	tc := h.client("events", "parcel-events")
	before := len(h.sc.Exchanges())
	if before != 2 {
		t.Fatalf("a drafted event should say nothing: %+v", h.sc.Exchanges())
	}
	// Nothing listens: the event is not published, and the step says nothing.
	h.sc.SetContext(canceled())
	if err := h.run("the parcel-events kafka event is published"); err == nil {
		t.Fatal("the publish should fail: no broker")
	}
	if got := h.sc.Exchanges(); len(got) != before {
		t.Fatalf("an event that was not published said: %+v", got[before:])
	}

	tc.published(h.sc, tc.Events()[0])
	long := strings.Repeat("x", exchangeBodyLimit+5)
	tc.published(h.sc, &Event{Payload: long})
	got := h.sc.Exchanges()[before:]
	if len(got) != 2 {
		t.Fatalf("exchanges: %+v", got)
	}
	x := got[0]
	if x.Peer != "parcel-events" || x.Kind != "topic" || x.From != "" || x.Seen != nil || x.Sent == nil {
		t.Fatalf("the published event: %+v", x)
	}
	if x.Sent.Title != "key PX-1" || x.Sent.MediaType != "application/json" || x.Sent.Body != `{"reference": "PX-1", "status": "REGISTERED"}` {
		t.Errorf("sent: %+v", x.Sent)
	}
	if cut := got[1].Sent; cut.Title != "no key" || cut.MediaType != "text/plain" || cut.Body != long[:exchangeBodyLimit]+"... (5 more bytes)" {
		t.Errorf("a long payload with no key: %q, %s, %d bytes", cut.Title, cut.MediaType, len(cut.Body))
	}
}

// canceled is a context that is over, so that a publish to brokers that
// refuse connections fails at once.
func canceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
