// Package hostscenario is what the packs with a side in the axx host
// (core.HostServer) share of it: a run's scenarios as the host has them.
//
// A run in an agent's sandbox cannot start a simulator, an emulator or a
// desktop app. When an axx host serves the project, the pack's side there
// does that work with the pack's own code, in a scenario of the host's own
// for each of the run's: made when the run's scenario first needs the host,
// in a suite of the project's packs confined to the project
// (core.HostRun.Suite), set up by the packs that build on this one
// (core.HostScenario), and closed when the run's ends or its connection
// does. What the host's scenario logs and attaches goes to the run's, a wait
// for what scenarios share stops the run's step clock, and what the packs
// note of the scenario on one side (core.Scenario.Note) reaches the other
// before the next thing either does there.
//
// The run is an agent's. It calls the operations the pack's side has, by
// name, and nothing else (package hostwire).
package hostscenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/hostwire"
)

// What both sides say of a scenario.
type (
	// scenarioParams makes one of the run's scenarios known to the host.
	scenarioParams struct {
		Scenario string            `json:"scenario"`
		Info     core.ScenarioInfo `json:"info"`
		// Dir is the feature file's folder, where the files its steps name
		// are looked for first.
		Dir   string            `json:"dir"`
		Notes map[string]string `json:"notes,omitempty"`
	}
	// notesParams are a scenario's notes that one side has and the other
	// does not know yet.
	notesParams struct {
		Scenario string            `json:"scenario"`
		Notes    map[string]string `json:"notes"`
	}
	scenarioRef struct {
		Scenario string `json:"scenario"`
	}
	toldLog struct {
		Scenario string `json:"scenario"`
		Text     string `json:"text"`
	}
	toldAttachment struct {
		Scenario  string `json:"scenario"`
		MediaType string `json:"mediaType"`
		Body      []byte `json:"body"`
		Name      string `json:"name"`
	}
)

// Link is a run's connection to a pack's side in the host: one for the run,
// which its scenarios share.
type Link struct {
	// Pack is the pack whose side in the host it is to.
	Pack   string
	client *hostwire.Client
	told   func(op string, params json.RawMessage)

	mu        sync.Mutex
	scenarios map[string]*core.Scenario
}

// Open is the suite's connection to pack's side in the host, opened when a
// scenario first needs it and closed with the suite: the host then stops
// what it ran for the run. told hears what the pack's side tells the run
// beyond what this package carries; it may be nil.
func Open(sc *core.Scenario, pack string, told func(op string, params json.RawMessage)) (*Link, error) {
	return core.Cached(sc.Suite(), "hostscenario/"+pack, func() (*Link, error) {
		// The connection outlives the scenario that opens it.
		conn, err := sc.Suite().HostDial(context.WithoutCancel(sc.Context()), pack)
		if err != nil {
			return nil, fmt.Errorf("the axx host's %s pack: %w", pack, err)
		}
		l := &Link{Pack: pack, told: told, scenarios: map[string]*core.Scenario{}}
		l.client = hostwire.NewClient(conn, l.hear)
		sc.Suite().OnClose(func(context.Context) error { return l.client.Close() })
		return l, nil
	})
}

// hear takes what the host tells the run.
func (l *Link) hear(op string, params json.RawMessage) {
	switch op {
	case "log":
		var m toldLog
		if hostwire.Read(params, &m) == nil {
			if sc := l.Scenario(m.Scenario); sc != nil {
				sc.Log("%s", m.Text)
			}
		}
	case "attach":
		var m toldAttachment
		if hostwire.Read(params, &m) == nil {
			if sc := l.Scenario(m.Scenario); sc != nil {
				sc.Attach(m.MediaType, m.Body, m.Name)
			}
		}
	case "hold", "release":
		// The host's side waits for what the run's scenarios share (a device
		// or a desktop another one has): that is not the step's own time.
		var m scenarioRef
		if hostwire.Read(params, &m) == nil {
			if sc := l.Scenario(m.Scenario); sc != nil {
				st := scenarios.Of(sc)
				st.toldMu.Lock()
				if op == "hold" {
					st.holds = append(st.holds, sc.Hold())
				} else if n := len(st.holds); n > 0 {
					st.holds[n-1]()
					st.holds = st.holds[:n-1]
				}
				st.toldMu.Unlock()
			}
		}
	case "notes":
		var m notesParams
		if hostwire.Read(params, &m) == nil {
			if sc := l.Scenario(m.Scenario); sc != nil {
				scenarios.Of(sc).heard(sc, l, m.Notes)
			}
		}
	default:
		if l.told != nil {
			l.told(op, params)
		}
	}
}

