package cloudstep

import (
	"fmt"
	"os"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// Messages describes the message steps of one messaging pack.
type Messages struct {
	// Pack is the pack name, the prefix of the step IDs.
	Pack string
	// Target is how steps name what messages go to: "sqs queue",
	// "pubsub topic", "service bus queue/topic".
	Target string
	// Verb is "sent" or "published".
	Verb string
	// Field names the values sent with a message: "attribute" or
	// "property"; Fields is its plural.
	Field, Fields string
	// Example is a target name for the step examples.
	Example string
	// Send sends a message.
	Send func(sc *core.Scenario, target string, body []byte, fields map[string]string) error
	// Inbox returns the listener for a target's checks; noun is the
	// target's kind as the step wrote it ("service bus topic").
	Inbox func(sc *core.Scenario, target, noun string) (*Inbox, error)
	// Received explains in the pack's docs how the checks receive
	// messages.
	Received string
}

// Steps are the pack's message steps: send a message given in the step or
// in a file (with its attributes), and wait for a message that meets
// conditions.
func (m Messages) Steps() []core.StepDef {
	t := m.Target
	return []core.StepDef{
		{
			ID: m.Pack + ".send", Keyword: "When", Arg: core.ArgDocString, Since: "0.1.0",
			Expr:     "a message is " + m.Verb + " to the {word} " + colon(t),
			Doc:      fmt.Sprintf("Send a message whose body is the doc string to a %s.", t),
			Examples: []string{fmt.Sprintf("When a message is %s to the %s %s:", m.Verb, m.Example, t)},
			Run: func(sc *core.Scenario, a core.Args) error {
				return m.send(sc, a.String(0), []byte(a.DocString.Content), nil)
			},
		},
		{
			ID: m.Pack + ".send.file", Keyword: "When", Arg: core.ArgOptional, Since: "0.1.0",
			Expr: "the {filepath} message is " + m.Verb + " to the {word} " + t + "[[ with the following " + m.Fields + ":]]",
			Doc: fmt.Sprintf("Send a message whose body is the file (resolved against `resources`) to a %s, "+
				"with the %s of the table (`name | value`).", t, m.Fields),
			Examples: []string{
				fmt.Sprintf("When the messages/shipment-delivered.json message is %s to the %s %s", m.Verb, m.Example, t),
				fmt.Sprintf("When the messages/shipment-delivered.json message is %s to the %s %s with the following %s:", m.Verb, m.Example, t, m.Fields),
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				p, err := sc.Suite().ResolvePath(a.String(0))
				if err != nil {
					return err
				}
				body, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				var fields map[string]string
				withTable := strings.HasSuffix(a.Text, ":")
				switch {
				case withTable && a.Table == nil:
					return fmt.Errorf("the %s are missing: add a table (| name | value |)", m.Fields)
				case !withTable && a.Table != nil:
					return fmt.Errorf("say \"with the following %s:\" to send a table of %s", m.Fields, m.Fields)
				case a.Table != nil:
					pairs, err := a.Table.Pairs()
					if err != nil {
						return err
					}
					fields = map[string]string{}
					for _, pr := range pairs {
						fields[pr.Key] = sc.Suite().Interpolate(pr.Value)
					}
				}
				return m.send(sc, a.String(1), body, fields)
			},
		},
		{
			ID: m.Pack + ".received", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "[[within {duration} ]]the {word} " + t + " has a message where:",
			Doc: fmt.Sprintf("Wait (10s, or the given time) until the %s has a message, received since the scenario started, "+
				"that meets every row: `path | value` on the JSON body (a field name, a dotted path or a JSONPath, compared as text; "+
				"`null` for null and `undefined` for absent), or `%s <name> | value` on a %s sent with it. %s",
				t, m.Field, m.Field, m.Received),
			Examples: []string{fmt.Sprintf("Then within 30s the %s %s has a message where:", m.Example, t)},
			Run: func(sc *core.Scenario, a core.Args) error {
				rs, err := Conditions(a.Table)
				if err != nil {
					return err
				}
				target, noun := a.String(1), m.Noun(a.Text)
				in, err := m.Inbox(sc, target, noun)
				if err != nil {
					return err
				}
				return ExpectMessage(sc, Wait(a, 0), in, rs, m.Field, fmt.Sprintf("the %s %s", target, noun))
			},
		},
	}
}

func (m Messages) send(sc *core.Scenario, target string, body []byte, fields map[string]string) error {
	if err := m.Send(sc, target, body, fields); err != nil {
		return fmt.Errorf("cannot send the message to the %s %s: %w", target, m.Target, err)
	}
	sc.Log("%s a message to the %s %s: %s", m.Verb, target, m.Target, Compact(body))
	return nil
}

// Noun is the target's kind as a step's text wrote it ("service bus topic"
// for "service bus queue/topic").
func (m Messages) Noun(text string) string {
	base, alts, ok := strings.Cut(m.Target, "/")
	if !ok {
		return m.Target
	}
	head := base[:strings.LastIndex(base, " ")+1]
	for _, alt := range append([]string{base[len(head):]}, strings.Split(alts, "/")...) {
		if strings.Contains(text, head+alt) {
			return head + alt
		}
	}
	return m.Target
}

// colon ends a target with a colon; an alternation ("queue/topic") takes it
// on every alternative, since the colon would otherwise belong to the last
// one only.
func colon(target string) string {
	head, last := "", target
	if i := strings.LastIndex(target, " "); i >= 0 {
		head, last = target[:i+1], target[i+1:]
	}
	alts := strings.Split(last, "/")
	for i := range alts {
		alts[i] += ":"
	}
	return head + strings.Join(alts, "/")
}
