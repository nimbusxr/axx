//go:build integration

package awseventbridge

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

func TestBuses(t *testing.T) {
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
	cfg, err := acct.Config(context.Background(), h.Suite)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	eb := eventbridge.NewFromConfig(cfg)
	for _, bus := range []string{"parcels", "carrier-events"} {
		if _, err := eb.CreateEventBus(ctx, &eventbridge.CreateEventBusInput{Name: aws.String(bus)}); err != nil {
			t.Fatal(err)
		}
	}

	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the parcels aws account with the following properties:", Table: account},
		cloudtest.PlannedStep{Text: "the parcels eventbridge bus has an event where:", Table: [][]string{{"detail.claim", "CLM-1"}}},
		cloudtest.PlannedStep{Text: "the carrier-events eventbridge bus has an event where:", Table: [][]string{{"detail.parcel", "PX-1"}}},
	))
	// The service under test puts a ClaimSettled event.
	_, err = eb.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []types.PutEventsRequestEntry{{
		EventBusName: aws.String("parcels"), Source: aws.String("parcels.claims"), DetailType: aws.String("Claim Settled"),
		Detail: aws.String(`{"claim":"CLM-1","amount":120.5}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	h.OK("within 10s the parcels eventbridge bus has an event where:", [][]string{
		{"detail-type", "Claim Settled"}, {"source", "parcels.claims"}, {"detail.claim", "CLM-1"}, {"detail.amount", "120.5"},
	})
	h.Fails("within 1s the parcels eventbridge bus has an event where:", `"detail-type":"Claim Settled"`, [][]string{{"detail.claim", "CLM-2"}})

	// Putting an event, checked through the same rule.
	h.OK(`a "Damage Reported" event from carrier.kestrel is put on the carrier-events eventbridge bus:`, `{"parcel":"PX-1","damage":"crushed"}`)
	h.OK("the carrier-events eventbridge bus has an event where:", [][]string{
		{"detail-type", "Damage Reported"}, {"source", "carrier.kestrel"}, {"detail.parcel", "PX-1"},
	})
	h.Fails(`a "Damage Reported" event from carrier.kestrel is put on the carrier-events eventbridge bus:`, "must be JSON", "crushed")

	// The run's rules and queues go when the suite closes.
	if err := h.Suite.Close(ctx); err != nil {
		t.Fatal(err)
	}
	rules, err := eb.ListRules(ctx, &eventbridge.ListRulesInput{EventBusName: aws.String("parcels")})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.Rules) != 0 {
		t.Errorf("rules left: %+v", rules.Rules)
	}
	qs, err := sqs.NewFromConfig(cfg).ListQueues(ctx, &sqs.ListQueuesInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(qs.QueueUrls) != 0 {
		t.Errorf("queues left: %v", qs.QueueUrls)
	}
}
