package agents

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
)

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func agent(t *testing.T, id string) Agent {
	t.Helper()
	a, ok := Lookup(id)
	if !ok {
		t.Fatalf("no agent %s", id)
	}
	return a
}

func testEnv(t *testing.T) Env {
	t.Helper()
	return Env{Project: t.TempDir(), Home: t.TempDir(), ConfigDir: t.TempDir(), Getenv: func(string) string { return "" }}
}

func TestDetectFindsTheAgentsAProjectUses(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"nothing", map[string]string{"README.md": "x"}, nil},
		{"a .claude directory", map[string]string{".claude/settings.json": "{}"}, []string{"claude"}},
		{"CLAUDE.md", map[string]string{"CLAUDE.md": "x"}, []string{"claude"}},
		{".mcp.json", map[string]string{".mcp.json": "{}"}, []string{"claude"}},
		{"a .codex directory", map[string]string{".codex/config.toml": ""}, []string{"codex"}},
		{"a .cursor directory", map[string]string{".cursor/rules/x.mdc": ""}, []string{"cursor"}},
		{"a .vscode directory", map[string]string{".vscode/settings.json": "{}"}, []string{"vscode"}},
		{"GEMINI.md", map[string]string{"GEMINI.md": "x"}, []string{"gemini"}},
		{"several, in order", map[string]string{".gemini/settings.json": "{}", "CLAUDE.md": "x", ".cursor/mcp.json": "{}"}, []string{"claude", "cursor", "gemini"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tt.files)
			var got []string
			for _, a := range Detect(dir) {
				got = append(got, a.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Detect = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeJSONKeepsWhatIsThere(t *testing.T) {
	tests := []struct {
		name, key, in, want string
		changed             bool
	}{
		{
			"an empty file", "mcpServers", "",
			"{\n  \"mcpServers\": {\n    \"axx\": {\n      \"command\": \"axx\",\n      \"args\": [\n        \"mcp\"\n      ]\n    }\n  }\n}\n", true,
		},
		{
			"other servers and settings keep their order", "mcpServers",
			`{"theme": "dark", "mcpServers": {"parcels-db": {"command": "pg-mcp"}}, "zeta": 1}`,
			"{\n  \"theme\": \"dark\",\n  \"mcpServers\": {\n    \"parcels-db\": {\n      \"command\": \"pg-mcp\"\n    },\n    \"axx\": {\n      \"command\": \"axx\",\n      \"args\": [\n        \"mcp\"\n      ]\n    }\n  },\n  \"zeta\": 1\n}\n", true,
		},
		{
			"no servers yet", "servers", `{"inputs": []}`,
			"{\n  \"inputs\": [],\n  \"servers\": {\n    \"axx\": {\n      \"command\": \"axx\",\n      \"args\": [\n        \"mcp\"\n      ]\n    }\n  }\n}\n", true,
		},
		{
			"null servers", "mcpServers", `{"mcpServers": null}`,
			"{\n  \"mcpServers\": {\n    \"axx\": {\n      \"command\": \"axx\",\n      \"args\": [\n        \"mcp\"\n      ]\n    }\n  }\n}\n", true,
		},
		{
			"axx is there", "mcpServers", `{"mcpServers": {"axx": {"command": "/opt/axx", "args": ["mcp", "--profile", "ci"]}}}`,
			`{"mcpServers": {"axx": {"command": "/opt/axx", "args": ["mcp", "--profile", "ci"]}}}`, false,
		},
		{
			"a server that runs axx mcp under another name", "mcpServers", `{"mcpServers": {"acceptance": {"command": "/usr/local/bin/axx", "args": ["mcp"]}}}`,
			`{"mcpServers": {"acceptance": {"command": "/usr/local/bin/axx", "args": ["mcp"]}}}`, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, err := mergeJSON([]byte(tt.in), tt.key, `{"command": "axx", "args": ["mcp"]}`)
			if err != nil {
				t.Fatal(err)
			}
			if changed != tt.changed || string(out) != tt.want {
				t.Errorf("changed %v, got:\n%s\nwant:\n%s", changed, out, tt.want)
			}
		})
	}
}

func TestMergeJSONRefusesWhatItCannotReadBack(t *testing.T) {
	tests := []struct{ name, in string }{
		{"comments", "{\n  // the parcels database\n  \"mcpServers\": {}\n}"},
		{"not an object", `["axx"]`},
		{"servers that are not an object", `{"mcpServers": ["axx"]}`},
		{"two values", `{} {}`},
		{"broken", `{"mcpServers": {`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := mergeJSON([]byte(tt.in), "mcpServers", stdioEntry); err == nil {
				t.Error("merged")
			}
		})
	}
}

func TestMergeTOML(t *testing.T) {
	tests := []struct {
		name, in, want string
		changed        bool
	}{
		{"an empty file", "", tomlEntry, true},
		{
			"other settings and servers stay", "model = \"o3\"\n\n[mcp_servers.parcels-db]\ncommand = \"pg-mcp\"",
			"model = \"o3\"\n\n[mcp_servers.parcels-db]\ncommand = \"pg-mcp\"\n\n" + tomlEntry, true,
		},
		{"axx is there", "[mcp_servers.axx]\ncommand = \"axx\"\n", "[mcp_servers.axx]\ncommand = \"axx\"\n", false},
		{"a quoted table name", "[mcp_servers.\"axx\"]\ncommand = \"axx\"\n", "[mcp_servers.\"axx\"]\ncommand = \"axx\"\n", false},
		{"its environment table", "[mcp_servers.axx.env]\nAXX_PROFILE = \"ci\"\n", "[mcp_servers.axx.env]\nAXX_PROFILE = \"ci\"\n", false},
		{
			"a server that runs axx mcp under another name", "[mcp_servers.acceptance]\ncommand = \"/usr/local/bin/axx\"\nargs = [\"mcp\"]\n",
			"[mcp_servers.acceptance]\ncommand = \"/usr/local/bin/axx\"\nargs = [\"mcp\"]\n", false,
		},
		{
			"axx as a key of [mcp_servers]", "[mcp_servers]\naxx = { command = \"axx\", args = [\"mcp\"] }\n",
			"[mcp_servers]\naxx = { command = \"axx\", args = [\"mcp\"] }\n", false,
		},
		{
			"another server's table mentions axx elsewhere", "[mcp_servers.docs]\ncommand = \"docs-mcp\"\nargs = [\"--for\", \"axx\"]\n",
			"[mcp_servers.docs]\ncommand = \"docs-mcp\"\nargs = [\"--for\", \"axx\"]\n\n" + tomlEntry, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, err := mergeTOML([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if changed != tt.changed || string(out) != tt.want {
				t.Errorf("changed %v, got:\n%s\nwant:\n%s", changed, out, tt.want)
			}
		})
	}
}

func TestMergeTOMLRefusesInlineServers(t *testing.T) {
	for _, in := range []string{
		"mcp_servers = { docs = { command = \"docs-mcp\" } }\n",
		"mcp_servers.docs.command = \"docs-mcp\"\n",
		"[[mcp_servers]]\nname = \"docs\"\n",
	} {
		if _, _, err := mergeTOML([]byte(in)); err == nil {
			t.Errorf("merged into %q", in)
		}
	}
}

func TestInstallWritesTheProjectsFileOnce(t *testing.T) {
	for _, a := range All {
		t.Run(a.ID, func(t *testing.T) {
			env := testEnv(t)
			ch, err := Install(a, ScopeProject, env, false)
			if err != nil {
				t.Fatal(err)
			}
			if ch.Action != ActionCreate || ch.Path != a.Project {
				t.Fatalf("first install: %+v", ch)
			}
			p := filepath.Join(env.Project, filepath.FromSlash(a.Project))
			first := read(t, p)
			if !a.has([]byte(first)) {
				t.Fatalf("no axx server in:\n%s", first)
			}
			if where, ok := Configured(a, env); !ok || where != a.Project {
				t.Errorf("Configured = %q, %v", where, ok)
			}
			ch, err = Install(a, ScopeProject, env, false)
			if err != nil || ch.Action != ActionSkip {
				t.Fatalf("second install: %+v, %v", ch, err)
			}
			if read(t, p) != first {
				t.Error("the second install changed the file")
			}
		})
	}
}

func TestInstallUpdatesAnExistingFile(t *testing.T) {
	env := testEnv(t)
	write(t, env.Project, map[string]string{".cursor/mcp.json": `{"mcpServers": {"parcels-db": {"command": "pg-mcp"}}}`})
	ch, err := Install(agent(t, "cursor"), ScopeProject, env, false)
	if err != nil || ch.Action != ActionUpdate {
		t.Fatalf("%+v, %v", ch, err)
	}
	got := read(t, filepath.Join(env.Project, ".cursor", "mcp.json"))
	if !strings.Contains(got, `"parcels-db"`) || !strings.Contains(got, `"axx"`) {
		t.Errorf("got:\n%s", got)
	}
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	env := testEnv(t)
	ch, err := Install(agent(t, "claude"), ScopeProject, env, true)
	if err != nil || ch.Action != ActionCreate {
		t.Fatalf("%+v, %v", ch, err)
	}
	if _, err := os.Stat(filepath.Join(env.Project, ".mcp.json")); err == nil {
		t.Error("a dry run wrote .mcp.json")
	}
}

func TestInstallKeepsTheFilesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	env := testEnv(t)
	p := filepath.Join(env.Project, ".gemini", "settings.json")
	write(t, env.Project, map[string]string{".gemini/settings.json": `{"theme": "dark"}`})
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(agent(t, "gemini"), ScopeProject, env, false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v", fi.Mode().Perm(), err)
	}
}

func TestInstallForTheUser(t *testing.T) {
	tests := []struct {
		agent, codexHome, wantPath, wantAction, wantCommand string
	}{
		{"cursor", "", "~/.cursor/mcp.json", ActionCreate, ""},
		{"gemini", "", "~/.gemini/settings.json", ActionCreate, ""},
		{"codex", "", "~/.codex/config.toml", ActionCreate, ""},
		{"codex", "codex-home", "~/codex-home/config.toml", ActionCreate, ""},
		{"claude", "", "", ActionManual, "claude mcp add --scope user axx -- axx mcp"},
		{"vscode", "", "", ActionManual, `code --add-mcp '{"name":"axx","command":"axx","args":["mcp"]}'`},
	}
	for _, tt := range tests {
		t.Run(tt.agent+" "+tt.codexHome, func(t *testing.T) {
			env := testEnv(t)
			if tt.codexHome != "" {
				home := filepath.Join(env.Home, tt.codexHome)
				env.Getenv = func(k string) string {
					if k == "CODEX_HOME" {
						return home
					}
					return ""
				}
			}
			ch, err := Install(agent(t, tt.agent), ScopeUser, env, false)
			if err != nil {
				t.Fatal(err)
			}
			if ch.Path != tt.wantPath || ch.Action != tt.wantAction || ch.Command != tt.wantCommand {
				t.Errorf("got %+v", ch)
			}
			if tt.wantPath != "" {
				if _, err := os.Stat(filepath.Join(env.Home, filepath.FromSlash(strings.TrimPrefix(tt.wantPath, "~/")))); err != nil {
					t.Error(err)
				}
			}
			entries, _ := os.ReadDir(env.Project)
			if len(entries) != 0 {
				t.Errorf("the user install wrote to the project: %v", entries)
			}
		})
	}
}

func TestInstallErrors(t *testing.T) {
	tests := []struct {
		name, scope, code string
		files             map[string]string
	}{
		{"an unknown scope", "team", CodeUsage, nil},
		{"a file with comments", ScopeProject, CodeMerge, map[string]string{".vscode/mcp.json": "{\n  // servers\n  \"servers\": {}\n}\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv(t)
			write(t, env.Project, tt.files)
			_, err := Install(agent(t, "vscode"), tt.scope, env, false)
			var ae *axxerr.Error
			if !errors.As(err, &ae) || ae.Code != tt.code || ae.Hint == "" {
				t.Fatalf("got %v", err)
			}
			for name, content := range tt.files {
				if read(t, filepath.Join(env.Project, filepath.FromSlash(name))) != content {
					t.Errorf("%s changed", name)
				}
			}
		})
	}
}

func TestConfiguredLooksWhereTheAgentLooks(t *testing.T) {
	tests := []struct {
		name, agent string
		project     map[string]string
		home        map[string]string
		config      map[string]string
		want        string
	}{
		{"nowhere", "claude", nil, nil, nil, ""},
		{"the project's file", "gemini", map[string]string{".gemini/settings.json": `{"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}`}, nil, nil, ".gemini/settings.json"},
		{"the user's file", "cursor", nil, map[string]string{".cursor/mcp.json": `{"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}`}, nil, "~/.cursor/mcp.json"},
		{"the user's Codex config", "codex", nil, map[string]string{".codex/config.toml": tomlEntry}, nil, "~/.codex/config.toml"},
		{"Claude Code's user servers", "claude", nil, map[string]string{".claude.json": `{"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}`}, nil, "~/.claude.json"},
		{"axx's Claude Code plugin", "claude", map[string]string{".claude/settings.json": `{"enabledPlugins": {"axx@nimbusxr": true}}`}, nil, nil, "the axx plugin"},
		{"a disabled plugin", "claude", map[string]string{".claude/settings.json": `{"enabledPlugins": {"axx@nimbusxr": false}}`}, nil, nil, ""},
		{"VS Code's profile", "vscode", nil, nil, map[string]string{"Code/User/mcp.json": `{"servers": {"axx": {"type": "stdio", "command": "axx", "args": ["mcp"]}}}`}, "config"},
		{"another server only", "cursor", map[string]string{".cursor/mcp.json": `{"mcpServers": {"parcels-db": {"command": "pg-mcp"}}}`}, nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv(t)
			write(t, env.Project, tt.project)
			write(t, env.Home, tt.home)
			write(t, env.ConfigDir, tt.config)
			where, ok := Configured(agent(t, tt.agent), env)
			if tt.want == "config" {
				tt.want = filepath.Join(env.ConfigDir, "Code", "User", "mcp.json")
			}
			if ok != (tt.want != "") || where != tt.want {
				t.Errorf("Configured = %q, %v; want %q", where, ok, tt.want)
			}
		})
	}
	// The project's entry in ~/.claude.json is Claude Code's local scope.
	env := testEnv(t)
	write(t, env.Home, map[string]string{".claude.json": `{"projects": {"` + filepath.ToSlash(env.Project) + `": {"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}}}`})
	if runtime.GOOS != "windows" {
		if where, ok := Configured(agent(t, "claude"), env); !ok || where != "~/.claude.json" {
			t.Errorf("local scope: %q, %v", where, ok)
		}
	}
}
