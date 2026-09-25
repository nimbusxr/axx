package runner

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
)

// TimeoutError reports a step or hook that exceeded its time limit.
type TimeoutError struct {
	What    string
	Limit   time.Duration
	Ignored bool // the step did not return after cancellation
}

func (e *TimeoutError) Error() string {
	s := fmt.Sprintf("%s timed out after %s", e.What, e.Limit)
	if e.Ignored {
		s += " (it ignored cancellation and was abandoned)"
	}
	return s
}

// PanicError reports a panic inside a step or hook.
type PanicError struct {
	Value any
	Stack string
}

func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// sink collects logs and attachments for the currently running step.
type sink struct {
	mu       sync.Mutex
	current  *StepResult
	onAttach func(sr *StepResult, a Attachment)
}

func (s *sink) set(sr *StepResult) {
	s.mu.Lock()
	s.current = sr
	s.mu.Unlock()
}

func (s *sink) Log(_ *core.Scenario, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		s.current.Logs = append(s.current.Logs, msg)
		if s.onAttach != nil {
			s.onAttach(s.current, Attachment{MediaType: "text/x.cucumber.log+plain", Body: []byte(msg)})
		}
	}
}

func (s *sink) Attach(_ *core.Scenario, mediaType string, body []byte, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		a := Attachment{MediaType: mediaType, Name: name, Body: body}
		s.current.Attachments = append(s.current.Attachments, a)
		if s.onAttach != nil {
			s.onAttach(s.current, a)
		}
	}
}

func (r *Runner) runScenario(ctx context.Context, p *feature.Pickle, worker int) *ScenarioResult {
	res := &ScenarioResult{Pickle: p, Started: r.opts.Now(), Attempt: 0, Worker: worker}
	scCtx := ctx
	if r.opts.ScenarioTimeout > 0 {
		var cancel context.CancelFunc
		scCtx, cancel = context.WithTimeout(ctx, r.opts.ScenarioTimeout)
		defer cancel()
	}
	sk := &sink{}
	sc := core.NewScenario(scCtx, core.ScenarioInfo{
		ID: p.Id, Name: p.Name, URI: p.Uri, Line: p.Line, Tags: p.TagNames,
	}, r.opts.Suite, sk)

	tc := r.msg.testCase(p, r.hooksFor(core.BeforeScenario, p.TagNames), r.hooksFor(core.AfterScenario, p.TagNames), r.opts.Registry)
	tcs := r.msg.testCaseStarted(tc, worker)
	sk.onAttach = func(sr *StepResult, a Attachment) { r.msg.attachment(tcs, tc, sr, a) }

	failed := false
	// Before hooks.
	for _, h := range r.hooksFor(core.BeforeScenario, p.TagNames) {
		sr := &StepResult{Hook: &h.Hook, Text: h.Hook.ID}
		res.Before = append(res.Before, sr)
		if failed || r.opts.DryRun {
			sr.Status = Skipped
			r.msg.hookStep(tcs, tc, sr, nil)
			continue
		}
		r.execHook(scCtx, sc, sk, h, sr, tcs, tc)
		if sr.Status != Passed {
			failed = true
		}
	}

	// Steps.
	for _, ps := range p.Steps {
		src := p.StepSource(ps)
		sr := &StepResult{
			Keyword: strings.TrimSpace(src.Keyword), Text: ps.Text, Line: src.Line,
			Background: src.Background, PickleStepID: ps.Id,
		}
		sr.Table, sr.DocString = stepArgument(ps)
		res.Steps = append(res.Steps, sr)
		r.execStep(scCtx, sc, sk, sr, failed, tcs, tc)
		if sr.Status != Passed {
			failed = true
		}
	}

	// Capture pack diagnostics before cleanup.
	if failed {
		res.Context = sc.Descriptions()
	}

	// After hooks always run (except in dry run).
	for _, h := range r.hooksFor(core.AfterScenario, p.TagNames) {
		sr := &StepResult{Hook: &h.Hook, Text: h.Hook.ID}
		res.After = append(res.After, sr)
		if r.opts.DryRun {
			sr.Status = Skipped
			r.msg.hookStep(tcs, tc, sr, nil)
			continue
		}
		r.execHook(ctx, sc, sk, h, sr, tcs, tc)
	}
	// Per-scenario state cleanup (closers registered by packs), which may
	// look at the outcome so far.
	sc.SetStatus(res.worst().String())
	if err := sc.Close(); err != nil {
		sr := &StepResult{Text: "scenario cleanup", Status: Failed, Err: err}
		res.After = append(res.After, sr)
	}

	res.Status = res.worst()
	res.Duration = r.opts.Now().Sub(res.Started)
	r.msg.testCaseFinished(tcs)
	return res
}

