package runner

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// emitter produces Cucumber Messages. All methods are nil-safe so the runner
// pays nothing when no messages-based reporter is configured.
type emitter struct {
	r          *Runner
	variantIDs map[*match.Variant]string
	hookIDs    map[*PackHook]string
	runID      string
}

type testCaseState struct {
	id      string
	steps   []*messages.TestStep
	cursor  int
	mu      sync.Mutex
	ids     map[*StepResult]string
	started time.Time
}

func newEmitter(r *Runner) *emitter {
	return &emitter{r: r, variantIDs: map[*match.Variant]string{}, hookIDs: map[*PackHook]string{}}
}

func (e *emitter) emit(env *messages.Envelope) {
	e.r.report(func(rep Reporter) { rep.Envelope(env) })
}

func (e *emitter) ts() *messages.Timestamp {
	t := messages.GoTimeToTimestamp(e.r.opts.Now())
	return &t
}

func (e *emitter) runStarted(pickles []*feature.Pickle) {
	if e == nil {
		return
	}
	meta := e.r.opts.Meta
	if meta == nil {
		meta = &messages.Meta{Implementation: &messages.Product{Name: "axx"}}
	}
	if meta.ProtocolVersion == "" {
		meta.ProtocolVersion = "34.2.1"
	}
	if meta.Runtime == nil {
		meta.Runtime = &messages.Product{Name: "go", Version: runtime.Version()}
	}
	if meta.Os == nil {
		meta.Os = &messages.Product{Name: runtime.GOOS}
	}
	if meta.Cpu == nil {
		meta.Cpu = &messages.Product{Name: runtime.GOARCH}
	}
	e.emit(&messages.Envelope{Meta: meta})

	selected := map[*feature.Document]bool{}
	for _, p := range pickles {
		selected[p.Doc] = true
	}
	for _, d := range e.r.opts.Docs {
		if !selected[d] {
			continue
		}
		e.emit(&messages.Envelope{Source: &messages.Source{Uri: d.URI, Data: string(d.Source), MediaType: messages.SourceMediaType_TEXT_X_CUCUMBER_GHERKIN_PLAIN}})
		e.emit(&messages.Envelope{GherkinDocument: d.AST})
	}
	for _, p := range pickles {
		e.emit(&messages.Envelope{Pickle: p.Pickle})
	}
	reg := e.r.opts.Registry
	for _, p := range reg.Params() {
		if p.Pack == "cucumber" {
			continue
		}
		e.emit(&messages.Envelope{ParameterType: &messages.ParameterType{
			Id: e.r.opts.NewID(), Name: p.Type.Name, RegularExpressions: p.Type.Regexps, UseForSnippets: p.Type.Snippet,
		}})
	}
	for _, v := range reg.Variants() {
		id := e.r.opts.NewID()
		e.variantIDs[v] = id
		src := v.Def.Step.Source
		e.emit(&messages.Envelope{StepDefinition: &messages.StepDefinition{
			Id:              id,
			Pattern:         &messages.StepDefinitionPattern{Source: v.Expr, Type: messages.StepDefinitionPatternType_CUCUMBER_EXPRESSION},
			SourceReference: sourceRef(v.Def.Pack, src),
		}})
	}
	for _, phase := range []core.Phase{core.BeforeScenario, core.AfterScenario, core.BeforeStep, core.AfterStep} {
		for _, h := range e.r.hooks[phase] {
			id := e.r.opts.NewID()
			e.hookIDs[h] = id
			typ := messages.HookType_BEFORE_TEST_CASE
			switch h.Hook.Phase {
			case core.AfterScenario:
				typ = messages.HookType_AFTER_TEST_CASE
			case core.BeforeStep:
				typ = messages.HookType_BEFORE_TEST_STEP
			case core.AfterStep:
				typ = messages.HookType_AFTER_TEST_STEP
			}
			e.emit(&messages.Envelope{Hook: &messages.Hook{
				Id: id, Name: h.Hook.ID, TagExpression: h.Hook.Tags, Type: typ,
				SourceReference: sourceRef(h.Pack, h.Hook.Source),
			}})
		}
	}
	e.runID = e.r.opts.NewID()
	e.emit(&messages.Envelope{TestRunStarted: &messages.TestRunStarted{Id: e.runID, Timestamp: e.ts()}})
}

