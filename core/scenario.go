package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ScenarioInfo identifies a running scenario (one pickle execution).
type ScenarioInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	URI     string   `json:"uri"`
	Line    int      `json:"line"`
	Tags    []string `json:"tags,omitempty"`
	Attempt int      `json:"attempt,omitempty"`
}

// Sink receives a scenario's logs and attachments. Hosts implement it.
type Sink interface {
	Log(sc *Scenario, msg string)
	Attach(sc *Scenario, mediaType string, body []byte, name string)
}

// Scenario is the per-scenario execution context handed to steps and hooks.
// Steps within one scenario run sequentially; Scenario is nonetheless safe
// for concurrent use.
type Scenario struct {
	ScenarioInfo

	suite *Suite
	sink  Sink

	mu         sync.Mutex
	ctx        context.Context
	state      map[any]any
	closers    []namedCloser
	describers map[string]func() any
	status     string
	started    time.Time
}

type namedCloser struct {
	name string
	fn   func() error
}

// NewScenario creates a scenario. It is intended for axx hosts and for
// testing step packs; step code receives scenarios from the host.
func NewScenario(ctx context.Context, info ScenarioInfo, suite *Suite, sink Sink) *Scenario {
	if suite == nil {
		suite = NewSuite(SuiteOptions{})
	}
	return &Scenario{
		ScenarioInfo: info,
		suite:        suite,
		sink:         sink,
		ctx:          ctx,
		state:        map[any]any{},
		started:      time.Now(),
	}
}

// Started is when the scenario started: what arrives from the services
// under test before it belongs to earlier scenarios.
func (sc *Scenario) Started() time.Time { return sc.started }

// Context returns the context of the currently executing step or hook; it is
// cancelled when the step times out or the run is interrupted.
func (sc *Scenario) Context() context.Context {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.ctx
}

// SetContext replaces the active context. Hosts call it around each step.
func (sc *Scenario) SetContext(ctx context.Context) {
	sc.mu.Lock()
	sc.ctx = ctx
	sc.mu.Unlock()
}

// Suite returns the suite-scoped context (configuration, shared resources).
func (sc *Scenario) Suite() *Suite { return sc.suite }

// Log records a message attached to the current step.
func (sc *Scenario) Log(format string, args ...any) {
	if sc.sink != nil {
		sc.sink.Log(sc, fmt.Sprintf(format, args...))
	}
}

// Attach records an attachment (request/response bodies, query results...)
// on the current step.
func (sc *Scenario) Attach(mediaType string, body []byte, name string) {
	if sc.sink != nil {
		sc.sink.Attach(sc, mediaType, body, name)
	}
}

// Describe registers fn to describe pack state when the scenario fails, for
// example the last HTTP request and response. Results appear in failure
// reports (the agent JSON "context" field). Registering the same name again
// replaces the previous describer.
func (sc *Scenario) Describe(name string, fn func() any) {
	sc.mu.Lock()
	if sc.describers == nil {
		sc.describers = map[string]func() any{}
	}
	sc.describers[name] = fn
	sc.mu.Unlock()
}

// Descriptions evaluates registered describers. Hosts call it on failure.
func (sc *Scenario) Descriptions() map[string]any {
	sc.mu.Lock()
	fns := make(map[string]func() any, len(sc.describers))
	for k, v := range sc.describers {
		fns[k] = v
	}
	sc.mu.Unlock()
	if len(fns) == 0 {
		return nil
	}
	out := make(map[string]any, len(fns))
	for k, fn := range fns {
		if v := fn(); v != nil {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Status reports the scenario's outcome so far: "passed", "failed",
// "pending", "skipped", "undefined" or "ambiguous". Hosts set it before
// running closers, so cleanup code can, for example, keep resources for
// inspection when the scenario failed. It is empty while steps still run.
func (sc *Scenario) Status() string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.status
}

// SetStatus records the scenario's outcome. Hosts call it before Close.
func (sc *Scenario) SetStatus(status string) {
	sc.mu.Lock()
	sc.status = status
	sc.mu.Unlock()
}

// OnClose registers fn to run when the scenario ends, in reverse
// registration order. A returned error fails the scenario, like a failing
// After hook.
func (sc *Scenario) OnClose(name string, fn func() error) {
	sc.mu.Lock()
	sc.closers = append(sc.closers, namedCloser{name, fn})
	sc.mu.Unlock()
}

// Close runs registered closers (LIFO) and returns their joined errors.
// Hosts call it exactly once when the scenario finishes.
func (sc *Scenario) Close() error {
	sc.mu.Lock()
	closers := sc.closers
	sc.closers = nil
	sc.mu.Unlock()
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].fn(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", closers[i].name, err))
		}
	}
	return errors.Join(errs...)
}

// StateKey declares a typed slot of per-scenario state, created lazily on
// first use and optionally cleaned up when the scenario ends. Packs keep
// their registries (services, requests, selections...) in state keys.
type StateKey[T any] struct {
	name  string
	init  func(*Scenario) *T
	close func(*Scenario, *T) error
}

// NewStateKey declares per-scenario state. init must not be nil; close may be.
func NewStateKey[T any](name string, init func(*Scenario) *T, close func(*Scenario, *T) error) *StateKey[T] {
	return &StateKey[T]{name: name, init: init, close: close}
}

// Of returns the scenario's value for the key, creating it on first use.
func (k *StateKey[T]) Of(sc *Scenario) *T {
	sc.mu.Lock()
	if v, ok := sc.state[k]; ok {
		sc.mu.Unlock()
		return v.(*T)
	}
	sc.mu.Unlock()

	v := k.init(sc) // outside the lock: init may use the scenario

	sc.mu.Lock()
	defer sc.mu.Unlock()
	if existing, ok := sc.state[k]; ok {
		return existing.(*T)
	}
	sc.state[k] = v
	if k.close != nil {
		sc.closers = append(sc.closers, namedCloser{k.name, func() error { return k.close(sc, v) }})
	}
	return v
}

// Peek returns the value if it was already created, without creating it.
func (k *StateKey[T]) Peek(sc *Scenario) (*T, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	v, ok := sc.state[k]
	if !ok {
		return nil, false
	}
	return v.(*T), true
}
