package report

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/runner"
)

const ordersFeature = `@orders
Feature: Orders
  Orders API behaviour.

  Background:
    Given the orders service is running

  Scenario: Create an order
    When I create an order:
      | sku | qty |
      | A1  | 2   |
    Then the response status code is 201

  @slow
  Scenario: Reject duplicate order
    When I send the order again
    Then the response body is:
      """json
      {"error": "duplicate", "code": 409}
      """

  Scenario: Cache responses
    When I send the order again
    Then the response is cached

  Scenario: Ambiguous check
    Then the order is valid

  Scenario: Pending work
    Then refunds are processed

  Scenario Outline: Order <qty> items
    When I order <qty> items
    Then the response status code is <status>

    Examples: quantities
      | qty | status |
      | 1   | 201    |
      | 0   | 400    |
`

const healthFeature = `Feature: Health

  Scenario: Panicky
    Given a panicking step

  Scenario: Slow
    Given a slow step

  Scenario: Skipped one
    Given a skipped step
    Then a panicking step

  Scenario: Broken setup
    Given a slow step
`

// fakeStack is a representative goroutine dump of a panicking step.
const fakeStack = `goroutine 42 [running]:
runtime/debug.Stack()
	/usr/local/go/src/runtime/debug/stack.go:26 +0x5e
github.com/nimbusxr/axx/internal/runner.(*Runner).withTimeout.func1.1()
	/src/axx/internal/runner/scenario.go:233 +0x45
panic({0x1029a4e80?, 0x102b8c2d0?})
	/usr/local/go/src/runtime/panic.go:787 +0x124
example.com/packs/health.panicky(0x14000132000, {0x0, 0x0})
	/work/packs/health/steps.go:17 +0x2c
github.com/nimbusxr/axx/internal/runner.(*Runner).execStep.func2()
	/src/axx/internal/runner/scenario.go:199 +0x38
`

var t0 = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

