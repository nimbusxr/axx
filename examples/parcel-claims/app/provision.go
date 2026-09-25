package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// provision creates the AWS resources of the service, as the infrastructure
// code of a real account does. It can run again: what exists is kept.
func provision(ctx context.Context, c clients, n names, log *slog.Logger) error {
	if err := waitForAWS(ctx, c); err != nil {
		return err
	}
	for table, key := range map[string]string{n.InsuredParcels: "reference", n.Claims: "id"} {
		_, err := c.db.CreateTable(ctx, &dynamodb.CreateTableInput{
			TableName:            aws.String(table),
			AttributeDefinitions: []dbtypes.AttributeDefinition{{AttributeName: aws.String(key), AttributeType: dbtypes.ScalarAttributeTypeS}},
			KeySchema:            []dbtypes.KeySchemaElement{{AttributeName: aws.String(key), KeyType: dbtypes.KeyTypeHash}},
			BillingMode:          dbtypes.BillingModePayPerRequest,
		})
		var inUse *dbtypes.ResourceInUseException
		if err != nil && !errors.As(err, &inUse) {
			return fmt.Errorf("table %s: %w", table, err)
		}
	}
	for _, b := range []string{n.Evidence, n.Letters, n.Reviews} {
		_, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(b)})
		var owned *s3types.BucketAlreadyOwnedByYou
		if err != nil && !errors.As(err, &owned) {
			return fmt.Errorf("bucket %s: %w", b, err)
		}
	}
	queueARN := map[string]string{}
	for _, q := range []string{n.EvidenceUploads, n.RefundRequests, n.RefundResults, n.CarrierDamage, n.ParcelEvents} {
		out, err := c.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(q)})
		if err != nil {
			return fmt.Errorf("queue %s: %w", q, err)
		}
		attrs, err := c.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: out.QueueUrl, AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
		})
		if err != nil {
			return err
		}
		queueARN[q] = attrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
	}
	// New evidence photos notify the evidence-uploads queue.
	_, err := c.s3.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket: aws.String(n.Evidence),
		NotificationConfiguration: &s3types.NotificationConfiguration{QueueConfigurations: []s3types.QueueConfiguration{{
			QueueArn: aws.String(queueARN[n.EvidenceUploads]), Events: []s3types.Event{"s3:ObjectCreated:*"},
		}}},
	})
	if err != nil {
		return fmt.Errorf("evidence notifications: %w", err)
	}
	for _, t := range []string{n.Decisions, n.ParcelEventsTopic} {
		if _, err := c.sns.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(t)}); err != nil {
			return fmt.Errorf("topic %s: %w", t, err)
		}
	}
	// The parcel-events topic delivers to the service's queue as it was sent.
	topic, err := c.sns.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(n.ParcelEventsTopic)})
	if err != nil {
		return err
	}
	if _, err := c.sns.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn: topic.TopicArn, Protocol: aws.String("sqs"), Endpoint: aws.String(queueARN[n.ParcelEvents]),
		Attributes: map[string]string{"RawMessageDelivery": "true"},
	}); err != nil {
		return fmt.Errorf("parcel-events subscription: %w", err)
	}
	for _, bus := range []string{n.ClaimsBus, n.CarrierBus} {
		_, err := c.eb.CreateEventBus(ctx, &eventbridge.CreateEventBusInput{Name: aws.String(bus)})
		var exists *ebtypes.ResourceAlreadyExistsException
		if err != nil && !errors.As(err, &exists) {
			return fmt.Errorf("bus %s: %w", bus, err)
		}
	}
	// Carriers' damage reports go to the service's queue.
	pattern, _ := json.Marshal(map[string][]string{"detail-type": {"Damage Reported"}})
	if _, err := c.eb.PutRule(ctx, &eventbridge.PutRuleInput{
		Name: aws.String("claims-damage-reports"), EventBusName: aws.String(n.CarrierBus), EventPattern: aws.String(string(pattern)),
	}); err != nil {
		return fmt.Errorf("damage report rule: %w", err)
	}
	if _, err := c.eb.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule: aws.String("claims-damage-reports"), EventBusName: aws.String(n.CarrierBus),
		Targets: []ebtypes.Target{{Id: aws.String("claims"), Arn: aws.String(queueARN[n.CarrierDamage])}},
	}); err != nil {
		return fmt.Errorf("damage report target: %w", err)
	}
	log.Info("provisioned", "tables", 2, "buckets", 3, "queues", len(queueARN), "topics", 2, "buses", 2)
	return nil
}

// waitForAWS waits until the AWS endpoint answers (an emulator starting
// next to the service).
func waitForAWS(ctx context.Context, c clients) error {
	deadline := time.Now().Add(time.Minute)
	for {
		_, err := c.sqs.ListQueues(ctx, &sqs.ListQueuesInput{})
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || !strings.Contains(err.Error(), "connect") {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
