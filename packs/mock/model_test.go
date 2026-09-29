package mock

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

// The requests a parcel assistant sent its models, as each provider's SDK
// sends them, with other traffic around them.
var modelJournal = []journaled{
	{Method: "GET", URL: "/v1/postcodes/DE/10115"},
	// OpenAI's Chat Completions: a tool loop, whose tool result a Python
	// service escaped (json.dumps).
	{Method: "POST", URL: "/v1/chat/completions", Body: `{"messages":[{"content":"You help recipients track their parcels.","role":"system"},` +
		`{"content":"Where is PX-AI-8102?","role":"user"}],"model":"gpt-4.1-mini",` +
		`"tools":[{"function":{"name":"track_parcel","parameters":{"type":"object","properties":{"reference":{"type":"string"}}}},"type":"function"}]}`},
	{Method: "POST", URL: "/v1/chat/completions", Body: `{"messages":[{"content":"You help recipients track their parcels.","role":"system"},` +
		`{"content":"Where is PX-AI-8102?","role":"user"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"track_parcel","arguments":"{\"reference\":\"PX-AI-8102\"}"}}]},` +
		`{"role":"tool","tool_call_id":"call_1","content":"{\"status\": \"OUT_FOR_DELIVERY\", \"street\": \"Hauptstra\\u00dfe 5\"}"}],` +
		`"model":"gpt-4.1-mini","tools":[{"function":{"name":"track_parcel"},"type":"function"}]}`},
	// OpenAI's Responses: the follow-up names the first response.
	{
		Method: "POST", URL: "/v1/responses", Body: `{"model":"gpt-4.1-mini","input":"Where is PX-AI-8120?","tools":[{"type":"function","name":"track_parcel"}]}`,
		Response: `{"id":"resp_0a1b","object":"response","output":[{"type":"function_call","call_id":"call_9","name":"track_parcel"}]}`,
	},
	{
		Method: "POST", URL: "/v1/responses", Body: `{"model":"gpt-4.1-mini","previous_response_id":"resp_0a1b",` +
			`"input":[{"type":"function_call_output","call_id":"call_9","output":"{\"status\":\"AT_DEPOT\"}"}]}`,
		Response: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_2c3d\"}}\n\n",
	},
	// Anthropic's Messages, with structured output.
	{Method: "POST", URL: "/v1/messages", Body: `{"model":"claude-sonnet-4-5","max_tokens":1024,"system":"You read addresses.",` +
		`"messages":[{"role":"user","content":[{"type":"text","text":"Deliver to Lindenweg 14, 04109 Leipzig"}]}],` +
		`"output_config":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"street":{"type":"string"},"postcode":{"type":"string"}},"required":["street","postcode"]}}}}`},
	// Gemini, whose responseSchema writes types in capitals.
	{Method: "POST", URL: "/v1beta/models/gemini-2.5-flash:generateContent", Body: `{"contents":[{"role":"user","parts":[{"text":"Deliver to Birkenallee 3, 01067 Dresden"}]}],` +
		`"generationConfig":{"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","properties":{"street":{"type":"STRING"},"postcode":{"type":"STRING"}},"required":["street","postcode"]}}}`},
	// Bedrock's Converse, and Ollama's /api/chat.
	{Method: "POST", URL: "/model/eu.amazon.nova-lite-v1%3A0/converse", Body: `{"messages":[{"role":"user","content":[{"text":"Where is PX-AI-8401?"}]}],` +
		`"toolConfig":{"tools":[{"toolSpec":{"name":"track_parcel","inputSchema":{"json":{"type":"object"}}}},{"toolSpec":{"name":"reschedule_delivery","inputSchema":{"json":{"type":"object"}}}}]}}`},
	{Method: "POST", URL: "/api/chat", Body: `{"model":"llama3.2","messages":[{"role":"user","content":"Where is PX-AI-8501?"}],"stream":false}`},
	// An embeddings request is not a question to the model.
	{Method: "POST", URL: "/v1/embeddings", Body: `{"model":"text-embedding-3-small","input":"PX-AI-8102"}`},
}

