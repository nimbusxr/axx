//go:build integration

package azureservicebus

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// The emulator's namespace: its broker listens on the host's port 5673.
const connection = "Endpoint=sb://localhost:5673;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=devkey;UseDevelopmentEmulator=true;"

func TestQueuesAndTopics(t *testing.T) {
	addr := cloudtest.EmulatorWithDocker(t, "floci/floci-az:latest", "4577", "", map[string]string{
		"FLOCI_AZ_SERVICES_SERVICE_BUS_MOCKED": "false",
	})
	// The emulator runs its broker in a container of its own.
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "floci-az-servicebus-default").Run() })
	namespace := [][]string{{"connection string", connection}, {"management endpoint", "http://" + addr + "/devstoreaccount1-servicebus"}}
	h := cloudtest.New(t, Pack())
	h.OK("the customs service bus namespace with the following properties:", namespace)
	n, _ := namespaces.Of(h.SC).Default()
	c, err := open(h.Suite, n)
	if err != nil {
		t.Fatal(err)
	}
	ctx := h.SC.Context()
	for _, q := range []string{"customs-filings", "customs-holds"} {
		if _, err := c.admin.CreateQueue(ctx, q, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.admin.CreateTopic(ctx, "customs-events", nil); err != nil {
		t.Fatal(err)
	}
	// The broker needs a moment after the first entities.
	time.Sleep(3 * time.Second)

	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the customs service bus namespace with the following properties:", Table: namespace},
		cloudtest.PlannedStep{Text: "the customs-holds service bus queue has a message where:", Table: [][]string{{"declaration", "DEC-1"}}},
		cloudtest.PlannedStep{Text: "the customs-events service bus topic has a message where:", Table: [][]string{{"declaration", "DEC-1"}}},
	))
	// The service under test puts a declaration on hold and announces a clearance.
	for entity, m := range map[string]*azservicebus.Message{
		"customs-holds":  {Body: []byte(`{"declaration":"DEC-1","reason":"MISSING_INVOICE"}`), ApplicationProperties: map[string]any{"broker": "ACME"}},
		"customs-events": {Body: []byte(`{"declaration":"DEC-2","status":"CLEARED"}`), ApplicationProperties: map[string]any{"eventType": "DeclarationCleared"}},
	} {
		snd, err := c.msg.NewSender(entity, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := snd.SendMessage(ctx, m, nil); err != nil {
			t.Fatal(err)
		}
		_ = snd.Close(ctx)
	}
	h.OK("within 10s the customs-holds service bus queue has a message where:", [][]string{
		{"declaration", "DEC-1"}, {"reason", "MISSING_INVOICE"}, {"property broker", "ACME"},
	})
	h.OK("within 10s the customs-events service bus topic has a message where:", [][]string{
		{"declaration", "DEC-2"}, {"property eventType", "DeclarationCleared"},
	})
	h.Fails("within 1s the customs-events service bus topic has a message where:", "No message on the customs-events service bus topic", [][]string{{"declaration", "DEC-9"}})

	// Sending, inline and from a file with application properties.
	h.OK("a message is sent to the customs-filings service bus queue:", `{"filing":"F-1"}`)
	h.File("messages/filing.json", `{"filing":"F-2"}`)
	h.OK("the messages/filing.json message is sent to the customs-filings service bus queue with the following properties:", [][]string{{"broker", "ACME"}})
	h.OK("a message is sent to the customs-events service bus topic:", `{"declaration":"DEC-3","status":"HELD"}`)
	h.OK("the customs-events service bus topic has a message where:", [][]string{{"declaration", "DEC-3"}})
	r, err := c.msg.NewReceiverForQueue("customs-filings", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	deadline := time.Now().Add(10 * time.Second)
	for len(got) < 2 && time.Now().Before(deadline) {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		ms, _ := r.ReceiveMessages(rctx, 10, nil)
		cancel()
		for _, m := range ms {
			broker, _ := m.ApplicationProperties["broker"].(string)
			got[string(m.Body)] = broker
			_ = r.CompleteMessage(ctx, m, nil)
		}
	}
	if b, ok := got[`{"filing":"F-1"}`]; !ok || b != "" || got[`{"filing":"F-2"}`] != "ACME" {
		t.Errorf("sent messages: %v", got)
	}

	// The run's subscription goes when the suite closes.
	if err := h.Suite.Close(ctx); err != nil {
		t.Fatal(err)
	}
	pager := c.admin.NewListSubscriptionsPager("customs-events", nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			break
		}
		for _, s := range page.Subscriptions {
			if strings.HasPrefix(s.SubscriptionName, "axx-") {
				t.Errorf("subscription left: %s", s.SubscriptionName)
			}
		}
	}
}
