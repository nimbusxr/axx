package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// ---- DynamoDB ----

func (s *service) parcel(ctx context.Context, reference string) (Parcel, bool, error) {
	var p Parcel
	ok, err := s.get(ctx, s.names.InsuredParcels, "reference", reference, &p)
	return p, ok, err
}

func (s *service) claim(ctx context.Context, id string) (Claim, bool, error) {
	var c Claim
	ok, err := s.get(ctx, s.names.Claims, "id", id, &c)
	return c, ok, err
}

func (s *service) get(ctx context.Context, table, key, value string, out any) (bool, error) {
	res, err := s.aws.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(table), Key: map[string]dbtypes.AttributeValue{key: &dbtypes.AttributeValueMemberS{Value: value}},
	})
	if err != nil {
		return false, fmt.Errorf("read %s: %w", table, err)
	}
	if res.Item == nil {
		return false, nil
	}
	return true, attributevalue.UnmarshalMap(res.Item, out)
}

// createClaim stores a new claim; a parcel is claimed once.
func (s *service) createClaim(ctx context.Context, c Claim) error {
	item, err := attributevalue.MarshalMap(c)
	if err != nil {
		return err
	}
	_, err = s.aws.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.names.Claims), Item: item, ConditionExpression: aws.String("attribute_not_exists(id)"),
	})
	var exists *dbtypes.ConditionalCheckFailedException
	if errors.As(err, &exists) {
		return errExists
	}
	return err
}

func (s *service) saveClaim(ctx context.Context, c Claim) error {
	item, err := attributevalue.MarshalMap(c)
	if err != nil {
		return err
	}
	_, err = s.aws.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.names.Claims), Item: item})
	return err
}

// delivered records that a parcel was delivered.
func (s *service) delivered(ctx context.Context, parcel, at string) error {
	_, err := s.aws.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.names.InsuredParcels),
		Key:                       map[string]dbtypes.AttributeValue{"reference": &dbtypes.AttributeValueMemberS{Value: parcel}},
		UpdateExpression:          aws.String("SET deliveredAt = :at"),
		ConditionExpression:       aws.String("attribute_exists(#ref)"), // "reference" is a reserved word
		ExpressionAttributeNames:  map[string]string{"#ref": "reference"},
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{":at": &dbtypes.AttributeValueMemberS{Value: at}},
	})
	var missing *dbtypes.ConditionalCheckFailedException
	if errors.As(err, &missing) {
		return nil // not an insured parcel
	}
	if err == nil {
		s.log.Info("parcel delivered", "parcel", parcel, "at", at)
	}
	return err
}

// ---- S3 ----

func (s *service) putJSON(ctx context.Context, bucket, key string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.aws.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(b), ContentType: aws.String("application/json"),
	})
	return err
}

func (s *service) copyObject(ctx context.Context, fromBucket, fromKey, toBucket, toKey string) error {
	_, err := s.aws.s3.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(toBucket), Key: aws.String(toKey), CopySource: aws.String(fromBucket + "/" + url.PathEscape(fromKey)),
	})
	return err
}

// ---- SQS, SNS, EventBridge ----

func (s *service) send(ctx context.Context, queue string, body any, attrs map[string]string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	in := &sqs.SendMessageInput{QueueUrl: aws.String(s.queues[queue]), MessageBody: aws.String(string(b))}
	if len(attrs) > 0 {
		in.MessageAttributes = map[string]sqstypes.MessageAttributeValue{}
		for k, v := range attrs {
			in.MessageAttributes[k] = sqstypes.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(v)}
		}
	}
	_, err = s.aws.sqs.SendMessage(ctx, in)
	return err
}

func (s *service) publish(ctx context.Context, topic string, body any, attrs map[string]string) error {
	arn, err := s.topicARN(ctx, topic)
	if err != nil {
		return err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	in := &sns.PublishInput{TopicArn: aws.String(arn), Message: aws.String(string(b))}
	if len(attrs) > 0 {
		in.MessageAttributes = map[string]snstypes.MessageAttributeValue{}
		for k, v := range attrs {
			in.MessageAttributes[k] = snstypes.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(v)}
		}
	}
	_, err = s.aws.sns.Publish(ctx, in)
	return err
}

func (s *service) topicARN(ctx context.Context, topic string) (string, error) {
	// CreateTopic is idempotent and returns the ARN of an existing topic.
	out, err := s.aws.sns.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(topic)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.TopicArn), nil
}

func (s *service) putEvent(ctx context.Context, bus, detailType string, detail any) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	out, err := s.aws.eb.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []ebtypes.PutEventsRequestEntry{{
		EventBusName: aws.String(bus), Source: aws.String("parcels.claims"), DetailType: aws.String(detailType), Detail: aws.String(string(b)),
	}}})
	if err != nil {
		return err
	}
	if out.FailedEntryCount > 0 {
		return fmt.Errorf("the %s bus refused the %s event", bus, detailType)
	}
	return nil
}

