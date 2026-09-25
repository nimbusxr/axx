//go:build integration

package awsdynamodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

func TestItems(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci:latest", "4566", "/_localstack/health", nil)
	h := cloudtest.New(t, awscore.Pack(), Pack())
	h.OK("the parcels aws account with the following properties:", [][]string{
		{"region", "eu-west-1"},
		{"endpoint", "http://" + addr},
		{"access key id", "test"},
		{"secret access key", "test"},
	})
	c, err := client(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, table := range []string{"insured-parcels", "claims"} {
		_, err := c.CreateTable(ctx, &dynamodb.CreateTableInput{
			TableName:            aws.String(table),
			AttributeDefinitions: []types.AttributeDefinition{{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS}},
			KeySchema:            []types.KeySchemaElement{{AttributeName: aws.String("id"), KeyType: types.KeyTypeHash}},
			BillingMode:          types.BillingModePayPerRequest,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	h.File("seeds/insured.yaml", `
insured-parcels:
  - id: PX-CLM-1001
    declaredValue: 120
    carrier: KESTREL
    recipient: {name: Ada Lovelace, address: {city: Leeds}}
    tags: [fragile, express]
    signature: null
  - id: PX-CLM-1002
    declaredValue: 49.99
    carrier: KESTREL
`)
	h.OK("a seeds/insured.yaml dynamodb seed")
	h.OK("the insured-parcels dynamodb table has an item where:", [][]string{
		{"id", "PX-CLM-1001"}, {"declaredValue", "120"}, {"recipient.address.city", "Leeds"}, {"$.tags[1]", "express"}, {"signature", "null"},
	})
	h.OK("the insured-parcels dynamodb table has 2 items where:", [][]string{{"carrier", "KESTREL"}})
	h.OK("the insured-parcels dynamodb table has 0 items where:", [][]string{{"carrier", "HERON"}})

	// An item the service writes later is waited for.
	go func() {
		time.Sleep(time.Second)
		_, _ = c.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("claims"), Item: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: "CLM-1"}, "status": &types.AttributeValueMemberS{Value: "APPROVED"},
			"amount": &types.AttributeValueMemberN{Value: "120"},
		}})
	}()
	h.OK("within 5s the claims dynamodb table has an item where:", [][]string{{"id", "CLM-1"}, {"status", "APPROVED"}, {"amount", "120"}})
	err = h.Fails("within 1s the claims dynamodb table has an item where:", "The claims dynamodb table did not have an item", [][]string{{"status", "REJECTED"}})
	if want := `{"amount":120,"id":"CLM-1","status":"APPROVED"}`; !strings.Contains(err.Error(), want) {
		t.Errorf("the failure should show the items: %v", err)
	}
	h.Fails("within 1s the insured-parcels dynamodb table has 1 item where:", "2 did", [][]string{{"carrier", "KESTREL"}})
}
