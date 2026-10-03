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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	messages "github.com/cucumber/messages/go/v34"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/check"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lifecycle"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/stepsearch"
	"github.com/nimbusxr/axx/internal/version"
)

// Instructions are sent to clients on connect, and stay in the agent's
// context: only the conventions the tools' descriptions do not carry.
const Instructions = `axx runs acceptance criteria, written as Gherkin features, against the project's services.

Writing tests: steps_search without a query lists every step once; write one scenario per acceptance criterion with steps that exist, written out: each {name} replaced with a value, a(n) as "a" or "an" and row(s) as "row" or "rows", [[...]] kept without the brackets or left out; scenarios_run runs them and also reports what feature_validate and lint_run find; failure_context explains a failure. env {"action":"up"} keeps the apps running between runs.
- A scenario registers each service it uses before other steps use it ("the {word} service with the following properties:").
- Requests, responses and selections are numbered in the order a scenario adds them; a step without an ordinal means the first.
- Scenarios run in parallel and data persists: give every id, key and name a value of the scenario's own.
- Check what a request did or why it was refused, not only its status code.`

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
	sess agentSession
	// shown are the steps this session's searches returned.
	shownMu sync.Mutex
	shown   map[string]bool
}

// New builds the MCP server.
func New(opts Options) *sdk.Server {
	srv, _ := newServer(opts)
	return srv
}

func newServer(opts Options) (*sdk.Server, *server) {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	if opts.Exe == "" {
		opts.Exe, _ = os.Executable()
	}
	s := &server{opts: opts}
	srv := sdk.NewServer(&sdk.Implementation{Name: "axx", Title: "axx acceptance testing", Version: version.Get().Version},
		&sdk.ServerOptions{Instructions: Instructions})

	// The tools work on the project and the apps it starts, nothing beyond.
	closed := false
	ro := &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
	notDestructive := &sdk.ToolAnnotations{DestructiveHint: &closed, OpenWorldHint: &closed}
	addTool(srv, &sdk.Tool{
		Name: "steps_search", Annotations: ro,
		Description: "Without a query: the catalog, every step of the project a line each (read it once). With one: the steps that fit it, briefly.",
	},
		s.stepsSearch)
	addTool(srv, &sdk.Tool{
		Name: "step_explain", Annotations: ro,
		Description: "With a step line: how axx reads it, or the closest steps. With step ids: their documentation and examples.",
	},
		s.stepExplain)
	addTool(srv, &sdk.Tool{
		Name: "feature_validate", Annotations: ro,
		Description: "Check features without running them. scenarios_run reports the same problems, so a run needs no check first.",
	},
		s.featureValidate)
	addTool(srv, &sdk.Tool{
		Name: "lint_run", Annotations: ro,
		Description: "axx lint: test-data values that collide across files, and the feature checks. scenarios_run reports them too.",
	},
		s.lintRun)
	addTool(srv, &sdk.Tool{
		Name: "scenarios_run", Annotations: notDestructive,
		Description: "Run scenarios (starting the apps unless they are up): failures with expected and actual, the warnings validate and lint give, and hints.",
	},
		s.scenariosRun)
	addTool(srv, &sdk.Tool{
		Name: "failure_context", Annotations: ro,
		Description: "One failure's logs and last request and response (default: the latest run's only failure).",
	},
		s.failureContext)
	addTool(srv, &sdk.Tool{
		// down stops the apps and runs their cleanup.
		Name: "env", Annotations: &sdk.ToolAnnotations{OpenWorldHint: &closed},
		Description: "The apps: up keeps them running between runs, down stops them and cleans up, status lists them.",
	},
		s.env)
	addTool(srv, &sdk.Tool{
		Name: "config_show", Annotations: ro,
		Description: "The effective axx.yaml (secrets redacted) and the project's packs.",
	},
		s.configShow)
	addTool(srv, &sdk.Tool{
		// The steps may reach whatever a scenario does (a web page, say).
		Name: "steps_try", Annotations: &sdk.ToolAnnotations{DestructiveHint: &closed},
		Description: "Run steps in a live scenario kept open between calls: each step's result, and what the packs see (the page it is on).",
	},
		s.stepsTry)
	s.packTools(srv)
	addTool(srv, &sdk.Tool{
		Name: "scaffold", Annotations: ro,
		Description: "Starter text for a feature or an axx.yaml; nothing is written.",
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
	return srv, s
}

// addTool registers a tool whose result text is its output as JSON, as the
// SDK writes it, but without escaping <, > and & (\u003c...), which agents
// read as noise.
func addTool[In, Out any](srv *sdk.Server, t *sdk.Tool, h sdk.ToolHandlerFor[In, Out]) {
	sdk.AddTool(srv, t, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err == nil && res == nil {
			res = &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: plainJSON(out)}}}
		}
		return res, out, err
	})
}

