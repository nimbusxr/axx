// Package cloudtest runs the steps of packs in tests, as a run does: the
// cloud service packs against local cloud emulators, the web pack against
// a site the test serves.
package cloudtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/interp"
	"github.com/nimbusxr/axx/internal/match"
)

// Harness runs step text against a set of packs, one scenario at a time.
type Harness struct {
	t     *testing.T
	reg   *match.Registry
	packs []core.Pack
	Suite *core.Suite
	// Dir is the project directory: relative files resolve against it.
	Dir string
	SC  *core.Scenario
	// Sink records what the scenarios log and attach.
	Sink *Sink
	// hooks are the packs' step hooks, which run around each step.
	hooks []core.Hook
	// line is the line of the scenario's last step in its feature file.
	line int
}

// Sink records scenario logs and attachments, and what packs announce to
// an IDE.
type Sink struct {
	mu          sync.Mutex
	Logs        []string
	Attachments []Attachment
	Announced   []string
}

// Attachment is something a step attached.
type Attachment struct {
	MediaType, Name string
	Body            []byte
}

func (s *Sink) Log(_ *core.Scenario, msg string) {
	s.mu.Lock()
	s.Logs = append(s.Logs, msg)
	s.mu.Unlock()
}

func (s *Sink) Attach(_ *core.Scenario, mediaType string, body []byte, name string) {
	s.mu.Lock()
	s.Attachments = append(s.Attachments, Attachment{MediaType: mediaType, Name: name, Body: body})
	s.mu.Unlock()
}

// Lines are the lines announced to an IDE so far.
func (s *Sink) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.Announced)
}

// Reset forgets what was recorded.
func (s *Sink) Reset() {
	s.mu.Lock()
	s.Logs, s.Attachments, s.Announced = nil, nil, nil
	s.mu.Unlock()
}

// New registers the core pack and packs, in order.
func New(t *testing.T, packs ...core.Pack) *Harness {
	t.Helper()
	return NewWith(t, nil, packs...)
}

// NewWith is New with the packs' sections of axx.yaml, by pack name.
func NewWith(t *testing.T, config map[string]any, packs ...core.Pack) *Harness {
	t.Helper()
	packConfig := map[string]json.RawMessage{}
	for name, c := range config {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		packConfig[name] = b
	}
	reg := match.NewRegistry()
	all := append([]core.Pack{core.ParamsPack()}, packs...)
	var hooks []core.Hook
	for _, p := range all {
		m := p.Manifest()
		if err := reg.AddPack(m.Name, m); err != nil {
			t.Fatal(err)
		}
		hooks = append(hooks, m.Hooks...)
	}
	dir := t.TempDir()
	sink := &Sink{}
	var h *Harness
	suite := core.NewSuite(core.SuiteOptions{
		// Steps run by text (a paused scenario's) run as the engine runs
		// them: in the scenario's current step.
		Invoke: func(sc *core.Scenario, text string, table *core.Table, doc *core.DocString) error {
			ms := h.reg.Match(text)
			if len(ms) != 1 {
				return fmt.Errorf("%q matches %d step definitions", text, len(ms))
			}
			args, err := h.reg.Resolve(sc, ms[0], text, table, doc)
			if err != nil {
				return err
			}
			return ms[0].Def().Step.Run(sc, args)
		},
		ProjectDir: dir,
		PackConfig: packConfig,
		Announce: func(line string) {
			sink.mu.Lock()
			sink.Announced = append(sink.Announced, line)
			sink.mu.Unlock()
		},
		// ${env:..} references expand as in a run; ${sys:..} ones stay as they are.
		Interpolate: (&interp.Resolver{Lookups: map[string]interp.Lookup{"env": os.LookupEnv}}).MustExpand,
		ResolvePath: func(p string) (string, error) {
			if !filepath.IsAbs(p) {
				p = filepath.Join(dir, filepath.FromSlash(p))
			}
			if _, err := os.Stat(p); err != nil {
				return "", fmt.Errorf("resource %s not found", p)
			}
			return p, nil
		},
	})
	h = &Harness{t: t, reg: reg, packs: packs, Suite: suite, Dir: dir, Sink: sink, hooks: hooks}
	t.Cleanup(func() {
		for i := len(packs) - 1; i >= 0; i-- {
			if c, ok := packs[i].(core.Closer); ok {
				_ = c.Close(context.Background())
			}
		}
		_ = suite.Close(context.Background())
	})
	h.NewScenario()
	return h
}

// scenarioTimeout bounds a scenario, as a run's step timeouts do: a service that stops
// answering fails the test instead of hanging it.
const scenarioTimeout = 5 * time.Minute

// NewScenario starts the next scenario, at line 1 of features/test.feature;
// its steps are on the lines after it.
func (h *Harness) NewScenario() *core.Scenario {
	ctx, cancel := context.WithTimeout(context.Background(), scenarioTimeout)
	h.t.Cleanup(cancel)
	h.SC = core.NewScenario(ctx, core.ScenarioInfo{ID: fmt.Sprint(time.Now().UnixNano()), Name: "test", URI: "features/test.feature", Line: 1}, h.Suite, h.Sink)
	h.line = 1
	return h.SC
}

// In returns a harness that runs steps in another scenario of the same run,
// for scenarios running side by side. Use Step (not OK or Fails) from other
// goroutines. It takes only what does not change as h runs steps, so it can
// be called while h runs one.
func (h *Harness) In(sc *core.Scenario) *Harness {
	return &Harness{t: h.t, reg: h.reg, packs: h.packs, Suite: h.Suite, Dir: h.Dir, SC: sc, Sink: h.Sink, hooks: h.hooks, line: 1}
}

