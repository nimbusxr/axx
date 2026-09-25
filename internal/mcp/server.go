// Package mcp serves axx to coding agents over the Model Context Protocol
// (stdio). The tools reuse the same engine and code paths as the CLI, so an
// agent sees exactly what `axx steps`, `axx explain`, `axx validate`,
// `axx lint` and `axx run --json` report.
package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	messages "github.com/cucumber/messages/go/v34"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/internal/check"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/version"
)

// Instructions are sent to clients on connect.
const Instructions = `axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework for the agentic era.

Workflow for writing acceptance tests:
1. steps_search for every action/assertion you need; use only step text that exists (never invent steps).
2. Write the .feature file; feature_validate it until there are no problems (step_explain shows how a single line is read).
3. env {"action":"up"} once to keep the apps running, then scenarios_run.
4. On failure, read the returned failures (expected/actual); failure_context gives logs and request/response details.
Every scenario must use unique test data (ids, names, keys): scenarios run in parallel and data persists between runs. After adding seeds, payloads or fixtures, lint_run reports values that collide with other files.`

// Options configures the server.
type Options struct {
	// ConfigPath is --config (empty: discover from WorkDir).
	ConfigPath string
	Profile    string
	WorkDir    string
	// Exe is the axx binary used for runs (default os.Executable()).
	Exe string
}

type server struct {
	opts Options
}

// New builds the MCP server.
func New(opts Options) *sdk.Server {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	if opts.Exe == "" {
		opts.Exe, _ = os.Executable()
	}
	s := &server{opts: opts}
	srv := sdk.NewServer(&sdk.Implementation{Name: "axx", Title: "axx acceptance testing", Version: version.Get().Version},
		&sdk.ServerOptions{Instructions: Instructions})

	ro := &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	sdk.AddTool(srv, &sdk.Tool{
		Name: "steps_search", Annotations: ro,
		Description: "Find Gherkin steps by intent (e.g. 'response status', 'seed database', 'kafka event published'). Returns step expressions with docs and examples. Use before writing any step.",
	},
		s.stepsSearch)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "step_explain", Annotations: ro,
		Description: "Explain how axx reads one step line: the matching definition and captured arguments, the candidates if ambiguous, or the closest steps if undefined.",
	},
		s.stepExplain)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "feature_validate", Annotations: ro,
		Description: "Check feature files (by path) or feature text (content) without running them: Gherkin syntax, undefined and ambiguous steps, data table/doc string arguments.",
	},
		s.featureValidate)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "lint_run", Annotations: ro,
		Description: "Run `axx lint`: the test-data isolation rules from axx.yaml (values such as seed ids that must be unique across files) and builtin feature checks (SQL selection/trigger ordinals). Returns every finding with file:line and the colliding value.",
	},
		s.lintRun)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "scenarios_run",
		Description: "Run scenarios (all, or paths like features/x.feature:14, filtered by tags/name). Starts apps unless they are already up. Returns counts and failures with expected/actual; use failure_context for details.",
	},
		s.scenariosRun)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "failure_context", Annotations: ro,
		Description: "Full details of one failed scenario from a previous scenarios_run: logs, attachments and pack context such as the last HTTP request/response.",
	},
		s.failureContext)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "env",
		Description: "Manage the apps from axx.yaml: 'up' starts them and keeps them running between runs (fast loop), 'down' stops them, 'status' reports what is running.",
	},
		s.env)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "config_show", Annotations: ro,
		Description: "The effective axx.yaml (profiles and -D applied, secrets redacted), plus the loaded step packs.",
	},
		s.configShow)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "scaffold", Annotations: ro,
		Description: "Return starter file contents for a 'feature' (from real steps) or an 'axx.yaml'. Nothing is written; create the file yourself.",
	},
		s.scaffold)

	srv.AddResource(&sdk.Resource{URI: "axx://schemas/axx.yaml", Name: "axx.yaml JSON Schema", MIMEType: "application/schema+json"},
		func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: "axx://schemas/axx.yaml", MIMEType: "application/schema+json", Text: string(config.SchemaJSON)}}}, nil
		})
	srv.AddPrompt(&sdk.Prompt{
		Name: "write-acceptance-tests", Description: "Turn acceptance criteria into axx feature files",
		Arguments: []*sdk.PromptArgument{{Name: "criteria", Description: "The acceptance criteria (plain text)", Required: true}},
	},
		func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: writeTestsPrompt(req.Params.Arguments["criteria"])}}}}, nil
		})
	return srv
}

