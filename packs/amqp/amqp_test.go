package amqp

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

func TestBrokerProperties(t *testing.T) {
	h := cloudtest.New(t, Pack())
	for _, tc := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"protocol", "1.0"}}, `the amqp broker property "url" is required`},
		{[][]string{{"url", "amqp://localhost"}, {"protocol", "0-10"}}, `the amqp protocol is 0-9-1 or 1.0, not "0-10"`},
		{[][]string{{"url", "http://localhost:5672"}}, "the amqp broker's url is amqp://user:password@host:port/vhost"},
		{[][]string{{"url", "amqp://localhost"}, {"vhost", "/"}}, `unknown amqp broker property "vhost" (supported: url, protocol, asyncapi)`},
	} {
		_ = h.Fails("the depot amqp broker with the following properties:", tc.want, tc.rows)
	}
}

// A password in the URL never shows, even in a URL the step refuses.
func TestTheURLsPasswordIsHidden(t *testing.T) {
	h := cloudtest.New(t, Pack())
	err := h.Fails("the depot amqp broker with the following properties:", "the amqp broker's url is",
		[][]string{{"url", "ampq://parcels:depot-password-depot@localhost:5672/"}})
	if strings.Contains(err.Error(), "depot-password-depot") {
		t.Errorf("the password shows: %v", err)
	}
}

func TestOutgoingProperties(t *testing.T) {
	m, err := outgoingFrom("exchange", []byte(`{"reference": "PX-8101"}`), map[string]string{
		"routing key": "print.express", "header sender": "maple-crafts", "correlation id": "C-8101", "reply to": "label-replies",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.routingKey != "print.express" || m.headers["sender"] != "maple-crafts" || m.correlationID != "C-8101" ||
		m.replyTo != "label-replies" || m.contentType != "application/json" {
		t.Errorf("%+v", m)
	}
	if m, _ := outgoingFrom("queue", []byte("PX-8101 printed"), nil); m.contentType != "text/plain" {
		t.Errorf("content type %q", m.contentType)
	}
	if m, _ := outgoingFrom("queue", []byte(`{}`), map[string]string{"content type": "application/vnd.parcels+json"}); m.contentType != "application/vnd.parcels+json" {
		t.Errorf("content type %q", m.contentType)
	}
	if _, err := outgoingFrom("queue", nil, map[string]string{"routing key": "printed.LEJ"}); err == nil ||
		!strings.Contains(err.Error(), "a message sent to a queue takes no routing key") {
		t.Errorf("err: %v", err)
	}
	if _, err := outgoingFrom("exchange", nil, map[string]string{"priority": "9"}); err == nil ||
		!strings.Contains(err.Error(), `unknown amqp message property "priority" (supported: routing key, header <name>`) {
		t.Errorf("err: %v", err)
	}
}

func TestAStepNeedsABroker(t *testing.T) {
	h := cloudtest.New(t, Pack())
	_ = h.Fails("a message is sent to the parcels.label-printed amqp queue:", "No AMQP broker is registered in this scenario", `{}`)
}
