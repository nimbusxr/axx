package runner

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	messages "github.com/cucumber/messages/go/v34"
	tagexpr "github.com/cucumber/tag-expressions/go/v11"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// Reporter receives run events. Methods are called from one goroutine at a
// time (the runner serializes them).
type Reporter interface {
	// Envelope receives Cucumber Messages as they are produced.
	Envelope(e *messages.Envelope)
	// ScenarioFinished receives each scenario in completion order.
	ScenarioFinished(r *ScenarioResult)
	// Finish receives the whole run.
	Finish(r *RunResult) error
}

// StepReporter is implemented by reporters that want each step's full
// result the moment it finishes (the TestStepFinished envelope, which
// follows, carries only its message). It is called with the ids of that
// envelope.
type StepReporter interface {
	StepFinished(testCaseStartedID, testStepID string, sr *StepResult)
}

// Options configures a run.
type Options struct {
	Registry *match.Registry
	// Hooks are scenario/step hooks contributed by packs.
	Hooks []PackHook
	Suite *core.Suite
	// Workers is the parallelism; <= 0 means runtime.NumCPU().
	Workers int
	// Exclusive tags run alone after the parallel phase.
	Exclusive []string
	FailFast  bool
	DryRun    bool
	// Order is "defined" (default) or "random" / "random:<seed>".
	Order           string
	StepTimeout     time.Duration
	ScenarioTimeout time.Duration
	HookTimeout     time.Duration
	// TimeoutGrace is how long a step may ignore cancellation before it is
	// abandoned. Default 5s.
	TimeoutGrace time.Duration
	// AfterRun checks the run as a whole once every scenario has finished
	// (packs implementing core.Finisher). Not called for dry or interrupted
	// runs.
	AfterRun  func(ctx context.Context) []RunError
	Reporters []Reporter
	// Messages controls Cucumber Messages emission (needed by messages/html
	// reporters). When false, Envelope is never called.
	Messages bool
	// Meta describes the implementation for the meta message.
	Meta *messages.Meta
	// Docs are the loaded documents (for source/gherkinDocument messages).
	Docs []*feature.Document
	// NewID generates message IDs; defaults to a thread-safe counter.
	NewID func() string
	// Now is the clock; defaults to time.Now.
	Now func() time.Time
}

// PackHook is a hook with its owning pack.
type PackHook struct {
	Pack string
	Hook core.Hook
	tags tagexpr.Evaluatable
}

// Runner executes pickles.
type Runner struct {
	opts   Options
	hooks  map[core.Phase][]*PackHook
	repMu  sync.Mutex
	msg    *emitter
	idMu   sync.Mutex
	nextID int
}

// New validates options and builds a runner.
func New(opts Options) (*Runner, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("runner: registry is required")
	}
	if opts.Suite == nil {
		opts.Suite = core.NewSuite(core.SuiteOptions{})
	}
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	if opts.StepTimeout <= 0 {
		opts.StepTimeout = 10 * time.Minute
	}
	if opts.HookTimeout <= 0 {
		opts.HookTimeout = 2 * time.Minute
	}
	if opts.TimeoutGrace <= 0 {
		opts.TimeoutGrace = 5 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	r := &Runner{opts: opts, hooks: map[core.Phase][]*PackHook{}}
	if r.opts.NewID == nil {
		r.opts.NewID = r.counterID
	}
	for i := range opts.Hooks {
		h := opts.Hooks[i]
		if strings.TrimSpace(h.Hook.Tags) != "" {
			ev, err := tagexpr.Parse(h.Hook.Tags)
			if err != nil {
				return nil, fmt.Errorf("hook %s: invalid tag expression %q: %w", h.Hook.ID, h.Hook.Tags, err)
			}
			h.tags = ev
		}
		r.hooks[h.Hook.Phase] = append(r.hooks[h.Hook.Phase], &h)
	}
	for phase, hs := range r.hooks {
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].Hook.Order < hs[j].Hook.Order })
		if phase == core.AfterScenario || phase == core.AfterStep {
			for i, j := 0, len(hs)-1; i < j; i, j = i+1, j-1 {
				hs[i], hs[j] = hs[j], hs[i]
			}
		}
	}
	if opts.Messages {
		r.msg = newEmitter(r)
	}
	return r, nil
}

