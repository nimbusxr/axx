//go:build integration

package gcppubsub

import (
	"context"
	"testing"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

func TestTopics(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci-gcp:latest", "4588", "/health", nil)
	project := [][]string{{"project", "parcels-dev"}, {"endpoint", "http://" + addr}}
	h := cloudtest.New(t, gcpcore.Pack(), Pack())
	h.OK("the billing gcp project with the following properties:", project)
	p, _ := gcpcore.Default(h.SC)
	c, err := client(context.Background(), h.Suite, p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, topic := range []string{"invoice-events", "shipment-events"} {
		if _, err := c.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName(p, topic)}); err != nil {
			t.Fatal(err)
		}
	}

	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the billing gcp project with the following properties:", Table: project},
		cloudtest.PlannedStep{Text: "the invoice-events pubsub topic has a message where:", Table: [][]string{{"invoice", "INV-1"}}},
		cloudtest.PlannedStep{Text: "the shipment-events pubsub topic has a message where:", Table: [][]string{{"shipment", "SHP-1"}}},
	))
	// The service under test publishes an InvoiceReconciled event.
	pub := c.Publisher(topicName(p, "invoice-events"))
	if _, err := pub.Publish(ctx, &pubsub.Message{
		Data: []byte(`{"invoice":"INV-1","status":"RECONCILED","overcharged":1}`), Attributes: map[string]string{"eventType": "InvoiceReconciled"},
	}).Get(ctx); err != nil {
		t.Fatal(err)
	}
	pub.Stop()
	h.OK("within 10s the invoice-events pubsub topic has a message where:", [][]string{
		{"attribute eventType", "InvoiceReconciled"}, {"invoice", "INV-1"}, {"overcharged", "1"},
	})
	h.Fails("within 1s the invoice-events pubsub topic has a message where:", `attribute eventType=InvoiceReconciled {"invoice":"INV-1"`, [][]string{{"status", "DISPUTED"}})

	// Publishing, inline and from a file with attributes.
	h.OK("a message is published to the shipment-events pubsub topic:", `{"shipment":"SHP-1","status":"DELIVERED"}`)
	h.File("messages/delivered.json", `{"shipment":"SHP-2","status":"DELIVERED"}`)
	h.OK("the messages/delivered.json message is published to the shipment-events pubsub topic with the following attributes:", [][]string{{"eventType", "ShipmentDelivered"}})
	h.OK("the shipment-events pubsub topic has a message where:", [][]string{{"shipment", "SHP-1"}, {"attribute eventType", "undefined"}})
	h.OK("the shipment-events pubsub topic has a message where:", [][]string{{"shipment", "SHP-2"}, {"attribute eventType", "ShipmentDelivered"}})

	// The run's subscriptions go when the suite closes.
	if err := h.Suite.Close(ctx); err != nil {
		t.Fatal(err)
	}
	c2, err := pubsub.NewClient(ctx, p.ID, p.GRPC()...)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	it := c2.SubscriptionAdminClient.ListSubscriptions(ctx, &pubsubpb.ListSubscriptionsRequest{Project: "projects/" + p.ID})
	if s, err := it.Next(); err == nil {
		t.Errorf("subscription left: %s", s.Name)
	}
}
