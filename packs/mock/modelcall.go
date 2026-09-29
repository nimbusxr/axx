package mock

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// modelRequest is a request a mocked model received, read from any
// provider's API, as the axx WireMock image's model mock reads it: what the
// model was given, the tools it was offered and the schema it was asked for.
type modelRequest struct {
	api          string
	conversation conversation
	tools        []string
	schema       any // the JSON Schema the answer is asked to follow, or nil
	body         map[string]any
	// previous is the response a request to OpenAI's Responses API continues
	// (previous_response_id), and responseID the id of the response it got.
	previous, responseID string
	// earlier is the conversation of the response it continues, once known.
	earlier *conversation
}

// conversation is what the model was given: the system prompt, and the
// messages with the tools called and their results.
type conversation struct {
	system   string
	messages []message
}

type message struct {
	role    string
	text    string
	calls   []toolCall
	results []toolResult
}

type toolCall struct {
	name      string
	arguments any
}

type toolResult struct {
	text string
}

var bedrockModel = regexp.MustCompile(`/model/[^/]+/(converse|converse-stream|invoke|invoke-with-response-stream)$`)

// readModelRequest reads a journaled request to a model's chat endpoint: a
// chat completion or response (OpenAI and the servers that speak its API),
// a message (Anthropic, directly, on Bedrock or on Vertex AI),
// generateContent (Gemini), converse (Bedrock) or /api/chat and
// /api/generate (Ollama). It is nil for any other request.
func readModelRequest(method, rawURL string, body []byte) *modelRequest {
	if method != "POST" {
		return nil
	}
	path := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		path = u.Path
	}
	var b map[string]any
	if json.Unmarshal(body, &b) != nil || b == nil {
		return nil
	}
	m := bedrockModel.FindStringSubmatch(path)
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return openaiChat(b)
	case strings.HasSuffix(path, "/responses"):
		return openaiResponses(b)
	case strings.HasSuffix(path, "/v1/messages"),
		strings.Contains(path, "/publishers/anthropic/models/") && (strings.HasSuffix(path, ":rawPredict") || strings.HasSuffix(path, ":streamRawPredict")):
		return anthropic(b)
	case strings.HasSuffix(path, ":generateContent"), strings.HasSuffix(path, ":streamGenerateContent"):
		return gemini(b)
	case m != nil && strings.HasPrefix(m[1], "converse"):
		return bedrockConverse(b)
	case m != nil && b["anthropic_version"] != nil:
		return anthropic(b)
	case strings.HasSuffix(path, "/api/chat"):
		return ollamaChat(b)
	case strings.HasSuffix(path, "/api/generate"):
		r := &modelRequest{api: "Ollama's /api/generate", body: b}
		r.conversation = conversation{system: str(b["system"]), messages: []message{{role: "user", text: str(b["prompt"])}}}
		if f, ok := b["format"].(map[string]any); ok {
			r.schema = f
		}
		return r
	}
	return nil
}

func openaiChat(b map[string]any) *modelRequest {
	r := &modelRequest{api: "OpenAI's Chat Completions", body: b}
	var system []string
	for _, o := range list(b["messages"]) {
		m := obj(o)
		text := contentText(m["content"])
		switch str(m["role"]) {
		case "system", "developer":
			system = append(system, text)
		case "tool", "function":
			r.conversation.messages = append(r.conversation.messages, message{role: "tool", results: []toolResult{{text: text}}})
		case "assistant":
			msg := message{role: "assistant", text: join(text, str(m["refusal"]))}
			for _, c := range list(m["tool_calls"]) {
				fn := obj(obj(c)["function"])
				if fn == nil {
					fn = obj(obj(c)["custom"])
					msg.calls = append(msg.calls, toolCall{name: str(fn["name"]), arguments: fn["input"]})
					continue
				}
				msg.calls = append(msg.calls, toolCall{name: str(fn["name"]), arguments: arguments(fn["arguments"])})
			}
			r.conversation.messages = append(r.conversation.messages, msg)
		default:
			r.conversation.messages = append(r.conversation.messages, message{role: "user", text: text})
		}
	}
	r.conversation.system = strings.Join(system, "\n")
	for _, t := range list(b["tools"]) {
		name := str(at(t, "function", "name"))
		if name == "" {
			name = str(at(t, "custom", "name"))
		}
		r.tools = append(r.tools, name)
	}
	if at(b, "response_format", "type") == "json_schema" {
		r.schema = at(b, "response_format", "json_schema", "schema")
	}
	return r
}

