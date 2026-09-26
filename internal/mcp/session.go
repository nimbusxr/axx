package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	messages "github.com/cucumber/messages/go/v34"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/runner"
)

// The agent's session: one scenario that stays open while the agent tries
// steps in it (steps_try), and the packs' tools look at what the steps did,
// such as the page one opened. It lasts until the agent starts over or the
// server stops.
type agentSession struct {
	mu     sync.Mutex
	e      *engine.Engine
	s      *runner.Session
	cancel context.CancelFunc
}

// live is the agent's session, started if need be (or again, on restart).
func (s *server) live(restart bool) (*runner.Session, error) {
	s.sess.mu.Lock()
	defer s.sess.mu.Unlock()
	if restart {
		s.endLocked()
	}
	if s.sess.s != nil {
		return s.sess.s, nil
	}
	cfg, err := config.Load(config.LoadOptions{Path: s.opts.ConfigPath, WorkDir: s.opts.WorkDir, Profile: s.opts.Profile})
	if err != nil {
		return nil, err
	}
	e, err := engine.New(engine.Options{Config: cfg})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Init(ctx); err != nil {
		cancel()
		_ = e.Close(context.Background())
		return nil, err
	}
	r, err := runner.New(runner.Options{
		Registry: e.Registry, Hooks: e.Hooks, Suite: e.Suite, Workers: 1,
		StepTimeout: cfg.Run.Timeouts.Step.D(), HookTimeout: cfg.Run.Timeouts.Hook.D(),
	})
	if err != nil {
		cancel()
		_ = e.Close(context.Background())
		return nil, err
	}
	sess, before := r.NewSession(ctx, core.ScenarioInfo{ID: newRunID(), Name: "agent session", URI: sessionURI, Line: 2})
	for _, h := range before {
		if h.Err != nil {
			_ = sess.Close(runner.Failed)
			cancel()
			_ = e.Close(context.Background())
			return nil, fmt.Errorf("hook %s: %w", h.Text, h.Err)
		}
	}
	s.sess.e, s.sess.s, s.sess.cancel = e, sess, cancel
	return sess, nil
}

// sessionURI is where the session's steps are, for packs that name steps
// by their place (traces).
const sessionURI = "features/agent-session.feature"

// end ends the agent's session, if there is one: its browsers close and its
// traces are kept.
func (s *server) end() {
	s.sess.mu.Lock()
	defer s.sess.mu.Unlock()
	s.endLocked()
}

func (s *server) endLocked() {
	if s.sess.s == nil {
		return
	}
	_ = s.sess.s.Close(runner.Passed)
	s.sess.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = s.sess.e.Close(ctx)
	s.sess.e, s.sess.s, s.sess.cancel = nil, nil, nil
}

// ---- steps_try ----

type stepsTryIn struct {
	Steps   string `json:"steps" jsonschema:"steps as in a scenario, one a line, with their keywords, data tables and doc strings: Given the \"/quote\" page is opened"`
	Restart bool   `json:"restart,omitempty" jsonschema:"end the session first and start a new one: a new scenario, with nothing open"`
}

