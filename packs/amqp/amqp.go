// Package amqp is the amqp pack: messages sent to AMQP queues and published
// to AMQP exchanges, and the messages the services under test send there,
// over AMQP 0-9-1 (RabbitMQ) or AMQP 1.0 (ActiveMQ Artemis, Qpid).
package amqp

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "amqp"

const since = "0.1.5"

const packDoc = `Send messages to AMQP queues and publish them to AMQP exchanges, and check the messages your services send there: RabbitMQ over AMQP 0-9-1, and brokers that speak AMQP 1.0, such as ActiveMQ Artemis.

Register the broker once, usually in the ` + "`Background`" + `; every AMQP step of the scenario uses it:

` + "```gherkin" + `
Given the depot amqp broker with the following properties:
  | url | amqp://parcels:${env:RABBITMQ_PASSWORD}@localhost:5672/ |
` + "```" + `

Steps name a **queue** or an **exchange**: ` + "`the parcels.label-printed amqp queue`" + `, ` + "`the printers amqp exchange`" + `. Messages carry a routing key (` + "`with the routing key '...'`" + `, or a ` + "`routing key`" + ` row), headers (` + "`header <name>`" + ` rows) and properties such as ` + "`content type`" + `. A message whose body is JSON is sent as ` + "`application/json`" + ` unless a ` + "`content type`" + ` row says otherwise, and every message is persistent.

- **Checking a queue receives from it:** axx acknowledges each message it receives, as any consumer would, so check the queues your services **write** to and nothing else reads.
- **Checking an exchange takes nothing from anyone:** for the exchanges a run checks, axx declares a queue of its own (` + "`axx-<run>.<exchange>`" + `, exclusive, deleted with the run) once the apps are up, and binds it with ` + "`#`" + ` and with the routing keys the checks name, which covers topic, fanout, headers and direct exchanges.
- **A check only looks at the messages received since its scenario started,** so scenarios running in parallel check their own messages, by data unique to them.
- **Publishing a message nothing receives fails the step:** the broker returns a message no queue is bound for, and the step says so, instead of the message going nowhere unnoticed.
- **AMQP 1.0:** with ` + "`protocol | 1.0`" + `, a queue is an anycast address and an exchange a multicast one, and the routing key is the message's subject. RabbitMQ routes the same messages over 0-9-1 whatever protocol your services use, so keep the default for it.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the URL are masked in logs and failures.`

// Pack returns the amqp pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Preparer    = pack{}
	_ core.Initializer = pack{}
)

// meta are the values of a message itself that checks read by name.
var meta = []string{"routing key", "content type", "correlation id", "message id", "reply to"}

var propertyRows = []core.TableRow{
	{Name: "header <name>", Takes: "a header sent with the message: its name after `header `, and its value"},
	{Name: "content type", Takes: "the body's media type", Default: "`application/json` for a JSON body, `text/plain` otherwise"},
	{Name: "correlation id", Takes: "the message's correlation ID"},
	{Name: "message id", Takes: "the message's ID"},
	{Name: "reply to", Takes: "where replies go: a queue's name"},
}

var queues = cloudstep.Messages{
	Pack: Name, Kind: "queue", Target: "amqp queue", Verb: "sent", Field: "header", Fields: "properties",
	Meta: meta, SendRows: propertyRows, Since: since,
	Send: sender("queue"), Inbox: inbox("queue"), Found: found,
	Example: cloudstep.Sample{
		To: "parcels.label-printed", Body: `{"reference": "PX-8103", "printer": "LEJ-3", "printedAt": "2026-09-28T08:15:00Z"}`,
		File: "amqp/label-printed.json", Fields: [][2]string{{"header printer", "LEJ-3"}, {"content type", "application/json"}},
		From: "parcels.label-printed.rejected", Where: [][2]string{{"reference", "PX-8199"}, {"header reason", "unknown parcel"}},
	},
	Received: "axx acknowledges the messages of a queue it checks, so check the queues your services write to.",
}

var exchanges = cloudstep.Messages{
	Pack: Name, Kind: "exchange", Target: "amqp exchange", Verb: "published", Field: "header", Fields: "properties",
	Key: "routing key", Meta: meta, Since: since,
	SendRows: append([]core.TableRow{{Name: "routing key", Takes: "the routing key the exchange routes the message by"}}, propertyRows...),
	Send:     sender("exchange"), Inbox: inbox("exchange"), Found: found,
	Example: cloudstep.Sample{
		To: "printers", Key: "printed.LEJ", Body: `{"reference": "PX-8102", "printer": "LEJ-3", "printedAt": "2026-09-28T08:15:00Z"}`,
		File: "amqp/label-printed.json", Fields: [][2]string{{"routing key", "printed.LEJ"}, {"header printer", "LEJ-3"}},
		From: "labels", Where: [][2]string{{"routing key", "print.express"}, {"reference", "PX-8101"}, {"header sender", "maple-crafts"}},
	},
	Received: "axx binds a queue of its own to an exchange it checks, for the run.",
}