// ---- workers ----

func (s *service) resolveQueues(ctx context.Context) error {
	s.queues = map[string]string{}
	for _, q := range []string{s.names.EvidenceUploads, s.names.RefundRequests, s.names.RefundResults, s.names.CarrierDamage, s.names.ParcelEvents} {
		out, err := s.aws.sqs.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(q)})
		if err != nil {
			return fmt.Errorf("queue %s: %w", q, err)
		}
		s.queues[q] = aws.ToString(out.QueueUrl)
	}
	return nil
}

// startWorkers processes the queues the service consumes.
func (s *service) startWorkers(ctx context.Context) {
	go s.consume(ctx, s.names.EvidenceUploads, s.evidenceUploaded)
	go s.consume(ctx, s.names.CarrierDamage, s.carrierDamage)
	go s.consume(ctx, s.names.RefundResults, s.refunded)
	go s.consume(ctx, s.names.ParcelEvents, s.parcelEvent)
}

// consume handles a queue's messages; a message whose handling fails comes
// back after its visibility timeout.
func (s *service) consume(ctx context.Context, queue string, handle func(context.Context, sqstypes.Message) error) {
	url := s.queues[queue]
	for ctx.Err() == nil {
		out, err := s.aws.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(url), MaxNumberOfMessages: 10, WaitTimeSeconds: 5, MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("receive failed", "queue", queue, "error", err)
				time.Sleep(time.Second)
			}
			continue
		}
		for _, m := range out.Messages {
			if err := handle(ctx, m); err != nil {
				s.log.Error("message failed; it will come back", "queue", queue, "error", err)
				continue
			}
			_, _ = s.aws.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(url), ReceiptHandle: m.ReceiptHandle})
		}
	}
}

// decode reads a message's JSON body, reporting whether it was JSON.
func decode(m sqstypes.Message, v any) bool {
	return json.Unmarshal([]byte(aws.ToString(m.Body)), v) == nil
}

// evidenceUploaded handles S3 event notifications from the evidence bucket.
func (s *service) evidenceUploaded(ctx context.Context, m sqstypes.Message) error {
	var n struct {
		Records []struct {
			EventName string `json:"eventName"`
			S3        struct {
				Bucket struct{ Name string } `json:"bucket"`
				Object struct{ Key string }  `json:"object"`
			} `json:"s3"`
		}
	}
	if !decode(m, &n) {
		s.log.Warn("evidence notification ignored: not JSON")
		return nil
	}
	for _, r := range n.Records {
		key, err := url.QueryUnescape(r.S3.Object.Key)
		if err != nil {
			key = r.S3.Object.Key
		}
		if err := s.assess(ctx, r.S3.Bucket.Name, key); err != nil {
			return err
		}
	}
	return nil
}

// carrierDamage handles the "Damage Reported" events carriers put on their
// bus, routed to the queue by a rule.
func (s *service) carrierDamage(ctx context.Context, m sqstypes.Message) error {
	var e struct {
		Source string `json:"source"`
		Detail struct{ Parcel, Carrier, Note string }
	}
	if !decode(m, &e) || e.Detail.Parcel == "" {
		s.log.Warn("carrier report ignored: not a Damage Reported event")
		return nil
	}
	return s.carrierReport(ctx, e.Detail.Parcel, e.Detail.Carrier, e.Detail.Note)
}

// refunded handles what payments reports about a refund request.
func (s *service) refunded(ctx context.Context, m sqstypes.Message) error {
	var r struct{ Claim, Status, PaidAt string }
	if !decode(m, &r) {
		s.log.Warn("refund result ignored: not JSON")
		return nil
	}
	failure := ""
	if a, ok := m.MessageAttributes["failureReason"]; ok {
		failure = aws.ToString(a.StringValue)
	}
	return s.refundResult(ctx, r.Claim, r.Status, r.PaidAt, failure)
}

// parcelEvent handles the parcel-events topic (delivered raw to the
// service's queue): deliveries of insured parcels.
func (s *service) parcelEvent(ctx context.Context, m sqstypes.Message) error {
	var e struct{ Parcel, DeliveredAt string }
	_ = json.Unmarshal([]byte(aws.ToString(m.Body)), &e)
	a, ok := m.MessageAttributes["eventType"]
	if !ok {
		s.log.Warn("parcel event ignored: no eventType", "parcel", e.Parcel)
		return nil
	}
	if aws.ToString(a.StringValue) != "ParcelDelivered" || e.Parcel == "" {
		return nil
	}
	return s.delivered(ctx, e.Parcel, e.DeliveredAt)
}