func testRegistry(t testing.TB) *match.Registry {
	t.Helper()
	reg := match.NewRegistry()
	noop := func(*core.Scenario, core.Args) error { return nil }
	packs := map[string][]core.StepDef{
		"orders": {
			{ID: "orders.running", Expr: "the orders service is running", Run: noop},
			{ID: "orders.create", Expr: "I create an order:", Arg: core.ArgTable, Run: noop},
			{ID: "orders.resend", Expr: "I send the order again", Run: noop},
			{ID: "orders.valid", Expr: "the order is valid", Run: noop},
			{ID: "orders.state", Expr: "the order is {word}", Run: noop},
			{ID: "orders.refunds", Expr: "refunds are processed", Run: noop},
			{ID: "orders.order", Expr: "I order {int} items", Run: noop},
		},
		"rest": {
			{ID: "rest.response.status", Expr: "the response status code is {int}", Run: noop},
			{ID: "rest.response.body", Expr: "the response body is:", Arg: core.ArgDocString, Run: noop},
			{ID: "rest.response.header", Expr: "the response header {word} is {string}", Run: noop},
		},
		"health": {
			{ID: "health.panic", Expr: "a panicking step", Run: noop},
			{ID: "health.slow", Expr: "a slow step", Run: noop},
			{ID: "health.skip", Expr: "a skipped step", Run: noop},
		},
	}
	for _, name := range []string{"orders", "rest", "health"} {
		if err := reg.AddSteps(name, packs[name]); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// syncIDs is a goroutine-safe incrementing ID generator shared by the
// parser and the runner so IDs never collide.
type syncIDs struct {
	mu   sync.Mutex
	next int
}

func (g *syncIDs) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return fmt.Sprint(g.next)
}

type fixtureDocs struct {
	docs    []*feature.Document
	pickles []*feature.Pickle
}

func parseFixtures(t testing.TB, ids func() string) fixtureDocs {
	t.Helper()
	var fx fixtureDocs
	for _, f := range []struct{ uri, src string }{
		{"features/orders.feature", ordersFeature},
		{"features/health.feature", healthFeature},
	} {
		doc, pickles, err := feature.ParseSource(f.uri, []byte(f.src), ids)
		if err != nil {
			t.Fatal(err)
		}
		fx.docs = append(fx.docs, doc)
		fx.pickles = append(fx.pickles, pickles...)
	}
	return fx
}

// outcome scripts the result of one matched step.
type outcome struct {
	status      runner.Status
	err         error
	dur         time.Duration
	logs        []string
	attachments []runner.Attachment
}

type scenarioScript struct {
	steps   map[int]outcome
	before  []*runner.StepResult
	after   []*runner.StepResult
	context map[string]any
}

// simulate builds a ScenarioResult the way the runner would: undefined and
// ambiguous steps come from the registry, matched steps follow the script
// and are skipped after the first non-passing step.
func simulate(reg *match.Registry, p *feature.Pickle, start time.Time, sc scenarioScript) *runner.ScenarioResult {
	res := &runner.ScenarioResult{Pickle: p, Started: start, Worker: 1, Before: sc.before, After: sc.after, Context: sc.context}
	failed := false
	total := 10 * time.Millisecond
	for _, h := range sc.before {
		failed = failed || h.Status != runner.Passed
		total += h.Duration
	}
	for i, ps := range p.Steps {
		src := p.StepSource(ps)
		sr := &runner.StepResult{
			Keyword: strings.TrimSpace(src.Keyword), Text: ps.Text, Line: src.Line,
			Background: src.Background, PickleStepID: ps.Id,
		}
		if arg := ps.Argument; arg != nil {
			if dt := arg.DataTable; dt != nil {
				sr.Table = &core.Table{}
				for _, row := range dt.Rows {
					var cells []string
					for _, c := range row.Cells {
						cells = append(cells, c.Value)
					}
					sr.Table.Rows = append(sr.Table.Rows, cells)
				}
			}
			if ds := arg.DocString; ds != nil {
				sr.DocString = &core.DocString{Content: ds.Content, MediaType: ds.MediaType}
			}
		}
		ms := reg.Match(ps.Text)
		switch {
		case len(ms) == 0:
			sr.Status = runner.Undefined
			sr.Suggestions = reg.Suggest(ps.Text, 3)
		case len(ms) > 1:
			sr.Status = runner.Ambiguous
			for _, m := range ms {
				sr.Candidates = append(sr.Candidates, fmt.Sprintf("%s (%s)", m.Variant.Expr, m.Def().Step.ID))
			}
			sr.Err = fmt.Errorf("ambiguous step: %d definitions match", len(ms))
		default:
			m := ms[0]
			sr.Match = &m
			o, scripted := sc.steps[i]
			switch {
			case failed:
				sr.Status = runner.Skipped
			case scripted:
				sr.Status, sr.Err, sr.Duration, sr.Logs, sr.Attachments = o.status, o.err, o.dur, o.logs, o.attachments
			default:
				sr.Status, sr.Duration = runner.Passed, 3*time.Millisecond
			}
		}
		failed = failed || sr.Status != runner.Passed
		total += sr.Duration
		res.Steps = append(res.Steps, sr)
	}
	for _, h := range sc.after {
		total += h.Duration
	}
	for _, group := range [][]*runner.StepResult{res.Before, res.Steps, res.After} {
		for _, s := range group {
			res.Status = max(res.Status, s.Status)
		}
	}
	res.Duration = total
	return res
}

func hookResult(id string, phase core.Phase, status runner.Status, dur time.Duration, err error) *runner.StepResult {
	return &runner.StepResult{Hook: &core.Hook{ID: id, Phase: phase}, Text: id, Status: status, Duration: dur, Err: err}
}

// handRun is the deterministic run behind the golden files. It covers every
// status, an assertion with a multi-line JSON diff, a panic, a timeout, a
// failing hook, an outline row, background steps, a data table, a doc
// string, logs, attachments, failure context and a not-run count.
func handRun(t testing.TB) (*runner.RunResult, fixtureDocs) {
	t.Helper()
	ids := &syncIDs{}
	fx := parseFixtures(t, ids.NewID)
	reg := testRegistry(t)
	scripts := map[string]scenarioScript{
		"Create an order": {
			steps:  map[int]outcome{1: {status: runner.Passed, dur: 150 * time.Millisecond}},
			before: []*runner.StepResult{hookResult("db.reset", core.BeforeScenario, runner.Passed, 5*time.Millisecond, nil)},
			after:  []*runner.StepResult{hookResult("db.cleanup", core.AfterScenario, runner.Passed, 2*time.Millisecond, nil)},
		},
		"Reject duplicate order": {
			steps: map[int]outcome{
				1: {
					status: runner.Passed, dur: 40 * time.Millisecond,
					logs: []string{"POST http://localhost:8080/orders -> 201 Created"},
					attachments: []runner.Attachment{
						{MediaType: "application/json", Name: "request.json", Body: []byte(`{"sku":"A1","qty":2}`)},
						{MediaType: "image/png", Name: "screenshot.png", Body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}},
					},
				},
				2: {status: runner.Failed, dur: 1250 * time.Millisecond, err: core.Fail("response body mismatch",
					`{"error": "duplicate", "code": 409}`,
					map[string]any{"id": 7, "status": "created", "sku": "A1"})},
			},
			context: map[string]any{"rest": map[string]any{
				"request":  map[string]any{"method": "POST", "url": "http://localhost:8080/orders"},
				"response": map[string]any{"status": 201, "body": map[string]any{"id": 7}},
			}},
		},
		"Pending work": {steps: map[int]outcome{1: {status: runner.Pending, err: core.ErrPending}}},
		"Order 0 items": {steps: map[int]outcome{2: {
			status: runner.Failed, dur: 12 * time.Millisecond, err: core.Fail("unexpected status code", 400, 201),
		}}},
		"Panicky": {steps: map[int]outcome{0: {status: runner.Failed, dur: time.Millisecond, err: &runner.PanicError{Value: "boom", Stack: fakeStack}}}},
		"Slow": {steps: map[int]outcome{0: {
			status: runner.Failed, dur: 2*time.Second + time.Millisecond, err: &runner.TimeoutError{What: "step", Limit: 2 * time.Second},
		}}},
		"Skipped one": {steps: map[int]outcome{0: {status: runner.Skipped, err: core.ErrSkip}}},
		"Broken setup": {
			before: []*runner.StepResult{hookResult("db.reset", core.BeforeScenario, runner.Failed, 30*time.Millisecond, errors.New("connection refused"))},
		},
	}
	res := &runner.RunResult{Started: t0, Duration: 12400 * time.Millisecond, NotRun: 2}
	for i, p := range fx.pickles {
		res.Scenarios = append(res.Scenarios, simulate(reg, p, t0.Add(time.Duration(i)*time.Second), scripts[p.Name]))
	}
	return res, fx
}