func (pack) Manifest() core.Manifest {
	steps := []core.StepDef{{
		ID: Name + ".broker", Keyword: "Given", Arg: core.ArgTable, Since: since,
		Expr: "the {word} amqp broker with the following properties:",
		Doc: "Register the AMQP broker the steps talk to.\n\n" +
			"- The first broker registered is the one the scenario's AMQP steps use.\n" +
			"- Values expand `${env:..}` and `${sys:..}`; `${env:..}` values are masked.",
		Table: &core.TableDoc{
			Columns: []string{"property", "value"},
			Rows: []core.TableRow{
				{Name: "url", Takes: "the broker's URL, with its user, password and virtual host: `amqp://user:password@host:5672/vhost`, or `amqps://` for TLS"},
				{Name: "protocol", Takes: "the AMQP version the broker speaks", Values: []string{"0-9-1", "1.0"}, Default: "0-9-1"},
				contract.TableRow("queues and exchanges"),
			},
		},
		Examples: []string{
			"Given the depot amqp broker with the following properties:\n" +
				"  | url | amqp://parcels:${env:RABBITMQ_PASSWORD}@localhost:5672/ |",
			"Given the depot amqp broker with the following properties:\n" +
				"  | url      | amqp://artemis:${env:ARTEMIS_PASSWORD}@localhost:5672 |\n" +
				"  | protocol | 1.0                                                  |",
		},
		Run: func(sc *core.Scenario, a core.Args) error {
			b, err := parse(a.String(0), a.Table, func(v string) string { return secrets.Expand(sc, v) })
			if err != nil {
				return err
			}
			if b.asyncapi != "" {
				if b.contract, err = contract.Open(sc, b.asyncapi); err != nil {
					return err
				}
			}
			if err := brokers.Of(sc).Add(b.name, b); err != nil {
				return err
			}
			sc.Log("registered the %s amqp broker: %s (AMQP %s)", b.name, secrets.Mask(sc, redact(b.url)), b.protocol)
			return nil
		},
	}}
	steps = append(steps, queues.Steps()...)
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: append(steps, exchanges.Steps()...)}
}

type broker struct {
	name, url, protocol string
	// asyncapi names the contract of the broker's messages; contract
	// checks them.
	asyncapi string
	contract contract.Checker
}

// asyncProtocol is the AsyncAPI protocol of the broker's messages.
func (b *broker) asyncProtocol() string {
	if b.protocol == "1.0" {
		return "amqp1"
	}
	return "amqp"
}

func (b *broker) key() string { return b.protocol + "|" + b.url }

func parse(name string, t *core.Table, expand func(string) string) (*broker, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	b := &broker{name: name, protocol: "0-9-1"}
	for _, p := range pairs {
		v := strings.TrimSpace(expand(p.Value))
		switch p.Key {
		case "url":
			b.url = v
		case "protocol":
			if v != "0-9-1" && v != "1.0" {
				return nil, fmt.Errorf("the amqp protocol is 0-9-1 or 1.0, not %q", v)
			}
			b.protocol = v
		case contract.Row:
			b.asyncapi = v
		default:
			return nil, fmt.Errorf("unknown amqp broker property %q (supported: url, protocol, asyncapi)", p.Key)
		}
	}
	if b.url == "" {
		return nil, fmt.Errorf(`the amqp broker property "url" is required`)
	}
	u, err := url.Parse(b.url)
	if err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") || u.Host == "" {
		return nil, fmt.Errorf("the amqp broker's url is amqp://user:password@host:port/vhost or amqps://..., not %q", redact(b.url))
	}
	return b, nil
}

// redact leaves a URL's password out.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	return u.Redacted()
}

var brokers = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*broker] {
	return core.NewServices[*broker]("AMQP broker",
		`No AMQP broker is registered in this scenario; register one with "the {word} amqp broker with the following properties:"`)
}, nil)

// client talks to a broker for the whole run.
type client interface {
	send(ctx context.Context, kind, target string, m outgoing) error
	// listen receives what a queue or an exchange (bound with keys) gets
	// into in, until ctx ends or stop is called.
	listen(ctx context.Context, kind, target string, keys []string, in *cloudstep.Inbox) (stop func(context.Context) error, err error)
	close() error
}

func open(s *core.Suite, b *broker) (client, error) {
	return core.Cached(s, Name+"/client/"+b.key(), func() (client, error) {
		var c client
		var err error
		if b.protocol == "1.0" {
			c, err = dial10(b.url)
		} else {
			c, err = dial091(b.url, cloudstep.RunID(s))
		}
		if err != nil {
			return nil, fmt.Errorf("cannot connect to the amqp broker at %s: %w", redact(b.url), err)
		}
		s.OnClose(func(context.Context) error { return c.close() })
		return c, nil
	})
}

// outgoing is a message to send, from a step's fields.
type outgoing struct {
	body                                           []byte
	routingKey                                     string
	headers                                        map[string]string
	contentType, correlationID, messageID, replyTo string
}

