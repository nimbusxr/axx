// Package awssqs is the aws-sqs pack: messages sent to SQS queues and the
// messages the services under test send to them.
package awssqs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
	"github.com/nimbusxr/axx/packs/aws/internal/queue"
)

const name = "aws-sqs"

const packDoc = `Send messages to SQS queues and check the messages your services send to them.

The steps use the scenario's AWS account (` + "`the {word} aws account with the following properties:`" + `, from aws-core).

**Checking a queue receives from it**: axx takes each message off the queue, as any consumer would. Check the queues your services **write** to (an outbound queue another system reads); a queue your service consumes is checked by what the service does with the messages. axx starts receiving from the queues a run checks once the apps are up, and a check only looks at the messages received since its scenario started. Match on data unique to the scenario: scenarios run in parallel and share the queue.`

// Pack returns the aws-sqs pack.
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
			Pack: name, Target: "sqs queue", Verb: "sent", Field: "attribute", Fields: "attributes",
			Example: "refund-requests", Send: send, Inbox: inbox,
			Received: "axx takes the queue's messages off it: check queues your services write to.",
		}.Steps(),
	}
}

// Prepare plans receiving from the queues the run's checks name.
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
			q := st.Args[1].Raw
			d.Add(acct.Key()+"|"+q, func(ctx context.Context) error {
				_, err := listen(ctx, s, acct, q)
				return err
			})
		}
	}
	return nil
}

// Init starts receiving from the planned queues, now that the apps run.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, name).Run(ctx)
}

func client(ctx context.Context, s *core.Suite, acct *awscore.Account) (*sqs.Client, error) {
	cfg, err := acct.Config(ctx, s)
	if err != nil {
		return nil, err
	}
	return sqs.NewFromConfig(cfg), nil
}

func listen(ctx context.Context, s *core.Suite, acct *awscore.Account, q string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, name).Start(acct.Key()+"|"+q, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		c, err := client(ctx, s, acct)
		if err != nil {
			return nil, err
		}
		url, err := queue.URL(ctx, c, q)
		if err != nil {
			return nil, err
		}
		go queue.Receive(run, c, url, in)
		return nil, nil
	})
}

func inbox(sc *core.Scenario, q, _ string) (*cloudstep.Inbox, error) {
	acct, err := awscore.Default(sc)
	if err != nil {
		return nil, err
	}
	return listen(sc.Context(), sc.Suite(), acct, q)
}

func send(sc *core.Scenario, q string, body []byte, attrs map[string]string) error {
	acct, err := awscore.Default(sc)
	if err != nil {
		return err
	}
	c, err := client(sc.Context(), sc.Suite(), acct)
	if err != nil {
		return err
	}
	url, err := queue.URL(sc.Context(), c, q)
	if err != nil {
		return err
	}
	in := &sqs.SendMessageInput{QueueUrl: aws.String(url), MessageBody: aws.String(string(body))}
	if len(attrs) > 0 {
		in.MessageAttributes = map[string]types.MessageAttributeValue{}
		for k, v := range attrs {
			in.MessageAttributes[k] = types.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(v)}
		}
	}
	_, err = c.SendMessage(sc.Context(), in)
	return err
}
