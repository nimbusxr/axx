// Package nats is the nats pack: messages published to NATS subjects, and
// the messages the services under test publish on subjects and into
// JetStream streams.
package nats

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "nats"

const since = "0.1.5"

const packDoc = `Publish messages to NATS subjects, and check the messages your services publish on subjects and into JetStream streams.

Register the server once, usually in the ` + "`Background`" + `; every NATS step of the scenario uses it:

` + "```gherkin" + `
Given the tracking nats server with the following properties:
  | url   | nats://localhost:4222 |
  | token | ${env:NATS_TOKEN}     |
` + "```" + `

Steps name a **subject** or a **stream**: ` + "`the deliveries.confirmed nats subject`" + `, ` + "`the TRACKING nats stream`" + `. Messages carry headers (` + "`with the following headers:`" + `, and ` + "`header <name>`" + ` rows in checks). A message published to a subject a stream captures is published through JetStream, and the step waits for the stream to store it.

- **A subject check subscribes for the run:** for the subjects a run checks, axx subscribes once the apps are up. A subject in a check can have the wildcards ` + "`*`" + ` and ` + "`>`" + `: ` + "`the tracking.> nats subject`" + `, and a ` + "`subject`" + ` row checks the subject a message came on.
- **A stream check reads only what is new:** for the streams a run checks, axx reads each with an ordered consumer of its own that starts with the messages stored after the apps are up, so what earlier runs stored never counts. It takes nothing from the stream's other consumers; a work-queue stream allows no other consumer, so check its subject instead.
- **A check only looks at the messages received since its scenario started,** so scenarios running in parallel check their own messages, by data unique to them.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties are masked in logs and failures.`

// Pack returns the nats pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Preparer    = pack{}
	_ core.Initializer = pack{}
)

var subjects = cloudstep.Messages{
	Pack: Name, Kind: "subject", Target: "nats subject", Verb: "published", Field: "header", Fields: "headers",
	Meta: []string{"subject"}, Since: since,
	Send: publish, Inbox: inbox("subject"), Found: found,
	Example: cloudstep.Sample{
		To: "deliveries.confirmed", Body: `{"reference": "PX-8301", "signedBy": "H. Wolf", "deliveredAt": "2026-09-28T10:12:00Z"}`,
		File: "nats/delivery-confirmed.json", Fields: [][2]string{{"courier", "CR-LEJ-12"}},
		From: "tracking.>", Where: [][2]string{{"subject", "tracking.PX-8301"}, {"status", "DELIVERED"}, {"header source", "courier"}},
	},
	Received: "axx subscribes to a subject it checks for the run.",
}

var streams = cloudstep.Messages{
	Pack: Name, Kind: "stream", Target: "nats stream", Field: "header",
	Meta: []string{"subject"}, Since: since, Inbox: inbox("stream"), Found: found,
	Example: cloudstep.Sample{
		From: "TRACKING", Where: [][2]string{{"subject", "tracking.PX-8302"}, {"status", "OUT_FOR_DELIVERY"}},
	},
	Received: "axx reads a stream it checks with an ordered consumer of its own, from the messages stored after the apps are up.",
}

func (pack) Manifest() core.Manifest {
	steps := []core.StepDef{{
		ID: Name + ".server", Keyword: "Given", Arg: core.ArgTable, Since: since,
		Expr: "the {word} nats server with the following properties:",
		Doc: "Register the NATS server the steps talk to.\n\n" +
			"- The first server registered is the one the scenario's NATS steps use.\n" +
			"- Give it at most one of a `token`, a `username` and `password`, and `creds`.\n" +
			"- Values expand `${env:..}` and `${sys:..}`; `${env:..}` values are masked.",
		Table: &core.TableDoc{
			Columns: []string{"property", "value"},
			Rows: []core.TableRow{
				{Name: "url", Takes: "the server's URL, `nats://host:4222` or `tls://` for TLS; several, separated by commas, for a cluster"},
				{Name: "token", Takes: "the token axx connects with, like `${env:NATS_TOKEN}`"},
				{Name: "username", Takes: "the user axx connects as"},
				{Name: "password", Takes: "its password"},
				{Name: "creds", Takes: "a credentials file (JWT and seed), relative to the directory of axx.yaml"},
				contract.TableRow("subjects"),
			},
		},
		Examples: []string{
			"Given the tracking nats server with the following properties:\n" +
				"  | url | nats://localhost:4222 |",
			"Given the tracking nats server with the following properties:\n" +
				"  | url   | tls://nats.parcels.example:4222 |\n" +
				"  | creds | nats/axx.creds                  |",
		},
		Run: func(sc *core.Scenario, a core.Args) error {
			s, err := parse(sc.Suite(), a.String(0), a.Table, func(v string) string { return secrets.Expand(sc, v) })
			if err != nil {
				return err
			}
			if s.asyncapi != "" {
				if s.contract, err = contract.Open(sc, s.asyncapi); err != nil {
					return err
				}
			}
			if err := servers.Of(sc).Add(s.name, s); err != nil {
				return err
			}
			sc.Log("registered the %s nats server: %s", s.name, secrets.Mask(sc, redact(s.url)))
			return nil
		},
	}}
	steps = append(steps, subjects.Steps()...)
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: append(steps, streams.ReceivedStep())}
}

