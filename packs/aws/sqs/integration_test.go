//go:build integration

package awssqs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/packs/asyncapi"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

func TestQueues(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci:latest", "4566", "/_localstack/health", nil)
	account := [][]string{
		{"region", "eu-west-1"},
		{"endpoint", "http://" + addr},
		{"access key id", "test"},
		{"secret access key", "test"},
	}
	h := cloudtest.New(t, awscore.Pack(), Pack())
	h.OK("the parcels aws account with the following properties:", account)
	acct, _ := awscore.Default(h.SC)
	c, err := client(context.Background(), h.Suite, acct)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	urls := map[string]string{}
	for _, q := range []string{"refund-requests", "damage-reports"} {
		out, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(q)})
		if err != nil {
			t.Fatal(err)
		}
		urls[q] = aws.ToString(out.QueueUrl)
	}

	// The run checks refund-requests: axx receives from it once the apps run.
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the parcels aws account with the following properties:", Table: account},
		cloudtest.PlannedStep{Text: "the refund-requests sqs queue has a message where:", Table: [][]string{{"claim", "CLM-1"}}},
	))
	// The service under test sends a refund request.
	_, err = c.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl: aws.String(urls["refund-requests"]), MessageBody: aws.String(`{"claim":"CLM-1","amount":120.5}`),
		MessageAttributes: map[string]types.MessageAttributeValue{"reason": {DataType: aws.String("String"), StringValue: aws.String("DAMAGE")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.OK("within 10s the refund-requests sqs queue has a message where:", [][]string{
		{"claim", "CLM-1"}, {"amount", "120.5"}, {"attribute reason", "DAMAGE"},
	})
	err = h.Fails("within 1s the refund-requests sqs queue has a message where:", "No message on the refund-requests sqs queue", [][]string{{"claim", "CLM-2"}})
	if want := `attribute reason=DAMAGE {"claim":"CLM-1","amount":120.5}`; !strings.Contains(err.Error(), want) {
		t.Errorf("the failure should show what arrived (%s): %v", want, err)
	}

	// A message received before the scenario started does not count.
	h.NewScenario()
	h.OK("the parcels aws account with the following properties:", account)
	h.Fails("within 1s the refund-requests sqs queue has a message where:", "It received no messages since the scenario started", [][]string{{"claim", "CLM-1"}})

	// Sending, inline and from a file with attributes.
	h.OK("a message is sent to the damage-reports sqs queue:", `{"parcel":"PX-1","damage":"crushed"}`)
	h.File("messages/report.json", `{"parcel":"PX-2","damage":"wet"}`)
	h.OK("the messages/report.json message is sent to the damage-reports sqs queue with the following attributes:", [][]string{{"carrier", "KESTREL"}})
	h.Fails("the messages/report.json message is sent to the damage-reports sqs queue with the following attributes:", "attributes are missing")
	got := map[string]string{}
	deadline := time.Now().Add(10 * time.Second)
	for len(got) < 2 && time.Now().Before(deadline) {
		out, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(urls["damage-reports"]), MaxNumberOfMessages: 10, WaitTimeSeconds: 1, MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range out.Messages {
			carrier := ""
			if a, ok := m.MessageAttributes["carrier"]; ok {
				carrier = aws.ToString(a.StringValue)
			}
			got[aws.ToString(m.Body)] = carrier
		}
	}
	inline, withAttributes := `{"parcel":"PX-1","damage":"crushed"}`, `{"parcel":"PX-2","damage":"wet"}`
	if carrier, ok := got[inline]; !ok || carrier != "" {
		t.Errorf("the inline message: %v", got)
	}
	if got[withAttributes] != "KESTREL" {
		t.Errorf("the message with attributes: %v", got)
	}

	t.Run("contract", func(t *testing.T) {
		// A queue of its own: the first run still receives from refund-requests.
		out, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String("refund-claims")})
		if err != nil {
			t.Fatal(err)
		}
		account := append(account, []string{"asyncapi", "asyncapi.yaml"})
		h := cloudtest.New(t, awscore.Pack(), asyncapi.Pack(), Pack())
		h.File("asyncapi.yaml", refundsContract)
		h.Start(h.Plan(
			cloudtest.PlannedStep{Text: "the parcels aws account with the following properties:", Table: account},
			cloudtest.PlannedStep{Text: "the refund-claims sqs queue has a message where:", Table: [][]string{{"claim", "CLM-5"}}},
		))
		h.OK("the parcels aws account with the following properties:", account)
		_ = h.Fails("a message is sent to the refund-claims sqs queue:",
			"the message to send breaks asyncapi.yaml:\n- validation.message.payload.schema.required: payload $: missing property 'amount'", `{"claim": "CLM-3"}`)
		h.File("messages/refund.json", `{"claim": "CLM-4", "amount": 20}`)
		_ = h.Fails("the messages/refund.json message is sent to the refund-claims sqs queue with the following attributes:",
			"- validation.message.headers.schema.enum: headers $.reason:", [][]string{{"reason", "BORED"}})
		// The service under test sends a refund whose amount is text.
		if _, err := c.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl: out.QueueUrl, MessageBody: aws.String(`{"claim":"CLM-5","amount":"lots"}`),
		}); err != nil {
			t.Fatal(err)
		}
		_ = h.Fails("within 10s the refund-claims sqs queue has a message where:",
			"the message the check found breaks asyncapi.yaml:\n- validation.message.payload.schema.type: payload $.amount:", [][]string{{"claim", "CLM-5"}})
	})
}

// refundsContract is the refunds' AsyncAPI document.
const refundsContract = `asyncapi: 3.0.0
info: {title: Refunds, version: 1.0.0}
servers:
  aws: {host: localhost:4566, protocol: sqs}
channels:
  refunds:
    address: refund-claims
    messages:
      refund:
        headers:
          type: object
          properties:
            reason: {type: string, enum: [DAMAGE, LOST]}
        payload:
          type: object
          required: [claim, amount]
          properties:
            amount: {type: number}
`
