// Package awseventbridge is the aws-eventbridge pack: events put on
// EventBridge buses and the events the services under test put there.
package awseventbridge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
	"github.com/nimbusxr/axx/packs/aws/internal/queue"
)

const name = "aws-eventbridge"

const packDoc = `Put events on EventBridge buses and check the events your services put there.

The steps use the scenario's AWS account (` + "`the {word} aws account with the following properties:`" + `, from aws-core). An event has a detail type, a source and a JSON detail, as in ` + "`When a \"DamageReported\" event from carrier.kestrel is put on the carrier-events eventbridge bus:`" + ` with the detail as the doc string.

**Checking a bus** does not take events from anyone: for the buses a run checks, axx adds a rule of its own (` + "`axx-<run>-<bus>`" + `, matching every event of the account) with a queue of its own as its target once the apps are up, and removes both when the run ends. The conditions are paths into the event as EventBridge delivers it: ` + "`detail-type`" + `, ` + "`source`" + `, and ` + "`detail.<field>`" + ` for the detail. A check only looks at the events received since its scenario started.`

// Pack returns the aws-eventbridge pack.
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
		Steps: []core.StepDef{
			{
				ID: name + ".put", Keyword: "When", Arg: core.ArgDocString, Since: "0.1.0",
				Expr: "a(n) {string} event from {word} is put on the {word} eventbridge bus:",
				Doc:  "Put an event with the detail type and source on a bus; the doc string is its JSON detail.",
				Examples: []string{
					`When a "DamageReported" event from carrier.kestrel is put on the carrier-events eventbridge bus:`,
				},
				Run: put,
			},
			{
				ID: name + ".received", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr: "[[within {duration} ]]the {word} eventbridge bus has an event where:",
				Doc: "Wait (10s, or the given time) until the bus has an event, put since the scenario started, that meets every row: " +
					"`path | value` on the event (`detail-type`, `source`, `detail.<field>`), compared as text; `null` for null and " +
					"`undefined` for absent. axx adds a rule of its own to the bus for the run.",
				Examples: []string{"Then within 30s the parcels eventbridge bus has an event where:"},
				Run: func(sc *core.Scenario, a core.Args) error {
					rs, err := cloudstep.Conditions(a.Table)
					if err != nil {
						return err
					}
					bus := a.String(1)
					acct, err := awscore.Default(sc)
					if err != nil {
						return err
					}
					in, err := listen(sc.Context(), sc.Suite(), acct, bus)
					if err != nil {
						return err
					}
					return cloudstep.ExpectMessage(sc, cloudstep.Wait(a, 0), in, rs, "attribute", "the "+bus+" eventbridge bus")
				},
			},
		},
	}
}

// Prepare plans listening to the buses the run's checks name.
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
			bus := st.Args[1].Raw
			d.Add(acct.Key()+"|"+bus, func(ctx context.Context) error {
				_, err := listen(ctx, s, acct, bus)
				return err
			})
		}
	}
	return nil
}

// Init adds the rules for the planned buses, now that the apps run.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, name).Run(ctx)
}

func put(sc *core.Scenario, a core.Args) error {
	detail := a.DocString.Content
	if !json.Valid([]byte(detail)) {
		return fmt.Errorf("the event detail must be JSON")
	}
	acct, err := awscore.Default(sc)
	if err != nil {
		return err
	}
	cfg, err := acct.Config(sc.Context(), sc.Suite())
	if err != nil {
		return err
	}
	detailType, source, bus := a.String(0), a.String(1), a.String(2)
	out, err := eventbridge.NewFromConfig(cfg).PutEvents(sc.Context(), &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{{
			EventBusName: aws.String(bus), DetailType: aws.String(detailType), Source: aws.String(source), Detail: aws.String(detail),
		}},
	})
	if err != nil {
		return fmt.Errorf("cannot put the event on the %s eventbridge bus: %w", bus, err)
	}
	if out.FailedEntryCount > 0 && len(out.Entries) > 0 {
		e := out.Entries[0]
		return fmt.Errorf("the %s eventbridge bus refused the event: %s %s", bus, aws.ToString(e.ErrorCode), aws.ToString(e.ErrorMessage))
	}
	sc.Log("put a %q event from %s on the %s eventbridge bus: %s", detailType, source, bus, cloudstep.Compact([]byte(detail)))
	return nil
}

func listen(ctx context.Context, s *core.Suite, acct *awscore.Account, bus string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, name).Start(acct.Key()+"|"+bus, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		cfg, err := acct.Config(ctx, s)
		if err != nil {
			return nil, err
		}
		eb, sqsc := eventbridge.NewFromConfig(cfg), sqs.NewFromConfig(cfg)
		id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("cannot tell the AWS account id: %w", err)
		}
		pattern, _ := json.Marshal(map[string][]string{"account": {aws.ToString(id.Account)}})
		rule := cloudstep.RunID(s) + "-" + bus
		r, err := eb.PutRule(ctx, &eventbridge.PutRuleInput{
			Name: aws.String(rule), EventBusName: aws.String(bus), EventPattern: aws.String(string(pattern)),
		})
		if err != nil {
			return nil, fmt.Errorf("cannot add a rule to the %s eventbridge bus: %w", bus, err)
		}
		removeRule := func(c context.Context) error {
			_, err := eb.DeleteRule(c, &eventbridge.DeleteRuleInput{Name: aws.String(rule), EventBusName: aws.String(bus)})
			return err
		}
		url, qarn, err := queue.Temporary(ctx, sqsc, rule, "events.amazonaws.com", aws.ToString(r.RuleArn))
		if err != nil {
			_ = removeRule(ctx)
			return nil, err
		}
		_, err = eb.PutTargets(ctx, &eventbridge.PutTargetsInput{
			Rule: aws.String(rule), EventBusName: aws.String(bus),
			Targets: []types.Target{{Id: aws.String("axx"), Arn: aws.String(qarn)}},
		})
		if err != nil {
			_ = removeRule(ctx)
			_, _ = sqsc.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(url)})
			return nil, fmt.Errorf("cannot target the rule on the %s eventbridge bus: %w", bus, err)
		}
		go queue.Receive(run, sqsc, url, in)
		return func(c context.Context) error {
			_, err1 := eb.RemoveTargets(c, &eventbridge.RemoveTargetsInput{
				Rule: aws.String(rule), EventBusName: aws.String(bus), Ids: []string{"axx"},
			})
			err2 := removeRule(c)
			_, err3 := sqsc.DeleteQueue(c, &sqs.DeleteQueueInput{QueueUrl: aws.String(url)})
			for _, e := range []error{err1, err2, err3} {
				if e != nil {
					return e
				}
			}
			return nil
		}, nil
	})
}
