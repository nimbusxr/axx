//go:build integration

package amqp

import (
	"context"
	"encoding/json"
	"testing"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/packs/asyncapi"
)

// labelService acts as the parcels service does: it reads the printers'
// reports from its queue, announces each on the labels exchange, and sets
// aside a report of a parcel it does not know, with the reason.
func labelService(t *testing.T, url string) {
	t.Helper()
	conn, err := amqp091.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range [][2]string{{"printers", "topic"}, {"labels", "topic"}, {"label-routes", "direct"}} {
		if err := ch.ExchangeDeclare(x[0], x[1], true, false, false, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"parcels.label-printed", "parcels.label-printed.rejected"} {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := ch.QueueBind("parcels.label-printed", "printed.#", "printers", false, nil); err != nil {
		t.Fatal(err)
	}
	reports, err := ch.Consume("parcels.label-printed", "", true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for d := range reports {
			var r struct{ Reference string }
			_ = json.Unmarshal(d.Body, &r)
			ctx := context.Background()
			if r.Reference == "PX-8199" {
				_ = ch.PublishWithContext(ctx, "", "parcels.label-printed.rejected", false, false, amqp091.Publishing{
					Headers: amqp091.Table{"reason": "unknown parcel"}, ContentType: "application/json", Body: d.Body,
				})
				continue
			}
			headers := amqp091.Table{}
			if p, ok := d.Headers["printer"]; ok {
				headers["printer"] = p
			}
			_ = ch.PublishWithContext(ctx, "labels", "printed."+d.RoutingKey, false, false, amqp091.Publishing{
				Headers: headers, CorrelationId: d.CorrelationId, ContentType: d.ContentType, Body: d.Body,
			})
		}
	}()
}

func TestRabbitMQ(t *testing.T) {
	addr := cloudtest.Server(t, "rabbitmq:4.2.9-alpine", "5672", nil,
		map[string]string{"RABBITMQ_DEFAULT_USER": "parcels", "RABBITMQ_DEFAULT_PASS": "parcels"})
	url := "amqp://parcels:parcels@" + addr + "/"
	labelService(t, url)
	broker := [][]string{{"url", url}}
	h := cloudtest.New(t, Pack())
	h.File("amqp/label-printed.json", `{"reference": "PX-8103", "printer": "LEJ-3"}`)
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the depot amqp broker with the following properties:", Table: broker},
		cloudtest.PlannedStep{Text: "within 10s the labels amqp exchange has a message where:", Table: [][]string{{"reference", "PX-8101"}}},
		cloudtest.PlannedStep{Text: "within 10s the label-routes amqp exchange has a message where:", Table: [][]string{{"routing key", "print.express"}}},
		cloudtest.PlannedStep{Text: "within 10s the parcels.label-printed.rejected amqp queue has a message where:", Table: [][]string{{"reference", "PX-8199"}}},
	))
	h.OK("the depot amqp broker with the following properties:", broker)

	// Published to an exchange with a routing key, received through the service.
	h.OK("a message is published to the printers amqp exchange with the routing key 'printed.LEJ':", `{"reference": "PX-8101", "printer": "LEJ-3"}`)
	h.OK("within 10s the labels amqp exchange has a message where:", [][]string{
		{"routing key", "printed.printed.LEJ"}, {"reference", "PX-8101"}, {"content type", "application/json"}, {"header printer", "undefined"},
	})

	// Sent straight to the service's queue, with properties.
	h.OK("the amqp/label-printed.json message is sent to the parcels.label-printed amqp queue with the following properties:",
		[][]string{{"header printer", "LEJ-3"}, {"correlation id", "C-8103"}})
	h.OK("within 10s the labels amqp exchange has a message where:", [][]string{
		{"routing key", "printed.parcels.label-printed"}, {"reference", "PX-8103"}, {"header printer", "LEJ-3"}, {"correlation id", "C-8103"},
	})

	// A queue the service writes to.
	h.OK("a message is published to the printers amqp exchange with the routing key 'printed.LEJ':", `{"reference": "PX-8199"}`)
	h.OK("within 10s the parcels.label-printed.rejected amqp queue has a message where:", [][]string{
		{"reference", "PX-8199"}, {"header reason", "unknown parcel"},
	})

	// A direct exchange delivers only the routing keys bound: the ones the checks name.
	h.OK("a message is published to the label-routes amqp exchange with the routing key 'print.express':", `{"reference": "PX-8104"}`)
	h.OK("within 10s the label-routes amqp exchange has a message where:", [][]string{{"routing key", "print.express"}, {"reference", "PX-8104"}})

	// What goes nowhere, or nowhere that exists, fails the step.
	_ = h.Fails("a message is published to the printers amqp exchange with the routing key 'misprint.LEJ':",
		`no queue is bound to the printers amqp exchange for the routing key "misprint.LEJ", so the message went nowhere`, `{}`)
	_ = h.Fails("a message is published to the depots amqp exchange:", "no amqp exchange named depots", `{}`)
	_ = h.Fails("a message is sent to the parcels.label-reprints amqp queue:", "no amqp queue named parcels.label-reprints", `{}`)
	_ = h.Fails("within 1s the labels amqp exchange has a message where:", "No message on the labels amqp exchange met the conditions within 1s",
		[][]string{{"reference", "PX-8102"}})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	// Another scenario sees none of the first one's messages.
	h.NewScenario()
	h.OK("the depot amqp broker with the following properties:", broker)
	_ = h.Fails("within 1s the labels amqp exchange has a message where:", "No message on the labels amqp exchange met the conditions within 1s",
		[][]string{{"reference", "PX-8101"}})

	// The run ends, and with it its listeners, which would take the queue's
	// messages from the next run's.
	if err := h.Suite.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Run("contract", func(t *testing.T) {
		broker := [][]string{{"url", url}, {"asyncapi", "asyncapi.yaml"}}
		h := cloudtest.New(t, asyncapi.Pack(), Pack())
		h.File("asyncapi.yaml", printingContract)
		h.Start(h.Plan(
			cloudtest.PlannedStep{Text: "the depot amqp broker with the following properties:", Table: broker},
			cloudtest.PlannedStep{Text: "within 10s the labels amqp exchange has a message where:", Table: [][]string{{"reference", "PX-8106"}}},
			cloudtest.PlannedStep{Text: "within 10s the parcels.label-printed.rejected amqp queue has a message where:", Table: [][]string{{"reference", "PX-8199"}}},
		))
		h.OK("the depot amqp broker with the following properties:", broker)
		// An exchange's messages are on the channel of the exchange, or of their routing key.
		_ = h.Fails("a message is published to the printers amqp exchange with the routing key 'printed.LEJ':",
			"the message to send breaks asyncapi.yaml:\n- validation.message.payload.schema.required: payload $: missing property 'printedAt' (the printed message of the labelPrinted channel)",
			`{"reference": "PX-8106", "printer": "LEJ-3"}`)
		h.OK("a message is published to the printers amqp exchange with the routing key 'printed.LEJ':",
			`{"reference": "PX-8106", "printer": "LEJ-3", "printedAt": "2026-09-28T08:15:00Z"}`)
		_ = h.Fails("within 10s the labels amqp exchange has a message where:",
			"the message the check found breaks asyncapi.yaml:\n- validation.message.payload.schema.required: payload $: missing property 'copies'",
			[][]string{{"reference", "PX-8106"}})
		// Headers are checked too.
		h.OK("a message is published to the printers amqp exchange with the routing key 'printed.LEJ':",
			`{"reference": "PX-8199", "printer": "LEJ-3", "printedAt": "2026-09-28T08:16:00Z"}`)
		h.OK("within 10s the parcels.label-printed.rejected amqp queue has a message where:", [][]string{{"reference", "PX-8199"}})
	})
}