func modelSetup(t *testing.T) (*match.Registry, *core.Scenario) {
	t.Helper()
	srv := httptest.NewServer((&fakeWireMock{journal: modelJournal}).handler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	address := "required: [street, postcode]\ntype: object\nproperties:\n  postcode: {type: string}\n  street: {type: string}\n"
	if err := os.WriteFile(filepath.Join(dir, "schemas", "address.yaml"), []byte(address), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schemas", "quote.json"), []byte(`{"type": "object", "properties": {"price": {"type": "number"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := match.NewRegistry()
	for _, p := range []struct {
		n string
		p core.Pack
	}{{"core", core.ParamsPack()}, {"mock", Pack()}} {
		if err := reg.AddPack(p.n, p.p.Manifest()); err != nil {
			t.Fatal(err)
		}
	}
	suite := core.NewSuite(core.SuiteOptions{ResolvePath: func(p string) (string, error) { return filepath.Join(dir, filepath.FromSlash(p)), nil }})
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, suite, nil)
	if err := step(t, reg, sc, "the mocked models service with the following properties:", [][]string{{"url", srv.URL}}); err != nil {
		t.Fatal(err)
	}
	return reg, sc
}

func TestModelChecks(t *testing.T) {
	reg, sc := modelSetup(t)
	for _, text := range []string{
		"the mocked models model was asked about 'PX-AI-8102'",
		"the mocked models model was asked about 'PX-AI-8102' 2 times",
		"the mocked models model was asked about 'You help recipients' 2 times",
		// The tool's result, escaped as it was sent, and a call's arguments.
		"the mocked models model's request about 'PX-AI-8102' contains 'Hauptstraße 5'",
		"the mocked models model's request about 'OUT_FOR_DELIVERY' contains '\"reference\":\"PX-AI-8102\"'",
		"the mocked models model's request about 'PX-AI-8102' does not contain 'Lindenweg 14'",
		"the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8102'",
		// The Responses API's follow-up is about what the response it continues was about.
		"the mocked models model was asked about 'PX-AI-8120' 2 times",
		"the mocked models model's request about 'PX-AI-8120' contains 'AT_DEPOT'",
		"the mocked models model was asked for the schemas/address.yaml schema in the request about 'Lindenweg 14'",
		"the mocked models model was asked for the schemas/address.yaml schema in the request about 'Birkenallee 3'",
		"the mocked models model was offered the reschedule_delivery tool in the request about 'PX-AI-8401'",
		"the mocked models model was asked about 'PX-AI-8501' 1 time",
	} {
		if err := step(t, reg, sc, text, nil); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
}

func TestModelCheckFailures(t *testing.T) {
	reg, sc := modelSetup(t)
	for _, c := range []struct{ text, want string }{
		{
			"the mocked models model was asked about 'PX-AI-9999'",
			"never asked about \"PX-AI-9999\"; of its 8 requests since the run started, the latest were about:\n  - Ollama's /api/chat: Where is PX-AI-8501?",
		},
		{"the mocked models model was asked about 'PX-AI-8102' 3 times", "was asked about \"PX-AI-8102\" 2 time(s), not 3"},
		{"the mocked models model's request about 'PX-AI-8102' contains 'DELIVERED'", "no request the mocked models model was asked about \"PX-AI-8102\" contains \"DELIVERED\""},
		// The street went to the model, escaped: the check finds it.
		{"the mocked models model's request about 'PX-AI-8102' does not contain 'Hauptstraße 5'", "contains \"Hauptstraße 5\""},
		{"the mocked models model's request about 'PX-AI-9999' does not contain 'Hauptstraße 5'", "never asked about \"PX-AI-9999\""},
		{
			"the mocked models model was offered the notify_recipient tool in the request about 'PX-AI-8401'",
			"was not offered the notify_recipient tool in the requests about \"PX-AI-8401\", only: reschedule_delivery, track_parcel",
		},
		{"the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8501'", "was offered no tool"},
		{"the mocked models model was asked for the schemas/quote.json schema in the request about 'Lindenweg 14'", "asked for another schema than schemas/quote.json"},
		{"the mocked models model was asked for the schemas/address.yaml schema in the request about 'PX-AI-8501'", "asked for no structured output"},
	} {
		err := step(t, reg, sc, c.text, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s:\n got %v\nwant %q", c.text, err, c.want)
			continue
		}
		if !core.IsAssertion(err) {
			t.Errorf("%s: a failed check is an assertion: %v", c.text, err)
		}
	}
}

func TestReadModelRequests(t *testing.T) {
	for _, c := range []struct {
		name, url, body string
		wantAPI         string
		wantText        []string
		wantTools       []string
	}{
		{
			"Anthropic on Bedrock", "/model/eu.anthropic.claude-sonnet-4-5-20250929-v1%3A0/invoke",
			`{"anthropic_version":"bedrock-2023-05-31","max_tokens":256,"messages":[{"role":"user","content":"PX-AI-8220"}],` +
				`"tools":[{"name":"track_parcel","input_schema":{"type":"object"}}]}`,
			"Anthropic's Messages",
			[]string{"PX-AI-8220"},
			[]string{"track_parcel"},
		},
		{
			"Anthropic on Vertex AI", "/v1/projects/p/locations/europe-west1/publishers/anthropic/models/claude-sonnet-4-5@20250929:streamRawPredict",
			`{"anthropic_version":"vertex-2023-10-16","max_tokens":256,"stream":true,"messages":[{"role":"user","content":"PX-AI-8222"}]}`,
			"Anthropic's Messages",
			[]string{"PX-AI-8222"},
			nil,
		},
		{
			"Gemini on Vertex AI, with a function's result", "/v1/projects/p/locations/europe-west1/publishers/google/models/gemini-2.5-flash:streamGenerateContent?alt=sse",
			`{"systemInstruction":{"parts":[{"text":"You help recipients."}]},"contents":[{"role":"user","parts":[{"text":"PX-AI-8302"}]},` +
				`{"role":"model","parts":[{"functionCall":{"name":"track_parcel","args":{"reference":"PX-AI-8302"}}}]},` +
				`{"role":"user","parts":[{"functionResponse":{"name":"track_parcel","response":{"status":"OUT_FOR_DELIVERY"}}}]}],` +
				`"tools":[{"functionDeclarations":[{"name":"track_parcel"}]}]}`,
			"Gemini's generateContent",
			[]string{"You help recipients.", "PX-AI-8302", "OUT_FOR_DELIVERY"},
			[]string{"track_parcel"},
		},
		{
			"Bedrock's ConverseStream, with a tool's result", "/model/eu.amazon.nova-lite-v1%3A0/converse-stream",
			`{"system":[{"text":"You help recipients."}],"messages":[{"role":"user","content":[{"text":"PX-AI-8405"}]},` +
				`{"role":"assistant","content":[{"toolUse":{"toolUseId":"t1","name":"track_parcel","input":{"reference":"PX-AI-8405"}}}]},` +
				`{"role":"user","content":[{"toolResult":{"toolUseId":"t1","content":[{"json":{"status":"AT_DEPOT"}}]}}]}]}`,
			"Bedrock's Converse",
			[]string{"You help recipients.", "AT_DEPOT"},
			nil,
		},
		{
			"Ollama's /api/generate", "/api/generate", `{"model":"llama3.2","system":"You help recipients.","prompt":"PX-AI-8504"}`,
			"Ollama's /api/generate",
			[]string{"You help recipients.", "PX-AI-8504"},
			nil,
		},
		{
			"Azure OpenAI", "/openai/deployments/parcels/chat/completions?api-version=2024-10-21",
			`{"messages":[{"role":"developer","content":[{"type":"text","text":"You help recipients."}]},{"role":"user","content":[{"type":"text","text":"PX-AI-8130"}]}]}`,
			"OpenAI's Chat Completions",
			[]string{"You help recipients.", "PX-AI-8130"},
			nil,
		},
	} {
		r := readModelRequest("POST", c.url, []byte(c.body))
		if r == nil {
			t.Errorf("%s: not read as a model request", c.name)
			continue
		}
		if r.api != c.wantAPI {
			t.Errorf("%s: api %q, want %q", c.name, r.api, c.wantAPI)
		}
		for _, w := range c.wantText {
			if !strings.Contains(r.text(), w) {
				t.Errorf("%s: the conversation lacks %q:\n%s", c.name, w, r.text())
			}
		}
		if strings.Join(r.tools, ",") != strings.Join(c.wantTools, ",") {
			t.Errorf("%s: tools %v, want %v", c.name, r.tools, c.wantTools)
		}
	}
	for _, c := range []struct{ method, url, body string }{
		{"GET", "/v1/models", ""},
		{"POST", "/v1/embeddings", `{"input":"x"}`},
		{"POST", "/model/amazon.titan-embed-text-v2%3A0/invoke", `{"inputText":"x"}`},
		{"POST", "/v1/chat/completions", `not json`},
	} {
		if r := readModelRequest(c.method, c.url, []byte(c.body)); r != nil {
			t.Errorf("%s %s is not a request to a model's chat, but was read as %s", c.method, c.url, r.api)
		}
	}
}
