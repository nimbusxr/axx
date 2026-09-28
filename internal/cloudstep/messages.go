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
	// Kind tells the steps of one kind of target from a pack's others, in
	// their IDs: "queue" gives amqp.queue.send. Empty for a pack with one.
	Kind string
	// Target is how steps name what messages go to: "sqs queue",
	// "pubsub topic", "service bus queue/topic".
	Target string
	// Verb is "sent" or "published".
	Verb string
	// Field names the values sent with a message: "attribute" or
	// "property"; Fields is its plural.
	Field, Fields string
	// Key is a value of the message that the doc string form can give
	// too, where a table cannot go: "routing key" gives "a message is
	// published to the {word} amqp exchange[[ with the routing key
	// {string}]]:". Send gets it as a field of that name. The Target must
	// have no alternatives.
	Key string
	// Meta are the values of a message itself, not sent with it as
	// fields, that checks read by name: "routing key", "topic".
	Meta []string
	// SendRows are the rows the "with the following <fields>:" table
	// knows, when it takes more than fields.
	SendRows []core.TableRow
	// Example is what the step examples send and check.
	Example Sample
	// Send sends a message.
	Send func(sc *core.Scenario, target string, body []byte, fields map[string]string) error
	// Inbox returns the listener for a target's checks; noun is the
	// target's kind as the step wrote it ("service bus topic").
	Inbox func(sc *core.Scenario, target, noun string) (*Inbox, error)
	// Received explains in the pack's docs how the checks receive
	// messages.
	Received string
	// Since is the axx version that introduced the pack, when it came
	// after the message steps (0.1.0).
	Since string
}

// Sample is what a messaging pack's step examples send and check, in the
// pack's own terms.
type Sample struct {
	// To is a target the services read, which the examples send the JSON
	// Body to, and the File with the Fields.
	To, Body, File string
	Fields         [][2]string
	// Key is the Key's value in the doc string form's example.
	Key string
	// From is a target the services write to, which the check example
	// checks with the Where rows.
	From  string
	Where [][2]string
}

