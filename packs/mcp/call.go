package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/schemadoc"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/tablevalue"
)

// call is a tool call: what the scenario sent, and what the server
// answered, or the error it refused the call with.
type call struct {
	server    *server
	tool      string
	arguments json.RawMessage
	result    *sdk.CallToolResult
	err       error // a call the server refused
	code      int64 // its JSON-RPC error code
}

// reading is a resource read.
type reading struct {
	server *server
	uri    string
	result *sdk.ReadResourceResult
}

// prompting is a prompt requested.
type prompting struct {
	server *server
	name   string
	result *sdk.GetPromptResult
}

type finding = schemadoc.Finding

// levelKeys are the keys MCP levels can be set on.
var levelKeys = oaslevel.NewKeys([]string{
	"validation.arguments.schema.{keyword}",
	"validation.result.schema.{keyword}",
	"validation.result.missing",
}, map[string][]string{"{keyword}": schemadoc.Keywords}, nil).Named("MCP", "validation.arguments")

// callTool calls a tool with the arguments of a table or of a file, checked
// against the tool's input schema first, and checks its structured result
// against its output schema.
func callTool(sc *core.Scenario, toolName, serverName string, t *core.Table, file string) error {
	s, err := get(sc, serverName)
	if err != nil {
		return err
	}
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	// A tool the server does not list is still called: a scenario may
	// check that the server refuses it.
	var tool *sdk.Tool
	if tools, err := s.tools(sc); err != nil {
		return err
	} else {
		for _, x := range tools {
			if x.Name == toolName {
				tool = x
			}
		}
	}
	var input map[string]any
	if tool != nil {
		input = asMap(tool.InputSchema)
	}
	args, err := arguments(sc, t, file, input)
	if err != nil {
		return err
	}
	if tool != nil && input != nil {
		found, err := validate(input, args, "validation.arguments.schema", "arguments")
		if err != nil {
			return fmt.Errorf("the %s tool's input schema: %w", toolName, err)
		}
		if err := report(sc, st, s, "the arguments of the "+toolName+" tool", found); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	res, callErr := s.session.CallTool(ctx, &sdk.CallToolParams{Name: toolName, Arguments: json.RawMessage(raw)})
	c := &call{server: s, tool: toolName, arguments: raw, result: res}
	var wire *jsonrpc.Error
	switch {
	case errors.As(callErr, &wire):
		c.err, c.code = callErr, wire.Code
	case callErr != nil:
		return secrets.Hide(sc, fmt.Errorf("cannot call the %s tool on the %s mcp server: %w", toolName, s.name, callErr))
	}
	scenarioState.Of(sc).record(c)
	if c.err != nil || res.IsError || tool == nil || tool.OutputSchema == nil {
		return nil
	}
	output := asMap(tool.OutputSchema)
	var found []finding
	if res.StructuredContent == nil {
		found = []finding{{Key: "validation.result.missing", Message: "the tool has an output schema, and its result has no structured content"}}
	} else {
		v, err := jsonValue(res.StructuredContent)
		if err != nil {
			return err
		}
		if found, err = validate(output, v, "validation.result.schema", "structured result"); err != nil {
			return fmt.Errorf("the %s tool's output schema: %w", toolName, err)
		}
	}
	return report(sc, st, s, "the result of the "+toolName+" tool", found)
}

func (st *state) record(c *call) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.calls = append(st.calls, c)
}

// arguments are a call's arguments: the table's, each taking the type the
// tool's input schema gives it, or the file's.
func arguments(sc *core.Scenario, t *core.Table, file string, schema map[string]any) (any, error) {
	if file != "" {
		p, err := sc.Suite().ResolvePath(file)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		text, err := secrets.Resolve(sc, string(raw))
		if err != nil {
			return nil, err
		}
		v, err := schemadoc.Decode([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("the arguments %s are not JSON or YAML: %w", file, err)
		}
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("the arguments %s are an object, of the arguments' names and values", file)
		}
		return v, nil
	}
	if t == nil {
		return map[string]any{}, nil
	}
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	rows := make([]tablevalue.Row, len(pairs))
	for i, p := range pairs {
		rows[i] = tablevalue.Row{Path: p.Key, Null: p.Null}
		if !p.Null {
			if rows[i].Value, err = secrets.Resolve(sc, p.Value); err != nil {
				return nil, err
			}
		}
	}
	// A string argument is its text as written: 01067 stays 01067.
	args, err := tablevalue.Build(rows, func(path string) bool { return types(schemaAt(schema, path)) == "string" })
	if err != nil {
		return nil, err
	}
	return conform(schema, args), nil
}

