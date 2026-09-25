//go:build integration

package awssns

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

func TestTopics(t *testing.T) {
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
	snsc, sqsc, err := clients(context.Background(), h.Suite, acct)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	topics := map[string]string{}
	for _, name := range []string{"claim-decisions", "parcel-events"} {
		out, err := snsc.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(name)})
		if err != nil {
			t.Fatal(err)
		}
		topics[name] = aws.ToString(out.TopicArn)
	}

	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the parcels aws account with the following properties:", Table: account},
		cloudtest.PlannedStep{Text: "the claim-decisions sns topic has a message where:", Table: [][]string{{"claim", "CLM-1"}}},
		cloudtest.PlannedStep{Text: "the parcel-events sns topic has a message where:", Table: [][]string{{"parcel", "PX-1"}}},
	))
	// The service under test publishes a decision.
	_, err = snsc.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(topics["claim-decisions"]), Message: aws.String(`{"claim":"CLM-1","decision":"APPROVED"}`),
		MessageAttributes: map[string]types.MessageAttributeValue{"eventType": {DataType: aws.String("String"), StringValue: aws.String("ClaimDecided")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.OK("the claim-decisions sns topic has a message where:", [][]string{
		{"claim", "CLM-1"}, {"decision", "APPROVED"}, {"attribute eventType", "ClaimDecided"},
	})
	h.Fails("within 1s the claim-decisions sns topic has a message where:", `1 message since the scenario started:
  attribute eventType=ClaimDecided {"claim":"CLM-1","decision":"APPROVED"}`, [][]string{{"decision", "REJECTED"}})

	// Publishing, checked through the same subscription.
	h.OK("a message is published to the parcel-events sns topic:", `{"parcel":"PX-1","event":"DELIVERED"}`)
	h.File("messages/delivered.json", `{"parcel":"PX-2","event":"DELIVERED"}`)
	h.OK("the messages/delivered.json message is published to the parcel-events sns topic with the following attributes:", [][]string{{"eventType", "ParcelDelivered"}})
	h.OK("the parcel-events sns topic has a message where:", [][]string{{"parcel", "PX-1"}, {"attribute eventType", "undefined"}})
	h.OK("the parcel-events sns topic has a message where:", [][]string{{"parcel", "PX-2"}, {"attribute eventType", "ParcelDelivered"}})

	// The run's subscriptions and queues go when the suite closes.
	if err := h.Suite.Close(ctx); err != nil {
		t.Fatal(err)
	}
	subs, err := snsc.ListSubscriptionsByTopic(ctx, &sns.ListSubscriptionsByTopicInput{TopicArn: aws.String(topics["claim-decisions"])})
	if err != nil {
		t.Fatal(err)
	}
	if len(subs.Subscriptions) != 0 {
		t.Errorf("subscriptions left: %+v", subs.Subscriptions)
	}
	qs, err := sqsc.ListQueues(ctx, &sqs.ListQueuesInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(qs.QueueUrls) != 0 {
		t.Errorf("queues left: %v", qs.QueueUrls)
	}
}
