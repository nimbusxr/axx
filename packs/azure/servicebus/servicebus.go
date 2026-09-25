// Package azureservicebus is the azure-servicebus pack: messages sent to
// Service Bus queues and topics, and the messages the services under test
// send there.
package azureservicebus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus/admin"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

const name = "azure-servicebus"

const packDoc = `Send messages to Service Bus queues and topics, and check the messages your services send there.

Register the namespace with ` + "`the {word} service bus namespace with the following properties:`" + ` (the first namespace registered is the default), set up as the Azure SDK is set up for the real service:

| Property | |
| --- | --- |
| ` + "`connection string`" + ` | the namespace's connection string, e.g. ` + "`${env:SERVICEBUS_CONNECTION_STRING}`" + `, or a local emulator's (` + "`UseDevelopmentEmulator=true`" + `) |
| ` + "`namespace`" + ` | the fully qualified namespace (` + "`<name>.servicebus.windows.net`" + `), signed in with the Azure default credential chain |
| ` + "`management endpoint`" + ` | where the namespace's management API is when it is not the namespace's own host, as with local emulators |

Steps name a **queue** or a **topic**: ` + "`the customs-filings service bus queue`" + `, ` + "`the customs-events service bus topic`" + `. Messages carry application properties (` + "`with the following properties:`" + `, and ` + "`property <name>`" + ` rows in checks).

**Checking a queue receives from it**: axx completes each message it receives, as any consumer would, so check the queues your services **write** to. **Checking a topic** takes nothing from anyone: for the topics a run checks, axx creates a subscription of its own (` + "`axx-<run>`" + `, through the management API, so it needs the Manage right) once the apps are up, and deletes it when the run ends. A check only looks at the messages received since its scenario started.`

// Pack returns the azure-servicebus pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Preparer    = pack{}
	_ core.Initializer = pack{}
)

var messages = cloudstep.Messages{
	Pack: name, Target: "service bus queue/topic", Verb: "sent", Field: "property", Fields: "properties",
	Example: "customs-filings", Send: send, Inbox: inbox,
	Received: "axx completes the messages of a queue it checks, and subscribes to a topic for the run.",
}

func (pack) Manifest() core.Manifest {
	steps := []core.StepDef{{
		ID: name + ".namespace", Keyword: "Given", Arg: core.ArgTable, Since: "0.1.0",
		Expr: "the {word} service bus namespace with the following properties:",
		Doc: "Register the Service Bus namespace the steps talk to: `connection string`, or `namespace` with the Azure default " +
			"credential chain; `management endpoint` for an emulator whose management API is elsewhere.",
		Examples: []string{"Given the customs service bus namespace with the following properties:"},
		Run: func(sc *core.Scenario, a core.Args) error {
			ns, err := parse(sc.Suite(), a.String(0), a.Table)
			if err != nil {
				return err
			}
			return namespaces.Of(sc).Add(ns.name, ns)
		},
	}}
	return core.Manifest{Name: name, Namespace: name, Doc: packDoc, Steps: append(steps, messages.Steps()...)}
}

type namespace struct {
	name, connection, fqdn, management string
}

func (n *namespace) key() string { return n.connection + "|" + n.fqdn + "|" + n.management }

func parse(s *core.Suite, nm string, t *core.Table) (*namespace, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	n := &namespace{name: nm}
	for _, p := range pairs {
		v := strings.TrimSpace(s.Interpolate(p.Value))
		switch p.Key {
		case "connection string":
			n.connection = v
		case "namespace":
			n.fqdn = v
		case "management endpoint":
			n.management = strings.TrimRight(v, "/")
		default:
			return nil, fmt.Errorf("unknown service bus namespace property %q (supported: connection string, namespace, management endpoint)", p.Key)
		}
	}
	if (n.connection == "") == (n.fqdn == "") {
		return nil, fmt.Errorf(`give the service bus namespace either a "connection string" or a "namespace"`)
	}
	return n, nil
}

var namespaces = core.NewStateKey(name, func(*core.Scenario) *core.Services[*namespace] {
	return core.NewServices[*namespace]("Service Bus namespace",
		`No Service Bus namespace is registered in this scenario; register one with "the {word} service bus namespace with the following properties:"`)
}, nil)

// clients are a namespace's messaging and management clients, shared by the
// run.
type clients struct {
	msg   *azservicebus.Client
	admin *admin.Client
}

func (c *clients) Close() error { return c.msg.Close(context.Background()) }