// conform gives a value the type its schema declares: the text of a number
// for a string, a number for an integer or a number, a boolean for a
// boolean, and the properties of an object and the items of an array
// theirs.
func conform(schema map[string]any, v any) any {
	if schema == nil {
		return v
	}
	switch x := v.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		for k, pv := range x {
			if ps, ok := props[k].(map[string]any); ok {
				x[k] = conform(ps, pv)
			}
		}
		return x
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range x {
				x[i] = conform(items, e)
			}
		}
		return x
	}
	switch types(schema) {
	case "string":
		switch x := v.(type) {
		case json.Number:
			return x.String()
		case bool:
			return strconv.FormatBool(x)
		}
	case "integer", "number":
		if s, ok := v.(string); ok {
			if _, err := strconv.ParseFloat(s, 64); err == nil {
				return json.Number(s)
			}
		}
	case "boolean":
		if s, ok := v.(string); ok {
			if b, err := strconv.ParseBool(s); err == nil {
				return b
			}
		}
	}
	return v
}

// schemaAt is the schema of a path into the arguments (`address.postcode`,
// `lines[0].reference`), or nil.
func schemaAt(schema map[string]any, path string) map[string]any {
	s := schema
	for _, seg := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		name, rest, _ := strings.Cut(seg, "[")
		if name != "" {
			props, _ := s["properties"].(map[string]any)
			if s, _ = props[name].(map[string]any); s == nil {
				return nil
			}
		}
		for ; rest != ""; _, rest, _ = strings.Cut(rest, "[") {
			if s, _ = s["items"].(map[string]any); s == nil {
				return nil
			}
		}
	}
	return s
}

// types is the one type a schema declares, besides null, or "".
func types(schema map[string]any) string {
	switch t := schema["type"].(type) {
	case string:
		return t
	case []any:
		var out string
		for _, e := range t {
			if s, ok := e.(string); ok && s != "null" {
				if out != "" {
					return ""
				}
				out = s
			}
		}
		return out
	}
	return ""
}

var compileMu sync.Mutex

// validate checks a value against a JSON Schema (2020-12 unless it says
// otherwise, as MCP's schemas are).
func validate(schema map[string]any, v any, prefix, what string) ([]finding, error) {
	doc, err := jsonValue(schema)
	if err != nil {
		return nil, err
	}
	compileMu.Lock()
	defer compileMu.Unlock()
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mcp://tool/schema.json", doc); err != nil {
		return nil, err
	}
	s, err := c.Compile("mcp://tool/schema.json")
	if err != nil {
		return nil, err
	}
	return schemadoc.Validate(s, v, prefix, what), nil
}

// jsonValue is a value as the jsonschema package validates it: plain maps,
// and json.Number numbers.
func jsonValue(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(b))
}

// asMap is a schema as plain JSON, or nil.
func asMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

type levelsState struct {
	mu     sync.Mutex
	levels oaslevel.Levels
}

var scenarioLevels = core.NewStateKey(Name+".levels", func(*core.Scenario) *levelsState {
	return &levelsState{levels: oaslevel.Levels{}}
}, nil)

func setLevels(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	m := map[string]string{}
	for _, p := range pairs {
		m[p.Key] = p.Value
	}
	lv, err := oaslevel.ParseMap(m, levelKeys)
	if err != nil {
		return err
	}
	ls := scenarioLevels.Of(sc)
	ls.mu.Lock()
	ls.levels = ls.levels.Merge(lv)
	ls.mu.Unlock()
	return nil
}