func sourceRef(pack string, src core.SourceRef) *messages.SourceReference {
	uri := src.URI
	if uri == "" {
		uri = "axx:" + pack
	}
	ref := &messages.SourceReference{Uri: uri}
	if src.Line > 0 {
		ref.Location = &messages.Location{Line: int64(src.Line)}
	}
	return ref
}

func (e *emitter) testCase(p *feature.Pickle, before, after []*PackHook, reg *match.Registry) *messages.TestCase {
	if e == nil {
		return nil
	}
	tc := &messages.TestCase{Id: e.r.opts.NewID(), PickleId: p.Id, TestRunStartedId: e.runID}
	for _, h := range before {
		tc.TestSteps = append(tc.TestSteps, &messages.TestStep{Id: e.r.opts.NewID(), HookId: e.hookIDs[h]})
	}
	for _, ps := range p.Steps {
		ts := &messages.TestStep{Id: e.r.opts.NewID(), PickleStepId: ps.Id}
		ts.StepDefinitionIds = []string{}
		ts.StepMatchArgumentsLists = []*messages.StepMatchArgumentsList{}
		for _, m := range reg.Match(ps.Text) {
			ts.StepDefinitionIds = append(ts.StepDefinitionIds, e.variantIDs[m.Variant])
			list := &messages.StepMatchArgumentsList{StepMatchArguments: []*messages.StepMatchArgument{}}
			for _, a := range m.Args {
				if !a.Present {
					continue
				}
				list.StepMatchArguments = append(list.StepMatchArguments, &messages.StepMatchArgument{
					ParameterTypeName: a.Param,
					Group:             &messages.Group{Start: int64(a.Start), Value: a.Raw, Children: childGroups(a.Groups)},
				})
			}
			ts.StepMatchArgumentsLists = append(ts.StepMatchArgumentsLists, list)
		}
		tc.TestSteps = append(tc.TestSteps, ts)
	}
	for _, h := range after {
		tc.TestSteps = append(tc.TestSteps, &messages.TestStep{Id: e.r.opts.NewID(), HookId: e.hookIDs[h]})
	}
	e.emit(&messages.Envelope{TestCase: tc})
	return tc
}

func childGroups(gs []*string) []*messages.Group {
	var out []*messages.Group
	for _, g := range gs {
		mg := &messages.Group{}
		if g != nil {
			mg.Value = *g
		}
		out = append(out, mg)
	}
	return out
}

func (e *emitter) testCaseStarted(tc *messages.TestCase, worker int) *testCaseState {
	if e == nil {
		return nil
	}
	tcs := &testCaseState{id: e.r.opts.NewID(), steps: tc.TestSteps, ids: map[*StepResult]string{}, started: e.r.opts.Now()}
	e.emit(&messages.Envelope{TestCaseStarted: &messages.TestCaseStarted{
		Id: tcs.id, TestCaseId: tc.Id, Attempt: 0, WorkerId: fmt.Sprint(worker), Timestamp: e.ts(),
	}})
	return tcs
}

func (e *emitter) stepStarted(tcs *testCaseState, _ *messages.TestCase, sr *StepResult) {
	if e == nil || tcs == nil {
		return
	}
	tcs.mu.Lock()
	id := ""
	if tcs.cursor < len(tcs.steps) {
		id = tcs.steps[tcs.cursor].Id
		tcs.cursor++
	}
	tcs.ids[sr] = id
	tcs.mu.Unlock()
	e.emit(&messages.Envelope{TestStepStarted: &messages.TestStepStarted{TestCaseStartedId: tcs.id, TestStepId: id, Timestamp: e.ts()}})
}