// End ends the scenario as the runner does: it records the outcome
// ("passed", "failed"...) and runs the packs' cleanup.
func (h *Harness) End(status string) error {
	h.SC.SetStatus(status)
	return h.SC.Close()
}

// Step runs one step. A table is given as rows; a doc string as a string.
func (h *Harness) Step(text string, arg ...any) error {
	h.t.Helper()
	ms := h.reg.Match(text)
	if len(ms) != 1 {
		h.t.Fatalf("%q matched %d step definitions", text, len(ms))
	}
	var tbl *core.Table
	var doc *core.DocString
	for _, a := range arg {
		switch x := a.(type) {
		case [][]string:
			tbl = &core.Table{Rows: x}
		case string:
			doc = &core.DocString{Content: x}
		}
	}
	// As in a run: the scenario knows its step, which runs between the
	// packs' step hooks.
	h.line++
	h.SC.SetStep(&core.StepInfo{Keyword: "*", Text: text, Line: h.line})
	defer h.SC.SetStep(nil)
	args, err := h.reg.Resolve(h.SC, ms[0], text, tbl, doc)
	if err != nil {
		return err
	}
	for _, hk := range h.hooks {
		if hk.Phase == core.BeforeStep {
			if err := hk.Run(h.SC); err != nil {
				return err
			}
		}
	}
	err = ms[0].Def().Step.Run(h.SC, args)
	for _, hk := range h.hooks {
		if hk.Phase == core.AfterStep {
			if herr := hk.Run(h.SC); herr != nil && err == nil {
				err = herr
			}
		}
	}
	return err
}

// OK runs a step that must pass.
func (h *Harness) OK(text string, arg ...any) {
	h.t.Helper()
	if err := h.Step(text, arg...); err != nil {
		h.t.Fatalf("%s: %v", text, err)
	}
}

// Fails runs a step that must fail with an error containing want.
func (h *Harness) Fails(text, want string, arg ...any) error {
	h.t.Helper()
	err := h.Step(text, arg...)
	if err == nil || !strings.Contains(err.Error(), want) {
		h.t.Fatalf("%s: want an error containing %q, got %v", text, want, err)
	}
	return err
}

// File writes a file into the project directory.
func (h *Harness) File(name, content string) {
	h.t.Helper()
	p := filepath.Join(h.Dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// Plan is a planned scenario of step texts with their tables, matched the
// way the engine plans a run.
func (h *Harness) Plan(steps ...PlannedStep) *core.Plan {
	sc := core.PlannedScenario{ScenarioInfo: core.ScenarioInfo{ID: h.SC.ID}}
	for _, s := range steps {
		ps := core.PlannedStep{Text: s.Text}
		if ms := h.reg.Match(s.Text); len(ms) == 1 {
			ps.Pack, ps.Definition, ps.Args = ms[0].Def().Pack, ms[0].Def().Step.ID, ms[0].Args
		}
		if s.Table != nil {
			ps.Table = &core.Table{Rows: s.Table}
		}
		sc.Steps = append(sc.Steps, ps)
	}
	return &core.Plan{Scenarios: []core.PlannedScenario{sc}}
}

// PlannedStep is a step of a planned scenario.
type PlannedStep struct {
	Text  string
	Table [][]string
}

// Start prepares and initializes the packs for a planned run, as the engine
// does before the scenarios start.
func (h *Harness) Start(plan *core.Plan) {
	h.t.Helper()
	ctx := context.Background()
	for _, p := range h.packs {
		if pr, ok := p.(core.Preparer); ok {
			if err := pr.Prepare(ctx, h.Suite, plan); err != nil {
				h.t.Fatal(err)
			}
		}
	}
	for _, p := range h.packs {
		if in, ok := p.(core.Initializer); ok {
			if err := in.Init(ctx, h.Suite); err != nil {
				h.t.Fatal(err)
			}
		}
	}
}

// Emulator starts a container and returns its host address for port
// ("localhost:49153"). It skips the test when Docker is unavailable.
func Emulator(t *testing.T, image, port, healthPath string, env map[string]string) string {
	return emulator(t, image, port, healthPath, env, false)
}

// EmulatorWithDocker is Emulator for emulators that run parts of a service
// in containers of their own (a message broker, say): the Docker socket is
// mounted.
func EmulatorWithDocker(t *testing.T, image, port, healthPath string, env map[string]string) string {
	return emulator(t, image, port, healthPath, env, true)
}

// Server starts a server's container, with the command it runs when cmd
// is not empty, and returns its host address for port once the port
// listens. It skips the test when Docker is unavailable.
func Server(t *testing.T, image, port string, cmd []string, env map[string]string) string {
	return start(t, image, port, "", cmd, env, false)
}

func emulator(t *testing.T, image, port, healthPath string, env map[string]string, docker bool) string {
	return start(t, image, port, healthPath, nil, env, docker)
}

func start(t *testing.T, image, port, healthPath string, cmd []string, env map[string]string, docker bool) string {
	t.Helper()
	ctx := context.Background()
	var strategy wait.Strategy = wait.ForListeningPort(port + "/tcp").WithStartupTimeout(2 * time.Minute)
	if healthPath != "" {
		strategy = wait.ForHTTP(healthPath).WithPort(port + "/tcp").WithStartupTimeout(2 * time.Minute)
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: image, ExposedPorts: []string{port + "/tcp"}, Cmd: cmd, Env: env, WaitingFor: strategy,
			HostConfigModifier: func(hc *container.HostConfig) {
				if docker {
					hc.Binds = append(hc.Binds, "/var/run/docker.sock:/var/run/docker.sock")
				}
			},
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("cannot start %s: %v", image, err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := c.MappedPort(ctx, port+"/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host + ":" + mapped.Port()
}
