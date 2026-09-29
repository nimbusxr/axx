package mock

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// The calls a parcels service made to a partner carrier's MCP server and
// A2A agent, with other traffic around them.
var agentJournal = []journaled{
	{Method: "POST", URL: "/mcp", Body: `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`},
	{Method: "POST", URL: "/mcp", Body: `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"shipment_status","arguments":{"reference":"PX-MCP-9301","country":"CH"}}}`},
	{Method: "POST", URL: "/mcp", Body: `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"shipment_status","arguments":{"reference":"PX-MCP-9302","country":"NO"}}}`},
	{Method: "POST", URL: "/mcp", Body: `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"shipment_status","arguments":{"reference":"PX-MCP-9302","country":"NO"}}}`},
	{Method: "POST", URL: "/mcp", Body: `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"book_pickup","arguments":{"reference":"PX-MCP-9301"}}}`},
	{Method: "POST", URL: "/a2a", Body: `{"jsonrpc":"2.0","id":"a","method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"Where is PX-A2A-9301?"}]}}}`},
	{Method: "POST", URL: "/a2a", Body: `{"jsonrpc":"2.0","id":"b","method":"SendStreamingMessage","params":{"message":{"messageId":"m2","role":"ROLE_USER","parts":[{"text":"Hold it"},{"data":{"reference":"PX-A2A-9302"}}]}}}`},
	{Method: "POST", URL: "/a2a/rest/message:send", Body: `{"message":{"messageId":"m3","role":"ROLE_USER","parts":[{"text":"Where is PX-A2A-9303?"}]}}`},
	{Method: "POST", URL: "/a2a", Body: `{"jsonrpc":"2.0","id":"c","method":"GetTask","params":{"id":"PX-A2A-9301"}}`},
	{Method: "GET", URL: "/.well-known/agent-card.json"},
}

func TestAgentChecks(t *testing.T) {
	srv := httptest.NewServer((&fakeWireMock{journal: agentJournal}).handler())
	t.Cleanup(srv.Close)
	reg, sc, _, _ := setup(t)
	if err := step(t, reg, sc, "the mocked partner service with the following properties:", [][]string{{"url", srv.URL}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		text  string
		table [][]string
	}{
		{"the mocked partner mcp server's shipment_status tool was called with the following arguments:", [][]string{{"reference", "PX-MCP-9301"}, {"country", "CH"}}},
		{"the mocked partner mcp server's shipment_status tool was called 2 times with the following arguments:", [][]string{{"reference", "PX-MCP-9302"}}},
		{"the mocked partner mcp server's book_pickup tool was called 1 time with the following arguments:", [][]string{{"reference", "PX-MCP-9301"}, {"country", "undefined"}}},
		{"the mocked partner a2a agent was sent a message containing 'PX-A2A-9301'", nil},
		{"the mocked partner a2a agent was sent a message containing 'PX-A2A-9302' 1 time", nil},
		{"the mocked partner a2a agent was sent a message containing 'PX-A2A-9303'", nil},
		{"the mocked partner a2a agent was sent a message containing 'Where is' 2 times", nil},
	} {
		if err := step(t, reg, sc, c.text, c.table); err != nil {
			t.Errorf("%s: %v", c.text, err)
		}
	}
	for _, c := range []struct {
		text  string
		table [][]string
		want  string
	}{
		{
			"the mocked partner mcp server's shipment_status tool was called with the following arguments:",
			[][]string{{"reference", "PX-MCP-9399"}},
			"the mocked partner mcp server's shipment_status tool was called with those arguments 0 time(s), not at least 1; its calls with other arguments:\n  {\"reference\":\"PX-MCP-9301\",\"country\":\"CH\"}",
		},
		{
			"the mocked partner mcp server's shipment_status tool was called 1 time with the following arguments:",
			[][]string{{"reference", "PX-MCP-9302"}},
			"with those arguments 2 time(s), not 1",
		},
		{
			"the mocked partner a2a agent was sent a message containing 'PX-A2A-9399'", nil,
			"the mocked partner a2a agent was sent a message containing \"PX-A2A-9399\" 0 time(s), not at least 1; the latest messages it was sent:\n  Where is PX-A2A-9301?",
		},
	} {
		err := step(t, reg, sc, c.text, c.table)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s:\n got %v\nwant %q", c.text, err, c.want)
			continue
		}
		if !core.IsAssertion(err) {
			t.Errorf("%s: a failed check is an assertion: %v", c.text, err)
		}
	}
}
