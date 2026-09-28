// Package mqtt is the mqtt pack: messages published to MQTT topics, and the
// messages the services under test publish there, over MQTT 5.
package mqtt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/eclipse/paho.golang/paho"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "mqtt"

const since = "0.1.5"

const packDoc = `Publish messages to MQTT topics, and check the messages your services publish there, over MQTT 5 (Mosquitto, EMQX, HiveMQ, RabbitMQ's MQTT plugin).

Register the broker once, usually in the ` + "`Background`" + `; every MQTT step of the scenario uses it:

` + "```gherkin" + `
Given the depots mqtt broker with the following properties:
  | url      | mqtt://localhost:1883    |
  | username | depot-scanners           |
  | password | ${env:MQTT_PASSWORD}     |
` + "```" + `

Steps name a **topic**: ` + "`the depots/LEJ/scans mqtt topic`" + `. Messages carry user properties (` + "`property <name>`" + ` rows), and are published at QoS 1, not retained, unless the ` + "`qos`" + ` and ` + "`retain`" + ` rows say otherwise.

- **A check subscribes for the run:** for the topics a run checks, axx subscribes once the apps are up, with a connection of its own. A topic in a check can be a filter with ` + "`+`" + ` and ` + "`#`" + `: ` + "`the depots/+/alerts mqtt topic`" + `, and a ` + "`topic`" + ` row checks the topic a message came on.
- **A check only looks at the messages received since its scenario started,** so scenarios running in parallel check their own messages, by data unique to them.
- **Retained messages from before never count:** axx subscribes without the messages the broker keeps for new subscribers, which earlier runs may have left.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties are masked in logs and failures.`

// Pack returns the mqtt pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Preparer    = pack{}
	_ core.Initializer = pack{}
)

var topics = cloudstep.Messages{
	Pack: Name, Target: "mqtt topic", Verb: "published", Field: "property", Fields: "properties",
	Meta: []string{"topic", "content type", "response topic", "correlation data"}, Since: since,
	SendRows: []core.TableRow{
		{Name: "qos", Takes: "the quality of service it is published at", Values: []string{"0", "1", "2"}, Default: "1"},
		{Name: "retain", Takes: "whether the broker keeps it for new subscribers", Values: []string{"true", "false"}, Default: "false"},
		{Name: "property <name>", Takes: "a user property sent with the message: its name after `property `, and its value"},
		{Name: "content type", Takes: "the body's media type"},
		{Name: "response topic", Takes: "the topic a reply goes to"},
		{Name: "correlation data", Takes: "what ties a reply to its request, as text"},
	},
	Send: publish, Inbox: inbox, Found: found,
	Example: cloudstep.Sample{
		To:   "depots/LEJ/scans",
		Body: `{"scanId": "SC-8201-1", "parcelRef": "PX-8201", "status": "OUT_FOR_DELIVERY", "location": "Leipzig"}`,
		File: "mqtt/scan-unknown.json", Fields: [][2]string{{"property scanner", "LEJ-HANDHELD-7"}, {"qos", "1"}},
		From: "depots/+/alerts", Where: [][2]string{{"topic", "depots/LEJ/alerts"}, {"parcelRef", "PX-8299"}, {"property scanner", "LEJ-HANDHELD-7"}},
	},
	Received: "axx subscribes to a topic it checks for the run, without the retained messages earlier runs left.",
}