// Serve runs the server on stdin/stdout until the client disconnects.
func Serve(ctx context.Context, opts Options) error {
	return New(opts).Run(ctx, &sdk.StdioTransport{})
}

func (s *server) engine() (*engine.Engine, error) {
	cfg, err := config.Load(config.LoadOptions{Path: s.opts.ConfigPath, WorkDir: s.opts.WorkDir, Profile: s.opts.Profile})
	if err != nil {
		return nil, err
	}
	return engine.New(engine.Options{Config: cfg})
}

// ---- steps_search ----

type stepsSearchIn struct {
	Query string `json:"query" jsonschema:"what the step should do, e.g. 'response header', 'rows in table'"`
	Pack  string `json:"pack,omitempty" jsonschema:"limit to one pack: rest, mock, sql, mongo, kafka, logs or a custom pack"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results (default 10)"`
}

// Step is a step definition as seen by agents.
type Step struct {
	ID       string   `json:"id"`
	Pack     string   `json:"pack"`
	Expr     string   `json:"expr"`
	Variants []string `json:"variants"`
	Arg      string   `json:"argument"`
	Doc      string   `json:"doc,omitempty"`
	Examples []string `json:"examples,omitempty"`
	Params   []string `json:"params,omitempty"`
}

type stepsSearchOut struct {
	Steps []Step `json:"steps"`
	Hint  string `json:"hint,omitempty"`
}

