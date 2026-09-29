package a2a

import (
	"encoding/json"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// defaultWait is how long a check waits for what a stream has not sent
// yet, or a task the agent still works on.
const defaultWait = cloudstep.DefaultWait

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".agent", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} a2a agent with the following properties:",
			Doc: "Register an A2A agent under a name: its card is read and checked for what A2A 1.0 requires, and says how to reach it " +
				"(JSON-RPC, HTTP+JSON or gRPC).",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "the agent's URL; its card is read at `/.well-known/agent-card.json` on its origin"},
					{Name: "card", Takes: "the agent's card, when it is not there: a file of the project, or a URL"},
					{Name: "transport", Takes: "how to reach the agent, among the interfaces its card lists", Values: []string{"JSONRPC", "HTTP+JSON", "GRPC"}, Default: "the card's first"},
					{Name: "header.<name>", Takes: "a header sent with every request, such as `header.Authorization`"},
					{Name: "timeout", Takes: "how long a call may take, like `10s`", Default: "30s"},
					{Name: "push url", Takes: "where the agent sends its tasks' updates (a webhook, such as a WireMock the mock pack checks)"},
					{Name: "push token", Takes: "the token the agent sends with them, for the webhook to check"},
				},
				Note: "A `url` or a `card` is required. Values are expanded (`${env:..}`, `${sys:..}`, `${token:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{
				"Given the parcels a2a agent with the following properties:\n" +
					"  | url | http://localhost:8400 |",
				"Given the carrier a2a agent with the following properties:\n" +
					"  | url                  | https://agents.carrier.example |\n" +
					"  | transport            | HTTP+JSON                      |\n" +
					"  | header.Authorization | Bearer ${token:carrier}        |",
			},
			TableTypes: map[string]string{"card": "filepath"},
			Run:        register,
		},
		{
			ID: Name + ".card", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr:  "the {word} a2a agent's card has the following properties:",
			Doc:   "Check the agent's card: what another agent reads to find out what it does and how to reach it.",
			Table: &core.TableDoc{Columns: []string{"property", "value"}, Note: "A row is a path into the card (`name`, `capabilities.streaming`, `skills[0].id`) and its value."},
			Examples: []string{"Then the parcels a2a agent's card has the following properties:\n" +
				"  | name                   | Parcels agent |\n" +
				"  | capabilities.streaming | true          |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				ag, err := get(sc, a.String(0))
				if err != nil {
					return err
				}
				b, err := json.Marshal(ag.card)
				if err != nil {
					return err
				}
				return jsonassert.Properties(string(b), a.Table, false)
			},
		},
		{
			ID: Name + ".skill", Keyword: "Then", Since: since,
			Expr:     "the {word} a2a agent has the {word} skill",
			Doc:      "Check that the agent's card lists a skill, by its id.",
			Examples: []string{"Then the parcels a2a agent has the track-parcel skill"},
			Run: func(sc *core.Scenario, a core.Args) error {
				ag, err := get(sc, a.String(0))
				if err != nil {
					return err
				}
				var ids []string
				for _, s := range ag.card.Skills {
					if s.ID == a.String(1) {
						return nil
					}
					ids = append(ids, s.ID)
				}
				return core.Failf("the %s a2a agent has no skill %s; its skills are: %s", ag.name, a.String(1), strings.Join(ids, ", "))
			},
		},
		{
			ID: Name + ".send", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr: "a message is sent to the {word} a2a agent:",
			Doc: "Send the doc string as a text message, which starts a conversation, and wait for the agent's answer: a reply, or a task " +
				"that ended or waits for input.",
			Examples: []string{"When a message is sent to the parcels a2a agent:\n  \"\"\"\n  Where is PX-A2A-9201?\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				m, err := message(sc, a.DocString.Content, "")
				if err != nil {
					return err
				}
				return send(sc, a.String(0), m, false, false)
			},
		},
		{
			ID: Name + ".send.file", Keyword: "When", Since: since,
			Expr: "the {filepath} message is sent to the {word} a2a agent",
			Doc: "Send the message of a file of the project, an A2A message in JSON (its `parts`: text, data, files), which starts a " +
				"conversation, and wait for the agent's answer.",
			Examples: []string{"When the a2a/hold-request.json message is sent to the parcels a2a agent"},
			Run: func(sc *core.Scenario, a core.Args) error {
				m, err := message(sc, "", a.String(0))
				if err != nil {
					return err
				}
				return send(sc, a.String(1), m, false, false)
			},
		},
		{
			ID: Name + ".stream", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr: "a message is streamed to the {word} a2a agent:",
			Doc: "Send the doc string as a text message, which starts a conversation, and take the agent's answer as it streams: the checks " +
				"read its updates as they come. The stream belongs to the scenario, which ends it when it ends.",
			Examples: []string{"When a message is streamed to the parcels a2a agent:\n  \"\"\"\n  Where is PX-A2A-9203?\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				m, err := message(sc, a.DocString.Content, "")
				if err != nil {
					return err
				}
				return send(sc, a.String(0), m, false, true)
			},
		},
		{
			ID: Name + ".reply", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr: "a reply is sent to the {word} a2a agent:",
			Doc: "Send the doc string as a text message that continues the agent's last task: how a task that asks for input " +
				"(`input-required`) gets it. Wait for the agent's answer.",
			Examples: []string{"When a reply is sent to the parcels a2a agent:\n  \"\"\"\n  2026-10-05\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				m, err := message(sc, a.DocString.Content, "")
				if err != nil {
					return err
				}
				return send(sc, a.String(0), m, true, false)
			},
		},
		{
			ID: Name + ".cancel", Keyword: "When", Since: since,
			Expr: "the {word} a2a agent is asked to cancel its task",
			Doc: "Ask the agent to cancel its last task; check that it did with `the {word} a2a agent's task is canceled`. An agent that " +
				"will not (a task that ended) fails the step with its error.",
			Examples: []string{"When the parcels a2a agent is asked to cancel its task"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return cancelTask(sc, a.String(0))
			},
		},
		{
			ID: Name + ".state", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {word} a2a agent's task is {word}",
			Doc: "Check the state of the agent's last task: `submitted`, `working`, `completed`, `input-required`, `auth-required`, " +
				"`failed`, `canceled` or `rejected`.\n\n" +
				"- The check waits for a task still at work (10 seconds, or `within {duration}`), and fails at once for a task that ended or " +
				"waits for input in another state.",
			Examples: []string{
				"Then the parcels a2a agent's task is input-required",
				"Then within 30s the parcels a2a agent's task is completed",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				return checkState(sc, a.String(1), a.String(2), cloudstep.Wait(a, 0))
			},
		},
		{
			ID: Name + ".answer", Keyword: "Then", Since: since,
			Expr: "the {word} a2a agent's answer contains {string}",
			Doc: "Check that the agent's answer has a text: its reply, or its task's status message and artifacts. It waits for a task " +
				"still at work.",
			Examples: []string{"Then the parcels a2a agent's answer contains 'out for delivery'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return checkAnswer(sc, a.String(0), a.String(1))
			},
		},
		{
			ID: Name + ".artifact", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} a2a agent's task has an artifact where:",
			Doc: "Check that an artifact of the agent's last task has the table's properties: `name`, `description`, `text` (its text " +
				"parts), `data` (its first data part: `data.status`) and `parts`. It waits for a task still at work.",
			Table: &core.TableDoc{Columns: []string{"property", "value"}, Note: "A row is a path into the artifact, and its value."},
			Examples: []string{"Then the parcels a2a agent's task has an artifact where:\n" +
				"  | name        | parcel-status    |\n" +
				"  | data.status | OUT_FOR_DELIVERY |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return checkArtifact(sc, a.String(0), a.Table)
			},
		},
		{
			ID: Name + ".update", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} a2a agent's stream received an update where:",
			Doc: "Check that the stream of the message streamed last sent an update with the table's properties.\n\n" +
				"- A status update has `kind` `status`, its `state` and its `message`; an artifact update has `kind` `artifact` and its " +
				"`artifact` (`artifact.name`, `artifact.text`, `artifact.data.status`...).\n" +
				"- The check waits (10 seconds, or `within {duration}`), and fails at once when the stream has ended.",
			Table: &core.TableDoc{Columns: []string{"property", "value"}, Note: "A row is a path into the update, and its value."},
			Examples: []string{"Then the parcels a2a agent's stream received an update where:\n" +
				"  | state   | working                  |\n" +
				"  | message | Looking up PX-A2A-9203   |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return checkUpdate(sc, a.String(1), a.Table, cloudstep.Wait(a, 0))
			},
		},
	}
}
