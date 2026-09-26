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
}

// Sample is what a messaging pack's step examples send and check, in the
// pack's own terms.
type Sample struct {
	// To is a target the services read, which the examples send the JSON
	// Body to, and the File with the Fields.
	To, Body, File string
	Fields         [][2]string
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
	return []core.StepDef{
		{
			ID: m.Pack + ".send", Keyword: "When", Arg: core.ArgDocString, Since: "0.1.0",
			Expr:     "a message is " + m.Verb + " to the {word} " + colon(t),
			Doc:      fmt.Sprintf("Send a message to the %s; the doc string is its body.", dt),
			Examples: []string{fmt.Sprintf("When a message is %s to the %s %s:", m.Verb, ex.To, et) + docString(ex.Body)},
			Run: func(sc *core.Scenario, a core.Args) error {
				return m.send(sc, a.String(0), []byte(a.DocString.Content), nil)
			},
		},
		{
			ID: m.Pack + ".send.file", Keyword: "When", Arg: core.ArgOptional, Since: "0.1.0",
			Expr: "the {filepath} message is " + m.Verb + " to the {word} " + t + "[[ with the following " + m.Fields + ":]]",
			Doc:  fmt.Sprintf("Send a message whose body is the file to the %s, with the %s of the table when the step has one.", dt, m.Fields),
			Table: &core.TableDoc{
				Columns: []string{"name", "value"},
				Note:    fmt.Sprintf("Each row is %s %s sent with the message, as text: its name and its value.", article(m.Field), m.Field),
			},
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
		{
			ID: m.Pack + ".received", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "[[within {duration} ]]the {word} " + t + " has a message where:",
			Doc: fmt.Sprintf("Check that the %s has a message with those values, received since the scenario started.\n\n"+
				"- The check waits for it: 10 seconds, or `within {duration}`.\n"+
				"- %s", dt, m.Received),
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: fmt.Sprintf("Each row is a path into the message's JSON body (a field name, a dotted path or a JSONPath) and "+
					"the value it has, compared as text: `null` for null and `undefined` for absent. A row `%s <name>` is instead "+
					"on the %s of that name sent with the message: `undefined` when there is none.", m.Field, m.Field),
			},
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