// printingContract is the printing's AsyncAPI document.
const printingContract = `asyncapi: 3.0.0
info: {title: Label printing, version: 1.0.0}
defaultContentType: application/json
servers:
  printing: {host: localhost:5672, protocol: amqp}
channels:
  labelPrinted:
    address: printed.{depot}
    messages:
      printed:
        payload: {type: object, required: [reference, printer, printedAt]}
  labels:
    address: labels
    messages:
      announced:
        payload: {type: object, required: [reference, printer, copies]}
  rejected:
    address: parcels.label-printed.rejected
    messages:
      rejected:
        headers: {type: object, required: [reason]}
        payload: {type: object, required: [reference]}
`

// AMQP 1.0: a queue is an anycast address and an exchange a multicast one,
// and the routing key is the subject.
func TestArtemis(t *testing.T) {
	addr := cloudtest.Server(t, "apache/activemq-artemis:2.44.0-alpine", "5672", nil,
		map[string]string{"ARTEMIS_USER": "parcels", "ARTEMIS_PASSWORD": "parcels"})
	broker := [][]string{{"url", "amqp://parcels:parcels@" + addr}, {"protocol", "1.0"}}
	h := cloudtest.New(t, Pack())
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the depot amqp broker with the following properties:", Table: broker},
		cloudtest.PlannedStep{Text: "within 10s the labels amqp exchange has a message where:", Table: [][]string{{"reference", "PX-8111"}}},
		cloudtest.PlannedStep{Text: "within 10s the label-jobs amqp queue has a message where:", Table: [][]string{{"reference", "PX-8112"}}},
	))
	h.OK("the depot amqp broker with the following properties:", broker)
	h.OK("a message is published to the labels amqp exchange with the routing key 'print.express':", `{"reference": "PX-8111"}`)
	h.OK("within 10s the labels amqp exchange has a message where:", [][]string{
		{"routing key", "print.express"}, {"reference", "PX-8111"}, {"content type", "application/json"},
	})
	h.File("amqp/label-job.json", `{"reference": "PX-8112", "printer": "LEJ-3"}`)
	h.OK("the amqp/label-job.json message is sent to the label-jobs amqp queue with the following properties:",
		[][]string{{"header depot", "LEJ"}, {"message id", "M-8112"}})
	h.OK("within 10s the label-jobs amqp queue has a message where:", [][]string{
		{"reference", "PX-8112"}, {"header depot", "LEJ"}, {"message id", "M-8112"},
	})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