func (s *server) stepsSearch(ctx context.Context, _ *sdk.CallToolRequest, in stepsSearchIn) (*sdk.CallToolResult, stepsSearchOut, error) {
	e, err := s.engine()
	if err != nil {
		return nil, stepsSearchOut{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	out := stepsSearchOut{Steps: searchSteps(e, in.Query, in.Pack, limit)}
	if len(out.Steps) == 0 {
		out.Hint = "no match; try fewer or different words, or omit pack"
	}
	return nil, out, nil
}

func allSteps(e *engine.Engine, pack string) []Step {
	variants := map[*match.Def][]string{}
	for _, v := range e.Registry.Variants() {
		variants[v.Def] = append(variants[v.Def], v.Expr)
	}
	var out []Step
	for _, d := range e.Registry.Defs() {
		if pack != "" && d.Pack != pack {
			continue
		}
		seen := map[string]bool{}
		var params []string
		for _, n := range d.Names {
			if !seen[n] {
				seen[n] = true
				params = append(params, n)
			}
		}
		out = append(out, Step{
			ID: d.Step.ID, Pack: d.Pack, Expr: d.Step.Expr, Variants: variants[d], Arg: d.Step.Arg.String(),
			Doc: d.Step.Doc, Examples: d.Step.Examples, Params: params,
		})
	}
	return out
}

func searchSteps(e *engine.Engine, q, pack string, limit int) []Step {
	words := strings.Fields(strings.ToLower(q))
	fuzzy := map[string]int{}
	for i, sg := range e.Registry.Suggest(q, 10) {
		fuzzy[sg.ID] = 10 - i
	}
	type scored struct {
		s     Step
		score int
	}
	var all []scored
	for _, st := range allSteps(e, pack) {
		hay := strings.ToLower(st.Expr + " " + st.ID + " " + st.Doc)
		score := fuzzy[st.ID]
		for _, w := range words {
			if strings.Contains(hay, w) {
				score += 3
				if strings.Contains(strings.ToLower(st.Expr), w) {
					score += 2
				}
			}
		}
		if score > 0 || len(words) == 0 {
			all = append(all, scored{st, score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	var out []Step
	for i := 0; i < len(all) && i < limit; i++ {
		out = append(out, all[i].s)
	}
	return out
}

// ---- step_explain ----

type stepExplainIn struct {
	Line string `json:"line" jsonschema:"one step line, with or without its Given/When/Then keyword"`
}

type explainedArg struct {
	Param   string `json:"param"`
	Present bool   `json:"present"`
	Raw     string `json:"raw,omitempty"`
}

type stepExplainOut struct {
	Status      string         `json:"status" jsonschema:"matched, undefined or ambiguous"`
	Step        *Step          `json:"step,omitempty"`
	Expr        string         `json:"matchedExpression,omitempty"`
	Args        []explainedArg `json:"args,omitempty"`
	Candidates  []string       `json:"candidates,omitempty"`
	Suggestions []string       `json:"suggestions,omitempty"`
}

func (s *server) stepExplain(ctx context.Context, _ *sdk.CallToolRequest, in stepExplainIn) (*sdk.CallToolResult, stepExplainOut, error) {
	e, err := s.engine()
	if err != nil {
		return nil, stepExplainOut{}, err
	}
	text := stripKeyword(in.Line)
	ms := e.Registry.Match(text)
	var out stepExplainOut
	switch len(ms) {
	case 0:
		out.Status = "undefined"
		for _, sg := range e.Registry.Suggest(text, 5) {
			out.Suggestions = append(out.Suggestions, sg.Expr)
		}
	case 1:
		out.Status = "matched"
		d := ms[0].Def()
		for _, st := range allSteps(e, d.Pack) {
			if st.ID == d.Step.ID {
				st := st
				out.Step = &st
			}
		}
		out.Expr = ms[0].Variant.Expr
		for _, a := range ms[0].Args {
			out.Args = append(out.Args, explainedArg{Param: a.Param, Present: a.Present, Raw: a.Raw})
		}
	default:
		out.Status = "ambiguous"
		for _, m := range ms {
			out.Candidates = append(out.Candidates, m.Def().Step.ID+": "+m.Variant.Expr)
		}
	}
	return nil, out, nil
}

func stripKeyword(line string) string {
	line = strings.TrimSpace(line)
	for _, kw := range []string{"Given ", "When ", "Then ", "And ", "But ", "* "} {
		if strings.HasPrefix(line, kw) {
			return strings.TrimSpace(line[len(kw):])
		}
	}
	return line
}

// ---- feature_validate ----

type featureValidateIn struct {
	Paths   []string `json:"paths,omitempty" jsonschema:"feature files or directories (default: run.paths from axx.yaml)"`
	Content string   `json:"content,omitempty" jsonschema:"feature file text to check instead of files on disk"`
}

// Problem is a validation finding.
type Problem struct {
	Kind        string   `json:"kind" jsonschema:"syntax, undefined, ambiguous or argument"`
	Location    string   `json:"location"`
	Text        string   `json:"text,omitempty"`
	Message     string   `json:"message,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

type featureValidateOut struct {
	Valid     bool      `json:"valid"`
	Scenarios int       `json:"scenarios"`
	Problems  []Problem `json:"problems"`
	// Warnings do not make the features invalid (see lint_run).
	Warnings []Problem `json:"warnings,omitempty"`
}

func (s *server) featureValidate(ctx context.Context, _ *sdk.CallToolRequest, in featureValidateIn) (*sdk.CallToolResult, featureValidateOut, error) {
	e, err := s.engine()
	if err != nil {
		return nil, featureValidateOut{}, err
	}
	var pickles []*feature.Pickle
	out := featureValidateOut{Problems: []Problem{}}
	if in.Content != "" {
		_, ps, err := feature.ParseSource("input.feature", []byte(in.Content), messages.UUID{}.NewId)
		if err != nil {
			out.Problems = append(out.Problems, Problem{Kind: "syntax", Location: "input.feature", Message: err.Error()})
			return nil, out, nil
		}
		pickles = ps
	} else {
		var paths []string
		if len(in.Paths) > 0 {
			for _, p := range in.Paths {
				if !filepath.IsAbs(p) {
					p = filepath.Join(s.opts.WorkDir, p)
				}
				paths = append(paths, p)
			}
		} else {
			paths, _, _ = e.FeaturePaths(nil)
		}
		set, err := e.LoadFeatures(paths)
		if err != nil {
			out.Problems = append(out.Problems, Problem{Kind: "syntax", Message: err.Error()})
			return nil, out, nil
		}
		pickles = set.Pickles
	}
	out.Scenarios = len(pickles)
	for _, pr := range check.Pickles(e.Registry, pickles).Problems {
		mp := Problem{Kind: pr.Kind, Location: fmt.Sprintf("%s:%d", pr.URI, pr.Line), Text: pr.Text, Message: pr.Message}
		for _, sg := range pr.Suggestions {
			mp.Suggestions = append(mp.Suggestions, sg.Expr)
		}
		mp.Suggestions = append(mp.Suggestions, pr.Candidates...)
		out.Problems = append(out.Problems, mp)
	}
	out.Valid = len(out.Problems) == 0
	rr := lint.CheckFeatures(e.Registry, pickles, e.Config.Dir)
	for _, f := range rr.Findings {
		l := f.Locations[0]
		out.Warnings = append(out.Warnings, Problem{Kind: "lint", Location: fmt.Sprintf("%s:%d", l.File, l.Line), Text: stripKeyword(l.Text), Message: f.Message + " [" + f.Code + "]"})
	}
	return nil, out, nil
}

// ---- lint_run ----

type lintRunIn struct {
	Paths []string `json:"paths,omitempty" jsonschema:"only report findings touching these files or directories (every file is still scanned)"`
	Mode  string   `json:"mode,omitempty" jsonschema:"override every rule's mode: error or warn"`
}

type lintRunOut struct {
	OK     bool         `json:"ok" jsonschema:"false when a finding is an error (axx lint would exit 3)"`
	Report *lint.Report `json:"report"`
}

func (s *server) lintRun(ctx context.Context, _ *sdk.CallToolRequest, in lintRunIn) (*sdk.CallToolResult, lintRunOut, error) {
	if in.Mode != "" && in.Mode != lint.ModeError && in.Mode != lint.ModeWarn {
		return nil, lintRunOut{}, fmt.Errorf("mode must be error or warn")
	}
	cfg, err := config.Load(config.LoadOptions{Path: s.opts.ConfigPath, WorkDir: s.opts.WorkDir, Profile: s.opts.Profile})
	if err != nil {
		return nil, lintRunOut{}, err
	}
	rep, err := lint.Project(ctx, cfg, lint.Options{WorkDir: s.opts.WorkDir, Mode: in.Mode, Paths: in.Paths})
	if err != nil {
		return nil, lintRunOut{}, err
	}
	return nil, lintRunOut{OK: rep.OK(), Report: rep}, nil
}

// ---- scenarios_run / failure_context ----

type scenariosRunIn struct {
	Paths    []string `json:"paths,omitempty" jsonschema:"feature files, directories or file:line (default: all)"`
	Tags     string   `json:"tags,omitempty" jsonschema:"tag expression, e.g. '@smoke and not @wip'"`
	Name     string   `json:"name,omitempty" jsonschema:"regular expression matched against scenario names"`
	FailFast bool     `json:"failFast,omitempty"`
}

type scenariosRunOut struct {
	RunID    string `json:"runId"`
	ExitCode int    `json:"exitCode"`
	Report   any    `json:"report,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (s *server) scenariosRun(ctx context.Context, _ *sdk.CallToolRequest, in scenariosRunIn) (*sdk.CallToolResult, scenariosRunOut, error) {
	args := []string{"run", "--json"}
	if s.opts.ConfigPath != "" {
		args = append(args, "--config", s.opts.ConfigPath)
	}
	if s.opts.Profile != "" {
		args = append(args, "--profile", s.opts.Profile)
	}
	if in.Tags != "" {
		args = append(args, "--tags", in.Tags)
	}
	if in.Name != "" {
		args = append(args, "--name", in.Name)
	}
	if in.FailFast {
		args = append(args, "--fail-fast")
	}
	args = append(args, in.Paths...)
	stdout, stderr, code, err := s.axx(ctx, args...)
	out := scenariosRunOut{RunID: newRunID(), ExitCode: code}
	if err != nil {
		out.Error = err.Error()
		return nil, out, nil
	}
	var envlp struct {
		OK     bool            `json:"ok"`
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"errors"`
	}
	if jerr := json.Unmarshal(stdout, &envlp); jerr != nil {
		out.Error = strings.TrimSpace(string(stderr))
		return nil, out, nil
	}
	if len(envlp.Errors) > 0 {
		out.Error = envlp.Errors[0].Message
		if envlp.Errors[0].Hint != "" {
			out.Error += " (hint: " + envlp.Errors[0].Hint + ")"
		}
	}
	out.Report = decode(compactReport(envlp.Data))
	if len(envlp.Data) > 0 {
		dir := filepath.Join(s.runsDir(), "")
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, out.RunID+".json"), envlp.Data, 0o644)
	}
	return nil, out, nil
}

// compactReport drops logs and context from failures to keep responses
// small; failure_context returns them on demand.
func compactReport(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if fs, ok := m["failures"].([]any); ok {
		for _, f := range fs {
			if fm, ok := f.(map[string]any); ok {
				delete(fm, "logs")
				delete(fm, "context")
			}
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

type failureContextIn struct {
	RunID    string `json:"runId"`
	Location string `json:"location" jsonschema:"the failing scenario's location, e.g. features/orders.feature:14"`
}

func (s *server) failureContext(ctx context.Context, _ *sdk.CallToolRequest, in failureContextIn) (*sdk.CallToolResult, map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(s.runsDir(), filepath.Base(in.RunID)+".json"))
	if err != nil {
		return nil, nil, fmt.Errorf("unknown runId %q (runs are kept in .axx/runs)", in.RunID)
	}
	var rep struct {
		Failures []map[string]any `json:"failures"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, nil, err
	}
	for _, f := range rep.Failures {
		if f["location"] == in.Location {
			return nil, f, nil
		}
	}
	var locs []string
	for _, f := range rep.Failures {
		locs = append(locs, fmt.Sprint(f["location"]))
	}
	return nil, nil, fmt.Errorf("no failure at %s in run %s; failures: %s", in.Location, in.RunID, strings.Join(locs, ", "))
}

func (s *server) runsDir() string {
	if e, err := s.engine(); err == nil {
		return filepath.Join(e.Config.Dir, ".axx", "runs")
	}
	return filepath.Join(s.opts.WorkDir, ".axx", "runs")
}

func newRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
}

// ---- env ----

type envIn struct {
	Action string `json:"action" jsonschema:"up, down or status"`
}

type envOut struct {
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *server) env(ctx context.Context, _ *sdk.CallToolRequest, in envIn) (*sdk.CallToolResult, envOut, error) {
	var args []string
	switch in.Action {
	case "up":
		args = []string{"up", "--json"}
	case "down":
		args = []string{"down", "--json"}
	case "status", "":
		args = []string{"doctor", "--json"}
	default:
		return nil, envOut{}, fmt.Errorf("action must be up, down or status")
	}
	if s.opts.ConfigPath != "" {
		args = append(args, "--config", s.opts.ConfigPath)
	}
	stdout, stderr, _, err := s.axx(ctx, args...)
	out := envOut{Action: in.Action}
	if err != nil {
		out.Error = err.Error()
		return nil, out, nil
	}
	var envlp struct {
		OK     bool            `json:"ok"`
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(stdout, &envlp) != nil {
		out.Error = strings.TrimSpace(string(stderr))
		return nil, out, nil
	}
	out.OK, out.Data = envlp.OK, decode(envlp.Data)
	if len(envlp.Errors) > 0 {
		out.Error = envlp.Errors[0].Message
	}
	return nil, out, nil
}

func (s *server) axx(ctx context.Context, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, s.opts.Exe, args...)
	cmd.Dir = s.opts.WorkDir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code, err = ee.ExitCode(), nil
	}
	return stdout.Bytes(), stderr.Bytes(), code, err
}

// ---- config_show ----

type configShowOut struct {
	File    string   `json:"file,omitempty"`
	Config  any      `json:"config"`
	Packs   []string `json:"packs"`
	Profile string   `json:"profile,omitempty"`
}

func (s *server) configShow(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, configShowOut, error) {
	e, err := s.engine()
	if err != nil {
		return nil, configShowOut{}, err
	}
	b, err := json.Marshal(e.Config)
	if err != nil {
		return nil, configShowOut{}, err
	}
	return nil, configShowOut{File: e.Config.File, Config: decode(redact(b)), Packs: e.PackNames(), Profile: s.opts.Profile}, nil
}

// redact masks values of keys that look like secrets.
func redact(b []byte) json.RawMessage {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				lk := strings.ToLower(k)
				if s, ok := val.(string); ok && s != "" && (strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "key")) {
					t[k] = "***"
					continue
				}
				t[k] = walk(val)
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		}
		return x
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		return b
	}
	return out
}

// ---- scaffold ----

type scaffoldIn struct {
	Kind string `json:"kind" jsonschema:"feature or config"`
	Name string `json:"name,omitempty" jsonschema:"feature or service name"`
}

type scaffoldOut struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Next    string `json:"next"`
}