func (e *emitter) stepFinished(tcs *testCaseState, _ *messages.TestCase, sr *StepResult) {
	if e == nil || tcs == nil {
		return
	}
	tcs.mu.Lock()
	id := tcs.ids[sr]
	tcs.mu.Unlock()
	e.r.report(func(rep Reporter) {
		if sr2, ok := rep.(StepReporter); ok {
			sr2.StepFinished(tcs.id, id, sr)
		}
	})
	d := messages.GoDurationToDuration(sr.Duration)
	result := &messages.TestStepResult{Duration: &d, Status: toMessageStatus(sr.Status)}
	if sr.Err != nil {
		result.Message = sr.Err.Error()
		result.Exception = &messages.Exception{Type: errorType(sr.Err), Message: sr.Err.Error()}
		var pe *PanicError
		if errors.As(sr.Err, &pe) {
			result.Exception.StackTrace = pe.Stack
		}
	}
	e.emit(&messages.Envelope{TestStepFinished: &messages.TestStepFinished{
		TestCaseStartedId: tcs.id, TestStepId: id, TestStepResult: result, Timestamp: e.ts(),
	}})
}

// step emits a started+finished pair for steps that did not execute.
func (e *emitter) step(tcs *testCaseState, tc *messages.TestCase, sr *StepResult, _ error) {
	e.stepStarted(tcs, tc, sr)
	e.stepFinished(tcs, tc, sr)
}

func (e *emitter) hookStep(tcs *testCaseState, tc *messages.TestCase, sr *StepResult, err error) {
	e.step(tcs, tc, sr, err)
}

func (e *emitter) attachment(tcs *testCaseState, _ *messages.TestCase, sr *StepResult, a Attachment) {
	if e == nil || tcs == nil {
		return
	}
	tcs.mu.Lock()
	id := tcs.ids[sr]
	tcs.mu.Unlock()
	body, enc := encodeBody(a)
	e.emit(&messages.Envelope{Attachment: &messages.Attachment{
		Body: body, ContentEncoding: enc, MediaType: a.MediaType, FileName: a.Name,
		TestCaseStartedId: tcs.id, TestStepId: id, Timestamp: e.ts(),
	}})
}

func (e *emitter) testCaseFinished(tcs *testCaseState) {
	if e == nil || tcs == nil {
		return
	}
	e.emit(&messages.Envelope{TestCaseFinished: &messages.TestCaseFinished{TestCaseStartedId: tcs.id, Timestamp: e.ts()}})
}

func (e *emitter) runFinished(res *RunResult) {
	if e == nil {
		return
	}
	finished := &messages.TestRunFinished{
		Success: res.Worst() <= Skipped && !res.Interrupted, Timestamp: e.ts(), TestRunStartedId: e.runID,
	}
	if len(res.RunErrors) > 0 {
		var msgs []string
		for _, re := range res.RunErrors {
			msgs = append(msgs, re.Source+": "+re.Message)
		}
		finished.Exception = &messages.Exception{Type: "axx.RunError", Message: strings.Join(msgs, "\n\n")}
	}
	e.emit(&messages.Envelope{TestRunFinished: finished})
}

func toMessageStatus(s Status) messages.TestStepResultStatus {
	switch s {
	case Passed:
		return messages.TestStepResultStatus_PASSED
	case Skipped:
		return messages.TestStepResultStatus_SKIPPED
	case Pending:
		return messages.TestStepResultStatus_PENDING
	case Undefined:
		return messages.TestStepResultStatus_UNDEFINED
	case Ambiguous:
		return messages.TestStepResultStatus_AMBIGUOUS
	case Failed:
		return messages.TestStepResultStatus_FAILED
	}
	return messages.TestStepResultStatus_UNKNOWN
}

func errorType(err error) string {
	switch {
	case core.IsAssertion(err):
		return "AssertionError"
	default:
		var te *TimeoutError
		if errors.As(err, &te) {
			return "TimeoutError"
		}
		var pe *PanicError
		if errors.As(err, &pe) {
			return "Panic"
		}
		return fmt.Sprintf("%T", err)
	}
}

func encodeBody(a Attachment) (string, messages.AttachmentContentEncoding) {
	if isText(a.MediaType) {
		return string(a.Body), messages.AttachmentContentEncoding_IDENTITY
	}
	return base64(a.Body), messages.AttachmentContentEncoding_BASE64
}
