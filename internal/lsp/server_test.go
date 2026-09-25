package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

// client drives a server over pipes, as an editor does.
type client struct {
	t     *testing.T
	in    io.WriteCloser
	out   *bufio.Reader
	id    int
	done  chan error
	notes []notificationIn
}

type notificationIn struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type messageIn struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func startServer(t *testing.T) *client {
	t.Helper()
	return startServerWith(t, map[string]any{})
}

// startServerWith starts a server for an editor with the given client
// capabilities.
func startServerWith(t *testing.T, capabilities map[string]any) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, out: bufio.NewReader(outR), done: make(chan error, 1)}
	go func() {
		err := Serve(context.Background(), inR, outW, Options{Version: "test"})
		_ = outW.Close()
		c.done <- err
	}()
	t.Cleanup(func() { _ = inW.Close() })
	c.request("initialize", map[string]any{"processId": nil, "capabilities": capabilities})
	c.notify("initialized", map[string]any{})
	return c
}

func (c *client) send(v any) {
	c.t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) next() messageIn {
	c.t.Helper()
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b, err := readMessage(c.out)
		ch <- res{b, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			c.t.Fatal(r.err)
		}
		var m messageIn
		if err := json.Unmarshal(r.b, &m); err != nil {
			c.t.Fatal(err)
		}
		return m
	case <-time.After(20 * time.Second):
		c.t.Fatal("no message from the server")
	}
	return messageIn{}
}

func (c *client) request(method string, params any) json.RawMessage {
	c.t.Helper()
	c.id++
	c.send(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	for {
		m := c.next()
		if m.Method != "" {
			c.notes = append(c.notes, notificationIn{m.Method, m.Params})
			continue
		}
		if string(m.ID) != fmt.Sprint(c.id) {
			c.t.Fatalf("response id %s, want %d", m.ID, c.id)
		}
		if m.Error != nil {
			c.t.Fatalf("%s: error %d: %s", method, m.Error.Code, m.Error.Message)
		}
		return m.Result
	}
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// open opens a feature file and returns the diagnostics published for it.
func (c *client) open(uri, text string) []diagnostic {
	c.t.Helper()
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "feature", "version": 1, "text": text}})
	return c.diagnostics(uri)
}

// diagnostics waits for the next diagnostics published for uri.
func (c *client) diagnostics(uri string) []diagnostic {
	c.t.Helper()
	for {
		m := c.next()
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p publishDiagnosticsParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			c.t.Fatal(err)
		}
		if p.URI == uri {
			return p.Diagnostics
		}
	}
}

// writeProject creates an axx project with files, and returns its directory
// and the URI of a feature file in it.
func writeProject(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, fileLocation(filepath.Join(dir, "features", "a.feature"), 0).URI
}

// packsFile lists the packs the test projects use.
const packsFile = "packs: [rest, mock, sql, mongo, kafka, logs]\n"

// project creates an axx project and returns the URI of a feature file in it.
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "axx-packs.yaml"), []byte(packsFile), 0o644); err != nil {
		t.Fatal(err)
	}
	return fileLocation(filepath.Join(dir, "features", "a.feature"), 0).URI
}

const parcelsFeature = `Feature: Parcels

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |

  Scenario: listing
    Given a GET request to /api/parcels
    When the request is executed
    Then the response status code is 200
    And the response status code is totally 200

  Scenario: a table is missing
    Given the mocked addresses service with the following properties:
`

func TestDiagnostics(t *testing.T) {
	c := startServer(t)
	uri := newProject(t)
	diags := c.open(uri, parcelsFeature)
	if len(diags) != 2 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	undef, arg := diags[0], diags[1]
	if undef.Code != "undefined" || undef.Range.Start.Line != 10 || undef.Range.Start.Character != 8 ||
		!strings.Contains(undef.Message, "Did you mean:\n  the response status code is {int}") {
		t.Errorf("undefined step: %+v", undef)
	}
	if arg.Code != "argument" || arg.Range.Start.Line != 13 || !strings.Contains(arg.Message, "requires a data table") {
		t.Errorf("argument problem: %+v", arg)
	}

	// A syntax error is reported at its line.
	diags = c.open(uri, "Feature: x\n  Scenario: y\n    Given a GET request to /x\n  | not | a table |\n  Oops\n")
	if len(diags) == 0 || diags[0].Code != "syntax" {
		t.Fatalf("syntax diagnostics: %+v", diags)
	}
}