func (pack) Manifest() core.Manifest {
	steps := []core.StepDef{{
		ID: Name + ".broker", Keyword: "Given", Arg: core.ArgTable, Since: since,
		Expr: "the {word} mqtt broker with the following properties:",
		Doc: "Register the MQTT broker the steps talk to.\n\n" +
			"- The first broker registered is the one the scenario's MQTT steps use.\n" +
			"- Values expand `${env:..}` and `${sys:..}`; `${env:..}` values are masked.",
		Table: &core.TableDoc{
			Columns: []string{"property", "value"},
			Rows: []core.TableRow{
				{Name: "url", Takes: "the broker's URL: `mqtt://host:1883`, `mqtts://` for TLS, or `ws://` and `wss://` for MQTT over WebSockets"},
				{Name: "username", Takes: "the user axx connects as"},
				{Name: "password", Takes: "its password, like `${env:MQTT_PASSWORD}`"},
				{Name: "client id", Takes: "the start of the client IDs axx connects with, one per connection", Default: "`axx-<run>`"},
				contract.TableRow("topics"),
			},
		},
		Examples: []string{
			"Given the depots mqtt broker with the following properties:\n" +
				"  | url | mqtt://localhost:1883 |",
			"Given the depots mqtt broker with the following properties:\n" +
				"  | url      | mqtts://mqtt.parcels.example:8883 |\n" +
				"  | username | depot-scanners                   |\n" +
				"  | password | ${env:MQTT_PASSWORD}             |",
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
			sc.Log("registered the %s mqtt broker: %s", b.name, secrets.Mask(sc, redact(b.url)))
			return nil
		},
	}}
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: append(steps, topics.Steps()...)}
}

type broker struct {
	name, url, username, password, clientID string
	// asyncapi names the contract of the broker's messages; contract
	// checks them.
	asyncapi string
	contract contract.Checker
}

func (b *broker) key() string { return b.url + "|" + b.username + "|" + b.clientID }

func parse(name string, t *core.Table, expand func(string) string) (*broker, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	b := &broker{name: name}
	for _, p := range pairs {
		v := strings.TrimSpace(expand(p.Value))
		switch p.Key {
		case "url":
			b.url = v
		case "username":
			b.username = v
		case "password":
			b.password = v
		case "client id":
			b.clientID = v
		case contract.Row:
			b.asyncapi = v
		default:
			return nil, fmt.Errorf("unknown mqtt broker property %q (supported: url, username, password, client id, asyncapi)", p.Key)
		}
	}
	if b.url == "" {
		return nil, fmt.Errorf(`the mqtt broker property "url" is required`)
	}
	u, err := url.Parse(b.url)
	if err != nil || u.Host == "" || defaultPort[u.Scheme] == "" {
		return nil, fmt.Errorf("the mqtt broker's url is mqtt://, mqtts://, ws:// or wss:// and the broker's host, not %q", redact(b.url))
	}
	if u.User != nil && b.username == "" {
		b.username = u.User.Username()
		b.password, _ = u.User.Password()
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

var defaultPort = map[string]string{"mqtt": "1883", "tcp": "1883", "mqtts": "8883", "ssl": "8883", "tls": "8883", "ws": "80", "wss": "443"}

var brokers = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*broker] {
	return core.NewServices[*broker]("MQTT broker",
		`No MQTT broker is registered in this scenario; register one with "the {word} mqtt broker with the following properties:"`)
}, nil)

// connections numbers the connections of a run, for their client IDs.
var connections atomic.Int64

// connect opens a connection to the broker, handing each message it
// receives to received.
func connect(ctx context.Context, s *core.Suite, b *broker, received func(*paho.Publish)) (*paho.Client, error) {
	conn, err := dial(ctx, b.url)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to the mqtt broker at %s: %w", redact(b.url), err)
	}
	prefix := b.clientID
	if prefix == "" {
		prefix = cloudstep.RunID(s)
	}
	cfg := paho.ClientConfig{ClientID: prefix + "-" + strconv.FormatInt(connections.Add(1), 10), Conn: conn}
	if received != nil {
		cfg.OnPublishReceived = []func(paho.PublishReceived) (bool, error){func(r paho.PublishReceived) (bool, error) {
			received(r.Packet)
			return true, nil
		}}
	}
	c := paho.NewClient(cfg)
	cp := &paho.Connect{ClientID: cfg.ClientID, KeepAlive: 30, CleanStart: true}
	if b.username != "" {
		cp.Username, cp.UsernameFlag = b.username, true
	}
	if b.password != "" {
		cp.Password, cp.PasswordFlag = []byte(b.password), true
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ack, err := c.Connect(cctx, cp)
	if err == nil && ack.ReasonCode >= 0x80 {
		err = fmt.Errorf("reason code %#x", ack.ReasonCode)
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("the mqtt broker at %s refused the connection: %w", redact(b.url), err)
	}
	return c, nil
}

