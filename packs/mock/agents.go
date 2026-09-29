package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/secrets"
)

// agentsSince is the axx version that introduced the MCP and A2A checks.
const agentsSince = "0.1.5"

// A mocked MCP server or A2A agent is a WireMock with the axx image's MCP
// or A2A mock. These steps check what the service sent it, from WireMock's
// journal: every request since the run started.
func agentSteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mock.mcp.called", Keyword: "Then", Arg: core.ArgTable, Since: agentsSince,
			Expr: "the mocked {mockedService} mcp server's {word} tool was called[[ {int} time(s)]] with the following arguments:",
			Doc: "Check that the service called the tool of a mocked MCP server with the table's arguments: at least once, or as many " +
				"times as the step says.\n\n" +
				"- Every call since the run started counts, other scenarios' too: put the scenario's own data in the table, like its " +
				"parcel's reference.\n" +
				"- Paths are dotted (`address.postcode`) or JSONPath, and values compare as text; `null` and `undefined` work as in " +
				"the other property steps.",
			Table: &core.TableDoc{Columns: []string{"argument", "value"}, Note: "A row is a path into the call's arguments, and its value."},
			Examples: []string{
				"Then the mocked partner-carrier mcp server's shipment_status tool was called with the following arguments:\n" +
					"  | reference | PX-MCP-9301 |",
				"Then the mocked partner-carrier mcp server's shipment_status tool was called 1 time with the following arguments:\n" +
					"  | reference | PX-MCP-9301 |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, tool := a.Value(0).(*Service), a.String(1)
				calls, err := svc.c.journal(sc.Context(), runContracts(sc.Suite()).start)
				if err != nil {
					return err
				}
				matched := 0
				var seen []string
				for _, c := range calls {
					args, ok := mcpToolCall(c.request, tool)
					if !ok {
						continue
					}
					if jsonassert.Properties(string(args), a.Table, false) == nil {
						matched++
					} else if len(seen) < 3 {
						seen = append(seen, string(args))
					}
				}
				return count(sc, a, 2, matched, fmt.Sprintf("the mocked %s mcp server's %s tool was called", svc.Name, tool),
					"with those arguments", seen, "its calls with other arguments")
			},
		},
		{
			ID: "mock.a2a.sent", Keyword: "Then", Since: agentsSince,
			Expr: "the mocked {mockedService} a2a agent was sent a message containing {string}[[ {int} time(s)]]",
			Doc: "Check that the service sent the mocked A2A agent a message whose text has the text (its text parts, and its data parts " +
				"as JSON): at least once, or as many times as the step says.\n\n" +
				"- Every message since the run started counts, other scenarios' too: look for the scenario's own data, like its " +
				"parcel's reference.\n" +
				"- Messages sent and streamed count alike, over JSON-RPC or HTTP+JSON.",
			Examples: []string{
				"Then the mocked partner-carrier a2a agent was sent a message containing 'PX-A2A-9301'",
				"Then the mocked partner-carrier a2a agent was sent a message containing 'PX-A2A-9301' 1 time",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, want := a.Value(0).(*Service), a.String(1)
				calls, err := svc.c.journal(sc.Context(), runContracts(sc.Suite()).start)
				if err != nil {
					return err
				}
				matched := 0
				var seen []string
				for _, c := range calls {
					text, ok := agentMessage(c.request)
					if !ok {
						continue
					}
					if strings.Contains(text, want) {
						matched++
					} else if len(seen) < 3 {
						seen = append(seen, excerpt(text))
					}
				}
				return count(sc, a, 2, matched, fmt.Sprintf("the mocked %s a2a agent was sent a message", svc.Name),
					fmt.Sprintf("containing %q", want), seen, "the latest messages it was sent")
			},
		},
	}
}

// count checks how many requests matched: at least one, or the step's
// number (argument i).
func count(sc *core.Scenario, a core.Args, i, matched int, what, which string, seen []string, seenAs string) error {
	switch {
	case a.Present(i) && matched == a.Int(i):
		return nil
	case !a.Present(i) && matched > 0:
		return nil
	}
	want := "at least 1"
	if a.Present(i) {
		want = fmt.Sprint(a.Int(i))
	}
	msg := fmt.Sprintf("%s %s %d time(s), not %s", what, which, matched, want)
	if len(seen) > 0 {
		msg += fmt.Sprintf("; %s:\n  %s", seenAs, strings.Join(seen, "\n  "))
	}
	return secrets.Hide(sc, core.Fail(msg, want, matched))
}

// jsonRPC is a JSON-RPC request.
type jsonRPC struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// mcpToolCall is the arguments of a request that calls the tool (MCP's
// tools/call), when it is one.
func mcpToolCall(l logged, tool string) (json.RawMessage, bool) {
	if l.Method != http.MethodPost {
		return nil, false
	}
	var req jsonRPC
	if json.Unmarshal(l.body(), &req) != nil || req.Method != "tools/call" {
		return nil, false
	}
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &p) != nil || p.Name != tool {
		return nil, false
	}
	if len(p.Arguments) == 0 {
		p.Arguments = json.RawMessage("{}")
	}
	return p.Arguments, true
}

// agentMessage is the text of the message a request sends an A2A agent,
// over JSON-RPC (SendMessage, SendStreamingMessage, and 0.3's message/send
// and message/stream) or HTTP+JSON (message:send, message:stream), when it
// sends one.
func agentMessage(l logged) (string, bool) {
	if l.Method != http.MethodPost {
		return "", false
	}
	path := l.URL
	if u, err := url.Parse(l.URL); err == nil {
		path = u.Path
	}
	var body json.RawMessage
	switch {
	case strings.HasSuffix(path, "/message:send"), strings.HasSuffix(path, "/message:stream"):
		body = l.body()
	default:
		var req jsonRPC
		if json.Unmarshal(l.body(), &req) != nil {
			return "", false
		}
		switch req.Method {
		case "SendMessage", "SendStreamingMessage", "message/send", "message/stream":
			body = req.Params
		default:
			return "", false
		}
	}
	var p struct {
		Message struct {
			Parts []map[string]any `json:"parts"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &p) != nil {
		return "", false
	}
	var texts []string
	for _, part := range p.Message.Parts {
		if t, ok := part["text"].(string); ok {
			texts = append(texts, t)
		}
		if d, ok := part["data"]; ok {
			texts = append(texts, searchable(compact(d)))
		}
	}
	return strings.Join(texts, "\n"), true
}