func open(s *core.Suite, n *namespace) (*clients, error) {
	return core.Cached(s, name+"/clients/"+n.key(), func() (*clients, error) {
		var opts azcore.ClientOptions
		if n.management != "" {
			u, err := url.Parse(n.management)
			if err != nil {
				return nil, fmt.Errorf("the management endpoint: %w", err)
			}
			opts.PerRetryPolicies = []policy.Policy{relocate{u}}
		}
		c := &clients{}
		var err error
		if n.connection != "" {
			if c.msg, err = azservicebus.NewClientFromConnectionString(n.connection, nil); err != nil {
				return nil, err
			}
			c.admin, err = admin.NewClientFromConnectionString(n.connection, &admin.ClientOptions{ClientOptions: opts})
		} else {
			cred, cerr := azidentity.NewDefaultAzureCredential(nil)
			if cerr != nil {
				return nil, cerr
			}
			if c.msg, err = azservicebus.NewClient(n.fqdn, cred, nil); err != nil {
				return nil, err
			}
			c.admin, err = admin.NewClient(n.fqdn, cred, &admin.ClientOptions{ClientOptions: opts})
		}
		if err != nil {
			return nil, err
		}
		s.OnClose(func(context.Context) error { return c.Close() })
		return c, nil
	})
}

// relocate sends management requests to the management endpoint.
type relocate struct{ base *url.URL }

func (r relocate) Do(req *policy.Request) (*http.Response, error) {
	u := req.Raw().URL
	u.Scheme, u.Host = r.base.Scheme, r.base.Host
	u.Path = strings.TrimRight(r.base.Path, "/") + u.Path
	req.Raw().Host = r.base.Host
	return req.Next()
}

// Prepare plans listening to the queues and topics the run's checks name.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	d := cloudstep.DeferredFor(s, name)
	for _, sc := range plan.Scenarios {
		var ns *namespace
		for _, st := range sc.Steps {
			if st.Definition == name+".namespace" && ns == nil {
				ns, _ = parse(s, st.Args[0].Raw, st.Table)
			}
			if st.Definition != name+".received" || ns == nil {
				continue
			}
			n, entity, noun := ns, st.Args[1].Raw, messages.Noun(st.Text)
			d.Add(n.key()+"|"+noun+"|"+entity, func(ctx context.Context) error {
				_, err := listen(ctx, s, n, entity, noun)
				return err
			})
		}
	}
	return nil
}

// Init starts listening to the planned queues and topics, now that the apps
// run.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, name).Run(ctx)
}

func listen(ctx context.Context, s *core.Suite, n *namespace, entity, noun string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, name).Start(n.key()+"|"+noun+"|"+entity, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		c, err := open(s, n)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(noun, "queue") {
			r, err := c.msg.NewReceiverForQueue(entity, nil)
			if err != nil {
				return nil, err
			}
			go receive(run, r, in)
			return func(c context.Context) error { return r.Close(c) }, nil
		}
		sub := cloudstep.RunID(s)
		if _, err := c.admin.CreateSubscription(ctx, entity, sub, nil); err != nil {
			return nil, fmt.Errorf("cannot subscribe to the %s service bus topic (axx needs the Manage right): %w", entity, err)
		}
		r, err := c.msg.NewReceiverForSubscription(entity, sub, nil)
		if err != nil {
			return nil, err
		}
		go receive(run, r, in)
		return func(cc context.Context) error {
			_ = r.Close(cc)
			_, err := c.admin.DeleteSubscription(cc, entity, sub, nil)
			return err
		}, nil
	})
}

func receive(ctx context.Context, r *azservicebus.Receiver, in *cloudstep.Inbox) {
	for ctx.Err() == nil {
		ms, err := r.ReceiveMessages(ctx, 10, nil)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, m := range ms {
			fields := map[string]string{}
			for k, v := range m.ApplicationProperties {
				fields[k] = fmt.Sprint(v)
			}
			in.Add(cloudstep.Message{Body: m.Body, Fields: fields})
			_ = r.CompleteMessage(ctx, m, nil)
		}
	}
}

func inbox(sc *core.Scenario, entity, noun string) (*cloudstep.Inbox, error) {
	n, err := namespaces.Of(sc).Default()
	if err != nil {
		return nil, err
	}
	return listen(sc.Context(), sc.Suite(), n, entity, noun)
}

func send(sc *core.Scenario, entity string, body []byte, props map[string]string) error {
	n, err := namespaces.Of(sc).Default()
	if err != nil {
		return err
	}
	c, err := open(sc.Suite(), n)
	if err != nil {
		return err
	}
	snd, err := c.msg.NewSender(entity, nil)
	if err != nil {
		return err
	}
	defer snd.Close(sc.Context())
	m := &azservicebus.Message{Body: body}
	if len(props) > 0 {
		m.ApplicationProperties = map[string]any{}
		for k, v := range props {
			m.ApplicationProperties[k] = v
		}
	}
	if err := snd.SendMessage(sc.Context(), m, nil); err != nil {
		var sbErr *azservicebus.Error
		if errors.As(err, &sbErr) && sbErr.Code == azservicebus.CodeNotFound {
			return fmt.Errorf("no service bus queue or topic named %s", entity)
		}
		return err
	}
	return nil
}
