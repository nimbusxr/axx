// Package mcp is the mcp pack: calls to MCP servers (Model Context Protocol)
// as an AI assistant makes them, over stdio or streamable HTTP: their tools,
// with the tools' schemas as the contract, their resources and their
// prompts.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/shellwords"
)

// Name is the pack's name.
const Name = "mcp"

const since = "0.1.5"

const defaultTimeout = 30 * time.Second

// Pack returns the mcp pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps(), ConfigSchema: []byte(configSchema)}
}

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "levels": {
      "type": "object",
      "description": "Validation key -> ERROR (or FAIL), WARN, INFO or IGNORE, for every scenario.",
      "additionalProperties": {"type": "string", "enum": ["ERROR", "FAIL", "WARN", "INFO", "IGNORE"]}
    }
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	// Levels maps validation keys to ERROR (or FAIL), WARN, INFO or IGNORE.
	Levels map[string]string `json:"levels,omitempty"`
}

type settings struct {
	levels oaslevel.Levels
}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		levels, err := oaslevel.ParseMap(c.Levels, levelKeys)
		if err != nil {
			return nil, fmt.Errorf("packs.mcp.levels: %w", err)
		}
		return &settings{levels: levels}, nil
	})
}

// server is an MCP server a scenario registered, and its session, which
// belongs to the scenario.
type server struct {
	name     string
	url      string   // over streamable HTTP
	argv     []string // or over stdio
	dir      string
	env      []string
	headers  http.Header
	timeout  time.Duration
	version  string // the protocol version asked for, or "" for the latest
	session  *sdk.ClientSession
	cmd      *exec.Cmd
	stdin    io.Closer
	group    *proc.Group // the command's processes
	stderr   *strings.Builder
	toolList []*sdk.Tool // listed once
}

// state is a scenario's MCP servers and what it asked them.
type state struct {
	mu      sync.Mutex
	servers *core.Services[*server]
	calls   []*call      // every tool call, the latest last
	reads   []*reading   // every resource read
	prompts []*prompting // every prompt requested
	folder  string       // the scenario's own folder, for commands without a dir
}

var scenarioState = core.NewStateKey(Name, func(sc *core.Scenario) *state {
	st := &state{servers: core.NewServices[*server]("MCP server",
		`No MCP server is registered in this scenario; register one with "the {word} mcp server with the following properties:"`).RegisteredBy("the {word} mcp server with the following properties:")}
	sc.Describe(Name, func() any { return describe(sc, st) })
	return st
}, closeState)

// closeState ends the scenario's sessions, and the processes of its
// commands, and removes its folder when it passed.
func closeState(sc *core.Scenario, st *state) error {
	var errs []error
	for _, s := range st.servers.All() {
		errs = append(errs, s.close())
	}
	if st.folder != "" {
		if sc.Status() == "failed" {
			sc.Log("the MCP servers' folder is kept: %s", st.folder)
		} else {
			errs = append(errs, os.RemoveAll(st.folder))
		}
	}
	return errors.Join(errs...)
}

func (s *server) close() error {
	var err error
	if s.session != nil {
		err = s.session.Close()
	}
	s.stop()
	return err
}

// stop ends the command as MCP says a client does: it closes the server's
// input, waits for it to exit, and then stops it, with whatever it started.
func (s *server) stop() {
	if s.cmd == nil {
		return
	}
	_ = s.stdin.Close()
	exited := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
	}
	if s.group != nil {
		// What the command left running is stopped with it, and gone before
		// the scenario's folder, which may be its working folder, is removed.
		_ = s.group.KillAndWait(2 * time.Second)
		s.group.Release()
	}
	<-exited
	s.cmd = nil
}

// start runs the server's command, in a group of its own from the start, so
// that whatever it starts is stopped with it (on Windows, a process joins the
// command's Job Object only if it starts after the command joined it).
func (s *server) start() (sdk.Transport, error) {
	// The session outlives this step: the command runs until the scenario
	// ends, so it is not tied to the step's context.
	cmd := exec.CommandContext(context.Background(), s.argv[0], s.argv[1:]...) //nolint:gosec // the scenario's own command
	cmd.Dir, cmd.Env = s.dir, s.env
	s.stderr = &strings.Builder{}
	cmd.Stderr = &capped{b: s.stderr, limit: 64 << 10}
	// A process the command started that holds its error output does not
	// keep the scenario waiting.
	cmd.WaitDelay = time.Second
	proc.Setup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot run the %s mcp server (%s): %w", s.name, strings.Join(s.argv, " "), err)
	}
	s.cmd, s.stdin = cmd, stdin
	if g, err := proc.NewGroup(cmd); err == nil {
		s.group = g
	}
	return &sdk.IOTransport{Reader: stdout, Writer: stdin}, nil
}

func (st *state) ownFolder() (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.folder == "" {
		dir, err := os.MkdirTemp("", "axx-mcp-")
		if err != nil {
			return "", err
		}
		st.folder = dir
	}
	return st.folder, nil
}