// Scenario is the run's scenario the host knows by key, or nil once it
// ended.
func (l *Link) Scenario(key string) *core.Scenario {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scenarios[key]
}

// count numbers a run's scenarios for the host: a scenario's ID is its
// feature's, and a scenario that runs again (a retry, an outline's row) is
// another scenario there.
var count atomic.Uint64

// state is what a run's scenario has in the host.
type state struct {
	key string

	// mu is held while the scenario is made known to a link's side, a call.
	mu     sync.Mutex
	joined map[*Link]bool

	// toldMu guards what the host tells the run of while the run waits for
	// a call's answer: it is never held over a call.
	toldMu sync.Mutex
	holds  []func() // the step's clock, stopped for the host's waits
	// noted is what each link's side in the host knows of the scenario's
	// notes.
	noted map[*Link]map[string]string
}

var scenarios = core.NewStateKey("hostscenario", func(sc *core.Scenario) *state {
	return &state{
		key:    fmt.Sprintf("%d %s", count.Add(1), sc.ID),
		joined: map[*Link]bool{}, noted: map[*Link]map[string]string{},
	}
}, func(sc *core.Scenario, s *state) error {
	// The scenario ends in the host too: what it kept there for it goes.
	s.mu.Lock()
	links := make([]*Link, 0, len(s.joined))
	for l := range s.joined {
		links = append(links, l)
	}
	s.mu.Unlock()
	var first error
	for _, l := range links {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(sc.Context()), 2*time.Minute)
		err := l.client.Call(ctx, "end", scenarioRef{Scenario: s.key}, nil)
		cancel()
		l.mu.Lock()
		delete(l.scenarios, s.key)
		l.mu.Unlock()
		if err != nil && first == nil {
			first = err
		}
	}
	return first
})

// Key is the scenario's key in the host, which every call about it says.
// The scenario's place in the host is kept from the first time a pack asks
// for its key: a pack asks as an app registers, so that the place ends
// after what the scenario makes later (its running apps, stopped in the
// host first).
func Key(sc *core.Scenario) string { return scenarios.Of(sc).key }

// unsent are the scenario's notes that l's side in the host does not know
// yet, which the caller sends it now.
func (s *state) unsent(sc *core.Scenario, l *Link) map[string]string {
	notes := sc.Notes()
	s.toldMu.Lock()
	defer s.toldMu.Unlock()
	known := s.noted[l]
	if known == nil {
		known = map[string]string{}
		s.noted[l] = known
	}
	var out map[string]string
	for name, value := range notes {
		if v, ok := known[name]; ok && v == value {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name], known[name] = value, value
	}
	return out
}

// heard notes in the run's scenario what a pack noted in the host's.
func (s *state) heard(sc *core.Scenario, l *Link, notes map[string]string) {
	s.toldMu.Lock()
	if s.noted[l] == nil {
		s.noted[l] = map[string]string{}
	}
	for name, value := range notes {
		s.noted[l][name] = value
	}
	s.toldMu.Unlock()
	for name, value := range notes {
		sc.Note(name, value)
	}
}

// join makes the scenario known to l's side in the host, the first time,
// and sends it what the scenario's packs noted since it last heard.
func (l *Link) join(ctx context.Context, sc *core.Scenario) error {
	s := scenarios.Of(sc)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.joined[l] {
		l.mu.Lock()
		l.scenarios[s.key] = sc
		l.mu.Unlock()
		p := scenarioParams{Scenario: s.key, Info: sc.ScenarioInfo, Dir: sc.Dir, Notes: s.unsent(sc, l)}
		if err := l.client.Call(ctx, "scenario", p, nil); err != nil {
			return err
		}
		s.joined[l] = true
		return nil
	}
	notes := s.unsent(sc, l)
	if len(notes) == 0 {
		return nil
	}
	return l.client.Call(ctx, "notes", notesParams{Scenario: s.key, Notes: notes}, nil)
}