type server struct {
	name, url, token, username, password, creds string
	// asyncapi names the contract of the server's messages; contract
	// checks them.
	asyncapi string
	contract contract.Checker
}

func (s *server) key() string { return s.url + "|" + s.username + "|" + s.creds }

func parse(suite *core.Suite, name string, t *core.Table, expand func(string) string) (*server, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	s := &server{name: name}
	for _, p := range pairs {
		v := strings.TrimSpace(expand(p.Value))
		switch p.Key {
		case "url":
			s.url = v
		case "token":
			s.token = v
		case "username":
			s.username = v
		case "password":
			s.password = v
		case "creds":
			if s.creds, err = suite.ResolvePath(v); err != nil {
				return nil, fmt.Errorf("the nats credentials file: %w", err)
			}
		case contract.Row:
			s.asyncapi = v
		default:
			return nil, fmt.Errorf("unknown nats server property %q (supported: url, token, username, password, creds, asyncapi)", p.Key)
		}
	}
	if s.url == "" {
		return nil, fmt.Errorf(`the nats server property "url" is required`)
	}
	ways := 0
	for _, v := range []string{s.token, s.username + s.password, s.creds} {
		if v != "" {
			ways++
		}
	}
	if ways > 1 {
		return nil, errors.New("give the nats server one way to sign in: a token, a username and password, or creds")
	}
	return s, nil
}

// redact leaves the users, passwords and tokens out of the servers' URLs.
func redact(urls string) string {
	parts := strings.Split(urls, ",")
	for i, p := range parts {
		if u, err := url.Parse(strings.TrimSpace(p)); err == nil && u.User != nil {
			u.User = url.User("xxxxx")
			parts[i] = u.String()
		}
	}
	return strings.Join(parts, ",")
}

var servers = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*server] {
	return core.NewServices[*server]("NATS server",
		`No NATS server is registered in this scenario; register one with "the {word} nats server with the following properties:"`).RegisteredBy("the {word} nats server with the following properties:")
}, nil)

// conn is the run's connection to a server.
type conn struct {
	nc *natsgo.Conn
	js jetstream.JetStream
}

func open(suite *core.Suite, s *server) (*conn, error) {
	return core.Cached(suite, Name+"/conn/"+s.key(), func() (*conn, error) {
		opts := []natsgo.Option{natsgo.Name("axx " + cloudstep.RunID(suite)), natsgo.Timeout(10 * time.Second)}
		switch {
		case s.token != "":
			opts = append(opts, natsgo.Token(s.token))
		case s.creds != "":
			opts = append(opts, natsgo.UserCredentials(s.creds))
		case s.username != "":
			opts = append(opts, natsgo.UserInfo(s.username, s.password))
		}
		nc, err := natsgo.Connect(s.url, opts...)
		if err != nil {
			return nil, fmt.Errorf("cannot connect to the nats server at %s: %w", redact(s.url), err)
		}
		js, err := jetstream.New(nc)
		if err != nil {
			nc.Close()
			return nil, err
		}
		suite.OnClose(func(context.Context) error {
			nc.Close()
			return nil
		})
		return &conn{nc: nc, js: js}, nil
	})
}

func publish(sc *core.Scenario, subject string, body []byte, headers map[string]string) error {
	if strings.ContainsAny(subject, "*>") {
		return fmt.Errorf("a message is published to a subject, not to one with wildcards: %s", subject)
	}
	s, err := servers.Of(sc).Default()
	if err != nil {
		return err
	}
	msg := &natsgo.Msg{Subject: subject, Data: body, Header: natsgo.Header{}}
	for k, v := range headers {
		msg.Header.Set(k, v)
	}
	if err := contract.Check(s.contract, sc, contractMessage(incoming(subject, msg.Header, body), true)); err != nil {
		return err
	}
	c, err := open(sc.Suite(), s)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	ctx, cancel := context.WithTimeout(sc.Context(), 10*time.Second)
	defer cancel()
	// A subject a stream captures goes through JetStream, which says when
	// the stream stored it.
	if _, err := c.js.StreamNameBySubject(ctx, subject); err == nil {
		_, err := c.js.PublishMsg(ctx, msg)
		return secrets.Hide(sc, err)
	}
	if err := c.nc.PublishMsg(msg); err != nil {
		return secrets.Hide(sc, err)
	}
	return secrets.Hide(sc, c.nc.FlushWithContext(ctx))
}