func (s *server) scaffold(ctx context.Context, _ *sdk.CallToolRequest, in scaffoldIn) (*sdk.CallToolResult, scaffoldOut, error) {
	name := in.Name
	if name == "" {
		name = "example"
	}
	switch in.Kind {
	case "feature":
		e, err := s.engine()
		if err != nil {
			return nil, scaffoldOut{}, err
		}
		var examples []string
		for _, st := range allSteps(e, "") {
			if len(st.Examples) > 0 && len(examples) < 6 {
				examples = append(examples, "    # "+st.ID+"\n    # "+strings.SplitN(st.Examples[0], "\n", 2)[0])
			}
		}
		content := fmt.Sprintf("Feature: %s\n  <one sentence: the capability and who benefits>\n\n  Scenario: <acceptance criterion in plain words>\n    Given <context>\n    When <action>\n    Then <observable outcome>\n\n  # Example steps available in this project (search more with steps_search):\n%s\n",
			name, strings.Join(examples, "\n"))
		return nil, scaffoldOut{
			Path: "features/" + slug(name) + ".feature", Content: content,
			Next: "replace the placeholders with real steps from steps_search, then feature_validate",
		}, nil
	case "config":
		return nil, scaffoldOut{
			Path: "axx.yaml", Content: "# yaml-language-server: $schema=" + config.SchemaID + "\nversion: 1\nrun:\n  paths: [features]\napps: {}\n",
			Next: "add apps (command + ready) and run env {action: status}",
		}, nil
	}
	return nil, scaffoldOut{}, fmt.Errorf("kind must be feature or config")
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeTestsPrompt(criteria string) string {
	return "Write axx acceptance tests (Gherkin) for these criteria:\n\n" + criteria + `

Rules:
- Use steps_search for every step; only use step text that exists. Never invent steps.
- One scenario per criterion; name scenarios after the behavior, not the implementation.
- Use unique test data in every scenario (ids, names, keys) because scenarios run in parallel.
- Validate with feature_validate until it reports no problems, then run with scenarios_run.
- Scenarios must fail if the behavior is broken: assert on observable outcomes (status, payload, rows, events).`
}

// decode turns raw JSON into plain values (so output schemas are objects).
func decode(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}
