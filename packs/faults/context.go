package faults

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// The directions of a connection's data, as Toxiproxy names them.
const (
	// StreamDownstream is the data coming back to the service.
	StreamDownstream = "downstream"
	// StreamUpstream is the data going out from the service.
	StreamUpstream = "upstream"
)

// Toxic is what Toxiproxy does to a connection's data.
type Toxic struct {
	// Name is Toxiproxy's name for it; Add gives it one of its own.
	Name string `json:"name"`
	// Type is the toxic's type: latency, bandwidth, timeout...
	Type string `json:"type"`
	// Stream is the direction it applies to.
	Stream string `json:"stream"`
	// Toxicity is the share of connections it applies to, from 0 to 1.
	Toxicity float64 `json:"toxicity"`
	// Attributes are the type's own.
	Attributes map[string]any `json:"attributes"`
}

// ScenarioContext is the pack's view of a running scenario.
type ScenarioContext struct {
	servers *core.Services[*server]

	mu     sync.Mutex
	active []*active
}

// active is what a scenario did to a connection, until it is undone.
type active struct {
	conn  *Connection
	label string
	undo  func(context.Context) error

	mu    sync.Mutex
	done  bool
	err   error
	timer *time.Timer
}

var stateKey = core.NewStateKey(Name, func(sc *core.Scenario) *ScenarioContext {
	st := &ScenarioContext{
		servers: core.NewServices[*server]("Fault proxy", "No fault proxy set").RegisteredBy("the {word} fault proxy with the following properties:"),
	}
	sc.Describe(Name, st.describe)
	return st
}, func(_ *core.Scenario, st *ScenarioContext) error {
	return st.undoAll(context.Background(), nil)
})

// Context returns the pack's context for a running scenario.
func Context(sc *core.Scenario) *ScenarioContext { return stateKey.Of(sc) }

// describe says what the scenario has done to its connections, for the report.
func (st *ScenarioContext) describe() any {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string][]string{}
	for _, f := range st.active {
		if f.isDone() {
			continue
		}
		out[f.conn.name] = append(out[f.conn.name], f.label)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// undoAll ends what the scenario did, to the connection named or to all of them.
func (st *ScenarioContext) undoAll(ctx context.Context, conn *Connection) error {
	st.mu.Lock()
	var kept, undo []*active
	for _, f := range st.active {
		if conn == nil || f.conn.name == conn.name && f.conn.server == conn.server {
			undo = append(undo, f)
		} else {
			kept = append(kept, f)
		}
	}
	st.active = kept
	st.mu.Unlock()
	var errs []error
	for _, f := range undo {
		if err := f.end(ctx); err != nil {
			errs = append(errs, fmt.Errorf("could not restore the %s connection: %w", f.conn.name, err))
		}
	}
	return errors.Join(errs...)
}

// end undoes it, once. A failure of a timer's end is reported here.
func (f *active) end(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.timer != nil {
		f.timer.Stop()
	}
	if !f.done {
		f.done = true
		f.err = f.undo(ctx)
	}
	return f.err
}

func (f *active) isDone() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.done
}

func (st *ScenarioContext) track(f *active, after time.Duration) {
	st.mu.Lock()
	st.active = append(st.active, f)
	st.mu.Unlock()
	if after > 0 {
		f.mu.Lock()
		f.timer = time.AfterFunc(after, func() {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			_ = f.end(ctx)
		})
		f.mu.Unlock()
	}
}

// Connection returns the connection of that name, on whichever registered
// Toxiproxy has it.
func (st *ScenarioContext) Connection(name string) (*Connection, error) {
	all := st.servers.All()
	if len(all) == 0 {
		_, err := st.servers.Default()
		return nil, err
	}
	var there []string
	for _, s := range all {
		proxies, err := s.proxies(context.Background())
		if err != nil {
			return nil, err
		}
		if _, ok := proxies[name]; ok {
			return &Connection{st: st, server: s, name: name}, nil
		}
		there = append(there, slices.Sorted(maps.Keys(proxies))...)
	}
	return nil, fmt.Errorf("no fault proxy has a %s connection; they have: %s; a connection is a proxy of Toxiproxy's config", name, strings.Join(there, ", "))
}

// Connection is a proxy of a Toxiproxy.
type Connection struct {
	st     *ScenarioContext
	server *server
	name   string
}

// Name is the proxy's name.
func (c *Connection) Name() string { return c.name }

// Add gives the connection a toxic, until the scenario ends, Restore, or after
// when that is more than zero.
func (c *Connection) Add(sc *core.Scenario, t Toxic, after time.Duration) error {
	t.Name = "axx-" + random()
	if err := c.server.addToxic(sc.Context(), c.name, t); err != nil {
		return err
	}
	label := fmt.Sprintf("%s %s %v", t.Type, t.Stream, t.Attributes)
	c.st.track(&active{conn: c, label: label, undo: func(ctx context.Context) error {
		return c.server.removeToxic(ctx, c.name, t.Name)
	}}, after)
	sc.Log("the %s connection has the %s", c.name, label)
	return nil
}

// CutOff takes the connection away, until the scenario ends, Restore, or after
// when that is more than zero.
func (c *Connection) CutOff(sc *core.Scenario, after time.Duration) error {
	if err := c.server.setEnabled(sc.Context(), c.name, false); err != nil {
		return err
	}
	c.st.track(&active{conn: c, label: "cut off", undo: func(ctx context.Context) error {
		return c.server.setEnabled(ctx, c.name, true)
	}}, after)
	sc.Log("the %s connection is cut off", c.name)
	return nil
}

// Restore ends what the scenario did to the connection.
func (c *Connection) Restore(sc *core.Scenario) error {
	return c.st.undoAll(sc.Context(), c)
}

func random() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func register(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	s := &server{name: a.String(0)}
	for _, p := range pairs {
		v := strings.TrimSpace(secrets.Expand(sc, p.Value))
		switch p.Key {
		case "url":
			s.url = strings.TrimRight(v, "/")
		case "token":
			s.token = v
		default:
			return fmt.Errorf("unknown fault proxy property %q (supported: url, token)", p.Key)
		}
	}
	if s.url == "" {
		return errors.New(`the fault proxy property "url" is required`)
	}
	if u, err := url.Parse(s.url); err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("a fault proxy's url is http:// or https:// and a host, not %q", s.url)
	}
	secrets.Keep(sc, s.token)
	if _, err := s.proxies(sc.Context()); err != nil {
		return secrets.Hide(sc, err)
	}
	if err := Context(sc).servers.Add(s.name, s); err != nil {
		return err
	}
	sc.Log("registered the %s fault proxy: %s", s.name, s.url)
	return nil
}
