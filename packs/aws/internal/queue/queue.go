// Package queue receives messages from SQS queues for the aws-* packs'
// checks: the queues the services under test write to, and the queues axx
// subscribes to SNS topics and EventBridge buses for a run.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/nimbusxr/axx/internal/cloudstep"
)

// URL returns the URL of a queue by name.
func URL(ctx context.Context, c *sqs.Client, name string) (string, error) {
	out, err := c.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return "", fmt.Errorf("no sqs queue named %s: %w", name, err)
	}
	return aws.ToString(out.QueueUrl), nil
}

// Receive receives the queue's messages into the inbox until ctx ends,
// deleting each one it received.
func Receive(ctx context.Context, c *sqs.Client, url string, in *cloudstep.Inbox) {
	for ctx.Err() == nil {
		out, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(url), MaxNumberOfMessages: 10, WaitTimeSeconds: 1,
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A transient failure (the emulator restarting, say) is retried;
			// the check reports if nothing arrives.
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, m := range out.Messages {
			in.Add(cloudstep.Message{Body: []byte(aws.ToString(m.Body)), Fields: attributes(m.MessageAttributes)})
			_, _ = c.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(url), ReceiptHandle: m.ReceiptHandle})
		}
	}
}

func attributes(in map[string]types.MessageAttributeValue) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch {
		case v.StringValue != nil:
			out[k] = aws.ToString(v.StringValue)
		case v.BinaryValue != nil:
			out[k] = string(v.BinaryValue)
		}
	}
	return out
}

// Temporary creates a queue for a run that the given service may send to
// (sns.amazonaws.com, events.amazonaws.com), and returns its URL and ARN.
func Temporary(ctx context.Context, c *sqs.Client, name, service, sourceARN string) (url, arn string, err error) {
	name = safeName(name)
	out, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name)})
	if err != nil {
		return "", "", fmt.Errorf("cannot create the queue %s: %w", name, err)
	}
	url = aws.ToString(out.QueueUrl)
	attrs, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(url), AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", "", err
	}
	arn = attrs.Attributes[string(types.QueueAttributeNameQueueArn)]
	policy, _ := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect": "Allow", "Principal": map[string]string{"Service": service},
			"Action": "sqs:SendMessage", "Resource": arn,
			"Condition": map[string]any{"ArnEquals": map[string]string{"aws:SourceArn": sourceARN}},
		}},
	})
	_, err = c.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(url), Attributes: map[string]string{string(types.QueueAttributeNamePolicy): string(policy)},
	})
	if err != nil {
		return "", "", fmt.Errorf("cannot let %s send to the queue %s: %w", service, name, err)
	}
	return url, arn, nil
}

// safeName keeps a queue name within SQS's rules: 80 characters of
// letters, digits, hyphens and underscores.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}
