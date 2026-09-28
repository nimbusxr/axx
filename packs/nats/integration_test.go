//go:build integration

package nats

import (
	"context"
	"encoding/json"
	"testing"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// trackingService acts as the parcels service does: it hears the couriers'
// delivery confirmations, and fans each out as a tracking update, which
// the TRACKING stream keeps.
func trackingService(t *testing.T, url string) {
	t.Helper()
	nc, err := natsgo.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "TRACKING", Subjects: []string{"tracking.>"}}); err != nil {
		t.Fatal(err)
	}
	// What an earlier run stored.
	if _, err := js.Publish(ctx, "tracking.PX-8302", []byte(`{"reference": "PX-8302", "status": "IN_TRANSIT"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := nc.Subscribe("deliveries.confirmed", func(m *natsgo.Msg) {
		var d struct{ Reference string }
		_ = json.Unmarshal(m.Data, &d)
		update := &natsgo.Msg{
			Subject: "tracking." + d.Reference, Header: natsgo.Header{"source": {"courier"}},
			Data: []byte(`{"reference": "` + d.Reference + `", "status": "DELIVERED"}`),
		}
		if c := m.Header.Get("courier"); c != "" {
			update.Header.Set("courier", c)
		}
		_, _ = js.PublishMsg(context.Background(), update)
	}); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestNATS(t *testing.T) {
	addr := cloudtest.Server(t, "nats:2.15.0-alpine", "4222", []string{"nats-server", "-js"}, nil)
	url := "nats://" + addr
	trackingService(t, url)
	rows := [][]string{{"url", url}}
	h := cloudtest.New(t, Pack())
	h.File("nats/delivery-confirmed.json", `{"reference": "PX-8303", "signedBy": "J. Roth"}`)
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the tracking nats server with the following properties:", Table: rows},
		cloudtest.PlannedStep{Text: "within 10s the tracking.> nats subject has a message where:", Table: [][]string{{"reference", "PX-8301"}}},
		cloudtest.PlannedStep{Text: "within 10s the TRACKING nats stream has a message where:", Table: [][]string{{"reference", "PX-8301"}}},
	))
	h.OK("the tracking nats server with the following properties:", rows)
	h.OK("a message is published to the deliveries.confirmed nats subject:", `{"reference": "PX-8301", "signedBy": "H. Wolf"}`)
	h.OK("within 10s the tracking.> nats subject has a message where:", [][]string{
		{"subject", "tracking.PX-8301"}, {"status", "DELIVERED"}, {"header source", "courier"}, {"header courier", "undefined"},
	})
	h.OK("within 10s the TRACKING nats stream has a message where:", [][]string{{"subject", "tracking.PX-8301"}, {"status", "DELIVERED"}})

	h.OK("the nats/delivery-confirmed.json message is published to the deliveries.confirmed nats subject with the following headers:",
		[][]string{{"courier", "CR-LEJ-12"}})
	h.OK("within 10s the TRACKING nats stream has a message where:", [][]string{
		{"reference", "PX-8303"}, {"header courier", "CR-LEJ-12"},
	})

	// Published to a subject the stream captures, through JetStream.
	h.OK("a message is published to the tracking.PX-8304 nats subject:", `{"reference": "PX-8304", "status": "OUT_FOR_DELIVERY"}`)
	h.OK("within 10s the TRACKING nats stream has a message where:", [][]string{{"reference", "PX-8304"}})

	// What the stream stored before the run never counts.
	_ = h.Fails("within 1s the TRACKING nats stream has a message where:", "No message on the TRACKING nats stream met the conditions within 1s",
		[][]string{{"reference", "PX-8302"}})
	_ = h.Fails("within 1s the PARCELS nats stream has a message where:", "no nats stream named PARCELS", [][]string{{"reference", "PX-8301"}})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	// Another scenario sees none of the first one's messages.
	h.NewScenario()
	h.OK("the tracking nats server with the following properties:", rows)
	_ = h.Fails("within 1s the tracking.> nats subject has a message where:", "No message on the tracking.> nats subject met the conditions within 1s",
		[][]string{{"reference", "PX-8301"}})
}
