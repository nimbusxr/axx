// Package sse is the sse pack: server-sent event streams a scenario opens,
// and the events they send.
package sse

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/stream"
)

// Name is the pack's name.
const Name = "sse"

const since = "0.1.5"

const packDoc = `Open server-sent event streams (` + "`text/event-stream`" + `) of your services and check the events they send.

Register a stream with ` + "`the {word} event stream with the following properties:`" + `: it opens there and then, with the headers the table gives, and belongs to the scenario, which closes it when it ends. A stream that does not answer 200 with ` + "`text/event-stream`" + ` fails that step and shows what it answered.

- **Events are checked like messages:** ` + "`has an event where:`" + ` takes a table of paths into the event's data, as JSON, and the values they have, with ` + "`event type`" + ` (the event's ` + "`event:`" + ` field, or ` + "`message`" + ` without one) and ` + "`event id`" + ` rows; ` + "`has an event containing {string}`" + ` looks for text in any event's data. The checks wait, 10 seconds unless ` + "`within {duration}`" + ` says otherwise.
- **A stream the server ends** fails a check that is still waiting at once, after looking at every event it sent.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the URL and the headers are masked in logs and failures.`

// Pack returns the sse pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps()}
}

// events is a stream a scenario opened.
type events struct {
	name, url string
	stream    *stream.EventStream
	in        *cloudstep.Inbox
	// contract checks the events, when the asyncapi row names one.
	contract contract.Checker
}

var streams = core.NewStateKey(Name, func(sc *core.Scenario) *core.Services[*events] {
	s := core.NewServices[*events]("Event stream",
		`No event stream is opened in this scenario; open one with "the {word} event stream with the following properties:"`)
	sc.Describe(Name, func() any { return describe(sc, s) })
	return s
}, func(_ *core.Scenario, s *core.Services[*events]) error {
	for _, e := range s.All() {
		e.stream.Close()
	}
	return nil
})

// field is the word for an event's named values: its type and id.
const field = "event"

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".stream", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} event stream with the following properties:",
			Doc:  "Open a server-sent event stream under a name. It belongs to the scenario, which closes it when it ends.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Required: true, Takes: "the stream's address"},
					{Name: "header.<name>", Takes: "a header of the request, such as `header.Authorization`"},
					contract.TableRow("events"),
				},
				Note: "Values are expanded (`${env:..}`, `${sys:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{"Given the tracking event stream with the following properties:\n  | url | http://localhost:8400/api/parcels/PX-LIV-5702/events |"},
			Run:      open,
		},
		{
			ID: Name + ".event", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} event stream has an event where:",
			Doc:  "Check that the stream sent an event with those values. The check waits for it: 10 seconds, or `within {duration}`.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: "Each row is a path into the event's data, as JSON (a field name, a dotted path or a JSONPath), and the value it has, compared as text: `null` for null and `undefined` for absent. " +
					"The rows `event type` and `event id` are the event's type (its `event:` field, or `message`) and id.",
			},
			Examples: []string{"Then within 20s the tracking event stream has an event where:\n  | event type | delivered   |\n  | reference  | PX-LIV-5702 |\n  | status     | DELIVERED   |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				e, err := get(sc, a.String(1))
				if err != nil {
					return err
				}
				rs, err := cloudstep.Conditions(a.Table)
				if err != nil {
					return err
				}
				match := func(m cloudstep.Message) (bool, error) { return rs.MatchMessage(m, field) }
				return secrets.Hide(sc, cloudstep.ExpectMatch(sc, cloudstep.Wait(a, 0), e.in, e.found(sc, match), field, "event", where(e)))
			},
		},
		{
			ID: Name + ".event.text", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {word} event stream has an event containing {string}",
			Doc:      "Check that the stream sent an event whose data contains the text. The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{"Then the tracking event stream has an event containing 'OUT_FOR_DELIVERY'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				e, err := get(sc, a.String(1))
				if err != nil {
					return err
				}
				text := secrets.Expand(sc, a.String(2))
				contains := func(m cloudstep.Message) (bool, error) { return strings.Contains(string(m.Body), text), nil }
				return secrets.Hide(sc, cloudstep.ExpectMatch(sc, cloudstep.Wait(a, 0), e.in, e.found(sc, contains), field, "event", where(e)))
			},
		},
	}
}

func open(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	if a.Table == nil {
		return errors.New(`the event stream property "url" is required`)
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	var url, asyncapi string
	header := http.Header{}
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
		case p.Key == contract.Row:
			asyncapi = strings.TrimSpace(v)
		default:
			return fmt.Errorf("unknown event stream property %q (supported: url, header.<name>, asyncapi)", p.Key)
		}
	}
	if url == "" {
		return errors.New(`the event stream property "url" is required`)
	}
	e := &events{name: name, url: url, in: &cloudstep.Inbox{}}
	if asyncapi != "" {
		if e.contract, err = contract.Open(sc, asyncapi); err != nil {
			return err
		}
	}
	s, err := stream.OpenEvents(url, header,
		func(ev stream.Event) {
			e.in.Add(cloudstep.Message{Body: []byte(ev.Data), Fields: map[string]string{"type": ev.Type, "id": ev.ID}})
		},
		func(err error) {
			if err != nil {
				e.in.End("the stream broke: " + err.Error())
				return
			}
			e.in.End("the server ended the stream")
		})
	if err != nil {
		return secrets.Hide(sc, fmt.Errorf("could not open the %s event stream at %s: %w", name, url, err))
	}
	e.stream = s
	if err := streams.Of(sc).Add(name, e); err != nil {
		s.Close()
		return err
	}
	sc.Log("opened the %s event stream: %s", name, secrets.Mask(sc, url))
	return nil
}

func get(sc *core.Scenario, name string) (*events, error) {
	e, err := streams.Of(sc).Get(name)
	if err != nil {
		return nil, fmt.Errorf("no event stream named %q in this scenario; open it first with \"the %s event stream with the following properties:\"", name, name)
	}
	return e, nil
}

// found checks the event a check found against the stream's contract: a
// message of the channel of the stream's path, named by its event type
// when the channel has one of that name.
func (e *events) found(sc *core.Scenario, match func(cloudstep.Message) (bool, error)) func(cloudstep.Message) (bool, error) {
	return func(m cloudstep.Message) (bool, error) {
		ok, err := match(m)
		if ok && err == nil {
			if err := contract.Check(e.contract, sc, contract.Message{
				Protocol: "http", Addresses: contract.URLAddresses(e.url), Payload: m.Body, Name: m.Fields["type"],
			}); err != nil {
				return false, err
			}
		}
		return ok, err
	}
}

func where(e *events) string { return "the " + e.name + " event stream" }

// describe is the scenario's streams, for failure reports.
func describe(sc *core.Scenario, s *core.Services[*events]) any {
	out := map[string]any{}
	for _, e := range s.All() {
		msgs, _ := e.in.Since(time.Time{})
		received := make([]string, 0, len(msgs))
		for _, m := range msgs[max(0, len(msgs)-10):] {
			received = append(received, secrets.Mask(sc, m.Describe(field)))
		}
		d := map[string]any{"url": secrets.Mask(sc, e.url), "events": received}
		if why, ended := e.in.Ended(); ended {
			d["ended"] = why
		}
		out[e.name] = d
	}
	return out
}
