// Package gcppubsub is the gcp-pubsub pack: messages published to Pub/Sub
// topics and the messages the services under test publish.
package gcppubsub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

const name = "gcp-pubsub"

const packDoc = `Publish messages to Pub/Sub topics and check the messages your services publish.

The steps use the scenario's project (` + "`the {word} gcp project with the following properties:`" + `, from gcp-core).

**Checking a topic** does not take messages from anyone: for the topics a run checks, axx creates a subscription of its own (` + "`axx-<run>-<topic>`" + `) once the apps are up, and deletes it when the run ends. A check only looks at the messages received since its scenario started; match on data unique to the scenario, since scenarios run in parallel. The conditions are paths into the message data (JSON) and ` + "`attribute <name>`" + ` rows for its attributes.`

// Pack returns the gcp-pubsub pack.
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
		Requires:  []string{gcpcore.Name},
		Steps: cloudstep.Messages{
			Pack: name, Target: "pubsub topic", Verb: "published", Field: "attribute", Fields: "attributes",
			Example: "invoice-events", Send: publish, Inbox: inbox,
			Received: "axx creates a subscription of its own to the topic for the run.",
		}.Steps(),
	}
}

// Prepare plans subscribing to the topics the run's checks name.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	projects := gcpcore.Planned(s, plan)
	d := cloudstep.DeferredFor(s, name)
	for _, sc := range plan.Scenarios {
		p := projects[sc.ID]
		if p == nil {
			continue
		}
		for _, st := range sc.Steps {
			if st.Definition != name+".received" {
				continue
			}
			topic := st.Args[1].Raw
			d.Add(p.Key()+"|"+topic, func(ctx context.Context) error {
				_, err := listen(ctx, s, p, topic)
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

func client(ctx context.Context, s *core.Suite, p *gcpcore.Project) (*pubsub.Client, error) {
	return gcpcore.Client(ctx, s, name, p, func(ctx context.Context) (*pubsub.Client, error) {
		return pubsub.NewClient(ctx, p.ID, p.GRPC()...)
	})
}

func topicName(p *gcpcore.Project, topic string) string {
	if strings.HasPrefix(topic, "projects/") {
		return topic
	}
	return "projects/" + p.ID + "/topics/" + topic
}

func listen(ctx context.Context, s *core.Suite, p *gcpcore.Project, topic string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, name).Start(p.Key()+"|"+topic, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		c, err := client(ctx, s, p)
		if err != nil {
			return nil, err
		}
		sub := "projects/" + p.ID + "/subscriptions/" + cloudstep.RunID(s) + "-" + topic
		_, err = c.SubscriptionAdminClient.CreateSubscription(ctx, &pubsubpb.Subscription{
			Name: sub, Topic: topicName(p, topic), AckDeadlineSeconds: 30,
		})
		if err != nil {
			return nil, fmt.Errorf("cannot subscribe to the %s pubsub topic: %w", topic, err)
		}
		go func() {
			for run.Err() == nil {
				err := c.Subscriber(sub).Receive(run, func(_ context.Context, m *pubsub.Message) {
					in.Add(cloudstep.Message{Body: m.Data, Fields: m.Attributes})
					m.Ack()
				})
				if err != nil && run.Err() == nil {
					// A broken stream is opened again; the check reports if
					// nothing arrives.
					time.Sleep(500 * time.Millisecond)
				}
			}
		}()
		return func(c2 context.Context) error {
			err := c.SubscriptionAdminClient.DeleteSubscription(c2, &pubsubpb.DeleteSubscriptionRequest{Subscription: sub})
			if status.Code(err) == codes.NotFound {
				return nil // already gone
			}
			return err
		}, nil
	})
}

func inbox(sc *core.Scenario, topic, _ string) (*cloudstep.Inbox, error) {
	p, err := gcpcore.Default(sc)
	if err != nil {
		return nil, err
	}
	return listen(sc.Context(), sc.Suite(), p, topic)
}

func publish(sc *core.Scenario, topic string, body []byte, attrs map[string]string) error {
	p, err := gcpcore.Default(sc)
	if err != nil {
		return err
	}
	c, err := client(sc.Context(), sc.Suite(), p)
	if err != nil {
		return err
	}
	pub := c.Publisher(topicName(p, topic))
	defer pub.Stop()
	_, err = pub.Publish(sc.Context(), &pubsub.Message{Data: body, Attributes: attrs}).Get(sc.Context())
	return err
}