func (r *Runner) counterID() string {
	r.idMu.Lock()
	defer r.idMu.Unlock()
	id := strconv.Itoa(r.nextID)
	r.nextID++
	return id
}

// Run executes pickles and reports results. It returns when every scenario
// has finished (or was skipped by fail-fast/interruption).
func (r *Runner) Run(ctx context.Context, pickles []*feature.Pickle) *RunResult {
	res := &RunResult{Started: r.opts.Now(), DryRun: r.opts.DryRun}
	pickles = r.order(pickles)
	r.msg.runStarted(pickles)

	parallel, serial := r.partition(pickles)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	record := func(sr *ScenarioResult) {
		mu.Lock()
		res.Scenarios = append(res.Scenarios, sr)
		mu.Unlock()
		r.repMu.Lock()
		for _, rep := range r.opts.Reporters {
			rep.ScenarioFinished(sr)
		}
		r.repMu.Unlock()
		if r.opts.FailFast && sr.Status >= Undefined {
			cancel()
		}
	}

	notRun := 0
	// Parallel phase.
	jobs := make(chan *feature.Pickle)
	var wg sync.WaitGroup
	for w := 1; w <= min(r.opts.Workers, max(1, len(parallel))); w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for p := range jobs {
				record(r.runScenario(runCtx, p, worker))
			}
		}(w)
	}
	dispatched := 0
	for _, p := range parallel {
		if runCtx.Err() != nil {
			break
		}
		select {
		case jobs <- p:
			dispatched++
		case <-runCtx.Done():
		}
	}
	notRun += len(parallel) - dispatched
	close(jobs)
	wg.Wait()

	// Serial phase for exclusive scenarios.
	for i, p := range serial {
		if runCtx.Err() != nil {
			notRun += len(serial) - i
			break
		}
		record(r.runScenario(runCtx, p, 0))
	}

	// Report in a stable order: feature file order, then line.
	index := map[*feature.Pickle]int{}
	for i, p := range pickles {
		index[p] = i
	}
	sort.SliceStable(res.Scenarios, func(i, j int) bool {
		return index[res.Scenarios[i].Pickle] < index[res.Scenarios[j].Pickle]
	})
	res.NotRun = notRun
	res.Interrupted = ctx.Err() != nil
	if r.opts.AfterRun != nil && !r.opts.DryRun && !res.Interrupted {
		res.RunErrors = r.opts.AfterRun(ctx)
	}
	res.Duration = r.opts.Now().Sub(res.Started)
	r.msg.runFinished(res)
	r.repMu.Lock()
	defer r.repMu.Unlock()
	for _, rep := range r.opts.Reporters {
		if err := rep.Finish(res); err != nil {
			r.opts.Suite.Logger().Error("reporter failed", "error", err)
		}
	}
	return res
}

func (r *Runner) order(pickles []*feature.Pickle) []*feature.Pickle {
	out := append([]*feature.Pickle(nil), pickles...)
	mode, seedStr, _ := strings.Cut(r.opts.Order, ":")
	if mode != "random" {
		return out
	}
	seed := uint64(time.Now().UnixNano())
	if n, err := strconv.ParseUint(seedStr, 10, 64); err == nil {
		seed = n
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func (r *Runner) partition(pickles []*feature.Pickle) (parallel, serial []*feature.Pickle) {
	if len(r.opts.Exclusive) == 0 {
		return pickles, nil
	}
	ex := map[string]bool{}
	for _, t := range r.opts.Exclusive {
		if !strings.HasPrefix(t, "@") {
			t = "@" + t
		}
		ex[t] = true
	}
	for _, p := range pickles {
		isEx := false
		for _, t := range p.TagNames {
			if ex[t] {
				isEx = true
				break
			}
		}
		if isEx {
			serial = append(serial, p)
		} else {
			parallel = append(parallel, p)
		}
	}
	return parallel, serial
}

func (r *Runner) hooksFor(phase core.Phase, tags []string) []*PackHook {
	var out []*PackHook
	for _, h := range r.hooks[phase] {
		if h.tags == nil || h.tags.Evaluate(tags) {
			out = append(out, h)
		}
	}
	return out
}

func (r *Runner) report(fn func(Reporter)) {
	r.repMu.Lock()
	defer r.repMu.Unlock()
	for _, rep := range r.opts.Reporters {
		fn(rep)
	}
}
