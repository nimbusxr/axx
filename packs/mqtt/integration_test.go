//go:build integration

package mqtt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eclipse/paho.golang/paho"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// scanService acts as the parcels service does: it hears the depots'
// scans, and alerts a depot whose scanner scanned a parcel it does not
// know, with the scanner's name.
func scanService(t *testing.T, url string) {
	t.Helper()
	b := &broker{url: url, clientID: "parcels"}
	s := core.NewSuite(core.SuiteOptions{})
	var c *paho.Client
	c, err := connect(context.Background(), s, b, func(p *paho.Publish) {
		var scan struct{ ParcelRef string }
		_ = json.Unmarshal(p.Payload, &scan)
		if scan.ParcelRef != "PX-8299" {
			return
		}
		alert := &paho.Publish{Topic: "depots/LEJ/alerts", QoS: 1, Payload: p.Payload, Properties: &paho.PublishProperties{
			User: paho.UserProperties{{Key: "scanner", Value: p.Properties.User.Get("scanner")}},
		}}
		go func() { _, _ = c.Publish(context.Background(), alert) }()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Disconnect(&paho.Disconnect{}) })
	if _, err := c.Subscribe(context.Background(), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "depots/+/scans", QoS: 1}}}); err != nil {
		t.Fatal(err)
	}
}

func TestMosquitto(t *testing.T) {
	addr := cloudtest.Server(t, "eclipse-mosquitto:2.0.22", "1883", []string{"mosquitto", "-c", "/mosquitto-no-auth.conf"}, nil)
	url := "mqtt://" + addr
	scanService(t, url)
	rows := [][]string{{"url", url}}

	// A message retained before the run never counts.
	s := core.NewSuite(core.SuiteOptions{})
	old, err := connect(context.Background(), s, &broker{url: url, clientID: "old-run"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Publish(context.Background(), &paho.Publish{
		Topic: "depots/LEJ/alerts", QoS: 1, Retain: true, Payload: []byte(`{"parcelRef": "PX-8298"}`),
	}); err != nil {
		t.Fatal(err)
	}
	_ = old.Disconnect(&paho.Disconnect{})

	h := cloudtest.New(t, Pack())
	h.File("mqtt/scan-unknown.json", `{"scanId": "SC-8299-1", "parcelRef": "PX-8299", "status": "IN_TRANSIT", "location": "Leipzig"}`)
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the depots mqtt broker with the following properties:", Table: rows},
		cloudtest.PlannedStep{Text: "within 10s the depots/+/alerts mqtt topic has a message where:", Table: [][]string{{"parcelRef", "PX-8299"}}},
		cloudtest.PlannedStep{Text: "within 10s the depots/LEJ/scans mqtt topic has a message where:", Table: [][]string{{"parcelRef", "PX-8201"}}},
	))
	h.OK("the depots mqtt broker with the following properties:", rows)
	h.OK("a message is published to the depots/LEJ/scans mqtt topic:", `{"scanId": "SC-8201-1", "parcelRef": "PX-8201", "status": "OUT_FOR_DELIVERY"}`)
	h.OK("within 10s the depots/LEJ/scans mqtt topic has a message where:", [][]string{
		{"topic", "depots/LEJ/scans"}, {"parcelRef", "PX-8201"}, {"property scanner", "undefined"},
	})
	h.OK("the mqtt/scan-unknown.json message is published to the depots/LEJ/scans mqtt topic with the following properties:",
		[][]string{{"property scanner", "LEJ-HANDHELD-7"}, {"qos", "2"}})
	h.OK("within 10s the depots/+/alerts mqtt topic has a message where:", [][]string{
		{"topic", "depots/LEJ/alerts"}, {"parcelRef", "PX-8299"}, {"property scanner", "LEJ-HANDHELD-7"},
	})
	_ = h.Fails("within 1s the depots/+/alerts mqtt topic has a message where:", "No message on the depots/+/alerts mqtt topic met the conditions within 1s",
		[][]string{{"parcelRef", "PX-8298"}})
	_ = h.Fails("a message is published to the depots/+/scans mqtt topic:", "not a filter", `{}`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	// Another scenario sees none of the first one's messages.
	h.NewScenario()
	h.OK("the depots mqtt broker with the following properties:", rows)
	_ = h.Fails("within 1s the depots/LEJ/scans mqtt topic has a message where:", "No message on the depots/LEJ/scans mqtt topic met the conditions within 1s",
		[][]string{{"parcelRef", "PX-8201"}})
}

// MQTT over WebSockets.
func TestMosquittoOverWebSockets(t *testing.T) {
	addr := cloudtest.Server(t, "eclipse-mosquitto:2.0.22", "9001", []string{
		"sh", "-c",
		`printf 'listener 9001\nprotocol websockets\nallow_anonymous true\n' > /tmp/ws.conf && exec mosquitto -c /tmp/ws.conf`,
	}, nil)
	rows := [][]string{{"url", "ws://" + addr}}
	h := cloudtest.New(t, Pack())
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the depots mqtt broker with the following properties:", Table: rows},
		cloudtest.PlannedStep{Text: "within 10s the depots/LEJ/scans mqtt topic has a message where:", Table: [][]string{{"parcelRef", "PX-8202"}}},
	))
	h.OK("the depots mqtt broker with the following properties:", rows)
	h.OK("a message is published to the depots/LEJ/scans mqtt topic:", `{"parcelRef": "PX-8202"}`)
	h.OK("within 10s the depots/LEJ/scans mqtt topic has a message where:", [][]string{{"parcelRef", "PX-8202"}})
}
