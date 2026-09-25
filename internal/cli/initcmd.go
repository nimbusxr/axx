package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/packset"
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
      - uses: nimbusxr/setup-axx@v1
      - run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: axx-report
          path: build/axx/
`,
}

var agentsBlock = agentsBegin + `
## Acceptance tests (axx)
axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework.
- Feature files are acceptance criteria a person can read: one scenario per criterion, in plain
  language, using the steps exactly as written. No programming constructs in Gherkin.
- Find steps before writing: ` + "`axx steps search \"<intent>\"`" + `; never invent step text.
- Steps come from the packs in ` + "`axx-packs.yaml`" + `; ` + "`axx pack list`" + ` shows the others, ` + "`axx pack add <name>`" + ` adds one.
- Check without running: ` + "`axx validate`" + `. Explain one line: ` + "`axx explain \"<step>\"`" + `.
- Check test data: ` + "`axx lint`" + ` reports ids and keys that collide across seed and fixture files.
- Run: ` + "`axx up`" + ` once (keeps apps running), then ` + "`axx run --compact`" + `; ` + "`axx down`" + ` when done.
- Every scenario uses unique data (IDs, names, keys): scenarios run in parallel and data persists.
- Features live in ` + "`features/`" + `; configuration in ` + "`axx.yaml`" + ` (schema: ` + "`axx schema`" + `).
- Diagnose failures from the report: ` + "`axx run --json`" + ` includes expected/actual and a rerun command.
` + agentsEnd + "\n"

func newInitCmd(app *App) *cobra.Command {
	var dryRun, force, noCI bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up axx in this project: axx.yaml, its packs, a smoke feature, CI and AGENTS.md",
		Long: `Create axx.yaml, axx-packs.yaml (with the rest pack, which the smoke feature
uses), features/smoke.feature, a GitHub Actions workflow and a managed section
in AGENTS.md. Existing files are left alone (use --force to
overwrite); the AGENTS.md section is updated in place. Safe to re-run.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			data := detect(wd)
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
			agents, action := mergeAgents(filepath.Join(wd, "AGENTS.md"))
			plan = append(plan, InitFile{Path: "AGENTS.md", Action: action, Reason: upToDate(action)})
			contents["AGENTS.md"] = agents
			gi, giAction := mergeGitignore(filepath.Join(wd, ".gitignore"))
			plan = append(plan, InitFile{Path: ".gitignore", Action: giAction, Reason: upToDate(giAction)})
			contents[".gitignore"] = gi

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
			return app.Emit(map[string]any{"dryRun": dryRun, "files": plan, "detected": data}, func(w io.Writer) error {
				verb := map[bool]string{true: "would ", false: ""}[dryRun]
				for _, f := range plan {
					if f.Action == "skip" {
						fmt.Fprintf(w, "  skip    %s: %s\n", f.Path, f.Reason)
						continue
					}
					fmt.Fprintf(w, "  %s%-7s %s\n", verb, f.Action, f.Path)
				}
				if !dryRun {
					fmt.Fprintln(w, "\nnext: edit apps in axx.yaml, then `axx doctor` and `axx run`")
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing axx.yaml, axx-packs.yaml, feature and workflow files")
	cmd.Flags().BoolVar(&noCI, "no-ci", false, "do not create a GitHub Actions workflow")
	return cmd
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

func upToDate(action string) string {
	if action == "skip" {
		return "up to date"
	}
	return ""
}