// plainJSON is v as JSON, without HTML escaping.
func plainJSON(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// Serve runs the server on stdin/stdout until the client disconnects, then
// ends the agent's session.
func Serve(ctx context.Context, opts Options) error {
	srv, s := newServer(opts)
	defer s.end()
	return srv.Run(ctx, &sdk.StdioTransport{})
}

func (s *server) engine() (*engine.Engine, error) {
	cfg, err := config.Load(config.LoadOptions{Path: s.opts.ConfigPath, WorkDir: s.opts.WorkDir, Profile: s.opts.Profile})
	if err != nil {
		return nil, err
	}
	return newEngine(cfg)
}

// newEngine loads the project's steps. The server has the packs the project
// listed when it started, prepared then; a pack added since is not in it,
// and only a new server has it.
func newEngine(cfg *config.Config) (*engine.Engine, error) {
	e, err := engine.New(engine.Options{Config: cfg})
	if err == nil {
		return e, nil
	}
	f, found, ferr := packset.Load(cfg.Dir)
	if ferr != nil || !found {
		return nil, err
	}
	entries, _ := f.Entries()
	compiled := engine.Compiled()
	var added []string
	for _, en := range packset.WithRequired(entries) {
		if _, ok := compiled[en.Key()]; !ok {
			added = append(added, en.Key())
		}
	}
	if len(added) == 0 {
		return nil, err
	}
	return nil, axxerr.New(engine.CodeUnknownPack, exitcode.Usage,
		"this axx MCP server started without the packs %s, which %s lists now: restart the axx MCP server (in your agent, reconnect or restart the MCP server named axx); the new one prepares the project's packs as it starts",
		strings.Join(added, ", "), packset.FileName).
		WithHint("the axx command line prepares them already: `axx steps search` works meanwhile")
}

// ---- steps_search ----

type stepsSearchIn struct {
	Query string `json:"query,omitempty" jsonschema:"what the step should do; empty for the catalog"`
	Pack  string `json:"pack,omitempty" jsonschema:"one pack only, like rest"`
	Limit int    `json:"limit,omitempty" jsonschema:"results (default 6)"`
}

// Step is a step definition as seen by agents.
type Step struct {
	ID       string   `json:"id"`
	Pack     string   `json:"pack,omitempty"`
	Expr     string   `json:"expr,omitempty"`
	Variants []string `json:"variants,omitempty"`
	Arg      string   `json:"argument,omitempty"`
	// Columns name the columns of the step's data table; the table may
	// start with a row of these names (two columns or more) or not.
	Columns  []string `json:"columns,omitempty"`
	Doc      string   `json:"doc,omitempty"`
	Examples []string `json:"examples,omitempty"`
	Params   []string `json:"params,omitempty"`
	// Twin is the id of the step's form for a named service (`… on
	// {service}`), folded into it.
	Twin string `json:"twin,omitempty"`
	// ShownEarlier marks a step this session's searches returned before:
	// only its id comes again (step_explain gives the whole step).
	ShownEarlier bool `json:"shownEarlier,omitempty"`
}

type stepsSearchOut struct {
	Steps []Step `json:"steps,omitempty"`
	// Catalog lists every step, one line each, when the query is empty.
	Catalog string `json:"catalog,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

func (s *server) stepsSearch(ctx context.Context, _ *sdk.CallToolRequest, in stepsSearchIn) (*sdk.CallToolResult, stepsSearchOut, error) {
	e, err := s.engine()
	if err != nil {
		return nil, stepsSearchOut{}, err
	}
	all := allSteps(e, in.Pack)
	if strings.TrimSpace(in.Query) == "" {
		entries := make([]stepsearch.Entry, len(all))
		for i, st := range all {
			entries[i] = stepsearch.Entry{ID: st.ID, Pack: st.Pack, Expr: st.Expr, Arg: st.Arg, Columns: st.Columns}
		}
		return nil, stepsSearchOut{
			Catalog: stepsearch.Catalog(entries),
			Hint:    "every step, one line each; step_explain {id} gives one step's documentation and examples",
		}, nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 6
	}
	found, strong := searchSteps(all, in.Query)
	out := stepsSearchOut{}
	// Every result stays in the agent's context, so it carries what picking
	// a step takes and no more: no documentation (step_explain has it), one
	// example, and only the id of a step an earlier search returned.
	s.shownMu.Lock()
	if s.shown == nil {
		s.shown = map[string]bool{}
	}
	again := 0
	for _, st := range found[:min(limit, len(found))] {
		if s.shown[st.ID] {
			out.Steps = append(out.Steps, Step{ID: st.ID, ShownEarlier: true})
			again++
			continue
		}
		s.shown[st.ID] = true
		st.Doc, st.Variants, st.Params = "", nil, nil
		if len(st.Examples) > 1 {
			st.Examples = st.Examples[:1]
		}
		out.Steps = append(out.Steps, st)
	}
	s.shownMu.Unlock()
	switch {
	case len(found) == 0:
		out.Hint = "no step reads like that; try other words, or call without a query for the catalog of every step"
	case !strong:
		out.Hint = "no step reads like that; these only mention it in their documentation. Try other words, or call without a query for the catalog"
	case len(found) > limit:
		out.Hint = fmt.Sprintf("%d more steps match; add words to narrow the search", len(found)-limit)
	}
	if again > 0 {
		out.Hint = strings.TrimPrefix(out.Hint+"; ", "; ") + "steps marked shownEarlier came in an earlier search: step_explain {id} gives one again"
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
		var columns []string
		if d.Step.Table != nil {
			columns = d.Step.Table.Columns
		}
		out = append(out, Step{
			ID: d.Step.ID, Pack: d.Pack, Expr: d.Step.Expr, Variants: variants[d], Arg: d.Step.Arg.String(),
			Columns: columns, Doc: d.Step.Doc, Examples: d.Step.Examples, Params: params,
		})
	}
	return out
}

// searchSteps ranks the steps for q (internal/stepsearch), folding each
// named-service twin into its step.
func searchSteps(all []Step, q string) ([]Step, bool) {
	ids := make([]string, len(all))
	docs := make([]stepsearch.Step, len(all))
	for i, st := range all {
		ids[i] = st.ID
		docs[i] = stepsearch.Step{ID: st.ID, Pack: st.Pack, Expr: st.Expr, Doc: st.Doc}
	}
	twins := stepsearch.Twins(ids)
	twinOf := map[string]string{}
	for twin, base := range twins {
		twinOf[base] = twin
	}
	ranked, strong := stepsearch.Search(docs, q)
	var out []Step
	listed := map[string]bool{}
	for _, r := range ranked {
		st := all[r.Index]
		if base, ok := twins[st.ID]; ok {
			st = all[slices.Index(ids, base)]
		}
		if listed[st.ID] {
			continue
		}
		listed[st.ID] = true
		st.Twin = twinOf[st.ID]
		out = append(out, st)
	}
	return out, strong
}

// ---- step_explain ----

type stepExplainIn struct {
	Line string `json:"line,omitempty" jsonschema:"a step line"`
	ID   string `json:"id,omitempty" jsonschema:"a step id"`
	// IDs ask for several steps in one call.
	IDs []string `json:"ids,omitempty" jsonschema:"several step ids"`
}

type explainedArg struct {
	Param   string `json:"param"`
	Present bool   `json:"present"`
	Raw     string `json:"raw,omitempty"`
}

type stepExplainOut struct {
	Status string `json:"status" jsonschema:"matched, undefined or ambiguous"`
	// Step is the step an id names, whole; for a line, its id.
	Step *Step `json:"step,omitempty"`
	// Steps are the steps ids name, and Unknown the ids no step has.
	Steps       []Step         `json:"steps,omitempty"`
	Unknown     []string       `json:"unknown,omitempty"`
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
	var out stepExplainOut
	if len(in.IDs) > 0 {
		byID := map[string]Step{}
		for _, st := range allSteps(e, "") {
			byID[st.ID] = st
		}
		for _, id := range in.IDs {
			if st, ok := byID[strings.TrimSpace(id)]; ok {
				out.Steps = append(out.Steps, st)
			} else {
				out.Unknown = append(out.Unknown, id)
			}
		}
		out.Status = "matched"
		if len(out.Steps) == 0 {
			out.Status = "undefined"
		}
		return nil, out, nil
	}
	if id := strings.TrimSpace(in.ID); id != "" {
		for _, st := range allSteps(e, "") {
			if st.ID == id {
				st := st
				out.Status, out.Step = "matched", &st
				return nil, out, nil
			}
		}
		out.Status = "undefined"
		found, _ := searchSteps(allSteps(e, ""), strings.ReplaceAll(id, ".", " "))
		for _, st := range found[:min(3, len(found))] {
			out.Suggestions = append(out.Suggestions, st.ID)
		}
		return nil, out, nil
	}
	if strings.TrimSpace(in.Line) == "" {
		return nil, out, fmt.Errorf("give a line to read, or step ids")
	}
	text := stripKeyword(in.Line)
	ms := e.Registry.Match(text)
	switch len(ms) {
	case 0:
		out.Status = "undefined"
		for _, sg := range e.Registry.Suggest(text, 5) {
			out.Suggestions = append(out.Suggestions, sg.Expr)
		}
	case 1:
		// How the line is read; the step's documentation is a call away
		// (step_explain {id}).
		out.Status = "matched"
		out.Step = &Step{ID: ms[0].Def().Step.ID}
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
	Paths   []string `json:"paths,omitempty" jsonschema:"feature files or directories (default: all)"`
	Content string   `json:"content,omitempty" jsonschema:"feature text, instead of files"`
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
	// Hints name scenarios whose checks prove little: only a success status
	// code, or only that something did not happen. They are for the author
	// to judge.
	Hints []string `json:"hints,omitempty"`
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
	for _, rr := range lint.FeatureChecks(e.Registry, pickles, e.Config.Dir) {
		for _, f := range rr.Findings {
			l := f.Locations[0]
			out.Warnings = append(out.Warnings, Problem{Kind: "lint", Location: fmt.Sprintf("%s:%d", l.File, l.Line), Text: stripKeyword(l.Text), Message: f.Message + " [" + f.Code + "]"})
		}
	}
	out.Hints = lint.ScenarioHints(e.Registry, pickles, lint.Options{WorkDir: s.opts.WorkDir})
	return nil, out, nil
}

// ---- lint_run ----

type lintRunIn struct {
	Paths []string `json:"paths,omitempty" jsonschema:"only findings in these files or directories"`
	Mode  string   `json:"mode,omitempty" jsonschema:"error or warn, for every rule"`
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
	ok := rep.OK()
	// The rules that found something; the summary counts every rule.
	rules := rep.Rules[:0:0]
	for _, rr := range rep.Rules {
		if len(rr.Findings) > 0 {
			rules = append(rules, rr)
		}
	}
	rep.Rules = rules
	return nil, lintRunOut{OK: ok, Report: rep}, nil
}

// ---- scenarios_run / failure_context ----

type scenariosRunIn struct {
	Paths    []string `json:"paths,omitempty" jsonschema:"files, directories or file:line (default: all)"`
	Tags     string   `json:"tags,omitempty" jsonschema:"a tag expression"`
	Name     string   `json:"name,omitempty" jsonschema:"a regular expression for scenario names"`
	FailFast bool     `json:"failFast,omitempty"`
}

type scenariosRunOut struct {
	RunID    string `json:"runId"`
	ExitCode int    `json:"exitCode"`
	Report   any    `json:"report,omitempty"`
	Error    string `json:"error,omitempty"`
	// Warnings are what feature_validate and lint_run would add: the
	// findings of the test-data rules and of the feature checks.
	Warnings []string `json:"warnings,omitempty"`
	// Hints are what a passing suite could do better, as feature_validate
	// gives them.
	Hints []string `json:"hints,omitempty"`
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
	for _, l := range strings.Split(string(stderr), "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(l), "hint: "); ok {
			out.Hints = append(out.Hints, h)
		}
		if w, ok := strings.CutPrefix(strings.TrimSpace(l), "warning: "); ok {
			out.Warnings = append(out.Warnings, w)
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
	RunID    string `json:"runId,omitempty" jsonschema:"default: the latest run"`
	Location string `json:"location,omitempty" jsonschema:"file:line of the failure (default: the only one)"`
}

func (s *server) failureContext(ctx context.Context, _ *sdk.CallToolRequest, in failureContextIn) (*sdk.CallToolResult, map[string]any, error) {
	dir := s.runsDir()
	id := in.RunID
	if id == "" {
		latest, err := latestRun(dir)
		if err != nil {
			return nil, nil, err
		}
		id = latest
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.Base(id)+".json"))
	if err != nil {
		return nil, nil, fmt.Errorf("unknown runId %q (runs are kept in .axx/runs)", id)
	}
	var rep struct {
		Failures []map[string]any `json:"failures"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, nil, err
	}
	var locs []string
	for _, f := range rep.Failures {
		locs = append(locs, fmt.Sprint(f["location"]))
	}
	switch {
	case len(rep.Failures) == 0:
		return nil, nil, fmt.Errorf("run %s has no failures", id)
	case in.Location == "" && len(rep.Failures) == 1:
		rep.Failures[0]["runId"] = id
		return nil, rep.Failures[0], nil
	case in.Location == "":
		return nil, nil, fmt.Errorf("run %s has %d failures; give the location of one: %s", id, len(rep.Failures), strings.Join(locs, ", "))
	}
	for _, f := range rep.Failures {
		if f["location"] == in.Location {
			f["runId"] = id
			return nil, f, nil
		}
	}
	return nil, nil, fmt.Errorf("no failure at %s in run %s; failures: %s", in.Location, id, strings.Join(locs, ", "))
}

