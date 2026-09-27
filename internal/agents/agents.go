// Package agents connects coding agents to axx: it finds the agents a
// project uses and adds the axx MCP server to the configuration they read,
// merged into what is there, never replacing it.
//
// Where each agent keeps its MCP servers comes from its own documentation:
// Claude Code .mcp.json (user servers in ~/.claude.json, which Claude Code
// manages itself), Codex .codex/config.toml (loaded in trusted projects) and
// ~/.codex/config.toml ($CODEX_HOME), Cursor .cursor/mcp.json and
// ~/.cursor/mcp.json, VS Code .vscode/mcp.json (user servers in its profile,
// added with `code --add-mcp`), Gemini CLI .gemini/settings.json and
// ~/.gemini/settings.json.
package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Scopes of a configuration.
const (
	// ScopeProject is the repository's own files, shared with everyone who clones it.
	ScopeProject = "project"
	// ScopeUser is the user's home directory, for every repository.
	ScopeUser = "user"
)

// Error codes.
const (
	// CodeUsage: an unknown agent or scope.
	CodeUsage = "AXX-E0012"
	// CodeMerge: a configuration file axx cannot add the server to safely.
	CodeMerge = "AXX-E0013"
)

// Actions of a Change.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionSkip   = "skip"
	// ActionManual: axx leaves the file to the agent; Command sets it up.
	ActionManual = "manual"
)

// Agent is a coding agent axx connects to.
type Agent struct {
	// ID is what --agent takes.
	ID string
	// Name is how people know the agent.
	Name string
	// Signals are files or directories whose presence in a project shows
	// that the project uses the agent.
	Signals []string
	// Project is where the agent reads a project's MCP servers, relative to
	// the project.
	Project string
	// user is where it reads the user's, relative to the home directory; ""
	// when axx leaves that file to the agent and UserCommand adds the server.
	user string
	// UserCommand is the agent's own command that adds axx for the user.
	UserCommand string
	// userReason says why axx leaves the user's file to the agent.
	userReason string

	toml  bool   // config.toml instead of JSON
	key   string // JSON: the object that holds the servers
	entry string // JSON: the axx server
}

const (
	stdioEntry = `{"command": "axx", "args": ["mcp"]}`
	tomlEntry  = "[mcp_servers.axx]\ncommand = \"axx\"\nargs = [\"mcp\"]\n"
)

// All are the agents axx connects to, in the order axx lists them.
var All = []Agent{
	{
		ID: "claude", Name: "Claude Code",
		Signals:     []string{".claude", "CLAUDE.md", ".mcp.json"},
		Project:     ".mcp.json",
		UserCommand: "claude mcp add --scope user axx -- axx mcp",
		userReason:  "Claude Code keeps your MCP servers with its own state in ~/.claude.json",
		key:         "mcpServers", entry: stdioEntry,
	},
	{
		ID: "codex", Name: "Codex",
		Signals: []string{".codex"},
		Project: ".codex/config.toml",
		toml:    true, // the user's is in CodexHome
	},
	{
		ID: "cursor", Name: "Cursor",
		Signals: []string{".cursor"},
		Project: ".cursor/mcp.json",
		user:    ".cursor/mcp.json",
		key:     "mcpServers", entry: stdioEntry,
	},
	{
		ID: "vscode", Name: "VS Code",
		Signals:     []string{".vscode"},
		Project:     ".vscode/mcp.json",
		UserCommand: `code --add-mcp '{"name":"axx","command":"axx","args":["mcp"]}'`,
		userReason:  "VS Code keeps your MCP servers in its profile",
		key:         "servers", entry: `{"type": "stdio", "command": "axx", "args": ["mcp"]}`,
	},
	{
		ID: "gemini", Name: "Gemini CLI",
		Signals: []string{".gemini", "GEMINI.md"},
		Project: ".gemini/settings.json",
		user:    ".gemini/settings.json",
		key:     "mcpServers", entry: stdioEntry,
	},
}

// IDs are the agents' ids, in order.
func IDs() []string {
	out := make([]string, len(All))
	for i, a := range All {
		out[i] = a.ID
	}
	return out
}

