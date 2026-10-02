// Package websocket is the websocket pack: WebSocket connections a
// scenario opens, the messages it sends on them, and what it receives.
package websocket

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/stream"
)

// Name is the pack's name.
const Name = "websocket"

const since = "0.1.5"

const packDoc = `Open WebSocket connections to your services, send messages on them, and check the messages they receive and how they close.

Register a connection with ` + "`the {word} websocket with the following properties:`" + `: it opens there and then, with the headers and subprotocol the table gives, and belongs to the scenario, which closes it when it ends.

- **What is received** is checked like the messages of queues and topics: ` + "`received a message where:`" + ` takes a table of paths into a JSON message and the values they have, and ` + "`received a message containing {string}`" + ` looks for text in any message. The checks wait, 10 seconds unless ` + "`within {duration}`" + ` says otherwise, and look at every message the connection received.
- **A connection the server closes** fails a check that is still waiting at once, saying why; ` + "`was closed with code {int}`" + ` checks the code it closed with.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the URL and the headers, such as a token, are masked in logs and failures.`

// Pack returns the websocket pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps()}
}

// socket is a connection a scenario opened.
type socket struct {
	name string
	url  string
	conn *stream.WebSocket
	in   *cloudstep.Inbox
	// contract checks the messages both ways, when the asyncapi row
	// names one.
	contract contract.Checker

	mu   sync.Mutex
	sent []string
}

var sockets = core.NewStateKey(Name, func(sc *core.Scenario) *core.Services[*socket] {
	s := core.NewServices[*socket]("WebSocket",
		`No websocket is opened in this scenario; open one with "the {word} websocket with the following properties:"`).RegisteredBy("the {word} websocket with the following properties:")
	sc.Describe(Name, func() any { return describe(sc, s) })
	return s
}, func(_ *core.Scenario, s *core.Services[*socket]) error {
	for _, w := range s.All() {
		w.conn.Close()
	}
	return nil
})

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".connect", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} websocket with the following properties:",
			Doc:  "Open a WebSocket connection under a name. It belongs to the scenario, which closes it when it ends.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Required: true, Takes: "the address, `ws://` or `wss://`"},
					{Name: "header.<name>", Takes: "a header of the opening request, such as `header.Authorization`"},
					{Name: "subprotocol", Takes: "the subprotocols to offer, separated by commas"},
					contract.TableRow("messages both ways"),
				},
				Note: "Values are expanded (`${env:..}`, `${sys:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{"Given the tracking websocket with the following properties:\n  | url | ws://localhost:8400/portal/track/PX-LIV-5701/live |"},
			Run:      connect,
		},
		{
			ID: Name + ".send", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr:     "a message is sent to the {word} websocket:",
			Doc:      "Send the doc string as a text message on the connection.",
			Examples: []string{"When a message is sent to the tracking websocket:\n  \"\"\"\n  {\"follow\": \"PX-LIV-5701\"}\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				body := ""
				if a.DocString != nil {
					body = a.DocString.Content
				}
				body, err := secrets.Resolve(sc, body)
				if err != nil {
					return err
				}
				return send(sc, a.String(0), body)
			},
		},
		{
			ID: Name + ".send.file", Keyword: "When", Since: since,
			Expr:     "the {filepath} message is sent to the {word} websocket",
			Doc:      "Send the file of the project as a text message on the connection.",
			Examples: []string{"When the tracking/follow-PX-LIV-5701.json message is sent to the tracking websocket"},
			Run: func(sc *core.Scenario, a core.Args) error {
				path, err := sc.Suite().ResolvePath(a.String(0))
				if err != nil {
					return err
				}
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				return send(sc, a.String(1), string(body))
			},
		},
		{
			ID: Name + ".received", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} websocket received a message where:",
			Doc: "Check that the connection received a JSON message with those values. The check waits for it: 10 seconds, or " +
				"`within {duration}`.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Each row is a path into the message (a field name, a dotted path or a JSONPath) and the value it has, compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{"Then within 20s the tracking websocket received a message where:\n  | reference | PX-LIV-5701      |\n  | status    | OUT_FOR_DELIVERY |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				w, err := get(sc, a.String(1))
				if err != nil {
					return err
				}
				rs, err := cloudstep.Conditions(a.Table)
				if err != nil {
					return err
				}
				match := func(m cloudstep.Message) (bool, error) { return rs.MatchMessage(m, "") }
				return secrets.Hide(sc, cloudstep.ExpectMatch(sc, cloudstep.Wait(a, 0), w.in, w.found(sc, match), "", "message", where(w)))
			},
		},
		{
			ID: Name + ".received.text", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {word} websocket received a message containing {string}",
			Doc:      "Check that the connection received a message containing the text. The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{"Then the tracking websocket received a message containing 'PX-LIV-5701'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				w, err := get(sc, a.String(1))
				if err != nil {
					return err
				}
				text := secrets.Expand(sc, a.String(2))
				contains := func(m cloudstep.Message) (bool, error) { return strings.Contains(string(m.Body), text), nil }
				return secrets.Hide(sc, cloudstep.ExpectMatch(sc, cloudstep.Wait(a, 0), w.in, w.found(sc, contains), "", "message", where(w)))
			},
		},
		{
			ID: Name + ".closed", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {word} websocket was closed with code {int}",
			Doc:      "Check that the server closed the connection with that close code, such as 1000 (normal) or 1008 (a policy it breaks). The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{"Then the tracking websocket was closed with code 1008"},
			Run: func(sc *core.Scenario, a core.Args) error {
				w, err := get(sc, a.String(1))
				if err != nil {
					return err
				}
				want, d := a.Int(2), cloudstep.Wait(a, 0)
				return cloudstep.Poll(sc, d, func() (bool, string, error) {
					code, reason, closed := w.conn.Closed()
					if !closed {
						return false, fmt.Sprintf("The %s websocket is still open after %s", w.name, d), nil
					}
					if code != want {
						return false, "", core.Fail(fmt.Sprintf("The %s websocket was closed with code %d (%s), not %d", w.name, code, reason, want), want, code)
					}
					return true, "", nil
				})
			},
		},
	}
}

