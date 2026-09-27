package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/agents"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/skills"
)

// agentProject is an empty repository with files, and a home directory of
// its own, which no test may write to unless it asks for the user scope.
func agentProject(t *testing.T, files map[string]string) (dir, home string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("CODEX_HOME", "")
	writeFiles(t, dir, files)
	t.Chdir(dir)
	return dir, home
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func homeIsEmpty(t *testing.T, home string) {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("wrote to the home directory: %v", entries)
	}
}

func hasAxxServer(t *testing.T, id, path string) {
	t.Helper()
	a, _ := agents.Lookup(id)
	env := agents.Env{Project: filepath.Dir(path), Getenv: func(string) string { return "" }}
	if a.Project != filepath.Base(path) {
		env.Project = filepath.Dir(filepath.Dir(path))
	}
	if _, ok := agents.Configured(a, env); !ok {
		b, _ := os.ReadFile(path)
		t.Errorf("%s has no axx server:\n%s", path, b)
	}
}

func TestInitConnectsOnlyTheAgentsTheProjectUses(t *testing.T) {
	dir, home := agentProject(t, map[string]string{"CLAUDE.md": "# parcels\n", ".cursor/rules/parcels.mdc": "x\n"})
	out, stderr, code := run(t, "init", "--no-ci")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	hasAxxServer(t, "claude", filepath.Join(dir, ".mcp.json"))
	hasAxxServer(t, "cursor", filepath.Join(dir, ".cursor", "mcp.json"))
	for _, p := range []string{".vscode", ".gemini", ".codex"} {
		if exists(filepath.Join(dir, p)) {
			t.Errorf("init wrote %s for an agent the project does not use", p)
		}
	}
	for _, n := range skills.Names() {
		for _, d := range []string{".agents", ".claude"} {
			if !exists(filepath.Join(dir, d, "skills", n, "SKILL.md")) {
				t.Errorf("%s/skills/%s is missing", d, n)
			}
		}
	}
	for _, want := range []string{
		"create  .agents/skills (", "create  .claude/skills (linked for Claude Code)",
		"create  .mcp.json (the axx MCP server for Claude Code)", "create  .cursor/mcp.json (the axx MCP server for Cursor)",
		"`axx mcp install --agent codex --scope user` adds axx there",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	homeIsEmpty(t, home)
}

func TestInitLinksSkillsForClaudeCodeOnlyWhenItIsUsed(t *testing.T) {
	dir, _ := agentProject(t, nil)
	if _, stderr, code := run(t, "init", "--no-ci"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !exists(filepath.Join(dir, ".agents", "skills", "axx-test-data", "SKILL.md")) {
		t.Error("no skills in .agents/skills")
	}
	for _, p := range []string{".claude", ".mcp.json"} {
		if exists(filepath.Join(dir, p)) {
			t.Errorf("init wrote %s in a project that does not use Claude Code", p)
		}
	}
}

func TestInitWithNoAgentsSetsUpNoAgent(t *testing.T) {
	dir, _ := agentProject(t, map[string]string{"CLAUDE.md": "# parcels\n"})
	out, stderr, code := run(t, "init", "--no-ci", "--no-agents", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, p := range []string{".agents", ".claude", ".mcp.json"} {
		if exists(filepath.Join(dir, p)) {
			t.Errorf("--no-agents wrote %s", p)
		}
	}
	if !exists(filepath.Join(dir, "AGENTS.md")) {
		t.Error("--no-agents skipped AGENTS.md")
	}
	if strings.Contains(out, `"agents"`) {
		t.Errorf("--no-agents reported agents:\n%s", out)
	}
}

func TestInitRunTwiceChangesNothing(t *testing.T) {
	dir, _ := agentProject(t, map[string]string{"CLAUDE.md": "# parcels\n", ".gemini/settings.json": `{"theme": "Dracula"}`})
	if _, stderr, code := run(t, "init", "--no-ci"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	before := map[string]string{}
	for _, p := range []string{".mcp.json", ".gemini/settings.json", "AGENTS.md"} {
		b, _ := os.ReadFile(filepath.Join(dir, p))
		before[p] = string(b)
	}
	out, stderr, code := run(t, "init", "--no-ci")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for p, content := range before {
		if b, _ := os.ReadFile(filepath.Join(dir, p)); string(b) != content {
			t.Errorf("the second init changed %s", p)
		}
	}
	for _, want := range []string{"skip    .agents/skills: up to date", "skip    .mcp.json: the axx MCP server is there already", "skip    .gemini/settings.json: the axx MCP server is there already"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(before[".gemini/settings.json"], `"theme": "Dracula"`) {
		t.Errorf("init dropped the Gemini CLI settings:\n%s", before[".gemini/settings.json"])
	}
}

func TestInitLeavesAFileItCannotReadBack(t *testing.T) {
	const withComments = "{\n  // the parcels database\n  \"servers\": {}\n}\n"
	dir, _ := agentProject(t, map[string]string{".vscode/mcp.json": withComments})
	out, stderr, code := run(t, "init", "--no-ci")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".vscode", "mcp.json")); string(b) != withComments {
		t.Errorf("init changed .vscode/mcp.json:\n%s", b)
	}
	if !strings.Contains(out, "skip    .vscode/mcp.json: left alone: it is not plain JSON") || !strings.Contains(out, "`axx mcp install --agent vscode`") {
		t.Errorf("output:\n%s", out)
	}
}

func TestInitDryRunWritesNoAgentFile(t *testing.T) {
	dir, _ := agentProject(t, map[string]string{"CLAUDE.md": "# parcels\n"})
	out, stderr, code := run(t, "init", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, p := range []string{".agents", ".claude", ".mcp.json"} {
		if exists(filepath.Join(dir, p)) {
			t.Errorf("a dry run wrote %s", p)
		}
	}
	if !strings.Contains(out, "would create  .mcp.json (the axx MCP server for Claude Code)") {
		t.Errorf("output:\n%s", out)
	}
}

func TestInitSaysNothingOfCodexWhenItHasAxx(t *testing.T) {
	_, home := agentProject(t, nil)
	writeFiles(t, home, map[string]string{".codex/config.toml": "[mcp_servers.axx]\ncommand = \"axx\"\nargs = [\"mcp\"]\n"})
	out, stderr, code := run(t, "init", "--no-ci")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(out, "Codex") {
		t.Errorf("output:\n%s", out)
	}
}

func TestInitReportsAgentsInJSON(t *testing.T) {
	_, _ = agentProject(t, map[string]string{".codex/config.toml": "model = \"o3\"\n"})
	out, stderr, code := run(t, "init", "--no-ci", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var env struct {
		Data struct {
			Files  []InitFile  `json:"files"`
			Agents *InitAgents `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	a := env.Data.Agents
	if a == nil || len(a.Detected) != 1 || a.Detected[0] != "codex" || len(a.MCP) != 1 {
		t.Fatalf("agents: %+v", a)
	}
	if ch := a.MCP[0]; ch.Agent != "codex" || ch.Scope != agents.ScopeProject || ch.Path != ".codex/config.toml" || ch.Action != agents.ActionUpdate {
		t.Errorf("codex: %+v", ch)
	}
	if len(env.Data.Files) != 5 {
		t.Errorf("files: %+v", env.Data.Files)
	}
}

func TestAgentsSectionPointsAtTheMCPToolsSkillsAndFactories(t *testing.T) {
	for _, want := range []string{
		"axx MCP tools", "steps_search", "feature_validate", "scenarios_run", "failure_context",
		".agents/skills", "axx-test-data", "fixture factories", "optional", "`axx fixtures adopt`",
	} {
		if !strings.Contains(agentsBlock, want) {
			t.Errorf("the AGENTS.md section lacks %q", want)
		}
	}
}

func TestMCPInstall(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		code      exitcode.Code
		errCode   string
		project   string // the file written in the project
		home      string // the file written in the home directory
		output    string
		projFiles map[string]string
	}{
		{name: "claude, in the project", args: []string{"--agent", "claude"}, project: ".mcp.json", output: "create .mcp.json: the axx MCP server for Claude Code"},
		{name: "codex, in the project", args: []string{"--agent", "codex"}, project: ".codex/config.toml", output: "Codex loads it once you trust the project"},
		{name: "codex, for the user", args: []string{"--agent", "codex", "--scope", "user"}, home: ".codex/config.toml", output: "create ~/.codex/config.toml"},
		{name: "cursor, for the user", args: []string{"--agent", "cursor", "--scope", "user"}, home: ".cursor/mcp.json"},
		{
			name: "gemini, into its settings", args: []string{"--agent", "gemini"}, project: ".gemini/settings.json", output: "update .gemini/settings.json",
			projFiles: map[string]string{".gemini/settings.json": `{"theme": "Dracula"}`},
		},
		{name: "claude, for the user: its own command", args: []string{"--agent", "claude", "--scope", "user"}, output: "claude mcp add --scope user axx -- axx mcp"},
		{name: "vscode, for the user: its own command", args: []string{"--agent", "vscode", "--scope", "user"}, output: "code --add-mcp"},
		{name: "a dry run", args: []string{"--agent", "vscode", "--dry-run"}, output: "would create .vscode/mcp.json"},
		{name: "no agent", args: nil, code: exitcode.Usage, errCode: agents.CodeUsage},
		{name: "an unknown agent", args: []string{"--agent", "vim"}, code: exitcode.Usage, errCode: agents.CodeUsage},
		{name: "an unknown scope", args: []string{"--agent", "cursor", "--scope", "team"}, code: exitcode.Usage, errCode: agents.CodeUsage},
		{
			name: "a file with comments", args: []string{"--agent", "cursor"}, code: exitcode.Usage, errCode: agents.CodeMerge,
			projFiles: map[string]string{".cursor/mcp.json": "{\n  // none yet\n}\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, home := agentProject(t, tt.projFiles)
			out, stderr, code := run(t, append([]string{"mcp", "install"}, tt.args...)...)
			if exitcode.Code(code) != tt.code {
				t.Fatalf("exit %d, want %d: %s%s", code, tt.code, out, stderr)
			}
			if tt.errCode != "" && !strings.Contains(stderr, tt.errCode) {
				t.Errorf("stderr lacks %s: %s", tt.errCode, stderr)
			}
			if !strings.Contains(out, tt.output) {
				t.Errorf("output lacks %q:\n%s", tt.output, out)
			}
			id := ""
			if len(tt.args) > 1 {
				id = tt.args[1]
			}
			if tt.project != "" {
				hasAxxServer(t, id, filepath.Join(dir, filepath.FromSlash(tt.project)))
			}
			if tt.home != "" {
				if !exists(filepath.Join(home, filepath.FromSlash(tt.home))) {
					t.Errorf("~/%s was not written", tt.home)
				}
			} else {
				homeIsEmpty(t, home)
			}
			if tt.project == "" {
				// Nothing written in the project, and what was there is unchanged.
				_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					rel, _ := filepath.Rel(dir, p)
					want, ok := tt.projFiles[filepath.ToSlash(rel)]
					if b, _ := os.ReadFile(p); !ok || string(b) != want {
						t.Errorf("wrote %s in the project", rel)
					}
					return nil
				})
			}
			for name := range tt.projFiles {
				if b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); tt.project == name && !strings.Contains(string(b), `"theme": "Dracula"`) {
					t.Errorf("%s lost its settings:\n%s", name, b)
				}
			}
		})
	}
}

func doctorChecks(t *testing.T) map[string]DoctorCheck {
	t.Helper()
	out, _, _ := run(t, "doctor", "--json")
	var env struct {
		Data DoctorReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	checks := map[string]DoctorCheck{}
	for _, c := range env.Data.Checks {
		checks[c.Name] = c
	}
	return checks
}

func TestDoctorWarnsOfWhatAnAgentLacks(t *testing.T) {
	_, _ = agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "CLAUDE.md": "# parcels\n", ".cursor/rules/x.mdc": "x\n"})
	checks := doctorChecks(t)
	for name, hint := range map[string]string{
		"agent skills":       "`axx skills install`",
		"Claude Code skills": "`axx skills install`",
		"Claude Code MCP":    "`axx mcp install --agent claude`",
		"Cursor MCP":         "`axx mcp install --agent cursor`",
	} {
		c, ok := checks[name]
		if !ok || c.Status != "warn" || !strings.Contains(c.Hint, hint) {
			t.Errorf("%s: %+v", name, c)
		}
	}
	for _, name := range []string{"VS Code MCP", "Gemini CLI MCP", "Codex MCP"} {
		if c, ok := checks[name]; ok {
			t.Errorf("a check for an agent the project does not use: %+v", c)
		}
	}
}

func TestDoctorIsHappyOnceInitSetUpTheAgents(t *testing.T) {
	_, _ = agentProject(t, map[string]string{"CLAUDE.md": "# parcels\n"})
	if _, stderr, code := run(t, "init", "--no-ci"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	checks := doctorChecks(t)
	for _, name := range []string{"agent skills", "Claude Code skills", "Claude Code MCP"} {
		if c := checks[name]; c.Status != "ok" {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

func TestDoctorFindsCodexInItsHome(t *testing.T) {
	_, home := agentProject(t, map[string]string{"axx.yaml": "version: 1\n"})
	writeFiles(t, home, map[string]string{".codex/config.toml": "model = \"o3\"\n"})
	c := doctorChecks(t)["Codex MCP"]
	if c.Status != "warn" || c.Detail != "no axx server in ~/.codex/config.toml" || !strings.Contains(c.Hint, "--scope user") {
		t.Errorf("%+v", c)
	}
}

func TestDoctorSaysNothingOfAgentsInAProjectWithout(t *testing.T) {
	_, _ = agentProject(t, map[string]string{"axx.yaml": "version: 1\n"})
	for name := range doctorChecks(t) {
		if strings.Contains(name, "MCP") || strings.Contains(name, "skills") {
			t.Errorf("unexpected check %s", name)
		}
	}
}