func register(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	if a.Table == nil {
		return errors.New(`the mcp server property "command" or "url" is required`)
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	s := &server{name: name, headers: http.Header{}, timeout: defaultTimeout}
	env := map[string]string{}
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return err
		}
		switch {
		case p.Key == "command":
			s.argv = shellwords.Split(v)
		case p.Key == "url":
			s.url = strings.TrimSpace(v)
		case p.Key == "dir":
			d := filepath.FromSlash(strings.TrimSpace(v))
			if !filepath.IsAbs(d) {
				d = filepath.Join(sc.Suite().ProjectDir(), d)
			}
			if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
				return fmt.Errorf("the %s mcp server's dir %s is not a folder", name, d)
			}
			s.dir = filepath.Clean(d)
		case strings.HasPrefix(p.Key, "env."):
			n := strings.TrimPrefix(p.Key, "env.")
			if n == "" || strings.ContainsAny(n, "= ") {
				return fmt.Errorf("the %s mcp server's property %q does not name an environment variable", name, p.Key)
			}
			env[n] = v
		case strings.HasPrefix(p.Key, "header."):
			s.headers.Add(strings.TrimPrefix(p.Key, "header."), v)
		case p.Key == "timeout":
			d, err := time.ParseDuration(strings.TrimSpace(v))
			if err != nil || d <= 0 {
				return fmt.Errorf("the %s mcp server's timeout %q is not a duration, like 30s or 2m", name, p.Value)
			}
			s.timeout = d
		case p.Key == "protocol version":
			s.version = strings.TrimSpace(v)
		default:
			return fmt.Errorf("unknown mcp server property %q (supported: command, url, dir, env.<NAME>, header.<name>, timeout, protocol version)", p.Key)
		}
	}
	switch {
	case len(s.argv) == 0 && s.url == "":
		return fmt.Errorf(`the %s mcp server needs a "command" (stdio) or a "url" (streamable HTTP)`, name)
	case len(s.argv) > 0 && s.url != "":
		return fmt.Errorf(`the %s mcp server has a "command" or a "url", not both`, name)
	case s.url != "" && !strings.HasPrefix(s.url, "http://") && !strings.HasPrefix(s.url, "https://"):
		return fmt.Errorf("the %s mcp server's url is http:// or https://, not %q", name, s.url)
	case s.url != "" && (len(env) > 0 || s.dir != ""):
		return fmt.Errorf("the %s mcp server's env.<NAME> and dir are for a command, not a url", name)
	case len(s.argv) > 0 && len(s.headers) > 0:
		return fmt.Errorf("the %s mcp server's header.<name> is for a url, not a command", name)
	}
	st := scenarioState.Of(sc)
	if len(s.argv) > 0 {
		// A program named by a relative path is found from the project's
		// directory; a bare name on the PATH.
		if p := s.argv[0]; !filepath.IsAbs(p) && strings.ContainsAny(p, `/\`) {
			s.argv[0] = filepath.Join(sc.Suite().ProjectDir(), filepath.FromSlash(p))
		}
		if s.dir == "" {
			if s.dir, err = st.ownFolder(); err != nil {
				return err
			}
		}
		s.env = environment(env)
	}
	if err := s.connect(sc); err != nil {
		_ = s.close()
		return secrets.Hide(sc, err)
	}
	if err := st.servers.Add(name, s); err != nil {
		_ = s.close()
		return err
	}
	init := s.session.InitializeResult()
	sc.Log("connected to the %s mcp server: %s, protocol %s", name, serverName(init), init.ProtocolVersion)
	return nil
}

func serverName(init *sdk.InitializeResult) string {
	if init == nil || init.ServerInfo == nil {
		return "(unnamed)"
	}
	return strings.TrimSpace(init.ServerInfo.Name + " " + init.ServerInfo.Version)
}

// connect opens the scenario's session: it starts the command, or reaches
// the url, and the SDK agrees on a protocol version with the server.
func (s *server) connect(sc *core.Scenario) error {
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "axx", Version: "0.1"}, nil)
	opts := &sdk.ClientSessionOptions{ProtocolVersion: s.version}
	var transport sdk.Transport
	if s.url != "" {
		transport = &sdk.StreamableClientTransport{
			Endpoint:             s.url,
			HTTPClient:           &http.Client{Transport: headerTransport{headers: s.headers, next: http.DefaultTransport}},
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
		}
	} else {
		t, err := s.start()
		if err != nil {
			return err
		}
		transport = t
	}
	session, err := client.Connect(ctx, transport, opts)
	if err != nil {
		if s.url != "" {
			return fmt.Errorf("cannot connect to the %s mcp server at %s: %w", s.name, s.url, err)
		}
		s.stop()
		msg := fmt.Sprintf("cannot connect to the %s mcp server (%s): %v", s.name, strings.Join(s.argv, " "), err)
		if e := strings.TrimSpace(s.stderr.String()); e != "" {
			msg += "\nits error output:\n" + e
		}
		return errors.New(msg)
	}
	s.session = session
	return nil
}

// tools is the server's tools, listed once per scenario.
func (s *server) tools(sc *core.Scenario) ([]*sdk.Tool, error) {
	if s.toolList != nil {
		return s.toolList, nil
	}
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	var out []*sdk.Tool
	for t, err := range s.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("cannot list the %s mcp server's tools: %w", s.name, err)
		}
		out = append(out, t)
	}
	s.toolList = out
	return out, nil
}

func (s *server) tool(sc *core.Scenario, name string) (*sdk.Tool, error) {
	tools, err := s.tools(sc)
	if err != nil {
		return nil, err
	}
	for _, t := range tools {
		if t.Name == name {
			return t, nil
		}
	}
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	sort.Strings(names)
	return nil, core.Failf("the %s mcp server has no tool %s; its tools are: %s", s.name, name, strings.Join(names, ", "))
}

func get(sc *core.Scenario, name string) (*server, error) {
	s, err := scenarioState.Of(sc).servers.Get(name)
	if err != nil {
		return nil, fmt.Errorf("no mcp server named %q in this scenario; register it first with \"the %s mcp server with the following properties:\"", name, name)
	}
	return s, nil
}

// headerTransport adds the registration's headers to every request.
type headerTransport struct {
	headers http.Header
	next    http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, vs := range t.headers {
		r.Header[http.CanonicalHeaderKey(k)] = vs
	}
	return t.next.RoundTrip(r)
}

// capped keeps the first limit bytes written to it.
type capped struct {
	mu    sync.Mutex
	b     *strings.Builder
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.limit - c.b.Len(); room > 0 {
		c.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// environment is this process's environment with vars set over it.
func environment(vars map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(vars))
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !has(vars, k) {
			out = append(out, kv)
		}
	}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		out = append(out, k+"="+vars[k])
	}
	return out
}

func has(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

// describe is what the scenario asked its MCP servers, for failure reports.
func describe(sc *core.Scenario, st *state) any {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string]any{}
	if n := len(st.calls); n > 0 {
		c := st.calls[n-1]
		d := map[string]any{"server": c.server.name, "tool": c.tool, "arguments": secrets.Mask(sc, string(c.arguments))}
		if c.err != nil {
			d["error"] = secrets.Mask(sc, c.err.Error())
		} else {
			b, _ := json.Marshal(c.result)
			d["result"] = secrets.Mask(sc, string(b))
		}
		out["last tool call"] = d
	}
	for _, s := range st.servers.All() {
		if s.stderr != nil && s.stderr.Len() > 0 {
			out[s.name+" error output"] = secrets.Mask(sc, s.stderr.String())
		}
	}
	return out
}

const packDoc = `Call the tools of MCP servers (Model Context Protocol) as an AI assistant calls them, and check their results, with the tools' own schemas as the contract. Read their resources and request their prompts.

` + "```gherkin" + `
Given the parcels mcp server with the following properties:
  | url | http://localhost:8400/mcp |
When the track_parcel tool is called on the parcels mcp server with the following arguments:
  | reference | PX-MCP-9101 |
Then the track_parcel tool's result is not an error
And the track_parcel tool's result has the following properties:
  | status | OUT_FOR_DELIVERY |
` + "```" + `

- **Over stdio or streamable HTTP:** a ` + "`command`" + ` runs the server for the scenario, which ends it when it ends; a ` + "`url`" + ` reaches it. The session agrees on the protocol version with the server: 2026-07-28, or an earlier one the server speaks.
- **Run it as its clients do:** a server assistants start over stdio is the scenario's ` + "`command`" + `, like a command-line tool, and not an app in axx.yaml; a server that runs on its own over HTTP is an app, and the ` + "`url`" + ` reaches it.
- **A tool's schemas are the contract:** its arguments take the types its input schema gives them (` + "`10115`" + ` is text for a string), and are checked against it before the call; its structured result is checked against its output schema.
- **Two kinds of failure:** a tool's result that is an error (the tool ran, and says what went wrong), and a call the server refused (an unknown tool, invalid parameters), with its JSON-RPC error code.

Findings have keys and levels, like the REST pack's OpenAPI findings:

| Key | Found when |
|---|---|
| ` + "`validation.arguments.schema.<keyword>`" + ` | the arguments break the tool's input schema (` + "`required`" + `, ` + "`type`" + `, ` + "`enum`" + `...) |
| ` + "`validation.result.schema.<keyword>`" + ` | the structured result breaks the tool's output schema |
| ` + "`validation.result.missing`" + ` | a tool with an output schema answered no structured result |

- **Levels:** ` + "`ERROR`" + ` (or ` + "`FAIL`" + `) fails the step, ` + "`WARN`" + ` and ` + "`INFO`" + ` log the finding, ` + "`IGNORE`" + ` drops it. Every finding is an ` + "`ERROR`" + ` unless ` + "`packs.mcp.levels`" + ` in axx.yaml, or the scenario's ` + "`the MCP validation levels are:`" + `, says otherwise.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties are masked in logs and failures, and ` + "`${token:..}`" + ` names a token of the scenario.`