func (r *Runner) execStep(ctx context.Context, sc *core.Scenario, sk *sink, sr *StepResult, skip bool, tcs *testCaseState, tc *messages.TestCase) {
	matches := r.opts.Registry.Match(sr.Text)
	switch {
	case len(matches) == 0:
		sr.Status = Undefined
		sr.Suggestions = r.opts.Registry.Suggest(sr.Text, 3)
		r.msg.step(tcs, tc, sr, nil)
		return
	case len(matches) > 1:
		sr.Status = Ambiguous
		for _, m := range matches {
			sr.Candidates = append(sr.Candidates, fmt.Sprintf("%s (%s)", m.Variant.Expr, m.Def().Step.ID))
		}
		sr.Err = fmt.Errorf("ambiguous step: %d definitions match", len(matches))
		r.msg.step(tcs, tc, sr, nil)
		return
	}
	m := matches[0]
	sr.Match = &m
	if skip || r.opts.DryRun {
		sr.Status = Skipped
		r.msg.step(tcs, tc, sr, nil)
		return
	}
	r.msg.stepStarted(tcs, tc, sr)
	start := r.opts.Now()
	sk.set(sr)
	defer sk.set(nil)

	err := r.checkArgKind(m.Def().Step.Arg, sr)
	var args core.Args
	if err == nil {
		args, err = r.opts.Registry.Resolve(sc, m, sr.Text, sr.Table, sr.DocString)
	}
	if err == nil {
		for _, h := range r.hooksFor(core.BeforeStep, sc.Tags) {
			if err = r.callHook(ctx, sc, h); err != nil {
				break
			}
		}
	}
	if err == nil {
		timeout := r.opts.StepTimeout
		if t := m.Def().Step.Timeout; t > 0 {
			timeout = t
		}
		run := m.Def().Step.Run
		if run == nil {
			err = fmt.Errorf("step %s has no implementation", m.Def().Step.ID)
		} else {
			err = r.withTimeout(ctx, sc, "step", timeout, func() error { return run(sc, args) })
		}
	}
	for _, h := range r.hooksFor(core.AfterStep, sc.Tags) {
		if herr := r.callHook(ctx, sc, h); herr != nil && err == nil {
			err = herr
		}
	}
	sr.Duration = r.opts.Now().Sub(start)
	sr.Err = err
	sr.Status = statusOf(err)
	r.msg.stepFinished(tcs, tc, sr)
}

func (r *Runner) execHook(ctx context.Context, sc *core.Scenario, sk *sink, h *PackHook, sr *StepResult, tcs *testCaseState, tc *messages.TestCase) {
	r.msg.stepStarted(tcs, tc, sr)
	start := r.opts.Now()
	sk.set(sr)
	err := r.callHook(ctx, sc, h)
	sk.set(nil)
	sr.Duration = r.opts.Now().Sub(start)
	sr.Err = err
	sr.Status = statusOf(err)
	r.msg.stepFinished(tcs, tc, sr)
}

func (r *Runner) callHook(ctx context.Context, sc *core.Scenario, h *PackHook) error {
	if h.Hook.Run == nil {
		return nil
	}
	return r.withTimeout(ctx, sc, "hook "+h.Hook.ID, r.opts.HookTimeout, func() error { return h.Hook.Run(sc) })
}

// withTimeout runs fn with a deadline. Steps that ignore cancellation are
// abandoned after the grace period so a hung step cannot hang the run.
func (r *Runner) withTimeout(parent context.Context, sc *core.Scenario, what string, limit time.Duration, fn func() error) error {
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	prev := sc.Context()
	sc.SetContext(ctx)
	defer sc.SetContext(prev)

	done := make(chan error, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				done <- &PanicError{Value: v, Stack: string(debug.Stack())}
			}
		}()
		done <- fn()
	}()
	select {
	case err := <-done:
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
			return &TimeoutError{What: what, Limit: limit}
		}
		return err
	case <-ctx.Done():
		select {
		case <-done:
			if parent.Err() != nil {
				return parent.Err()
			}
			return &TimeoutError{What: what, Limit: limit}
		case <-time.After(r.opts.TimeoutGrace):
			if parent.Err() != nil {
				return parent.Err()
			}
			return &TimeoutError{What: what, Limit: limit, Ignored: true}
		}
	}
}

func (r *Runner) checkArgKind(kind core.ArgKind, sr *StepResult) error {
	switch kind {
	case core.ArgTable:
		if sr.Table == nil {
			return errors.New("this step requires a data table")
		}
	case core.ArgDocString:
		if sr.DocString == nil {
			return errors.New("this step requires a doc string")
		}
	case core.ArgNone:
		if sr.Table != nil {
			return errors.New("this step does not accept a data table")
		}
		if sr.DocString != nil {
			return errors.New("this step does not accept a doc string")
		}
	}
	return nil
}

func statusOf(err error) Status {
	switch {
	case err == nil:
		return Passed
	case errors.Is(err, core.ErrPending):
		return Pending
	case errors.Is(err, core.ErrSkip):
		return Skipped
	default:
		return Failed
	}
}

func stepArgument(ps *messages.PickleStep) (*core.Table, *core.DocString) {
	if ps.Argument == nil {
		return nil, nil
	}
	if dt := ps.Argument.DataTable; dt != nil {
		t := &core.Table{Rows: make([][]string, len(dt.Rows))}
		for i, row := range dt.Rows {
			for _, c := range row.Cells {
				t.Rows[i] = append(t.Rows[i], c.Value)
			}
		}
		return t, nil
	}
	if ds := ps.Argument.DocString; ds != nil {
		return nil, &core.DocString{Content: ds.Content, MediaType: ds.MediaType}
	}
	return nil, nil
}