// Steps are the pack's message steps: send a message given in the step or
// in a file (with its attributes), and wait for a message that meets
// conditions.
func (m Messages) Steps() []core.StepDef {
	t, ex := m.Target, m.Example
	// Examples name one target: "service bus queue", not "service bus queue/topic";
	// docs name them all: "service bus queue or topic".
	et, dt := oneAlternative(t), strings.ReplaceAll(t, "/", " or ")
	since := m.version()
	send := core.StepDef{
		ID: m.ID("send"), Keyword: "When", Arg: core.ArgDocString, Since: since,
		Expr:     "a message is " + m.Verb + " to the {word} " + colon(t),
		Doc:      fmt.Sprintf("Send a message to the %s; the doc string is its body.", dt),
		Examples: []string{fmt.Sprintf("When a message is %s to the %s %s:", m.Verb, ex.To, et) + docString(ex.Body)},
		Run: func(sc *core.Scenario, a core.Args) error {
			return m.send(sc, a.String(0), []byte(a.DocString.Content), nil)
		},
	}
	if m.Key != "" {
		send.Expr = "a message is " + m.Verb + " to the {word} " + t + "[[ with the " + m.Key + " {string}]]:"
		send.Doc = fmt.Sprintf("Send a message to the %s, with the %s the step gives; the doc string is its body.", dt, m.Key)
		send.Examples = append(send.Examples,
			fmt.Sprintf("When a message is %s to the %s %s with the %s '%s':", m.Verb, ex.To, et, m.Key, ex.Key)+docString(ex.Body))
		send.Run = func(sc *core.Scenario, a core.Args) error {
			var fields map[string]string
			if a.Present(1) {
				fields = map[string]string{m.Key: sc.Suite().Interpolate(a.String(1))}
			}
			return m.send(sc, a.String(0), []byte(a.DocString.Content), fields)
		}
	}
	sendTable := &core.TableDoc{
		Columns: []string{"name", "value"},
		Note:    fmt.Sprintf("Each row is %s %s sent with the message, as text: its name and its value.", article(m.Field), m.Field),
	}
	if len(m.SendRows) > 0 {
		sendTable = &core.TableDoc{Columns: []string{"name", "value"}, Rows: m.SendRows}
	}
	return []core.StepDef{
		send,
		{
			ID: m.ID("send.file"), Keyword: "When", Arg: core.ArgOptional, Since: since,
			Expr:  "the {filepath} message is " + m.Verb + " to the {word} " + t + "[[ with the following " + m.Fields + ":]]",
			Doc:   fmt.Sprintf("Send a message whose body is the file to the %s, with the %s of the table when the step has one.", dt, m.Fields),
			Table: sendTable,
			Examples: []string{
				fmt.Sprintf("When the %s message is %s to the %s %s", ex.File, m.Verb, ex.To, et),
				fmt.Sprintf("When the %s message is %s to the %s %s with the following %s:", ex.File, m.Verb, ex.To, et, m.Fields) +
					exampleTable(ex.Fields),
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
		m.ReceivedStep(),
	}
}

// ReceivedStep is the check of the pack's message steps alone, for a
// kind of target that messages are not sent to (a NATS stream, whose
// subjects they are published to).
func (m Messages) ReceivedStep() core.StepDef {
	t, ex := m.Target, m.Example
	et, dt := oneAlternative(t), strings.ReplaceAll(t, "/", " or ")
	since := m.version()
	whereNote := fmt.Sprintf("Each row is a path into the message's JSON body (a field name, a dotted path or a JSONPath) and "+
		"the value it has, compared as text: `null` for null and `undefined` for absent. A row `%s <name>` is instead "+
		"on the %s of that name sent with the message: `undefined` when there is none.", m.Field, m.Field)
	if len(m.Meta) > 0 {
		whereNote += fmt.Sprintf(" %s on the message itself, not its body.", rowNames(m.Meta))
	}
	return core.StepDef{
		ID: m.ID("received"), Keyword: "Then", Arg: core.ArgTable, Since: since,
		Expr: "[[within {duration} ]]the {word} " + t + " has a message where:",
		Doc: fmt.Sprintf("Check that the %s has a message with those values, received since the scenario started.\n\n"+
			"- The check waits for it: 10 seconds, or `within {duration}`.\n"+
			"- %s", dt, m.Received),
		Table:    &core.TableDoc{Columns: []string{"path", "value"}, Note: whereNote},
		Examples: []string{fmt.Sprintf("Then within 30s the %s %s has a message where:", ex.From, et) + exampleTable(ex.Where)},
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
			return ExpectMatch(sc, Wait(a, 0), in, func(msg Message) (bool, error) { return rs.MatchMessageIn(msg, m.Field, m.Meta) },
				m.Field, "message", fmt.Sprintf("the %s %s", target, noun))
		},
	}
}

// version is the axx version that introduced the steps.
func (m Messages) version() string {
	if m.Since == "" {
		return "0.1.0"
	}
	return m.Since
}

// ID is the ID of one of the pack's message steps: "send", "send.file" or
// "received".
func (m Messages) ID(step string) string {
	if m.Kind != "" {
		return m.Pack + "." + m.Kind + "." + step
	}
	return m.Pack + "." + step
}

// rowNames lists row names for the docs: "Rows `routing key` and `topic` are".
func rowNames(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	if len(quoted) == 1 {
		return "A row " + quoted[0] + " is"
	}
	return "Rows " + strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1] + " are"
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

// oneAlternative replaces each Cucumber alternation ("queue/topic") in text
// by its first alternative, for examples.
func oneAlternative(text string) string {
	words := strings.Fields(text)
	for i, w := range words {
		words[i], _, _ = strings.Cut(w, "/")
	}
	return strings.Join(words, " ")
}

// exampleTable renders rows as the data table of a step example, its cells
// aligned as in a feature file.
func exampleTable(rows [][2]string) string {
	var w [2]int
	for _, r := range rows {
		w[0], w[1] = max(w[0], len(r[0])), max(w[1], len(r[1]))
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "\n  | %-*s | %-*s |", w[0], r[0], w[1], r[1])
	}
	return b.String()
}

// docString renders text as the doc string of a step example.
func docString(text string) string {
	return "\n  \"\"\"\n  " + text + "\n  \"\"\""
}