// dial opens the connection an MQTT client talks over: TCP, TLS or a
// WebSocket, by the URL's scheme.
func dial(ctx context.Context, raw string) (net.Conn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), defaultPort[u.Scheme])
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch u.Scheme {
	case "mqtts", "ssl", "tls":
		d := tls.Dialer{Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}
		return d.DialContext(dctx, "tcp", host)
	case "ws", "wss":
		ws, _, err := websocket.Dial(dctx, raw, &websocket.DialOptions{Subprotocols: []string{"mqtt"}})
		if err != nil {
			return nil, err
		}
		return &packetConn{Conn: websocket.NetConn(context.Background(), ws, websocket.MessageBinary)}, nil
	default:
		var d net.Dialer
		return d.DialContext(dctx, "tcp", host)
	}
}

// packetConn sends each MQTT packet in a WebSocket message of its own, as
// brokers expect, though the client writes a packet in parts. It is a
// sync.Locker, which the client holds while it writes a packet.
type packetConn struct {
	net.Conn
	sync.Mutex
	buf []byte
}

func (c *packetConn) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	for {
		n, ok := packetLen(c.buf)
		if !ok {
			return len(p), nil
		}
		if _, err := c.Conn.Write(c.buf[:n]); err != nil {
			return 0, err
		}
		c.buf = append(c.buf[:0], c.buf[n:]...)
	}
}

// packetLen is the length of the whole MQTT packet b starts with, from its
// fixed header, once b holds all of it.
func packetLen(b []byte) (int, bool) {
	length, scale := 0, 1
	for i := 1; i < len(b) && i < 5; i++ {
		length += int(b[i]&0x7f) * scale
		if b[i]&0x80 == 0 {
			n := i + 1 + length
			return n, len(b) >= n
		}
		scale *= 128
	}
	return 0, false
}

// publisher is the run's connection for publishing to a broker.
func publisher(s *core.Suite, b *broker) (*paho.Client, error) {
	return core.Cached(s, Name+"/publisher/"+b.key(), func() (*paho.Client, error) {
		c, err := connect(context.Background(), s, b, nil)
		if err != nil {
			return nil, err
		}
		s.OnClose(func(context.Context) error { return c.Disconnect(&paho.Disconnect{}) })
		return c, nil
	})
}

func publish(sc *core.Scenario, topic string, body []byte, fields map[string]string) error {
	p, err := message(topic, body, fields)
	if err != nil {
		return err
	}
	b, err := brokers.Of(sc).Default()
	if err != nil {
		return err
	}
	if err := contract.Check(b.contract, sc, contractMessage(incoming(p), true)); err != nil {
		return err
	}
	c, err := publisher(sc.Suite(), b)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	res, err := c.Publish(sc.Context(), p)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	// 0x10: the broker has no subscriber for it.
	if res != nil && res.ReasonCode == 0x10 && !p.Retain {
		return fmt.Errorf("nothing subscribes to the %s mqtt topic, so the message went nowhere", topic)
	}
	return nil
}

// message is a message to publish, from a step's fields.
func message(topic string, body []byte, fields map[string]string) (*paho.Publish, error) {
	p := &paho.Publish{Topic: topic, QoS: 1, Payload: body, Properties: &paho.PublishProperties{}}
	for k, v := range fields {
		if name, ok := strings.CutPrefix(k, "property "); ok {
			p.Properties.User = append(p.Properties.User, paho.UserProperty{Key: strings.TrimSpace(name), Value: v})
			continue
		}
		switch k {
		case "qos":
			q, err := strconv.Atoi(v)
			if err != nil || q < 0 || q > 2 {
				return nil, fmt.Errorf("the qos is 0, 1 or 2, not %q", v)
			}
			p.QoS = byte(q)
		case "retain":
			r, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("retain is true or false, not %q", v)
			}
			p.Retain = r
		case "content type":
			p.Properties.ContentType = v
		case "response topic":
			p.Properties.ResponseTopic = v
		case "correlation data":
			p.Properties.CorrelationData = []byte(v)
		default:
			return nil, fmt.Errorf("unknown mqtt message property %q (supported: qos, retain, property <name>, content type, response topic, correlation data)", k)
		}
	}
	if strings.ContainsAny(topic, "+#") {
		return nil, fmt.Errorf("a message is published to a topic, not a filter: %s has a wildcard", topic)
	}
	return p, nil
}

