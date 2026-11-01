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
	"slices"
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
	// Stack is the compose project the service runs as: its own, so that
	// it never takes over (or, cleaning up, deletes the volumes of) the
	// stack someone develops with from the same file.
	Stack string
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

services:
  {{.AppName}}:
{{- if .ComposeFile}}
    # Starts everything in {{.ComposeFile}} as a stack of its own, not the one you develop
    # with; stopped after the run, and the stack's volumes removed.
    command: docker compose -f {{.ComposeFile}} -p {{.Stack}} up --build
    cleanup: docker compose -f {{.ComposeFile}} -p {{.Stack}} down -v --remove-orphans
{{- else}}
    # The command that starts your service locally (argv, no shell; set shell: true for pipes).
    command: echo "replace with the command that starts your app" && exit 1
    shell: true
{{- end}}
    ready:
      http:
        # A guess: your service's port and health check (the smoke feature calls them too).
        url: http://${sys:local.host}:{{.Port}}/health
      timeout: 120s
      interval: 1s
`,
	packset.FileName: (&packset.File{Packs: []string{"rest"}}).String(),
	"features/smoke/smoke.feature": `Feature: Smoke test
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

// agentsBlock is the AGENTS.md section: the one guide an agent always has in
// its context, so it holds the workflow and the rules that agents otherwise
// learn by failing (each is a warning or a failure axx reports), and points
// to what axx answers for itself; the skills add only the specialized parts.
var agentsBlock = agentsBegin + "\n" + `## Acceptance tests (axx)
axx (github.com/nimbusxr/axx, "axxeptance") runs acceptance criteria, written as Gherkin scenarios, black-box against the
running services. It answers for itself, offline and for the version installed: no need for its website or source.

1. Look at the project first: its README and docs, ` + "`axx.yaml`" + ` (the services), ` + "`axx-packs.yaml`" + ` (the packs), ` + "`features/`" + `.
2. Find the steps: read the list of every step of the project's packs once, then the documentation of those you use.
   With the axx MCP tools: ` + "`steps_search`" + ` without a query, then ` + "`step_explain`" + ` with ids (` + "`steps_try`" + ` tries steps in a live
   scenario). Without them: ` + "`axx steps`" + `, then ` + "`axx steps show <id>...`" + `. A step the project's packs lack:
   ` + "`axx steps search <words>`" + ` finds it in axx's other packs and names the pack (` + "`axx pack add <name>`" + `).
3. Write one scenario per acceptance criterion with those steps, written out: each {name} replaced with a value, a(n) as
   "a" or "an", [[...]] kept without the brackets or left out. Never invent step text.
4. Check, and fix what is reported: ` + "`axx validate`" + ` (needs no services) and ` + "`axx lint`" + ` (the test-data rules), then ` + "`axx up`" + `
   once and ` + "`axx run --compact`" + ` (it reports what validate and lint find too); ` + "`axx down`" + ` when done. With the MCP tools:
   ` + "`feature_validate`" + `, ` + "`lint_run`" + `, ` + "`scenarios_run`" + `, ` + "`failure_context`" + `. An error's code: ` + "`axx explain AXX-E…`" + `.

Rules axx checks, and agents get wrong:
- A scenario is one behavior: Given the state it starts from (a request it needs done first is executed in a Given),
  one When for what someone does, Then what is true afterwards. A request that checks the outcome (a lookup that finds
  nothing stored) is added with the others and executed in the When. A When after a Then is another scenario.
  Each acceptance criterion is a scenario of its own, even when one action shows several.
- Ordinals count in the order a scenario adds requests, responses and selections: the 2nd is its second; none, the 1st.
- Every scenario uses data of its own (ids, names, keys): scenarios run in parallel and data persists between runs.
- A value means what it says in every table: ` + "`53111`" + ` a number, ` + "`\"53111\"`" + ` the string, ` + "`null`" + `, ` + "`undefined`" + ` (no such property).
  Checks are strict; an empty cell, ` + "`01067`" + ` or a number where only text fits (a header) is an error saying what to write.
- Check what the service did (a response property, a row, an event) and why it refused, not only status codes.
- Feature files read as acceptance criteria, in plain language: no programming constructs, no variables.
- ` + "`services:`" + ` in axx.yaml are what runs on its own, reached by an address; a program its user starts (a CLI, an MCP
  server over stdio) is run by its step's ` + "`command`" + `. ` + "`axx schema --outline`" + ` lists axx.yaml's keys.
- Payloads, seeds or mock bodies that repeat: fixture factories generate them (` + "`axx fixtures`" + `; optional, recommended where
  data repeats); ` + "`axx fixtures adopt`" + ` turns hand-written ones into one.

If ` + "`.agents/skills`" + ` has the axx skills, they add what this leaves out: axx-acceptance-tests (feature shape, payload
tables), axx-test-data (fixture factories), axx-debugging (failures), axx-setup, axx-custom-steps. ` + "`axx skills install`" + `
installs or updates them.
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
uses), features/smoke/smoke.feature, a GitHub Actions workflow and a managed section
in AGENTS.md. Existing files are left alone (use --force to
overwrite); the AGENTS.md section is updated in place. Safe to re-run.

The service in axx.yaml is a guess to replace: with a compose file, it starts
that file as a stack of its own (` + "`-p <name>-axx`" + `), whose cleanup removes the stack's
volumes but never those of the stack you develop with; the port (8080) and health
check (/health), in axx.yaml and the smoke feature, are placeholders.

It also sets up coding agents, in this repository only: it installs the skills
in .agents/skills (marked generated in .gitattributes, so their diffs collapse
in pull requests), and connects the axx MCP server to the agents the repository
already uses, in their own files (Claude Code: .claude/, CLAUDE.md or .mcp.json
→ .mcp.json and .claude/skills; Codex: .codex/ → .codex/config.toml; Cursor:
.cursor/ → .cursor/mcp.json; VS Code: .vscode/ → .vscode/mcp.json; Gemini CLI:
.gemini/ or GEMINI.md → .gemini/settings.json). Other servers and settings in
those files stay. Claude Code reads CLAUDE.md instead of AGENTS.md when this
folder or one above it has a CLAUDE.md, .claude/CLAUDE.md or CLAUDE.local.md:
there, init imports AGENTS.md into the project's CLAUDE.md (@AGENTS.md). It
never writes your home directory: for Codex, whose servers usually live in
~/.codex/config.toml, it prints the command that adds axx there.
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
			for _, name := range []string{"axx.yaml", packset.FileName, "features/smoke/smoke.feature", ".github/workflows/acceptance.yml"} {
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
			if slices.ContainsFunc(used, func(a agents.Agent) bool { return a.ID == "claude" }) {
				home, _ := os.UserHomeDir()
				if name, md, action := mergeClaude(wd, home); name != "" {
					reason := upToDate(action)
					if action != "skip" {
						reason = "imports AGENTS.md for Claude Code"
					}
					plan = append(plan, InitFile{Path: name, Action: action, Reason: reason})
					contents[name] = md
				}
			}
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
			// The packs the repository points to, beyond those it lists: suggested,
			// never added (an agent or a person decides).
			have := []string{"rest"}
			if f, ok, err := packset.Load(wd); err == nil && ok {
				have = f.Packs
			}
			suggested := suggestPacks(wd, have)
			result := map[string]any{"dryRun": dryRun, "files": plan, "detected": data, "suggestedPacks": suggested}
			var setup *InitAgents
			if !noAgents {
				setup = app.initAgents(cmd.Context(), wd, used, dryRun)
				result["agents"] = setup
			}
			return app.Emit(result, func(w io.Writer) error {
				return renderInit(w, plan, setup, suggested, dryRun)
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing axx.yaml, axx-packs.yaml, feature and workflow files")
	cmd.Flags().BoolVar(&noCI, "no-ci", false, "do not create a GitHub Actions workflow")
	cmd.Flags().BoolVar(&noAgents, "no-agents", false, "do not install the skills or connect agents to the axx MCP server")
	return cmd
}

func renderInit(w io.Writer, plan []InitFile, setup *InitAgents, suggested []PackSuggestion, dryRun bool) error {
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
		line(f, f.Reason)
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
	if len(suggested) > 0 {
		fmt.Fprintln(w, "\npacks this repository points to, not added (`axx pack show <name>` says what one does):")
		names := make([]string, 0, len(suggested))
		for _, s := range suggested {
			fmt.Fprintf(w, "  %-10s %s\n", s.Pack, strings.Join(s.Reasons, "; "))
			names = append(names, s.Pack)
		}
		fmt.Fprintf(w, "  add those it tests with `axx pack add %s`\n", strings.Join(names, " "))
	}
	if !dryRun {
		fmt.Fprintln(w)
		for _, m := range manual {
			fmt.Fprintln(w, m)
		}
		fmt.Fprintln(w, "next: put your service's command, port and health check in axx.yaml and the smoke feature, then `axx doctor` and `axx run`")
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
	// A compose project's name starts with a letter or a digit.
	d.Stack = "axx"
	if name := strings.TrimLeft(d.AppName, "-"); name != "" {
		d.Stack = name + "-axx"
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

// claudeFiles are the files Claude Code reads instead of AGENTS.md when a
// folder or one above it has any of them (the user's ~/.claude/CLAUDE.md aside).
var claudeFiles = []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md"), "CLAUDE.local.md"}

// mergeClaude imports AGENTS.md into the project's CLAUDE.md (or
// .claude/CLAUDE.md, or a new CLAUDE.md) when Claude Code would read CLAUDE.md
// instead of AGENTS.md, so that it reads the axx section. It returns the file,
// relative to dir, or "" when Claude Code reads AGENTS.md itself.
func mergeClaude(dir, home string) (string, string, string) {
	if !claudeSkipsAgents(dir, home) {
		return "", "", ""
	}
	agentsMD := filepath.Join(dir, "AGENTS.md")
	target := ""
	for _, name := range claudeFiles[:2] { // CLAUDE.local.md is the user's own
		p := filepath.Join(dir, name)
		if sameFile(p, agentsMD) {
			return filepath.ToSlash(name), "", "skip"
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if importsFile(string(b), p, agentsMD) {
			return filepath.ToSlash(name), "", "skip"
		}
		if target == "" {
			target = name
		}
	}
	if target == "" {
		return "CLAUDE.md", "@AGENTS.md\n", "create"
	}
	b, _ := os.ReadFile(filepath.Join(dir, target))
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if s != "" {
		s += "\n"
	}
	rel, _ := filepath.Rel(filepath.Dir(filepath.Join(dir, target)), agentsMD)
	return filepath.ToSlash(target), s + "@" + filepath.ToSlash(rel) + "\n", "update"
}

// claudeSkipsAgents reports whether Claude Code, working in dir, reads
// CLAUDE.md files instead of AGENTS.md: whether dir or a folder above it has
// one of claudeFiles.
func claudeSkipsAgents(dir, home string) bool {
	var user os.FileInfo
	if home != "" {
		user, _ = os.Stat(filepath.Join(home, ".claude", "CLAUDE.md"))
	}
	for d := dir; ; d = filepath.Dir(d) {
		for _, name := range claudeFiles {
			fi, err := os.Stat(filepath.Join(d, name))
			if err == nil && !fi.IsDir() && (user == nil || !os.SameFile(fi, user)) {
				return true
			}
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// importsFile reports whether the instructions in file import target with an
// @path outside code, as Claude Code reads them.
func importsFile(content, file, target string) bool {
	fenced := false
	for _, line := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		spans := strings.Split(line, "`")
		for i := 0; i < len(spans); i += 2 { // the odd ones are code spans
			for _, f := range strings.Fields(spans[i]) {
				p, ok := strings.CutPrefix(f, "@")
				if !ok || p == "" {
					continue
				}
				if !filepath.IsAbs(p) {
					p = filepath.Join(filepath.Dir(file), p)
				}
				if filepath.Clean(p) == target {
					return true
				}
			}
		}
	}
	return false
}

// sameFile reports whether p is AGENTS.md by another name: a link to it, or
// the file it links to.
func sameFile(p, agentsMD string) bool {
	if t, err := os.Readlink(p); err == nil {
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(p), t)
		}
		if filepath.Clean(t) == agentsMD {
			return true
		}
	}
	a, err := os.Stat(p)
	if err != nil {
		return false
	}
	b, err := os.Stat(agentsMD)
	return err == nil && os.SameFile(a, b)
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