func openaiResponses(b map[string]any) *modelRequest {
	r := &modelRequest{api: "OpenAI's Responses", body: b, previous: str(b["previous_response_id"])}
	system := []string{contentText(instructions(b["instructions"]))}
	if s, ok := b["input"].(string); ok {
		r.conversation.messages = append(r.conversation.messages, message{role: "user", text: s})
	}
	for _, o := range list(b["input"]) {
		item := obj(o)
		switch str(item["type"]) {
		case "function_call":
			r.conversation.messages = append(r.conversation.messages, message{
				role:  "assistant",
				calls: []toolCall{{name: str(item["name"]), arguments: arguments(item["arguments"])}},
			})
		case "custom_tool_call":
			r.conversation.messages = append(r.conversation.messages, message{
				role:  "assistant",
				calls: []toolCall{{name: str(item["name"]), arguments: item["input"]}},
			})
		case "function_call_output", "custom_tool_call_output":
			r.conversation.messages = append(r.conversation.messages, message{
				role:    "tool",
				results: []toolResult{{text: contentText(item["output"])}},
			})
		case "message", "":
			role, text := str(item["role"]), contentText(item["content"])
			if role == "system" || role == "developer" {
				system = append(system, text)
				continue
			}
			if role == "" {
				role = "user"
			}
			r.conversation.messages = append(r.conversation.messages, message{role: role, text: text})
		}
	}
	r.conversation.system = strings.TrimSpace(strings.Join(system, "\n"))
	for _, t := range list(b["tools"]) {
		if name := str(obj(t)["name"]); name != "" {
			r.tools = append(r.tools, name)
		}
	}
	if at(b, "text", "format", "type") == "json_schema" {
		r.schema = at(b, "text", "format", "schema")
	}
	return r
}

// instructions is the text of the Responses API's instructions: a string,
// or input items.
func instructions(v any) any {
	items, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, contentText(obj(item)["content"]))
	}
	return out
}

func anthropic(b map[string]any) *modelRequest {
	r := &modelRequest{api: "Anthropic's Messages", body: b}
	r.conversation.system = contentText(b["system"])
	for _, o := range list(b["messages"]) {
		m := obj(o)
		msg := message{role: str(m["role"])}
		if s, ok := m["content"].(string); ok {
			msg.text = s
			r.conversation.messages = append(r.conversation.messages, msg)
			continue
		}
		var texts []string
		for _, bl := range list(m["content"]) {
			block := obj(bl)
			switch str(block["type"]) {
			case "text":
				texts = append(texts, str(block["text"]))
			case "tool_use", "server_tool_use":
				msg.calls = append(msg.calls, toolCall{name: str(block["name"]), arguments: block["input"]})
			case "tool_result":
				msg.results = append(msg.results, toolResult{text: contentText(block["content"])})
			}
		}
		msg.text = strings.Join(texts, "\n")
		r.conversation.messages = append(r.conversation.messages, msg)
	}
	for _, t := range list(b["tools"]) {
		r.tools = append(r.tools, str(obj(t)["name"]))
	}
	r.schema = at(b, "output_format", "schema")
	if r.schema == nil {
		r.schema = at(b, "output_config", "format", "schema")
	}
	return r
}

func gemini(b map[string]any) *modelRequest {
	r := &modelRequest{api: "Gemini's generateContent", body: b}
	r.conversation.system = contentText(at(b, "systemInstruction", "parts"))
	for _, o := range list(b["contents"]) {
		c := obj(o)
		msg := message{role: "user"}
		if str(c["role"]) == "model" {
			msg.role = "assistant"
		}
		var texts []string
		for _, p := range list(c["parts"]) {
			part := obj(p)
			if t, ok := part["text"].(string); ok && part["thought"] != true {
				texts = append(texts, t)
			}
			if call := obj(part["functionCall"]); call != nil {
				msg.calls = append(msg.calls, toolCall{name: str(call["name"]), arguments: call["args"]})
			}
			if res := obj(part["functionResponse"]); res != nil {
				msg.results = append(msg.results, toolResult{text: compact(res["response"])})
			}
		}
		msg.text = strings.Join(texts, "\n")
		r.conversation.messages = append(r.conversation.messages, msg)
	}
	for _, t := range list(b["tools"]) {
		for _, d := range list(obj(t)["functionDeclarations"]) {
			r.tools = append(r.tools, str(obj(d)["name"]))
		}
	}
	config := obj(b["generationConfig"])
	for _, k := range []string{"responseJsonSchema", "_responseJsonSchema", "responseSchema"} {
		if config[k] != nil {
			r.schema = config[k]
			break
		}
	}
	return r
}

func bedrockConverse(b map[string]any) *modelRequest {
	r := &modelRequest{api: "Bedrock's Converse", body: b}
	r.conversation.system = contentText(b["system"])
	for _, o := range list(b["messages"]) {
		m := obj(o)
		msg := message{role: str(m["role"])}
		var texts []string
		for _, bl := range list(m["content"]) {
			block := obj(bl)
			if t, ok := block["text"].(string); ok {
				texts = append(texts, t)
			}
			if use := obj(block["toolUse"]); use != nil {
				msg.calls = append(msg.calls, toolCall{name: str(use["name"]), arguments: use["input"]})
			}
			if res := obj(block["toolResult"]); res != nil {
				msg.results = append(msg.results, toolResult{text: contentText(res["content"])})
			}
		}
		msg.text = strings.Join(texts, "\n")
		r.conversation.messages = append(r.conversation.messages, msg)
	}
	for _, t := range list(at(b, "toolConfig", "tools")) {
		if name := str(at(t, "toolSpec", "name")); name != "" {
			r.tools = append(r.tools, name)
		}
	}
	r.schema = at(b, "outputConfig", "textFormat", "structure", "jsonSchema", "schema")
	if s, ok := r.schema.(string); ok {
		var v any
		if json.Unmarshal([]byte(s), &v) == nil {
			r.schema = v
		}
	}
	return r
}

