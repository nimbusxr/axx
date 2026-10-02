package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/agents"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/skills"
)

// InitFile is one file `axx init` writes (or would write).
type InitFile struct {
	Path   string `json:"path"`
	Action string `json:"action"` // create | update | skip
	Reason string `json:"reason,omitempty"`
}

type initData struct {
	SchemaID    string
	ComposeFile string
	OpenAPI     string
	Port        string
	AppName     string
}

const (
	agentsBegin = "<!-- axx:begin (managed by `axx init`; edit outside this block) -->"
	agentsEnd   = "<!-- axx:end -->"
)

var initTemplates = map[string]string{
	"axx.yaml": `# yaml-language-server: $schema={{.SchemaID}}
# axx configuration: https://axx.nimbusxr.us/references/config/
version: 1

run:
  paths: [features]
  # exclusive: ["@isolated"]   # scenarios with these tags run alone, after the parallel ones

# Properties back ${sys:name} in steps and here; override with -D name=value.
properties:
  local.host: localhost

apps:
  {{.AppName}}:
{{- if .ComposeFile}}
    # Starts everything in {{.ComposeFile}}; stopped (and cleaned up) after the run.
    command: docker compose -f {{.ComposeFile}} up --build
    cleanup: docker compose -f {{.ComposeFile}} down -v --remove-orphans
{{- else}}
    # The command that starts your service locally (argv, no shell; set shell: true for pipes).
    command: echo "replace with the command that starts your app" && exit 1
    shell: true
{{- end}}
    ready:
      http:
        url: http://${sys:local.host}:{{.Port}}/health
      timeout: 120s
      interval: 1s
`,
	packset.FileName: (&packset.File{Packs: []string{"rest"}}).String(),
	"features/smoke.feature": `Feature: Smoke test
  The service starts and answers requests.

  Background:
    Given the {{.AppName}} service with the following properties:
      | url     | http://${sys:local.host}:{{.Port}} |
{{- if .OpenAPI}}
      | openapi | {{.OpenAPI}} |
{{- end}}

  Scenario: the service is healthy
    Given a GET request to /health
    When the request is executed
    Then the response status code is 200
`,
	".github/workflows/acceptance.yml": `name: acceptance

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  acceptance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: nimbusxr/setup-axx@v0
      - run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
      - uses: actions/upload-artifact@v7
        if: always()
        with:
          name: axx-report
          path: build/axx/
`,
}

var agentsBlock = agentsBegin + `
## Acceptance tests (axx)
axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework.
- Use the axx MCP tools when you have them (steps_search, step_explain, feature_validate, lint_run,
  scenarios_run, failure_context, env, steps_try); otherwise the axx commands below.
- If ` + "`.agents/skills`" + ` has the axx skills, they teach the details: axx-acceptance-tests (write and
  run tests), axx-test-data (fixture factories), axx-debugging (failures). ` + "`axx skills install`" + `
  installs or updates them.
- Feature files are acceptance criteria a person can read: one scenario per criterion, in plain
  language, using the steps exactly as written. No programming constructs in Gherkin.
- Find steps before writing: ` + "`axx steps search \"<intent>\"`" + `; never invent step text.
- Steps come from the packs in ` + "`axx-packs.yaml`" + `; ` + "`axx pack list`" + ` shows the others, ` + "`axx pack add <name>`" + ` adds one.
- Check without running: ` + "`axx validate`" + `. Explain one line: ` + "`axx explain \"<step>\"`" + `.
- Ask axx rather than reading files: ` + "`axx doctor --json`" + ` (prerequisites, packs, agents),
  ` + "`axx validate --json`" + ` (features, scenarios), ` + "`axx fixtures generate --dry-run --json`" + ` (the files the factories generate),
  ` + "`axx fixtures explain <file> <path>`" + ` (the source that sets a generated value).
- Run: ` + "`axx up`" + ` once (keeps apps running), then ` + "`axx run --compact`" + `; ` + "`axx down`" + ` when done.
- Every scenario uses unique data (IDs, names, keys): scenarios run in parallel and data persists.
- Test data: when payloads, seeds or mock bodies repeat, generate them with fixture factories
  (` + "`axx fixtures`" + `; optional, strongly recommended where data repeats); ` + "`axx fixtures adopt`" + ` turns
  existing hand-written ones into a factory. ` + "`axx lint`" + ` reports ids and keys that collide across files.
- Features live in ` + "`features/`" + `; configuration in ` + "`axx.yaml`" + ` (schema: ` + "`axx schema`" + `).
- Diagnose failures from the report: ` + "`axx run --json`" + ` includes expected/actual and a rerun command;
  the MCP tool failure_context reads the latest scenarios_run when given no run ID.
` + agentsEnd + "\n"