func connect(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	if a.Table == nil {
		return errors.New(`the websocket property "url" is required`)
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	var url, asyncapi string
	header := http.Header{}
	var protocols []string
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return err
		}
		switch {
		case p.Key == "url":
			url = strings.TrimSpace(v)
		case strings.HasPrefix(p.Key, "header."):
			header.Add(strings.TrimPrefix(p.Key, "header."), v)
		case p.Key == "subprotocol":
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					protocols = append(protocols, s)
				}
			}
		case p.Key == contract.Row:
			asyncapi = strings.TrimSpace(v)
		default:
			return fmt.Errorf("unknown websocket property %q (supported: url, header.<name>, subprotocol, asyncapi)", p.Key)
		}
	}
	if url == "" {
		return errors.New(`the websocket property "url" is required`)
	}
	w := &socket{name: name, url: url, in: &cloudstep.Inbox{}}
	if asyncapi != "" {
		if w.contract, err = contract.Open(sc, asyncapi); err != nil {
			return err
		}
	}
	conn, err := stream.DialWebSocket(url, header, protocols,
		func(r stream.Received) {
			m := cloudstep.Message{Body: r.Data}
			if r.Binary {
				m = cloudstep.Message{Body: []byte(fmt.Sprintf("(a binary message of %d bytes)", len(r.Data))), Meta: map[string]string{"binary": "true"}}
			}
			w.in.Add(m)
		},
		func(code int, reason string) {
			w.in.End(fmt.Sprintf("the server closed the connection: %d %s", code, reason))
		})
	if err != nil {
		return secrets.Hide(sc, fmt.Errorf("could not open the %s websocket at %s: %w", name, url, err))
	}
	w.conn = conn
	if err := sockets.Of(sc).Add(name, w); err != nil {
		conn.Close()
		return err
	}
	sc.Log("opened the %s websocket: %s", name, secrets.Mask(sc, url))
	return nil
}

func send(sc *core.Scenario, name, body string) error {
	w, err := get(sc, name)
	if err != nil {
		return err
	}
	if err := contract.Check(w.contract, sc, w.message([]byte(body), true)); err != nil {
		return err
	}
	if err := w.conn.Send(sc.Context(), []byte(body)); err != nil {
		return secrets.Hide(sc, fmt.Errorf("could not send on the %s websocket: %w", name, err))
	}
	w.mu.Lock()
	w.sent = append(w.sent, body)
	w.mu.Unlock()
	return nil
}

func get(sc *core.Scenario, name string) (*socket, error) {
	w, err := sockets.Of(sc).Get(name)
	if err != nil {
		return nil, fmt.Errorf("no websocket named %q in this scenario; open it first with \"the %s websocket with the following properties:\"", name, name)
	}
	return w, nil
}

// message is a message on the connection as its contract sees it: on
// the path of the connection's URL.
func (w *socket) message(body []byte, sent bool) contract.Message {
	return contract.Message{Protocol: "ws", Addresses: contract.URLAddresses(w.url), Payload: body, Sent: sent}
}

// found checks the message a check found against the connection's
// contract; binary messages are left out.
func (w *socket) found(sc *core.Scenario, match func(cloudstep.Message) (bool, error)) func(cloudstep.Message) (bool, error) {
	return func(m cloudstep.Message) (bool, error) {
		ok, err := match(m)
		if ok && err == nil && m.Meta["binary"] == "" {
			if err := contract.Check(w.contract, sc, w.message(m.Body, false)); err != nil {
				return false, err
			}
		}
		return ok, err
	}
}

func where(w *socket) string { return "the " + w.name + " websocket" }

// describe is the scenario's connections, for failure reports.
func describe(sc *core.Scenario, s *core.Services[*socket]) any {
	out := map[string]any{}
	for _, w := range s.All() {
		msgs, _ := w.in.Since(time.Time{})
		received := make([]string, 0, len(msgs))
		for _, m := range msgs[max(0, len(msgs)-10):] {
			received = append(received, secrets.Mask(sc, string(cloudstep.Compact(m.Body))))
		}
		w.mu.Lock()
		sent := make([]string, 0, len(w.sent))
		for _, b := range w.sent[max(0, len(w.sent)-10):] {
			sent = append(sent, secrets.Mask(sc, string(cloudstep.Compact([]byte(b)))))
		}
		w.mu.Unlock()
		d := map[string]any{"url": secrets.Mask(sc, w.url), "sent": sent, "received": received}
		if code, reason, closed := w.conn.Closed(); closed {
			d["closed"] = fmt.Sprintf("%d %s", code, reason)
		}
		out[w.name] = d
	}
	return out
}
