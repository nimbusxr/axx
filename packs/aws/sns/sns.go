// Package awssns is the aws-sns pack: messages published to SNS topics and
// the messages the services under test publish.
package awssns

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
	"github.com/nimbusxr/axx/packs/aws/internal/queue"
)

const name = "aws-sns"

const packDoc = `Publish messages to SNS topics and check the messages your services publish.

The steps use the scenario's AWS account (` + "`the {word} aws account with the following properties:`" + `, from aws-core).

**Checking a topic** does not take messages from anyone: for the topics a run checks, axx subscribes a queue of its own (` + "`axx-<run>-<topic>`" + `, with raw message delivery) once the apps are up, and removes the subscription and the queue when the run ends. A check only looks at the messages received since its scenario started; match on data unique to the scenario, since scenarios run in parallel.`

// Pack returns the aws-sns pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Preparer    = pack{}
	_ core.Initializer = pack{}
)

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      name,
		Namespace: name,
		Doc:       packDoc,
		Requires:  []string{awscore.Name},
		Steps: cloudstep.Messages{
			Pack: name, Target: "sns topic", Verb: "published", Field: "attribute", Fields: "attributes",
			Example: "claim-decisions", Send: publish, Inbox: inbox,
			Received: "axx subscribes a queue of its own to the topic for the run.",
		}.Steps(),
	}
}

// Prepare plans subscribing to the topics the run's checks name.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	accounts := awscore.Planned(s, plan)
	d := cloudstep.DeferredFor(s, name)
	for _, sc := range plan.Scenarios {
		acct := accounts[sc.ID]
		if acct == nil {
			continue
		}
		for _, st := range sc.Steps {
			if st.Definition != name+".received" {
				continue
			}
			topic := st.Args[1].Raw
			d.Add(acct.Key()+"|"+topic, func(ctx context.Context) error {
				_, err := listen(ctx, s, acct, topic)
				return err
			})
		}
	}
	return nil
}

// Init subscribes to the planned topics, now that the apps run.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, name).Run(ctx)
}

func clients(ctx context.Context, s *core.Suite, acct *awscore.Account) (*sns.Client, *sqs.Client, error) {
	cfg, err := acct.Config(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	return sns.NewFromConfig(cfg), sqs.NewFromConfig(cfg), nil
}

// topicARN finds a topic by name.
func topicARN(ctx context.Context, c *sns.Client, topic string) (string, error) {
	p := sns.NewListTopicsPaginator(c, &sns.ListTopicsInput{})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, t := range out.Topics {
			if arn := aws.ToString(t.TopicArn); strings.HasSuffix(arn, ":"+topic) {
				return arn, nil
			}
		}
	}
	return "", fmt.Errorf("no sns topic named %s", topic)
}

func listen(ctx context.Context, s *core.Suite, acct *awscore.Account, topic string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, name).Start(acct.Key()+"|"+topic, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		snsc, sqsc, err := clients(ctx, s, acct)
		if err != nil {
			return nil, err
		}
		arn, err := topicARN(ctx, snsc, topic)
		if err != nil {
			return nil, err
		}
		url, qarn, err := queue.Temporary(ctx, sqsc, cloudstep.RunID(s)+"-"+topic, "sns.amazonaws.com", arn)
		if err != nil {
			return nil, err
		}
		sub, err := snsc.Subscribe(ctx, &sns.SubscribeInput{
			TopicArn: aws.String(arn), Protocol: aws.String("sqs"), Endpoint: aws.String(qarn),
			Attributes: map[string]string{"RawMessageDelivery": "true"}, ReturnSubscriptionArn: true,
		})
		if err != nil {
			_, _ = sqsc.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(url)})
			return nil, fmt.Errorf("cannot subscribe to the sns topic %s: %w", topic, err)
		}
		go queue.Receive(run, sqsc, url, in)
		return func(c context.Context) error {
			_, err1 := snsc.Unsubscribe(c, &sns.UnsubscribeInput{SubscriptionArn: sub.SubscriptionArn})
			_, err2 := sqsc.DeleteQueue(c, &sqs.DeleteQueueInput{QueueUrl: aws.String(url)})
			if err1 != nil {
				return err1
			}
			return err2
		}, nil
	})
}

func inbox(sc *core.Scenario, topic, _ string) (*cloudstep.Inbox, error) {
	acct, err := awscore.Default(sc)
	if err != nil {
		return nil, err
	}
	return listen(sc.Context(), sc.Suite(), acct, topic)
}

func publish(sc *core.Scenario, topic string, body []byte, attrs map[string]string) error {
	acct, err := awscore.Default(sc)
	if err != nil {
		return err
	}
	c, _, err := clients(sc.Context(), sc.Suite(), acct)
	if err != nil {
		return err
	}
	arn, err := topicARN(sc.Context(), c, topic)
	if err != nil {
		return err
	}
	in := &sns.PublishInput{TopicArn: aws.String(arn), Message: aws.String(string(body))}
	if len(attrs) > 0 {
		in.MessageAttributes = map[string]types.MessageAttributeValue{}
		for k, v := range attrs {
			in.MessageAttributes[k] = types.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(v)}
		}
	}
	_, err = c.Publish(sc.Context(), in)
	return err
}