func ollamaChat(b map[string]any) *modelRequest {
	r := &modelRequest{api: "Ollama's /api/chat", body: b}
	var system []string
	for _, o := range list(b["messages"]) {
		m := obj(o)
		content := str(m["content"])
		switch str(m["role"]) {
		case "system":
			system = append(system, content)
		case "tool":
			r.conversation.messages = append(r.conversation.messages, message{role: "tool", results: []toolResult{{text: content}}})
		case "assistant":
			msg := message{role: "assistant", text: content}
			for _, c := range list(m["tool_calls"]) {
				fn := obj(obj(c)["function"])
				msg.calls = append(msg.calls, toolCall{name: str(fn["name"]), arguments: fn["arguments"]})
			}
			r.conversation.messages = append(r.conversation.messages, msg)
		default:
			r.conversation.messages = append(r.conversation.messages, message{role: "user", text: content})
		}
	}
	r.conversation.system = strings.Join(system, "\n")
	for _, t := range list(b["tools"]) {
		r.tools = append(r.tools, str(at(t, "function", "name")))
	}
	if f, ok := b["format"].(map[string]any); ok {
		r.schema = f
	}
	return r
}

// text is everything the model was given, as the model mock's about
// matches it: the system prompt, the messages, the tools called and their
// results, and the conversation a request continues.
func (r *modelRequest) text() string {
	var b strings.Builder
	if r.earlier != nil {
		b.WriteString(r.earlier.text())
		b.WriteByte('\n')
	}
	b.WriteString(r.conversation.text())
	return b.String()
}

func (c conversation) text() string {
	parts := []string{searchable(c.system)}
	for _, m := range c.messages {
		parts = append(parts, searchable(m.text))
		for _, call := range m.calls {
			parts = append(parts, call.name, searchable(compact(call.arguments)))
		}
		for _, res := range m.results {
			parts = append(parts, searchable(res.text))
		}
	}
	return strings.Join(parts, "\n")
}

// sent is every text the request sent: every string in its body, its
// tools and settings included, and what it continues.
func (r *modelRequest) sent() string {
	var out []string
	strs(r.body, &out)
	if r.earlier != nil {
		out = append(out, r.earlier.text())
	}
	return strings.Join(out, "\n")
}

// lastUser is the latest thing the user wrote, to say what a request was
// about in a failure.
func (r *modelRequest) lastUser() string {
	for i := len(r.conversation.messages) - 1; i >= 0; i-- {
		if m := r.conversation.messages[i]; m.role == "user" && strings.TrimSpace(m.text) != "" {
			return m.text
		}
	}
	return ""
}

// searchable is a text, and when it is JSON, the strings in it as well: a
// service that sends a tool's result as JSON text may have escaped what it
// holds ("Hauptstraße"), which a search of the text alone would miss.
func searchable(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return text
	}
	var v any
	if json.Unmarshal([]byte(t), &v) != nil {
		return text
	}
	var out []string
	strs(v, &out)
	return text + "\n" + strings.Join(out, "\n")
}

// strs appends every string in a JSON value, with the strings in strings
// that are JSON themselves.
func strs(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, searchable(x))
	case map[string]any:
		for _, e := range x {
			strs(e, out)
		}
	case []any:
		for _, e := range x {
			strs(e, out)
		}
	case nil:
	default:
		*out = append(*out, compact(x))
	}
}

// contentText is the text of a message's content: a string, or the text
// of its parts.
func contentText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var out []string
		for _, p := range x {
			if s, ok := p.(string); ok {
				out = append(out, s)
				continue
			}
			part := obj(p)
			for _, k := range []string{"text", "refusal", "thinking"} {
				if s, ok := part[k].(string); ok {
					out = append(out, s)
				}
			}
			if part["json"] != nil {
				out = append(out, compact(part["json"]))
			}
		}
		return strings.Join(out, "\n")
	case map[string]any:
		return contentText([]any{x})
	}
	return ""
}

// arguments is a tool call's arguments: JSON text, parsed.
func arguments(v any) any {
	if s, ok := v.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			return parsed
		}
	}
	return v
}

func compact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func at(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func join(a, b string) string {
	switch {
	case b == "":
		return a
	case a == "":
		return b
	}
	return a + "\n" + b
}