// handEnvelopes is a short, deterministic Cucumber Messages stream for the
// health feature, including characters that need escaping inside HTML.
func handEnvelopes(fx fixtureDocs) []*messages.Envelope {
	doc := fx.docs[1]
	ts := func(sec int64) *messages.Timestamp { return &messages.Timestamp{Seconds: t0.Unix() + sec} }
	envs := []*messages.Envelope{
		{Meta: &messages.Meta{
			ProtocolVersion: "34.2.1", Implementation: &messages.Product{Name: "axx", Version: "0.1.0"},
			Runtime: &messages.Product{Name: "go", Version: "go1.27.1"}, Os: &messages.Product{Name: "linux"}, Cpu: &messages.Product{Name: "amd64"},
		}},
		{Source: &messages.Source{Uri: doc.URI, Data: string(doc.Source), MediaType: messages.SourceMediaType_TEXT_X_CUCUMBER_GHERKIN_PLAIN}},
		{GherkinDocument: doc.AST},
	}
	var first *feature.Pickle
	for _, p := range fx.pickles {
		if p.Doc == doc {
			if first == nil {
				first = p
			}
			envs = append(envs, &messages.Envelope{Pickle: p.Pickle})
		}
	}
	envs = append(envs,
		&messages.Envelope{StepDefinition: &messages.StepDefinition{
			Id: "sd1", Pattern: &messages.StepDefinitionPattern{Source: "a panicking step", Type: messages.StepDefinitionPatternType_CUCUMBER_EXPRESSION},
			SourceReference: &messages.SourceReference{Uri: "axx:health"},
		}},
		&messages.Envelope{TestRunStarted: &messages.TestRunStarted{Id: "run1", Timestamp: ts(0)}},
		&messages.Envelope{TestCase: &messages.TestCase{Id: "tc1", PickleId: first.Id, TestRunStartedId: "run1", TestSteps: []*messages.TestStep{
			{Id: "ts1", PickleStepId: first.Steps[0].Id, StepDefinitionIds: []string{"sd1"}, StepMatchArgumentsLists: []*messages.StepMatchArgumentsList{{StepMatchArguments: []*messages.StepMatchArgument{}}}},
		}}},
		&messages.Envelope{TestCaseStarted: &messages.TestCaseStarted{Id: "tcs1", TestCaseId: "tc1", WorkerId: "1", Timestamp: ts(1)}},
		&messages.Envelope{TestStepStarted: &messages.TestStepStarted{TestCaseStartedId: "tcs1", TestStepId: "ts1", Timestamp: ts(1)}},
		&messages.Envelope{Attachment: &messages.Attachment{
			Body: "<script>alert('x')</script> & more", MediaType: "text/plain", ContentEncoding: messages.AttachmentContentEncoding_IDENTITY,
			TestCaseStartedId: "tcs1", TestStepId: "ts1", Timestamp: ts(1),
		}},
		&messages.Envelope{TestStepFinished: &messages.TestStepFinished{
			TestCaseStartedId: "tcs1", TestStepId: "ts1", Timestamp: ts(2),
			TestStepResult: &messages.TestStepResult{
				Duration: &messages.Duration{Nanos: 1000000}, Status: messages.TestStepResultStatus_FAILED, Message: "panic: boom </script>",
				Exception: &messages.Exception{Type: "Panic", Message: "panic: boom </script>", StackTrace: fakeStack},
			},
		}},
		&messages.Envelope{TestCaseFinished: &messages.TestCaseFinished{TestCaseStartedId: "tcs1", Timestamp: ts(2)}},
		&messages.Envelope{TestRunFinished: &messages.TestRunFinished{TestRunStartedId: "run1", Success: false, Timestamp: ts(3)}},
	)
	return envs
}