// report fails on the findings at the ERROR level, and logs the others.
func report(sc *core.Scenario, st *settings, s *server, what string, found []finding) error {
	if len(found) == 0 {
		return nil
	}
	levels := st.levels
	if ls, ok := scenarioLevels.Peek(sc); ok {
		ls.mu.Lock()
		levels = levels.Merge(ls.levels)
		ls.mu.Unlock()
	}
	var errs []string
	for _, f := range found {
		switch lv := levels.Resolve(f.Key); lv {
		case oaslevel.Error:
			errs = append(errs, "- "+f.Key+": "+f.Message)
		case oaslevel.Warn, oaslevel.Info:
			sc.Log("MCP %s %s: %s", lv, f.Key, f.Message)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return secrets.Hide(sc, core.Failf("%s break the schema the %s mcp server gives it:\n%s\nTo relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with "+
		"\"Given the MCP validation levels are:\" or packs.mcp.levels in axx.yaml.", what, s.name, strings.Join(errs, "\n")))
}

// lastCall is the scenario's latest call of the tool, on the server when it
// is named.
func lastCall(sc *core.Scenario, tool, serverName string) (*call, error) {
	st := scenarioState.Of(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	var found *call
	servers := map[string]bool{}
	for _, c := range st.calls {
		if c.tool == tool && (serverName == "" || c.server.name == serverName) {
			found = c
			servers[c.server.name] = true
		}
	}
	switch {
	case found == nil && serverName != "":
		return nil, fmt.Errorf("the %s tool was not called on the %s mcp server in this scenario; call it first with \"the %s tool is called on the %s mcp server\"", tool, serverName, tool, serverName)
	case found == nil:
		return nil, fmt.Errorf("the %s tool was not called in this scenario; call it first with \"the %s tool is called on the {word} mcp server\"", tool, tool)
	case len(servers) > 1:
		return nil, fmt.Errorf("the %s tool was called on more than one mcp server: name the one to check, \"the %s tool's result on the {word} mcp server ...\"", tool, tool)
	}
	return found, nil
}

// answered is the call's result, when the server did not refuse it.
func (c *call) answered() (*sdk.CallToolResult, error) {
	if c.err != nil {
		return nil, core.Failf("the %s mcp server refused the call of the %s tool with the error %d: %v", c.server.name, c.tool, c.code, message(c.err))
	}
	return c.result, nil
}

// message is a JSON-RPC error's message.
func message(err error) string {
	var wire *jsonrpc.Error
	if errors.As(err, &wire) {
		return wire.Message
	}
	return err.Error()
}

// text is the text of a result's content, and its structured content as
// JSON.
func text(r *sdk.CallToolResult) string {
	parts := contentText(r.Content)
	if r.StructuredContent != nil {
		b, _ := json.Marshal(r.StructuredContent)
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "\n")
}

func contentText(content []sdk.Content) []string {
	var parts []string
	for _, c := range content {
		switch x := c.(type) {
		case *sdk.TextContent:
			parts = append(parts, x.Text)
		case *sdk.EmbeddedResource:
			if x.Resource != nil {
				parts = append(parts, x.Resource.Text)
			}
		case *sdk.ResourceLink:
			parts = append(parts, x.URI)
		}
	}
	return parts
}

// structured is a result's structured content as JSON, or the JSON of its
// text, for a tool without an output schema that answers JSON in text.
func structured(c *call, r *sdk.CallToolResult) (string, error) {
	if r.StructuredContent != nil {
		b, err := json.Marshal(r.StructuredContent)
		return string(b), err
	}
	t := strings.TrimSpace(strings.Join(contentText(r.Content), "\n"))
	if json.Valid([]byte(t)) {
		return t, nil
	}
	return "", core.Failf("the %s tool's result has no structured content, and its text is not JSON: %s", c.tool, t)
}

func readResource(sc *core.Scenario, uri, serverName string) error {
	s, err := get(sc, serverName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	res, err := s.session.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
	if err != nil {
		return secrets.Hide(sc, fmt.Errorf("cannot read the resource %s from the %s mcp server: %w", uri, s.name, err))
	}
	st := scenarioState.Of(sc)
	st.mu.Lock()
	st.reads = append(st.reads, &reading{server: s, uri: uri, result: res})
	st.mu.Unlock()
	return nil
}

func lastReading(sc *core.Scenario, uri string) (*reading, error) {
	st := scenarioState.Of(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := len(st.reads) - 1; i >= 0; i-- {
		if st.reads[i].uri == uri {
			return st.reads[i], nil
		}
	}
	return nil, fmt.Errorf("the resource %s was not read in this scenario; read it first with \"the %s resource is read from the {word} mcp server\"", uri, uri)
}

func (r *reading) text() string {
	var parts []string
	for _, c := range r.result.Contents {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n")
}

func requestPrompt(sc *core.Scenario, name, serverName string, t *core.Table) error {
	s, err := get(sc, serverName)
	if err != nil {
		return err
	}
	args := map[string]string{}
	if t != nil {
		pairs, err := t.Pairs()
		if err != nil {
			return err
		}
		for _, p := range pairs {
			v, err := secrets.Resolve(sc, p.Value)
			if err != nil {
				return err
			}
			args[p.Key] = v
		}
	}
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	res, err := s.session.GetPrompt(ctx, &sdk.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		return secrets.Hide(sc, fmt.Errorf("cannot get the %s prompt from the %s mcp server: %w", name, s.name, err))
	}
	st := scenarioState.Of(sc)
	st.mu.Lock()
	st.prompts = append(st.prompts, &prompting{server: s, name: name, result: res})
	st.mu.Unlock()
	return nil
}

func lastPrompt(sc *core.Scenario, name string) (*prompting, error) {
	st := scenarioState.Of(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := len(st.prompts) - 1; i >= 0; i-- {
		if st.prompts[i].name == name {
			return st.prompts[i], nil
		}
	}
	return nil, fmt.Errorf("the %s prompt was not requested in this scenario; request it first with \"the %s prompt is requested from the {word} mcp server\"", name, name)
}

func (p *prompting) text() string {
	var parts []string
	for _, m := range p.result.Messages {
		parts = append(parts, contentText([]sdk.Content{m.Content})...)
	}
	return strings.Join(parts, "\n")
}