// Call asks the pack's side in the host for an operation in sc's scenario
// there, and waits for its answer (hostwire.Client.Call). The host knows
// the scenario first, and what its packs noted of it: the pack's side may
// act on a note in this call. A nil sc is a scenario that ended in the run:
// the call goes as it is.
func (l *Link) Call(ctx context.Context, sc *core.Scenario, op string, params, result any) error {
	if sc != nil {
		if err := l.join(ctx, sc); err != nil {
			return err
		}
	}
	return l.client.Call(ctx, op, params, result)
}

// Host is one run's connection, in the host.
type Host struct {
	ctx  context.Context
	run  core.HostRun
	srv  *hostwire.Server
	side Side

	mu        sync.Mutex
	scenarios map[string]*Mirror
}

// Mirror is one of the run's scenarios, in the host.
type Mirror struct {
	// Key is what the run calls it.
	Key string
	// SC is the host's scenario for it.
	SC *core.Scenario
	// Data is the pack's own for the scenario (Side.Begin).
	Data any

	cancel context.CancelFunc
	// op is held by the call that works in the scenario: a scenario's steps
	// come one after the other, and the scenario's context is the call's.
	op sync.Mutex

	mu sync.Mutex
	// noted is what the run knows of the scenario's notes.
	noted map[string]string
}

// Side is a pack's side in the host.
type Side struct {
	// Ops are the pack's operations, by the names the run calls them.
	Ops func(h *Host) map[string]hostwire.Handler
	// Begin makes the pack's own for a scenario the run made known
	// (Mirror.Data); it may be nil.
	Begin func(m *Mirror) any
	// End stops what the pack runs for a scenario as it ends, before the
	// scenario closes; it may be nil.
	End func(ctx context.Context, m *Mirror) error
}