func incoming(p *paho.Publish) cloudstep.Message {
	m := cloudstep.Message{Body: p.Payload, Fields: map[string]string{}, Meta: map[string]string{"topic": p.Topic}}
	if pp := p.Properties; pp != nil {
		for _, u := range pp.User {
			m.Fields[u.Key] = u.Value
		}
		for k, v := range map[string]string{
			"content type": pp.ContentType, "response topic": pp.ResponseTopic, "correlation data": string(pp.CorrelationData),
		} {
			if v != "" {
				m.Meta[k] = v
			}
		}
	}
	return m
}

// found checks a message a check found against the broker's contract.
func found(sc *core.Scenario, _, _ string, msg cloudstep.Message) error {
	b, err := brokers.Of(sc).Default()
	if err != nil {
		return err
	}
	return contract.Check(b.contract, sc, contractMessage(msg, false))
}

// contractMessage is a message as its contract sees it: on the topic it
// was published to, which a check's filter only matches.
func contractMessage(m cloudstep.Message, sent bool) contract.Message {
	return contract.Message{
		Protocol: "mqtt", Addresses: []string{m.Meta["topic"]}, Sent: sent,
		Payload: m.Body, ContentType: m.Meta["content type"], Headers: m.Fields,
	}
}

func inbox(sc *core.Scenario, topic, _ string) (*cloudstep.Inbox, error) {
	b, err := brokers.Of(sc).Default()
	if err != nil {
		return nil, err
	}
	in, err := listen(sc.Suite(), b, topic)
	return in, secrets.Hide(sc, err)
}

// listen subscribes to a topic (or a filter) for the run, with a connection
// of its own.
func listen(s *core.Suite, b *broker, topic string) (*cloudstep.Inbox, error) {
	return cloudstep.RunListeners(s, Name).Start(b.key()+"|"+topic, func(run context.Context, in *cloudstep.Inbox) (func(context.Context) error, error) {
		c, err := connect(run, s, b, func(p *paho.Publish) { in.Add(incoming(p)) })
		if err != nil {
			return nil, err
		}
		// Retain handling 2: none of the messages the broker kept before.
		ack, err := c.Subscribe(run, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: topic, QoS: 1, RetainHandling: 2}}})
		if err == nil && len(ack.Reasons) > 0 && ack.Reasons[0] >= 0x80 {
			err = fmt.Errorf("reason code %#x", ack.Reasons[0])
		}
		if err != nil {
			_ = c.Disconnect(&paho.Disconnect{})
			return nil, fmt.Errorf("the mqtt broker refused the subscription to %s: %w", topic, err)
		}
		go func() {
			select {
			case <-run.Done():
			case <-c.Done():
				in.Fail(errors.New("the connection to the mqtt broker was lost"))
			}
		}()
		return func(context.Context) error { return c.Disconnect(&paho.Disconnect{}) }, nil
	})
}

// Prepare plans subscribing to the topics the run's checks name.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	d := cloudstep.DeferredFor(s, Name)
	for _, sc := range plan.Scenarios {
		var b *broker
		for _, st := range sc.Steps {
			if st.Definition == Name+".broker" && b == nil {
				b, _ = parse(st.Args[0].Raw, st.Table, s.Interpolate)
			}
			if st.Definition != topics.ID("received") || b == nil || len(st.Args) < 2 {
				continue
			}
			br, topic := b, st.Args[1].Raw
			d.Add(br.key()+"|"+topic, func(context.Context) error {
				_, err := listen(s, br, topic)
				return err
			})
		}
	}
	return nil
}

// Init subscribes to the planned topics, now that the apps run.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, Name).Run(ctx)
}