// InitAgents is what `axx init` set up for coding agents.
type InitAgents struct {
	// Detected are the agents the repository shows signs of using.
	Detected []string `json:"detected"`
	// Skills are where the skills were installed: .agents/skills, and
	// .claude/skills for Claude Code.
	Skills []InitFile `json:"skills"`
	// MCP are the agents' MCP configurations: written, already there, or
	// (Codex, whose servers live in the home directory) the command that adds axx.
	MCP []agents.Change `json:"mcp"`
}

func newInitCmd(app *App) *cobra.Command {
	var dryRun, force, noCI, noAgents bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up axx in this project: axx.yaml, its packs, a smoke feature, CI, AGENTS.md and agents",
		Long: `Create axx.yaml, axx-packs.yaml (with the rest pack, which the smoke feature
uses), features/smoke.feature, a GitHub Actions workflow and a managed section
in AGENTS.md. Existing files are left alone (use --force to
overwrite); the AGENTS.md section is updated in place. Safe to re-run.

It also sets up coding agents, in this repository only: it installs the skills
in .agents/skills (marked generated in .gitattributes, so their diffs collapse
in pull requests), and connects the axx MCP server to the agents the repository
already uses, in their own files (Claude Code: .claude/, CLAUDE.md or .mcp.json
→ .mcp.json and .claude/skills; Codex: .codex/ → .codex/config.toml; Cursor:
.cursor/ → .cursor/mcp.json; VS Code: .vscode/ → .vscode/mcp.json; Gemini CLI:
.gemini/ or GEMINI.md → .gemini/settings.json). Other servers and settings in
those files stay. It never writes your home directory: for Codex, whose servers
usually live in ~/.codex/config.toml, it prints the command that adds axx there.
--no-agents skips all of this.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			data := detect(wd)
			var used []agents.Agent
			if !noAgents {
				used = agents.Detect(wd) // before init writes anything
			}
			var plan []InitFile
			contents := map[string]string{}
			for _, name := range []string{"axx.yaml", packset.FileName, "features/smoke.feature", ".github/workflows/acceptance.yml"} {
				if noCI && strings.HasPrefix(name, ".github/") {
					continue
				}
				out, err := render(initTemplates[name], data)
				if err != nil {
					return err
				}
				f := InitFile{Path: name, Action: "create"}
				if _, err := os.Stat(filepath.Join(wd, name)); err == nil {
					if force {
						f.Action = "update"
					} else {
						f.Action, f.Reason = "skip", "already exists (use --force to overwrite)"
					}
				}
				plan = append(plan, f)
				contents[name] = out
			}
			agentsMD, action := mergeAgents(filepath.Join(wd, "AGENTS.md"))
			plan = append(plan, InitFile{Path: "AGENTS.md", Action: action, Reason: upToDate(action)})
			contents["AGENTS.md"] = agentsMD
			gi, giAction := mergeGitignore(filepath.Join(wd, ".gitignore"))
			plan = append(plan, InitFile{Path: ".gitignore", Action: giAction, Reason: upToDate(giAction)})
			contents[".gitignore"] = gi
			if !noAgents {
				ga, gaAction := mergeGitattributes(filepath.Join(wd, ".gitattributes"))
				plan = append(plan, InitFile{Path: ".gitattributes", Action: gaAction, Reason: upToDate(gaAction)})
				contents[".gitattributes"] = ga
			}

			if !dryRun {
				for _, f := range plan {
					if f.Action == "skip" {
						continue
					}
					p := filepath.Join(wd, filepath.FromSlash(f.Path))
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(p, []byte(contents[f.Path]), 0o644); err != nil {
						return err
					}
				}
			}
			result := map[string]any{"dryRun": dryRun, "files": plan, "detected": data}
			var setup *InitAgents
			if !noAgents {
				setup = app.initAgents(cmd.Context(), wd, used, dryRun)
				result["agents"] = setup
			}
			return app.Emit(result, func(w io.Writer) error {
				return renderInit(w, plan, setup, dryRun)
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing axx.yaml, axx-packs.yaml, feature and workflow files")
	cmd.Flags().BoolVar(&noCI, "no-ci", false, "do not create a GitHub Actions workflow")
	cmd.Flags().BoolVar(&noAgents, "no-agents", false, "do not install the skills or connect agents to the axx MCP server")
	return cmd
}

func renderInit(w io.Writer, plan []InitFile, setup *InitAgents, dryRun bool) error {
	verb := map[bool]string{true: "would ", false: ""}[dryRun]
	line := func(f InitFile, note string) {
		if f.Action == "skip" {
			fmt.Fprintf(w, "  skip    %s: %s\n", f.Path, f.Reason)
			return
		}
		if note != "" {
			note = " (" + note + ")"
		}
		fmt.Fprintf(w, "  %s%-7s %s%s\n", verb, f.Action, f.Path, note)
	}
	for _, f := range plan {
		line(f, "")
	}
	var manual []string
	if setup != nil {
		for _, f := range setup.Skills {
			line(f, f.Reason)
		}
		for _, ch := range setup.MCP {
			a, _ := agents.Lookup(ch.Agent)
			switch ch.Action {
			case agents.ActionManual:
				manual = append(manual, fmt.Sprintf("%s keeps its MCP servers in %s; `%s` adds axx there.", a.Name, ch.Path, ch.Command))
			case agents.ActionSkip:
				reason := ch.Reason
				if ch.Command != "" {
					reason += " (see `" + ch.Command + "`)"
				}
				line(InitFile{Path: ch.Path, Action: ch.Action, Reason: reason}, "")
			default:
				line(InitFile{Path: ch.Path, Action: ch.Action}, serverNote(a, ch))
			}
		}
	}
	if !dryRun {
		fmt.Fprintln(w)
		for _, m := range manual {
			fmt.Fprintln(w, m)
		}
		fmt.Fprintln(w, "next: edit apps in axx.yaml, then `axx doctor` and `axx run`")
	}
	return nil
}

// initAgents installs the skills and connects the agents the project uses
// to the axx MCP server, in the project's own files.
func (a *App) initAgents(ctx context.Context, dir string, used []agents.Agent, dryRun bool) *InitAgents {
	out := &InitAgents{Detected: []string{}, MCP: []agents.Change{}}
	claude, codex := false, false
	for _, ag := range used {
		out.Detected = append(out.Detected, ag.ID)
		claude = claude || ag.ID == "claude"
		codex = codex || ag.ID == "codex"
	}
	out.Skills = a.initSkills(ctx, dir, claude, dryRun)
	env := agents.NewEnv(dir)
	for _, ag := range used {
		ch, err := agents.Install(ag, agents.ScopeProject, env, dryRun)
		if err != nil {
			// Leave the file alone; the command says what to add by hand.
			reason := firstLine(err)
			if cause := errors.Unwrap(err); cause != nil {
				reason = "left alone: " + firstLine(cause)
			}
			ch.Action, ch.Reason, ch.Command = agents.ActionSkip, reason, "axx mcp install --agent "+ag.ID
			if ch.Path == "" {
				ch.Path = ag.Project
			}
		}
		out.MCP = append(out.MCP, ch)
	}
	if cx, _ := agents.Lookup("codex"); !codex {
		// Codex usually keeps its servers in the home directory, which init
		// never writes: say how to add axx there, unless it is there.
		if _, ok := agents.Configured(cx, env); !ok {
			out.MCP = append(out.MCP, agents.Change{
				Agent: cx.ID, Scope: agents.ScopeUser, Path: env.Display(cx.Path(agents.ScopeUser, env)),
				Action: agents.ActionManual, Command: "axx mcp install --agent codex --scope user",
				Reason: "Codex keeps its MCP servers in your home directory",
			})
		}
	}
	return out
}

// initSkills installs the skills as `axx skills install` does, linking them
// for Claude Code only when the project uses it.
func (a *App) initSkills(ctx context.Context, dir string, claude, dryRun bool) []InitFile {
	existed := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
		return err == nil
	}
	skillsAction, claudeAction := "create", "create"
	if existed(".agents/skills") {
		skillsAction = "update"
	}
	if existed(".claude/skills") {
		claudeAction = "update"
	}
	n := len(skills.Names())
	if dryRun {
		out := []InitFile{{Path: ".agents/skills", Action: skillsAction, Reason: plural(n, "skill")}}
		if claude {
			out = append(out, InitFile{Path: ".claude/skills", Action: claudeAction, Reason: "linked for Claude Code"})
		}
		return out
	}
	res, err := a.installProjectSkills(ctx, dir, claude)
	if err != nil {
		return []InitFile{{Path: ".agents/skills", Action: "skip", Reason: firstLine(err) + "; run `axx skills install` when it is fixed"}}
	}
	f := InitFile{Path: ".agents/skills", Action: skillsAction, Reason: plural(len(res.Skills), "skill")}
	if len(res.Written) == 0 {
		f.Action, f.Reason = "skip", "up to date"
	}
	if len(res.Kept) > 0 {
		f.Reason += fmt.Sprintf("; kept %s you edited (`axx skills install --force` replaces them)", plural(len(res.Kept), "file"))
	}
	out := []InitFile{f}
	if claude {
		c := InitFile{Path: ".claude/skills", Action: claudeAction, Reason: "linked for Claude Code"}
		if len(res.Linked) == 0 {
			c.Action, c.Reason = "skip", "up to date"
		}
		out = append(out, c)
	}
	return out
}

// installProjectSkills installs the skills with the project's steps. An axx
// without the project's packs has the axx prepared with them do it, as
// `axx skills install` does.
func (a *App) installProjectSkills(ctx context.Context, dir string, claude bool) (*skills.Result, error) {
	if _, missing := missingPacks(dir); !missing {
		e, err := a.loadEngine(&configFlags{})
		if err != nil {
			return nil, err
		}
		sks, err := skills.Build(e)
		if err != nil {
			return nil, err
		}
		return skills.Install(sks, skills.InstallOptions{Root: dir, Claude: claude})
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"skills", "install", "--json", "--no-claude"}
	if claude {
		args[3] = "--claude"
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir, cmd.Stderr = dir, a.Stderr
	stdout, runErr := cmd.Output()
	var env struct {
		OK     bool            `json:"ok"`
		Data   skills.Result   `json:"data"`
		Errors []EnvelopeError `json:"errors"`
	}
	switch {
	case json.Unmarshal(stdout, &env) != nil:
		if runErr == nil {
			runErr = errors.New("axx skills install printed no result")
		}
		return nil, runErr
	case !env.OK && len(env.Errors) > 0:
		return nil, errors.New(env.Errors[0].Message)
	case !env.OK:
		return nil, errors.New("axx skills install failed")
	}
	return &env.Data, nil
}

// firstLine is an error's first line.
func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSuffix(s, ":")
}

func detect(dir string) initData {
	d := initData{SchemaID: config.SchemaID, Port: "8080", AppName: "app"}
	for _, n := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			d.ComposeFile = n
			break
		}
	}
	for _, n := range []string{"openapi.yaml", "openapi.yml", "openapi.json", "api/openapi.yaml", "docs/openapi.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			d.OpenAPI = n
			break
		}
	}
	if base := filepath.Base(dir); base != "" && base != "." && base != "/" {
		d.AppName = sanitizeName(base)
	}
	return d
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_' || r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func render(tmpl string, data initData) (string, error) {
	t, err := template.New("init").Delims("{{", "}}").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// mergeAgents inserts or replaces the managed block in AGENTS.md.
func mergeAgents(path string) (string, string) {
	old, err := os.ReadFile(path)
	if err != nil {
		return "# AGENTS.md\n\n" + agentsBlock, "create"
	}
	s := string(old)
	if i := strings.Index(s, agentsBegin); i >= 0 {
		if j := strings.Index(s[i:], agentsEnd); j >= 0 {
			end := i + j + len(agentsEnd)
			if end < len(s) && s[end] == '\n' {
				end++
			}
			updated := s[:i] + agentsBlock + s[end:]
			if updated == s {
				return s, "skip"
			}
			return updated, "update"
		}
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s + "\n" + agentsBlock, "update"
}

func mergeGitignore(path string) (string, string) {
	old, err := os.ReadFile(path)
	if err != nil {
		return "# axx runtime state and logs\n.axx/\n", "create"
	}
	s := string(old)
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == ".axx/" || strings.TrimSpace(line) == ".axx" {
			return s, "skip"
		}
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s + "\n# axx runtime state and logs\n.axx/\n", "update"
}

// skillsAttributes marks the skills as generated: GitHub collapses their diffs
// in pull requests, which every axx upgrade or new step changes.
const skillsAttributes = "# axx's agent skills, which `axx skills install` writes\n.agents/skills/** linguist-generated\n"

func mergeGitattributes(path string) (string, string) {
	old, err := os.ReadFile(path)
	if err != nil {
		return skillsAttributes, "create"
	}
	s := string(old)
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && strings.HasPrefix(f[0], ".agents/") {
			for _, attr := range f[1:] {
				if attr == "linguist-generated" || attr == "linguist-generated=true" {
					return s, "skip"
				}
			}
		}
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s + "\n" + skillsAttributes, "update"
}

func upToDate(action string) string {
	if action == "skip" {
		return "up to date"
	}
	return ""
}