// Serve serves a pack's side in the axx host over conn, until the run
// closes it or ctx ends: then every scenario the run left is ended.
func Serve(ctx context.Context, run core.HostRun, conn net.Conn, side Side) error {
	defer conn.Close()
	h := &Host{ctx: ctx, run: run, side: side, scenarios: map[string]*Mirror{}}
	ops := map[string]hostwire.Handler{}
	if side.Ops != nil {
		ops = side.Ops(h)
	}
	ops["scenario"] = hostwire.Op(h.scenario)
	ops["notes"] = hostwire.Op(h.notes)
	ops["end"] = hostwire.Op(h.end)
	h.srv = hostwire.NewServer(conn, ops)
	err := h.srv.Serve(ctx, conn)
	h.close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// Run is what the host gives the pack's side for the run.
func (h *Host) Run() core.HostRun { return h.run }

// Tell tells the run something outside any call's answer.
func (h *Host) Tell(op string, params any) { h.srv.Tell(op, params) }

// sink sends what the host's scenario logs and attaches to the run's.
type sink struct {
	h   *Host
	key string
}

func (s sink) Log(_ *core.Scenario, msg string) {
	s.h.srv.Tell("log", toldLog{Scenario: s.key, Text: msg})
}

func (s sink) Attach(_ *core.Scenario, mediaType string, body []byte, name string) {
	s.h.srv.Tell("attach", toldAttachment{Scenario: s.key, MediaType: mediaType, Body: body, Name: name})
}

// Mirror is the host's scenario for the run's of that key.
func (h *Host) Mirror(key string) (*Mirror, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.scenarios[key]
	if m == nil {
		return nil, fmt.Errorf("the axx host has no scenario %q of this run: it ended, or never began there", key)
	}
	return m, nil
}

// In runs fn in the run's scenario: alone in it, with the scenario's
// context the call's, so that what the pack's code does for the step ends
// when the run no longer waits for it. What the packs note of the scenario
// meanwhile reaches the run before the call's answer does.
func (h *Host) In(ctx context.Context, key string, fn func(m *Mirror) (any, error)) (any, error) {
	m, err := h.Mirror(key)
	if err != nil {
		return nil, err
	}
	m.op.Lock()
	defer m.op.Unlock()
	m.SC.SetContext(ctx)
	defer m.SC.SetContext(h.ctx)
	defer h.tellNotes(m)
	return fn(m)
}

func (h *Host) tellNotes(m *Mirror) {
	notes := m.SC.Notes()
	m.mu.Lock()
	var news map[string]string
	for name, value := range notes {
		if v, ok := m.noted[name]; ok && v == value {
			continue
		}
		if news == nil {
			news = map[string]string{}
		}
		news[name], m.noted[name] = value, value
	}
	m.mu.Unlock()
	if len(news) > 0 {
		h.srv.Tell("notes", notesParams{Scenario: m.Key, Notes: news})
	}
}

func (m *Mirror) take(notes map[string]string) {
	m.mu.Lock()
	for name, value := range notes {
		m.noted[name] = value
	}
	m.mu.Unlock()
	for name, value := range notes {
		m.SC.Note(name, value)
	}
}

// scenario makes one of the run's scenarios in the host.
func (h *Host) scenario(ctx context.Context, p scenarioParams) (any, error) {
	if h.run.Suite == nil {
		return nil, errors.New("the axx host gives this pack no suite of the project's packs")
	}
	suite, err := h.run.Suite()
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	m := h.scenarios[p.Scenario]
	made := m == nil
	if made {
		if p.Dir != "" {
			if err := suite.Admits(p.Dir); err != nil {
				h.mu.Unlock()
				return nil, fmt.Errorf("the scenario's feature file: %w", err)
			}
		}
		info := p.Info
		info.Dir = p.Dir
		scCtx, cancel := context.WithCancel(h.ctx)
		m = &Mirror{Key: p.Scenario, cancel: cancel, noted: map[string]string{}}
		m.SC = core.NewScenario(scCtx, info, suite, sink{h: h, key: p.Scenario})
		// A wait for what the run's scenarios share is no part of the
		// step's own time, in the run either.
		m.SC.SetHold(func() func() {
			h.srv.Tell("hold", scenarioRef{Scenario: m.Key})
			return func() { h.srv.Tell("release", scenarioRef{Scenario: m.Key}) }
		})
		if h.side.Begin != nil {
			m.Data = h.side.Begin(m)
		}
		h.scenarios[p.Scenario] = m
	}
	h.mu.Unlock()
	return h.In(ctx, p.Scenario, func(m *Mirror) (any, error) {
		m.take(p.Notes)
		if !made {
			return nil, nil
		}
		// The packs that build on this one set up in the host's scenario
		// what they set up in the run's (core.HostScenario).
		if err := suite.BeginHostScenario(m.SC); err != nil {
			h.mu.Lock()
			delete(h.scenarios, p.Scenario)
			h.mu.Unlock()
			m.cancel()
			return nil, err
		}
		return nil, nil
	})
}

// notes takes what the run's packs noted of the scenario.
func (h *Host) notes(ctx context.Context, p notesParams) (any, error) {
	return h.In(ctx, p.Scenario, func(m *Mirror) (any, error) {
		m.take(p.Notes)
		return nil, nil
	})
}

// end ends one of the run's scenarios in the host: what the pack runs for
// it is stopped, and what the packs kept for the scenario goes.
func (h *Host) end(_ context.Context, p scenarioRef) (any, error) {
	h.mu.Lock()
	m := h.scenarios[p.Scenario]
	delete(h.scenarios, p.Scenario)
	h.mu.Unlock()
	if m == nil {
		return nil, nil
	}
	return nil, h.endMirror(h.ctx, m)
}

func (h *Host) endMirror(ctx context.Context, m *Mirror) error {
	m.op.Lock()
	defer m.op.Unlock()
	ctx = context.WithoutCancel(ctx)
	m.SC.SetContext(ctx)
	var errs []error
	if h.side.End != nil {
		if err := h.side.End(ctx, m); err != nil {
			errs = append(errs, err)
		}
	}
	if err := m.SC.Close(); err != nil {
		errs = append(errs, err)
	}
	m.cancel()
	return errors.Join(errs...)
}

// close ends every scenario the run left: its connection ended.
func (h *Host) close() {
	h.mu.Lock()
	left := h.scenarios
	h.scenarios = map[string]*Mirror{}
	h.mu.Unlock()
	for _, m := range left {
		if err := h.endMirror(h.ctx, m); err != nil && h.run.Logger != nil {
			h.run.Logger.Warn("ending a scenario the run left", "scenario", m.Key, "error", err)
		}
	}
}