// TriedStep is a step the agent tried, and how it went.
type TriedStep struct {
	Keyword     string   `json:"keyword"`
	Text        string   `json:"text"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
	Logs        []string `json:"logs,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

type stepsTryOut struct {
	Steps []TriedStep `json:"steps"`
	// State is what the packs say of the session now (the page it is on...).
	State map[string]any `json:"state,omitempty"`
}

func (s *server) stepsTry(_ context.Context, _ *sdk.CallToolRequest, in stepsTryIn) (*sdk.CallToolResult, stepsTryOut, error) {
	if strings.TrimSpace(in.Steps) == "" && !in.Restart {
		return nil, stepsTryOut{}, errors.New("no steps: give the steps to try, one a line, like `Given the \"/quote\" page is opened`")
	}
	p, err := sessionPickle(in.Steps)
	if err != nil {
		return nil, stepsTryOut{}, err
	}
	sess, err := s.live(in.Restart)
	if err != nil {
		return nil, stepsTryOut{}, err
	}
	out := stepsTryOut{Steps: []TriedStep{}}
	var images []sdk.Content
	if p != nil {
		for _, sr := range sess.Run(p) {
			t := TriedStep{Keyword: sr.Keyword, Text: sr.Text, Status: sr.Status.String(), Logs: sr.Logs}
			if sr.Err != nil {
				t.Error = sr.Err.Error()
			}
			for _, sg := range sr.Suggestions {
				t.Suggestions = append(t.Suggestions, sg.Expr)
			}
			for _, a := range sr.Attachments {
				if strings.HasPrefix(a.MediaType, "image/") && sr.Status == runner.Failed {
					images = append(images, &sdk.ImageContent{MIMEType: a.MediaType, Data: a.Body})
				}
			}
			out.Steps = append(out.Steps, t)
		}
	}
	out.State = sess.Scenario().Descriptions()
	if len(images) == 0 {
		return nil, out, nil
	}
	b, _ := json.Marshal(out)
	return &sdk.CallToolResult{Content: append([]sdk.Content{&sdk.TextContent{Text: string(b)}}, images...), StructuredContent: out}, out, nil
}

// sessionPickle reads steps as a scenario's: nil for none.
func sessionPickle(steps string) (*feature.Pickle, error) {
	if strings.TrimSpace(steps) == "" {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("Feature: agent session\n  Scenario: agent session\n")
	for _, line := range strings.Split(strings.ReplaceAll(steps, "\r\n", "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	_, pickles, err := feature.ParseSource(sessionURI, []byte(b.String()), (&messages.Incrementing{}).NewId)
	if err != nil {
		return nil, fmt.Errorf("the steps are not Gherkin: %w", err)
	}
	if len(pickles) != 1 {
		return nil, errors.New("give steps only, with no Feature or Scenario line")
	}
	return pickles[0], nil
}

// ---- the packs' tools ----

// packTools adds the tools of the project's packs, which work in the
// agent's session. A project whose configuration does not load has none.
func (s *server) packTools(srv *sdk.Server) {
	cfg, err := config.Load(config.LoadOptions{Path: s.opts.ConfigPath, WorkDir: s.opts.WorkDir, Profile: s.opts.Profile})
	if err != nil {
		return
	}
	e, err := engine.New(engine.Options{Config: cfg})
	if err != nil {
		return
	}
	defer func() { _ = e.Close(context.Background()) }()
	for _, name := range e.PackNames() {
		for _, t := range e.Manifests()[name].Tools {
			schema, err := compileSchema(t)
			if err != nil {
				continue
			}
			var ann *sdk.ToolAnnotations
			if t.ReadOnly {
				ann = &sdk.ToolAnnotations{ReadOnlyHint: true}
			}
			srv.AddTool(&sdk.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Input, Annotations: ann}, s.packTool(t, schema))
		}
	}
}

func compileSchema(t core.Tool) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(t.Input))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	url := "axx:tool/" + t.Name
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	return c.Compile(url)
}

func (s *server) packTool(t core.Tool, schema *jsonschema.Schema) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		input := req.Params.Arguments
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(input))
		if err == nil {
			err = schema.Validate(inst)
		}
		if err != nil {
			return toolError(fmt.Errorf("the input is not valid: %w", err)), nil
		}
		sess, err := s.live(false)
		if err != nil {
			return toolError(err), nil
		}
		res, err := t.Run(&core.ToolCall{Context: ctx, Scenario: sess.Scenario(), Input: input})
		if err != nil {
			return toolError(err), nil
		}
		if res == nil {
			res = &core.ToolResult{}
		}
		b, err := json.Marshal(res.Data)
		if err != nil {
			return toolError(err), nil
		}
		out := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}
		if bytes.HasPrefix(b, []byte("{")) {
			out.StructuredContent = json.RawMessage(b)
		}
		for _, img := range res.Images {
			out.Content = append(out.Content, &sdk.ImageContent{MIMEType: img.MediaType, Data: img.Data})
		}
		return out, nil
	}
}

func toolError(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}
}