func inbox(kind string) func(sc *core.Scenario, target, noun string) (*cloudstep.Inbox, error) {
	return func(sc *core.Scenario, target, _ string) (*cloudstep.Inbox, error) {
		s, err := servers.Of(sc).Default()
		if err != nil {
			return nil, err
		}
		in, err := listen(sc.Context(), sc.Suite(), s, kind, target)
		return in, secrets.Hide(sc, err)
	}
}

// listen subscribes to a subject, or reads a stream, for the run.
func listen(ctx context.Context, suite *core.Suite, s *server, kind, target string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(suite, Name).Start(s.key()+"|"+kind+"|"+target, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		c, err := open(suite, s)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if kind == "subject" {
			sub, err := c.nc.Subscribe(target, func(m *natsgo.Msg) { in.Add(incoming(m.Subject, m.Header, m.Data)) })
			if err != nil {
				return nil, fmt.Errorf("cannot subscribe to the %s nats subject: %w", target, err)
			}
			if err := c.nc.FlushWithContext(ctx); err != nil {
				return nil, err
			}
			return func(context.Context) error { return sub.Unsubscribe() }, nil
		}
		cons, err := c.js.OrderedConsumer(ctx, target, jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverNewPolicy})
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			return nil, fmt.Errorf("no nats stream named %s", target)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read the %s nats stream: %w", target, err)
		}
		cc, err := cons.Consume(func(m jetstream.Msg) { in.Add(incoming(m.Subject(), m.Headers(), m.Data())) },
			jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
				if run.Err() == nil && errors.Is(err, jetstream.ErrConsumerDeleted) {
					in.Fail(fmt.Errorf("the %s nats stream's consumer was deleted: %w", target, err))
				}
			}))
		if err != nil {
			return nil, err
		}
		return func(context.Context) error {
			cc.Stop()
			return nil
		}, nil
	})
}

func incoming(subject string, h natsgo.Header, data []byte) cloudstep.Message {
	m := cloudstep.Message{Body: data, Fields: map[string]string{}, Meta: map[string]string{"subject": subject}}
	for k, vs := range h {
		m.Fields[k] = strings.Join(vs, ", ")
	}
	return m
}

// found checks a message a check found against the server's contract.
func found(sc *core.Scenario, _, _ string, msg cloudstep.Message) error {
	s, err := servers.Of(sc).Default()
	if err != nil {
		return err
	}
	return contract.Check(s.contract, sc, contractMessage(msg, false))
}

// contractMessage is a message as its contract sees it: on the subject it
// was published to, which a check's wildcards or stream only match.
func contractMessage(m cloudstep.Message, sent bool) contract.Message {
	cm := contract.Message{Protocol: "nats", Addresses: []string{m.Meta["subject"]}, Sent: sent, Payload: m.Body, Headers: m.Fields}
	for k, v := range m.Fields {
		if strings.EqualFold(k, "Content-Type") {
			cm.ContentType = v
		}
	}
	return cm
}

// Prepare plans subscribing to the subjects and reading the streams the
// run's checks name.
func (pack) Prepare(_ context.Context, suite *core.Suite, plan *core.Plan) error {
	d := cloudstep.DeferredFor(suite, Name)
	for _, sc := range plan.Scenarios {
		var s *server
		for _, st := range sc.Steps {
			if st.Definition == Name+".server" && s == nil {
				s, _ = parse(suite, st.Args[0].Raw, st.Table, suite.Interpolate)
			}
			kind := ""
			switch st.Definition {
			case subjects.ID("received"):
				kind = "subject"
			case streams.ID("received"):
				kind = "stream"
			}
			if kind == "" || s == nil || len(st.Args) < 2 {
				continue
			}
			srv, target := s, st.Args[1].Raw
			d.Add(srv.key()+"|"+kind+"|"+target, func(ctx context.Context) error {
				_, err := listen(ctx, suite, srv, kind, target)
				return err
			})
		}
	}
	return nil
}

// Init subscribes to the planned subjects and reads the planned streams,
// now that the apps run.
func (pack) Init(ctx context.Context, suite *core.Suite) error {
	return cloudstep.DeferredFor(suite, Name).Run(ctx)
}
