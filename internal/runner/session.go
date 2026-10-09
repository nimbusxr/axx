package runner

import (
	"context"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
)

// Session is one scenario that stays open while steps run in it a few at a
// time, as a run runs them: the live scenario of `axx mcp`, where an agent
// tries steps before it writes them down.
type Session struct {
	r  *Runner
	sk *sink
	sc *core.Scenario

	mu sync.Mutex
	// line is the last line of the session's steps: those of each call
	// come after it, as if in one feature file.
	line int
}

// NewSession starts a session in ctx, which it lasts for: its scenario, and
// the packs' Before hooks, whose results come back.
func (r *Runner) NewSession(ctx context.Context, info core.ScenarioInfo) (*Session, []*StepResult) {
	s := &Session{r: r, sk: &sink{step: -1}}
	s.sc = core.NewScenario(ctx, info, r.opts.Suite, s.sk)
	var out []*StepResult
	for _, h := range r.hooksFor(core.BeforeScenario, info.Tags) {
		sr := &StepResult{Hook: &h.Hook, Text: h.Hook.ID}
		r.execHook(ctx, s.sc, s.sk, h, sr, nil, nil)
		out = append(out, sr)
	}
	return s, out
}

// Scenario is the session's scenario.
func (s *Session) Scenario() *core.Scenario { return s.sc }

// Run runs the steps of a pickle in the session's scenario, in order: those
// after one that does not pass are skipped.
func (s *Session) Run(p *feature.Pickle) []*StepResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*StepResult
	failed, last := false, s.line
	// The session's steps so far, and these after them.
	progress := s.sc.Progress()
	first := len(progress)
	for _, sp := range progressOf(p) {
		sp.Line += s.line
		progress = append(progress, sp)
	}
	s.sc.SetProgress(progress)
	for i, ps := range p.Steps {
		src := p.StepSource(ps)
		sr := &StepResult{Keyword: strings.TrimSpace(src.Keyword), Text: ps.Text, Line: s.line + src.Line, PickleStepID: ps.Id}
		sr.Table, sr.DocString = stepArgument(ps)
		s.r.execStep(s.sc.Context(), s.sc, s.sk, sr, first+i, failed, nil, nil)
		s.sc.UpdateProgress(first+i, func(sp *core.StepProgress) {
			sp.Status = sr.Status.String()
			if sr.Err != nil {
				sp.Error = sr.Err.Error()
			}
		})
		out = append(out, sr)
		failed = failed || sr.Status != Passed
		last = max(last, sr.Line)
	}
	s.line = last
	return out
}

// Close ends the session: the packs' After hooks, then their cleanup.
func (s *Session) Close(status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.r.hooksFor(core.AfterScenario, s.sc.Tags) {
		sr := &StepResult{Hook: &h.Hook, Text: h.Hook.ID}
		s.r.execHook(context.WithoutCancel(s.sc.Context()), s.sc, s.sk, h, sr, nil, nil)
	}
	s.sc.SetStatus(status.String())
	return s.sc.Close()
}