func outgoingFrom(kind string, body []byte, fields map[string]string) (outgoing, error) {
	m := outgoing{body: body, headers: map[string]string{}}
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := fields[k]
		if h, ok := strings.CutPrefix(k, "header "); ok {
			m.headers[strings.TrimSpace(h)] = v
			continue
		}
		switch k {
		case "routing key":
			if kind == "queue" {
				return m, fmt.Errorf("a message sent to a queue takes no routing key: publish it to an exchange to route it")
			}
			m.routingKey = v
		case "content type":
			m.contentType = v
		case "correlation id":
			m.correlationID = v
		case "message id":
			m.messageID = v
		case "reply to":
			m.replyTo = v
		default:
			supported := "header <name>, content type, correlation id, message id, reply to"
			if kind == "exchange" {
				supported = "routing key, " + supported
			}
			return m, fmt.Errorf("unknown amqp message property %q (supported: %s)", k, supported)
		}
	}
	if m.contentType == "" {
		m.contentType = "text/plain"
		if isJSON(body) {
			m.contentType = "application/json"
		}
	}
	return m, nil
}

func sender(kind string) func(sc *core.Scenario, target string, body []byte, fields map[string]string) error {
	return func(sc *core.Scenario, target string, body []byte, fields map[string]string) error {
		m, err := outgoingFrom(kind, body, fields)
		if err != nil {
			return err
		}
		b, err := brokers.Of(sc).Default()
		if err != nil {
			return err
		}
		if err := contract.Check(b.contract, sc, contract.Message{
			Protocol: b.asyncProtocol(), Addresses: addresses(target, m.routingKey), Sent: true,
			Payload: body, ContentType: m.contentType, Headers: m.headers,
		}); err != nil {
			return err
		}
		c, err := open(sc.Suite(), b)
		if err != nil {
			return secrets.Hide(sc, err)
		}
		return secrets.Hide(sc, c.send(sc.Context(), kind, target, m))
	}
}

// found checks a message a check found against the broker's contract.
func found(sc *core.Scenario, target, _ string, msg cloudstep.Message) error {
	b, err := brokers.Of(sc).Default()
	if err != nil || b.contract == nil {
		return err
	}
	return b.contract.Check(sc, contract.Message{
		Protocol: b.asyncProtocol(), Addresses: addresses(target, msg.Meta["routing key"]),
		Payload: msg.Body, ContentType: msg.Meta["content type"], Headers: msg.Fields,
	})
}

// addresses are what a contract's channel can name a message's
// destination by: the queue or the exchange, then the routing key.
func addresses(target, routingKey string) []string {
	if routingKey == "" {
		return []string{target}
	}
	return []string{target, routingKey}
}

func inbox(kind string) func(sc *core.Scenario, target, noun string) (*cloudstep.Inbox, error) {
	return func(sc *core.Scenario, target, _ string) (*cloudstep.Inbox, error) {
		b, err := brokers.Of(sc).Default()
		if err != nil {
			return nil, err
		}
		in, err := listen(sc.Suite(), b, kind, target, nil)
		return in, secrets.Hide(sc, err)
	}
}

func listen(s *core.Suite, b *broker, kind, target string, keys []string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, Name).Start(b.key()+"|"+kind+"|"+target, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		c, err := open(s, b)
		if err != nil {
			return nil, err
		}
		return c.listen(run, kind, target, keys, in)
	})
}

// Prepare plans listening to the queues and exchanges the run's checks
// name, binding each exchange's queue with the routing keys they name.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	type target struct {
		b            *broker
		kind, target string
	}
	var order []string
	planned := map[string]target{}
	keys := map[string][]string{}
	for _, sc := range plan.Scenarios {
		var b *broker
		for _, st := range sc.Steps {
			if st.Definition == Name+".broker" && b == nil {
				b, _ = parse(st.Args[0].Raw, st.Table, s.Interpolate)
			}
			kind := ""
			switch st.Definition {
			case queues.ID("received"):
				kind = "queue"
			case exchanges.ID("received"):
				kind = "exchange"
			}
			if kind == "" || b == nil || len(st.Args) < 2 {
				continue
			}
			k := b.key() + "|" + kind + "|" + st.Args[1].Raw
			if _, ok := planned[k]; !ok {
				order = append(order, k)
				planned[k] = target{b, kind, st.Args[1].Raw}
			}
			if st.Table == nil {
				continue
			}
			for _, row := range st.Table.Rows {
				if len(row) == 2 && strings.TrimSpace(row[0]) == "routing key" {
					if key := s.Interpolate(strings.TrimSpace(row[1])); !slices.Contains(keys[k], key) {
						keys[k] = append(keys[k], key)
					}
				}
			}
		}
	}
	d := cloudstep.DeferredFor(s, Name)
	for _, k := range order {
		t, ks := planned[k], keys[k]
		d.Add(k, func(context.Context) error {
			_, err := listen(s, t.b, t.kind, t.target, ks)
			return err
		})
	}
	return nil
}

// Init starts listening to the planned queues and exchanges, now that the
// apps run and have declared them.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, Name).Run(ctx)
}