// Lookup returns the agent with id.
func Lookup(id string) (Agent, bool) {
	for _, a := range All {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// Env is where agents keep their configuration.
type Env struct {
	// Project is the project directory.
	Project string
	// Home is the user's home directory ("" when unknown).
	Home string
	// ConfigDir is the user's configuration directory (os.UserConfigDir),
	// where VS Code keeps its profile; only read.
	ConfigDir string
	// Getenv reads the environment (CODEX_HOME); nil means os.Getenv.
	Getenv func(string) string
}

// NewEnv is the environment of this process for a project.
func NewEnv(project string) Env {
	home, _ := os.UserHomeDir()
	cfg, _ := os.UserConfigDir()
	return Env{Project: project, Home: home, ConfigDir: cfg, Getenv: os.Getenv}
}

func (e Env) getenv(k string) string {
	if e.Getenv == nil {
		return os.Getenv(k)
	}
	return e.Getenv(k)
}

// Display renders an absolute path the way people read it: relative to the
// project, or with ~ for the home directory.
func (e Env) Display(p string) string {
	if e.Project != "" {
		if rel, err := filepath.Rel(e.Project, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	if e.Home != "" {
		if rel, err := filepath.Rel(e.Home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

// Detect returns the agents a project shows signs of using.
func Detect(project string) []Agent {
	var out []Agent
	for _, a := range All {
		for _, s := range a.Signals {
			if _, err := os.Stat(filepath.Join(project, s)); err == nil && !onlyAxxSkills(project, s) {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// Uses reports whether the project (or home directory) shows signs of the
// agent with this id.
func Uses(project, id string) bool {
	return slices.ContainsFunc(Detect(project), func(a Agent) bool { return a.ID == id })
}

// onlyAxxSkills reports whether the signal is a .claude directory that
// holds nothing but the skills axx linked there: axx made it, so it does
// not show that the project uses Claude Code.
func onlyAxxSkills(project, signal string) bool {
	if signal != ".claude" {
		return false
	}
	dir := filepath.Join(project, ".claude")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "skills" {
		return false
	}
	skills, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		return false
	}
	for _, e := range skills {
		if !strings.HasPrefix(e.Name(), "axx-") {
			return false
		}
	}
	return true
}

// CodexHome is Codex's own directory: $CODEX_HOME, or ~/.codex.
func CodexHome(env Env) string {
	if h := env.getenv("CODEX_HOME"); h != "" {
		return h
	}
	if env.Home == "" {
		return ""
	}
	return filepath.Join(env.Home, ".codex")
}

// Path is the file the agent reads its MCP servers from for scope, or ""
// when axx leaves that file to the agent.
func (a Agent) Path(scope string, env Env) string {
	switch scope {
	case ScopeProject:
		return filepath.Join(env.Project, filepath.FromSlash(a.Project))
	case ScopeUser:
		if a.ID == "codex" {
			if h := CodexHome(env); h != "" {
				return filepath.Join(h, "config.toml")
			}
			return ""
		}
		if a.user == "" || env.Home == "" {
			return ""
		}
		return filepath.Join(env.Home, filepath.FromSlash(a.user))
	}
	return ""
}

// Snippet is what to add to the agent's file by hand.
func (a Agent) Snippet() string {
	if a.toml {
		return tomlEntry
	}
	return fmt.Sprintf(`{"%s": {"axx": %s}}`, a.key, a.entry)
}

// Change is what Install did, or would do, to one agent's configuration.
type Change struct {
	Agent string `json:"agent"`
	Scope string `json:"scope"`
	// Path is the file, relative to the project or with ~ for the home directory.
	Path string `json:"path,omitempty"`
	// Action is create, update, skip (the server is there already) or manual
	// (Command adds it).
	Action  string `json:"action"`
	Reason  string `json:"reason,omitempty"`
	Command string `json:"command,omitempty"`
}

// Install adds the axx MCP server to the agent's configuration for scope,
// keeping every other server and setting. A file that already has it is left
// alone. With dryRun nothing is written.
func Install(a Agent, scope string, env Env, dryRun bool) (Change, error) {
	ch := Change{Agent: a.ID, Scope: scope}
	if scope != ScopeProject && scope != ScopeUser {
		return ch, axxerr.New(CodeUsage, exitcode.Usage, "--scope must be project or user, not %q", scope).
			WithHint("use --scope project (this repository's files) or --scope user (your home directory)")
	}
	path := a.Path(scope, env)
	if path == "" && a.UserCommand == "" {
		return ch, errors.New("cannot find your home directory")
	}
	if path == "" {
		ch.Action, ch.Command, ch.Reason = ActionManual, a.UserCommand, a.userReason
		return ch, nil
	}
	ch.Path = env.Display(path)
	old, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ch, err
	}
	out, changed, err := a.merge(old)
	if err != nil {
		return ch, axxerr.Wrap(err, CodeMerge, exitcode.Usage, "cannot add the axx MCP server to %s", ch.Path).
			WithHint("add it to %s yourself:\n%s", ch.Path, a.Snippet())
	}
	if !changed {
		ch.Action, ch.Reason = ActionSkip, "the axx MCP server is there already"
		return ch, nil
	}
	ch.Action = ActionCreate
	if exists {
		ch.Action = ActionUpdate
	}
	if dryRun {
		return ch, nil
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ch, err
	}
	return ch, os.WriteFile(path, out, mode)
}

func (a Agent) merge(old []byte) ([]byte, bool, error) {
	if a.toml {
		return mergeTOML(old)
	}
	return mergeJSON(old, a.key, a.entry)
}

// Configured reports where the agent finds the axx MCP server, if anywhere:
// the project's file, the user's, or (Claude Code) axx's plugin.
func Configured(a Agent, env Env) (string, bool) {
	for _, scope := range []string{ScopeProject, ScopeUser} {
		p := a.Path(scope, env)
		if p == "" {
			continue
		}
		if b, err := os.ReadFile(p); err == nil && a.has(b) {
			return env.Display(p), true
		}
	}
	switch a.ID {
	case "claude":
		return claudeConfigured(env)
	case "vscode":
		if env.ConfigDir != "" {
			p := filepath.Join(env.ConfigDir, "Code", "User", "mcp.json")
			if b, err := os.ReadFile(p); err == nil && a.has(b) {
				return env.Display(p), true
			}
		}
	}
	return "", false
}

func (a Agent) has(b []byte) bool {
	if a.toml {
		return scanTOML(string(b)).configured
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return false
	}
	return hasServer(doc[a.key])
}

// claudeConfigured looks where else Claude Code finds servers: the user's
// and the project's entries in ~/.claude.json, and axx's plugin.
func claudeConfigured(env Env) (string, bool) {
	if env.Home == "" {
		return "", false
	}
	p := filepath.Join(env.Home, ".claude.json")
	if b, err := os.ReadFile(p); err == nil {
		var doc struct {
			MCPServers json.RawMessage `json:"mcpServers"`
			Projects   map[string]struct {
				MCPServers json.RawMessage `json:"mcpServers"`
			} `json:"projects"`
		}
		if json.Unmarshal(b, &doc) == nil {
			if hasServer(doc.MCPServers) || hasServer(doc.Projects[env.Project].MCPServers) {
				return "~/.claude.json", true
			}
		}
	}
	for _, s := range []string{
		filepath.Join(env.Project, ".claude", "settings.json"),
		filepath.Join(env.Project, ".claude", "settings.local.json"),
		filepath.Join(env.Home, ".claude", "settings.json"),
	} {
		b, err := os.ReadFile(s)
		if err != nil {
			continue
		}
		var doc struct {
			EnabledPlugins map[string]any `json:"enabledPlugins"`
		}
		if json.Unmarshal(b, &doc) == nil && doc.EnabledPlugins["axx@nimbusxr"] == true {
			return "the axx plugin", true
		}
	}
	return "", false
}

// hasServer reports whether a JSON object of servers has axx: a server named
// axx, or one that runs `axx mcp`.
func hasServer(raw json.RawMessage) bool {
	var servers map[string]json.RawMessage
	if json.Unmarshal(raw, &servers) != nil {
		return false
	}
	for name, s := range servers {
		if name == "axx" || runsAxx(s) {
			return true
		}
	}
	return false
}

func runsAxx(raw json.RawMessage) bool {
	var s struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	return isAxx(s.Command) && len(s.Args) > 0 && s.Args[0] == "mcp"
}

func isAxx(command string) bool {
	base := command
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return base == "axx" || base == "axx.exe"
}