func TestHover(t *testing.T) {
	c := startServer(t)
	uri := newProject(t)
	c.open(uri, parcelsFeature)
	raw := c.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 9, "character": 20}})
	var h hover
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"`rest.response.status`", "| `{int}` | `200` |", "status code"} {
		if !strings.Contains(h.Contents.Value, want) {
			t.Errorf("hover lacks %q:\n%s", want, h.Contents.Value)
		}
	}
	if h.Range == nil || h.Range.Start != (position{9, 9}) {
		t.Errorf("hover range %+v", h.Range)
	}
	// No hover away from steps.
	if raw := c.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 3}}); string(raw) != "null" {
		t.Errorf("hover on the feature line: %s", raw)
	}
}

func TestCompletion(t *testing.T) {
	c := startServer(t)
	uri := newProject(t)
	c.open(uri, "Feature: x\n  Scenario: y\n    Then the response st\n")
	raw := c.request("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 24}})
	var list completionList
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	var found *completionItem
	for i := range list.Items {
		if list.Items[i].Label == "the response status code is {int}" {
			found = &list.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("no status code completion among %d items", len(list.Items))
	}
	best := list.Items[0]
	for _, it := range list.Items {
		if it.SortText < best.SortText {
			best = it
		}
	}
	if !strings.HasPrefix(best.Label, "the response status") {
		t.Errorf("first completion %q", best.Label)
	}
	if found.TextEdit.NewText != "the response status code is ${1:int}" || found.TextEdit.Range.Start != (position{2, 9}) || found.TextEdit.Range.End != (position{2, 24}) {
		t.Errorf("edit %+v", found.TextEdit)
	}
}

func TestSemanticTokens(t *testing.T) {
	c := startServer(t)
	uri := newProject(t)
	text := "Feature: x\n  Scenario: y\n    Given a GET request to /api/😀\n    Then the response status code is 201\n\n  Scenario Outline: z\n    Then the response status code is <code>\n    Examples:\n      | code |\n      | 200  |\n"
	if diags := c.open(uri, text); len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	raw := c.request("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}})
	var toks semanticTokens
	if err := json.Unmarshal(raw, &toks); err != nil {
		t.Fatal(err)
	}
	// GET and /api/😀 (7 UTF-16 units) on line 2, 201 on line 3, <code> on line 6.
	want := []uint32{
		2, 12, 3, tokenParameter, 0,
		0, 15, 7, tokenParameter, 0,
		1, 37, 3, tokenParameter, 0,
		3, 37, 6, tokenVariable, 0,
	}
	if fmt.Sprint(toks.Data) != fmt.Sprint(want) {
		t.Errorf("tokens %v\nwant   %v", toks.Data, want)
	}
}

func TestShutdown(t *testing.T) {
	c := startServer(t)
	if raw := c.request("shutdown", nil); string(raw) != "null" {
		t.Errorf("shutdown result %s", raw)
	}
	c.notify("exit", nil)
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not exit")
	}
}

func TestContinuesAndSnippet(t *testing.T) {
	for _, c := range []struct {
		expr, typed string
		want        bool
	}{
		{"the response status code is {int}", "", true},
		{"the response status code is {int}", "the resp", true},
		{"the response status code is {int}", "the response status code is 2", true},
		{"the response status code is {int}", "the request", false},
		{"the mocked request named {word} was received exactly {int} time(s)", "the mocked request named x was received exactly 2 times", true},
		{"a(n) {word} kafka event", "an x kafka", true},
		{"the response payload property {word} is {string}", "the response payload property name is 'two words' x", false},
	} {
		if got, _ := continues(c.expr, c.typed); got != c.want {
			t.Errorf("continues(%q, %q) = %v", c.expr, c.typed, got)
		}
	}
	if got := snippet("the mocked request named {word} was received exactly {int} time(s)"); got != "the mocked request named ${1:word} was received exactly ${2:int} times" {
		t.Errorf("snippet %q", got)
	}
	if got := snippet("a(n) {word} kafka event"); got != "a ${1:word} kafka event" {
		t.Errorf("snippet %q", got)
	}
}

func TestFindProject(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "acceptance")
	for _, d := range []string{sub, filepath.Join(root, "node_modules", "x")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "x", "axx.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindProject(root); got != root {
		t.Errorf("without a project: %s", got)
	}
	if err := os.WriteFile(filepath.Join(sub, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindProject(root); got != sub {
		t.Errorf("below: %s, want %s", got, sub)
	}
	if got := FindProject(sub); got != sub {
		t.Errorf("at: %s", got)
	}
}

func TestDefinition(t *testing.T) {
	c := startServer(t)
	uri := newProject(t)
	c.open(uri, parcelsFeature)
	raw := c.request("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 9, "character": 20}})
	var locs []location
	if err := json.Unmarshal(raw, &locs); err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 {
		t.Fatalf("locations %s", raw)
	}
	path := uriToPath(locs[0].URI)
	if filepath.Base(filepath.Dir(path)) != "rest" {
		t.Fatalf("definition in %s, want the rest pack", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if line := splitLines(string(data))[locs[0].Range.Start.Line]; !strings.Contains(line, `"rest.response.status"`) {
		t.Errorf("definition line %q", line)
	}
	// No definition away from steps.
	if raw := c.request("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 1}}); string(raw) != "[]" {
		t.Errorf("definition on the feature line: %s", raw)
	}
}

// A step whose code is not on the machine is shown in a reference page.
func TestDefinitionReferencePage(t *testing.T) {
	reg := match.NewRegistry()
	manifest := core.Manifest{Name: "t", Doc: "Test steps.", Steps: []core.StepDef{{ID: "t.hello", Keyword: "Given", Expr: "hello {word}", Doc: "Says hello."}}}
	if err := reg.AddPack("t", manifest); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	s := &server{opts: Options{Version: "test", CacheDir: cache}, pages: map[string][]string{}}
	pr := &project{dir: "/p", reg: reg, manifests: map[string]core.Manifest{"t": manifest}}
	d := newDocument("file:///p/a.feature", 1, "Feature: f\n  Scenario: s\n    Given hello world\n")
	locs := s.definitionAt(d, pr, position{Line: 2, Character: 12})
	if len(locs) != 1 {
		t.Fatalf("locations %+v", locs)
	}
	path := uriToPath(locs[0].URI)
	if !strings.HasPrefix(path, cache) || filepath.Base(path) != "t.md" {
		t.Fatalf("reference page %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if line := splitLines(string(data))[locs[0].Range.Start.Line]; line != "## `t.hello`" {
		t.Errorf("reference line %q", line)
	}
}

// A completion adds to what is written; it never replaces it.
func TestCompletionKeepsWhatIsWritten(t *testing.T) {
	for _, c := range []struct{ expr, typed, after, want string }{
		{"the response status code is {int}", "the response st", "", "atus code is ${1:int}"},
		{"the response status code is {int} on {service}", "the response status code is 200 ", "", "on ${1:service}"},
		{"the response status code is {int} on {service}", "the response status code is 200", "", " on ${1:service}"},
		{"the response status code is {int}", "the response status code is 200", "", ""},
		{"the mocked request named {word} was received exactly {int} time(s)", "the mocked request named x was received exactly 2 tim", "", "es"},
		{"the response status code is {int}", "The Resp", "", "onse status code is ${1:int}"},
		{"the response status code is {int}", "the response st", " code is 200", "atus"},
		{"the response status code is {int}", "the response st", "atus code is 200", ""},
		{"the request payload property {word} is {string}", "the request payload property $.x is ", "", "${1:string}"},
	} {
		if got := completion(c.expr, c.typed, c.after); got != c.want {
			t.Errorf("completion(%q, %q, %q) = %q, want %q", c.expr, c.typed, c.after, got, c.want)
		}
	}

	c := startServer(t)
	uri := newProject(t)
	c.open(uri, "Feature: x\n  Scenario: y\n    Then the response status code is 200 \n")
	raw := c.request("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 41}})
	var list completionList
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, it := range list.Items {
		texts = append(texts, it.TextEdit.NewText)
		if it.TextEdit.Range.Start != (position{2, 9}) || it.TextEdit.Range.End != (position{2, 41}) {
			t.Errorf("edit range %+v", it.TextEdit.Range)
		}
	}
	if len(texts) == 0 || !contains(texts, "the response status code is 200 on ${1:service}") {
		t.Errorf("completions %q", texts)
	}
}

// Binaries built with -trimpath name files by module path; the server finds
// them in a replaced module's directory or in the module cache.
func TestResolveModuleFile(t *testing.T) {
	src := t.TempDir() // a checkout that a module is replaced with
	cache := t.TempDir()
	write := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(src, "packs", "rest", "responses.go"))
	write(filepath.Join(cache, "github.com", "!big", "lib@v1.2.0", "x.go"))
	mods := []*debug.Module{
		{Path: "axx.local/build", Version: "(devel)"},
		{Path: "github.com/nimbusxr/axx", Version: "v0.0.0", Replace: &debug.Module{Path: src}},
		{Path: "github.com/Big/lib", Version: "v1.2.0"},
	}
	if got := resolveModuleFile("github.com/nimbusxr/axx/packs/rest/responses.go", mods, cache); got != filepath.Join(src, "packs", "rest", "responses.go") {
		t.Errorf("replaced module: %q", got)
	}
	// Dependencies are named with their version.
	if got := resolveModuleFile("github.com/nimbusxr/axx@v0.0.0/packs/rest/responses.go", mods, cache); got != filepath.Join(src, "packs", "rest", "responses.go") {
		t.Errorf("replaced dependency: %q", got)
	}
	if got := resolveModuleFile("github.com/Big/lib@v1.2.0/x.go", mods, cache); got != filepath.Join(cache, "github.com", "!big", "lib@v1.2.0", "x.go") {
		t.Errorf("module cache: %q", got)
	}
	if got := resolveModuleFile("axx.local/build/main.go", mods, cache); got != "" {
		t.Errorf("devel main module: %q", got)
	}
}

func TestDocumentLinks(t *testing.T) {
	c := startServer(t)
	dir, uri := writeProject(t, map[string]string{
		"axx.yaml":                "version: 1\nresources: [data]\n",
		"axx-packs.yaml":          packsFile,
		"data/seeds/parcels.yaml": "parcels: []\n",
		"schemas/depot-scan.avsc": "{}\n",
		"openapi/parcels.yaml":    "openapi: 3.1.0\n",
		"certs/ca.pem":            "",
		".axx/logs/apps.log":      "",
		"data/seeds/manifests/x":  "", // a directory is no link
	})
	text := `Feature: Links

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400 |
      | openapi | openapi/parcels.yaml  |
    And the console log with the following properties:
      | url | file://.axx/logs/apps.log |

  Scenario: seeded
    Given a seeds/parcels.yaml db seed
    And a seeds/missing.yaml db seed
    And a seeds/manifests db seed
    And a depot-scans kafka topic client with the following properties:
      | producer.ssl.truststore.location               | certs/ca.pem |
      | consumer.schema.registry.ssl.keystore.location | certs/ca.pem |
      | producer.client.id                             | certs/ca.pem |
    When the 1st ordered depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then the request body is:
      """
      | openapi/parcels.yaml |
      """

  Scenario Outline: outline
    Given a <seed> db seed
    And the parcels service with the following properties:
      | url     | http://localhost:8400 |
      | openapi | <spec>                |

    Examples:
      | note         | seed               | spec                 | other                | escaped               |
      | ünïcode 😀   | seeds/parcels.yaml | openapi/parcels.yaml | openapi/parcels.yaml | openapi\|parcels.yaml |
`
	c.open(uri, text)
	raw := c.request("textDocument/documentLink", map[string]any{"textDocument": map[string]any{"uri": uri}})
	var links []documentLink
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(text, "\n")
	type want struct {
		line  int
		value string // its first occurrence on the line
		file  string
	}
	wants := []want{
		{5, "openapi/parcels.yaml", "openapi/parcels.yaml"},
		{7, "file://.axx/logs/apps.log", ".axx/logs/apps.log"},
		{10, "seeds/parcels.yaml", "data/seeds/parcels.yaml"},
		{14, "certs/ca.pem", "certs/ca.pem"},
		{15, "certs/ca.pem", "certs/ca.pem"},
		{17, "schemas/depot-scan.avsc", "schemas/depot-scan.avsc"},
		{31, "seeds/parcels.yaml", "data/seeds/parcels.yaml"},
		{31, "openapi/parcels.yaml", "openapi/parcels.yaml"},
	}
	if len(links) != len(wants) {
		t.Fatalf("links: %+v", links)
	}
	for i, w := range wants {
		l := links[i]
		from := strings.Index(lines[w.line], w.value)
		start := position{w.line, utf16Len(lines[w.line][:from])}
		end := position{w.line, start.Character + utf16Len(w.value)}
		if l.Range.Start != start || l.Range.End != end {
			t.Errorf("link %d range %+v, want %q at %+v", i, l.Range, w.value, start)
		}
		if got := uriToPath(l.Target); got != filepath.Join(dir, filepath.FromSlash(w.file)) {
			t.Errorf("link %d target %s, want %s", i, got, w.file)
		}
		if l.Tooltip != w.file {
			t.Errorf("link %d tooltip %q", i, l.Tooltip)
		}
	}

	// Going to the definition of a file a step names opens the file; the rest
	// of the step goes to the step's definition.
	definition := func(line, character int) []location {
		raw := c.request("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": character}})
		var locs []location
		if err := json.Unmarshal(raw, &locs); err != nil {
			t.Fatal(err)
		}
		return locs
	}
	if locs := definition(10, 20); len(locs) != 1 || uriToPath(locs[0].URI) != filepath.Join(dir, "data", "seeds", "parcels.yaml") || locs[0].Range.Start.Line != 0 {
		t.Errorf("definition on the seed: %+v", locs)
	}
	if locs := definition(10, 32); len(locs) != 1 || !strings.Contains(locs[0].URI, "sql") {
		t.Errorf("definition on the seed step: %+v", locs)
	}
	if locs := definition(7, 20); len(locs) != 1 || !strings.HasSuffix(uriToPath(locs[0].URI), filepath.Join(".axx", "logs", "apps.log")) {
		t.Errorf("definition on the log url: %+v", locs)
	}
	if locs := definition(31, links[6].Range.Start.Character+2); len(locs) != 1 || uriToPath(locs[0].URI) != filepath.Join(dir, "data", "seeds", "parcels.yaml") {
		t.Errorf("definition on an Examples cell: %+v", locs)
	}
}

func TestMissingFiles(t *testing.T) {
	c := startServerWith(t, map[string]any{"workspace": map[string]any{"didChangeWatchedFiles": map[string]any{"dynamicRegistration": true}}})
	// Once initialized, the server asks to hear about changed files.
	if m := c.next(); m.Method != "client/registerCapability" || !strings.Contains(string(m.Params), "workspace/didChangeWatchedFiles") {
		t.Fatalf("no file watch registered: %s %s", m.Method, m.Params)
	}
	dir, uri := writeProject(t, map[string]string{
		"axx.yaml":       "version: 1\nresources: [data]\n",
		"axx-packs.yaml": packsFile,
		"axx-fixtures.manifest.yaml": "files:\n- path: seeds/generated.csv\n  sha256: " + strings.Repeat("0", 64) +
			"\n  factory: seeds/manifests.factory.yaml\n  fixture: generated\n",
		"data/seeds/manifests/x": "", // a CSV dataset is a directory
	})
	text := `Feature: Missing files

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400 |
      | openapi | openapi/missing.yaml  |
    And the console log with the following properties:
      | url | file://.axx/logs/later.log |

  Scenario: seeded
    Given a seeds/missing.yaml db seed
    And a seeds/manifests db seed
    And a seeds/generated.csv db seed
    And a ${env:SEED_FILE} db seed
    And the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |

  Scenario Outline: outline
    Given a <seed> db seed

    Examples:
      | seed              |
      | seeds/absent.yaml |
`
	var missing []diagnostic
	for _, d := range c.open(uri, text) {
		if d.Code == "missing-file" {
			missing = append(missing, d)
		}
	}
	lines := strings.Split(text, "\n")
	wants := []struct {
		line          int
		value, detail string
	}{
		{5, "openapi/missing.yaml", "No file openapi/missing.yaml. Looked for data/openapi/missing.yaml, openapi/missing.yaml."},
		{10, "seeds/missing.yaml", "No file seeds/missing.yaml. Looked for data/seeds/missing.yaml, seeds/missing.yaml."},
		{12, "seeds/generated.csv", "seeds/generated.csv does not exist yet: `axx fixtures generate` writes it."},
		{23, "seeds/absent.yaml", "No file seeds/absent.yaml. Looked for data/seeds/absent.yaml, seeds/absent.yaml."},
	}
	if len(missing) != len(wants) {
		t.Fatalf("missing-file warnings: %+v", missing)
	}
	for i, w := range wants {
		d := missing[i]
		from := strings.Index(lines[w.line], w.value)
		if d.Range != span(lines, w.line, from, from+len(w.value)) || d.Severity != severityWarning || d.Message != w.detail {
			t.Errorf("warning %d: %+v, want %q at line %d: %s", i, d, w.value, w.line, w.detail)
		}
	}

	// A file that appears takes its warning away.
	seed := filepath.Join(dir, "seeds", "missing.yaml")
	if err := os.MkdirAll(filepath.Dir(seed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	seedURI := fileLocation(seed, 0).URI
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": seedURI, "type": 1}}})
	for _, d := range c.diagnostics(uri) {
		if d.Code == "missing-file" && d.Range.Start.Line == 10 {
			t.Errorf("warning left after the file appeared: %+v", d)
		}
	}
}

func TestPathCompletion(t *testing.T) {
	c := startServer(t)
	_, uri := writeProject(t, map[string]string{
		"axx.yaml":                "version: 1\nresources: [data]\n",
		"axx-packs.yaml":          packsFile,
		"data/seeds/parcels.yaml": "",
		"data/seeds/manifests/x":  "",
		"seeds/local.yaml":        "",
		"openapi/parcels.yaml":    "",
		".axx/logs/apps.log":      "",
	})
	text := `Feature: Paths

  Scenario: typing
    Given a seeds/pa
    And a 
    And the parcels service with the following properties:
      | url     | http://lo
      | openapi | openapi/
    And the console log with the following properties:
      | url | file://.a |
    When a GET request to /api/

  Scenario Outline: outline
    Given a <seed> db seed

    Examples:
      | seed    |
      | seeds/m |
`
	c.open(uri, text)
	lines := strings.Split(text, "\n")
	complete := func(line int, after, trigger string) completionList {
		t.Helper()
		ctx := map[string]any{"triggerKind": 1}
		if trigger != "" {
			ctx = map[string]any{"triggerKind": 2, "triggerCharacter": trigger}
		}
		at := strings.Index(lines[line], after) + len(after)
		raw := c.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri}, "position": position{line, utf16Len(lines[line][:at])}, "context": ctx,
		})
		var list completionList
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		return list
	}
	find := func(list completionList, label string) *completionItem {
		for i := range list.Items {
			if list.Items[i].Label == label {
				return &list.Items[i]
			}
		}
		return nil
	}
	paths := func(list completionList) []string {
		var out []string
		for _, it := range list.Items {
			if it.Kind == completionKindFile || it.Kind == completionKindFolder {
				out = append(out, it.Label)
			}
		}
		return out
	}

	// A {filepath} parameter: the edit covers the step text, like a step
	// completion.
	list := complete(3, "seeds/pa", "")
	if got := paths(list); len(got) != 1 || got[0] != "parcels.yaml" {
		t.Fatalf("paths for seeds/pa: %v", got)
	}
	it := find(list, "parcels.yaml")
	if it.TextEdit.NewText != "a seeds/parcels.yaml" || it.FilterText != it.TextEdit.NewText ||
		it.TextEdit.Range != span(lines, 3, 10, len(lines[3])) || it.Detail != "data/seeds/parcels.yaml" {
		t.Errorf("item %+v", it)
	}
	// After a slash, the directory's entries from every resource root, and
	// no steps.
	list = complete(3, "seeds/", "/")
	if got := strings.Join(paths(list), " "); got != "manifests/ parcels.yaml local.yaml" || len(list.Items) != 3 {
		t.Errorf("entries of seeds/: %v (%d items)", got, len(list.Items))
	}
	// Where a step can take a file or other words, both.
	list = complete(4, "a ", "")
	if find(list, "seeds/") == nil || find(list, "openapi/") == nil || find(list, ".axx/") != nil || find(list, "a {filepath} db seed") == nil {
		t.Errorf("after a: %v", paths(list))
	}
	// A file property, in a row being typed; a property that is not one.
	if list := complete(7, "openapi/", "/"); find(list, "parcels.yaml") == nil || find(list, "parcels.yaml").TextEdit.NewText != "openapi/parcels.yaml" {
		t.Errorf("openapi property: %+v", list.Items)
	}
	if list := complete(6, "http://lo", ""); len(list.Items) != 0 {
		t.Errorf("url property: %+v", list.Items)
	}
	// A log url, as a file: URL.
	if list := complete(9, "file://.a", ""); find(list, ".axx/") == nil || find(list, ".axx/").TextEdit.NewText != "file://.axx/" {
		t.Errorf("log url: %+v", list.Items)
	}
	// An Examples cell that fills a {filepath}.
	if list := complete(17, "seeds/m", ""); find(list, "manifests/") == nil || find(list, "manifests/").TextEdit.NewText != "seeds/manifests/" {
		t.Errorf("Examples cell: %+v", list.Items)
	}
	// A slash elsewhere offers nothing.
	if list := complete(10, "/api/", "/"); len(list.Items) != 0 {
		t.Errorf("slash in a request path: %d items", len(list.Items))
	}
}