// latestRun is the ID of the newest run in dir.
func latestRun(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	var id string
	var newest time.Time
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			id, newest = name, info.ModTime()
		}
	}
	if id == "" {
		return "", errors.New("no runs yet: scenarios_run keeps its runs in .axx/runs")
	}
	return id, nil
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
		return s.envStatus()
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

// envStatus reports the apps the project's state file records: running,
// left over from an earlier run, or not cleaned up.
func (s *server) envStatus() (*sdk.CallToolResult, envOut, error) {
	out := envOut{Action: "status"}
	cfg, err := config.Load(config.LoadOptions{Path: s.opts.ConfigPath, WorkDir: s.opts.WorkDir, Profile: s.opts.Profile})
	if err != nil {
		out.Error = err.Error()
		return nil, out, nil
	}
	apps, err := lifecycle.Status(lifecycle.StateFile(cfg.Dir))
	if err != nil {
		out.Error = err.Error()
		return nil, out, nil
	}
	if apps == nil {
		apps = []lifecycle.AppStatus{}
	}
	out.OK = true
	out.Data = map[string]any{"apps": apps}
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
	return nil, configShowOut{File: e.Config.File, Config: decode(Redact(b)), Packs: e.PackNames(), Profile: s.opts.Profile}, nil
}

// Redact masks the values of keys that look like secrets (password,
// secret, token, key) in a JSON document.
func Redact(b []byte) json.RawMessage {
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
	Name string `json:"name,omitempty" jsonschema:"the feature's or the service's name"`
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
- Read the step catalog once (steps_search without a query); only use step text that exists.
- One scenario per criterion; name scenarios after the behavior, not the implementation.
- Use unique test data in every scenario (ids, names, keys) because scenarios run in parallel.
- Run with scenarios_run: it reports failures, and what feature_validate and lint_run find.
- Scenarios must fail if the behavior is broken: check what the service did (payload, rows, events) and why it refused, not only status codes.`
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
