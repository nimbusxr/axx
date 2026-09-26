package webcore

import (
	"path/filepath"
	"slices"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
)

// Each Gherkin step is a group in the traces of its scenario: the trace
// viewer shows the scenario step by step, each step's actions under it, at
// its line of the feature file. The group's location is also that of the
// step's calls (a patch of the driver), which Playwright's Inspector shows
// when the run pauses.

func stepHooks() []core.Hook {
	return []core.Hook{
		{ID: "web-core.step.start", Phase: core.BeforeStep, Run: func(sc *core.Scenario) error {
			if step := sc.Step(); step != nil {
				if st, ok := scenarioPages.Peek(sc); ok {
					st.mu.Lock()
					st.lastStep = step
					st.mu.Unlock()
				}
				for _, s := range sessionsOf(sc) {
					s.startGroup(sc, step)
				}
				pauseAtStep(sc, step)
			}
			return nil
		}},
		{ID: "web-core.step.end", Phase: core.AfterStep, Run: func(sc *core.Scenario) error {
			for _, s := range sessionsOf(sc) {
				s.endGroup()
			}
			return nil
		}},
	}
}

// sessionsOf are the scenario's sessions, without starting any.
func sessionsOf(sc *core.Scenario) []*session {
	st, ok := scenarioPages.Peek(sc)
	if !ok {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return slices.Clone(st.order)
}

// featurePath is the absolute path of the scenario's feature file.
func featurePath(sc *core.Scenario) string {
	if filepath.IsAbs(sc.URI) {
		return sc.URI
	}
	return filepath.Join(sc.Suite().ProjectDir(), filepath.FromSlash(sc.URI))
}

// startGroup starts the step's group in the session's trace. Without a
// trace, Playwright keeps the group's location all the same: the location
// of the step's calls, which the Inspector shows. The scenario's goroutine
// alone starts and ends groups.
func (s *session) startGroup(sc *core.Scenario, step *core.StepInfo) {
	if s.group {
		return
	}
	loc := &playwright.TracingGroupOptionsLocation{File: featurePath(sc), Line: playwright.Int(step.Line)}
	if err := s.ctx.Tracing().Group(step.Keyword+" "+step.Text, playwright.TracingGroupOptions{Location: loc}); err == nil {
		s.group = true
	}
}

// endGroup ends the session's step group, if one is open.
func (s *session) endGroup() {
	if s.group {
		_ = s.ctx.Tracing().GroupEnd()
		s.group = false
	}
}
