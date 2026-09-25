// Package engine assembles configuration, step packs, the match registry
// and the shared suite. Every command that works with steps (run, validate,
// steps, explain, mcp) builds on it so they always agree.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/interp"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/runner"
)

// Error codes produced while assembling packs.
const (
	CodePack     = "AXX-E0300"
	CodeResource = "AXX-E0301"
)

// NamedPack is a pack with its registration name.
type NamedPack struct {
	Name string
	Pack core.Pack
}

var (
	compiledMu sync.Mutex
	compiled   = map[string]core.Pack{}
)

// Register makes a pack compiled into this axx available under its
// axx-packs.yaml entry (rest, ./steps, a module path). axx builds itself
// with a project's packs; the builds call it before running.
func Register(key string, p core.Pack) {
	compiledMu.Lock()
	compiled[key] = p
	compiledMu.Unlock()
}

// Compiled returns the packs compiled into this axx, by entry.
func Compiled() map[string]core.Pack {
	compiledMu.Lock()
	defer compiledMu.Unlock()
	return maps.Clone(compiled)
}

// CompiledPacks returns core and every pack compiled into this axx.
func CompiledPacks() []NamedPack { return Ordered(Compiled()) }

// Ordered returns core and the packs: axx's packs in catalog order, then
// the others by entry.
func Ordered(comp map[string]core.Pack) []NamedPack {
	comp = maps.Clone(comp)
	out := []NamedPack{{Name: "core", Pack: core.ParamsPack()}}
	for _, p := range packset.Catalog {
		if pk, ok := comp[p.Name]; ok {
			out = append(out, NamedPack{Name: p.Name, Pack: pk})
			delete(comp, p.Name)
		}
	}
	keys := make([]string, 0, len(comp))
	for k := range comp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, NamedPack{Name: comp[k].Manifest().Name, Pack: comp[k]})
	}
	return out
}

// CodeUnknownPack: axx-packs.yaml names a pack that is not compiled into
// this axx.
const CodeUnknownPack = "AXX-E0302"

// selectPacks returns core plus the listed packs, in the order listed, each
// after the packs it requires.
func selectPacks(entries []packset.Entry, compiled map[string]core.Pack) ([]NamedPack, error) {
	out := []NamedPack{{Name: "core", Pack: core.ParamsPack()}}
	loaded := map[string]bool{"core": true}
	var add func(key string, np NamedPack) error
	add = func(key string, np NamedPack) error {
		if loaded[np.Name] {
			return nil
		}
		loaded[np.Name] = true
		for _, req := range np.Pack.Manifest().Requires {
			pk, ok := compiled[req]
			if !ok {
				return axxerr.New(CodeUnknownPack, exitcode.Usage, "pack %s builds on %s: add %s to %s", key, req, req, packset.FileName)
			}
			if err := add(req, NamedPack{Name: req, Pack: pk}); err != nil {
				return err
			}
		}
		out = append(out, np)
		return nil
	}
	for _, e := range entries {
		pk, ok := compiled[e.Key()]
		if !ok {
			if e.Kind == packset.Published {
				if _, known := packset.Lookup(e.Name); !known {
					return nil, axxerr.New(CodeUnknownPack, exitcode.Usage, "%s: %q is not a pack axx publishes", packset.FileName, e.Name).
						WithHint("axx's packs: %s; other packs are listed by path (./steps) or Go module path", strings.Join(packset.Names(), ", "))
				}
			}
			return nil, axxerr.New(CodeUnknownPack, exitcode.Usage, "%s: pack %s is not built into this axx", packset.FileName, e.Key()).
				WithHint("run axx from the project directory so it can build itself with the project's packs")
		}
		name := e.Key()
		if e.Kind != packset.Published {
			name = pk.Manifest().Name
		}
		if err := add(e.Key(), NamedPack{Name: name, Pack: pk}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Engine is an assembled axx environment.
type Engine struct {
	// Declared is false for a project without axx-packs.yaml: it loads no
	// packs.
	Declared bool
	Config   *config.Config
	Packs    []NamedPack
	Registry *match.Registry
	Hooks    []runner.PackHook
	Suite    *core.Suite
	Logger   *slog.Logger

	resolver *interp.Resolver
	inited   []NamedPack
}

// Options configures New.
type Options struct {
	Config *config.Config
	Logger *slog.Logger
	// Packs overrides the pack list (tests and tools); nil means the
	// project's axx-packs.yaml.
	Packs []NamedPack
	// Selection overrides the project's axx-packs.yaml (tests). When nil,
	// the file next to axx.yaml is used; without one, no pack is loaded.
	Selection []packset.Entry
	// Compiled overrides the packs compiled into this axx (tests).
	Compiled map[string]core.Pack
}

// New registers packs and builds the suite. It does not start anything.
func New(opts Options) (*Engine, error) {
	cfg := opts.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	packs := opts.Packs
	declared := packs != nil
	if packs == nil {
		sel := opts.Selection
		if sel == nil && cfg.Dir != "" {
			f, found, err := packset.Load(cfg.Dir)
			if err != nil {
				return nil, axxerr.Wrap(err, CodeUnknownPack, exitcode.Usage, "cannot read %s", packset.FileName)
			}
			if found {
				if sel, err = f.Entries(); err != nil {
					return nil, axxerr.Wrap(err, CodeUnknownPack, exitcode.Usage, "invalid %s", packset.FileName)
				}
			}
		}
		declared = sel != nil
		comp := opts.Compiled
		if comp == nil {
			comp = Compiled()
		}
		var err error
		if packs, err = selectPacks(sel, comp); err != nil {
			return nil, err
		}
	}
	e := &Engine{Config: cfg, Registry: match.NewRegistry(), Logger: logger, Declared: declared}
	e.resolver = config.NewResolver(cfg.Properties, os.LookupEnv)
	e.Packs = packs

	manifests := make([]core.Manifest, len(packs))
	for i, p := range packs {
		manifests[i] = p.Pack.Manifest()
		if err := e.Registry.AddParams(p.Name, manifests[i].Params); err != nil {
			return nil, axxerr.Wrap(err, CodePack, exitcode.Usage, "cannot load pack %s", p.Name)
		}
	}
	for i, p := range packs {
		if err := e.Registry.AddSteps(p.Name, manifests[i].Steps); err != nil {
			return nil, axxerr.Wrap(err, CodePack, exitcode.Usage, "cannot load pack %s", p.Name)
		}
		for _, h := range manifests[i].Hooks {
			e.Hooks = append(e.Hooks, runner.PackHook{Pack: p.Name, Hook: h})
		}
	}

	e.Suite = core.NewSuite(core.SuiteOptions{
		PackConfig:  packConfig(cfg),
		Interpolate: func(s string) string { return e.resolver.MustExpand(s) },
		ResolvePath: e.ResolvePath,
		Logger:      logger,
		ProjectDir:  cfg.Dir,
	})
	return e, nil
}

// Plan matches the steps of the scenarios a run will execute, for packs
// implementing core.Preparer.
func (e *Engine) Plan(pickles []*feature.Pickle) *core.Plan {
	plan := &core.Plan{Scenarios: make([]core.PlannedScenario, 0, len(pickles))}
	for _, p := range pickles {
		sc := core.PlannedScenario{ScenarioInfo: core.ScenarioInfo{
			ID: p.Id, Name: p.Name, URI: p.Uri, Line: p.Line, Tags: p.TagNames,
		}}
		for _, ps := range p.Steps {
			st := core.PlannedStep{Text: ps.Text}
			if ms := e.Registry.Match(ps.Text); len(ms) == 1 {
				st.Pack, st.Definition, st.Args = ms[0].Def().Pack, ms[0].Def().Step.ID, ms[0].Args
			}
			if ps.Argument != nil && ps.Argument.DataTable != nil {
				t := &core.Table{Rows: make([][]string, len(ps.Argument.DataTable.Rows))}
				for i, row := range ps.Argument.DataTable.Rows {
					for _, c := range row.Cells {
						t.Rows[i] = append(t.Rows[i], c.Value)
					}
				}
				st.Table = t
			}
			sc.Steps = append(sc.Steps, st)
		}
		plan.Scenarios = append(plan.Scenarios, sc)
	}
	return plan
}

// Prepare runs the packs' before-the-apps-start setup (core.Preparer) with
// the plan of the given scenarios. Hosts call it before starting the apps.
func (e *Engine) Prepare(ctx context.Context, pickles []*feature.Pickle) error {
	var plan *core.Plan
	for _, p := range e.Packs {
		pr, ok := p.Pack.(core.Preparer)
		if !ok {
			continue
		}
		if plan == nil {
			plan = e.Plan(pickles)
		}
		if err := pr.Prepare(ctx, e.Suite, plan); err != nil {
			return axxerr.Wrap(err, CodePack, exitcode.Environment, "pack %s failed to prepare the run", p.Name)
		}
	}
	return nil
}

// Init runs suite-scoped setup for packs that need it (connection pools...).
func (e *Engine) Init(ctx context.Context) error {
	for _, p := range e.Packs {
		if in, ok := p.Pack.(core.Initializer); ok {
			if err := in.Init(ctx, e.Suite); err != nil {
				return axxerr.Wrap(err, CodePack, exitcode.Environment, "pack %s failed to initialize", p.Name)
			}
		}
		e.inited = append(e.inited, p)
	}
	return nil
}

// AfterRun lets packs implementing core.Finisher check the run as a whole;
// each returned error becomes a run error.
func (e *Engine) AfterRun(ctx context.Context) []runner.RunError {
	var out []runner.RunError
	for _, p := range e.inited {
		if f, ok := p.Pack.(core.Finisher); ok {
			if err := f.Finish(ctx, e.Suite); err != nil {
				out = append(out, runner.RunError{Source: p.Name, Message: err.Error()})
			}
		}
	}
	return out
}

// Close releases suite resources and closes packs (reverse order).
func (e *Engine) Close(ctx context.Context) error {
	errs := []error{e.Suite.Close(ctx)}
	for i := len(e.inited) - 1; i >= 0; i-- {
		if c, ok := e.inited[i].Pack.(core.Closer); ok {
			errs = append(errs, c.Close(ctx))
		}
	}
	e.inited = nil
	return errors.Join(errs...)
}

// ResolvePath resolves a resource path referenced by a step (seed file,
// schema, payload): absolute paths are used as-is; relative paths are
// searched in `resources` roots and then the config directory.
func (e *Engine) ResolvePath(p string) (string, error) {
	p = strings.TrimPrefix(p, "classpath:")
	if filepath.IsAbs(p) {
		if _, err := os.Stat(p); err != nil {
			return "", axxerr.New(CodeResource, exitcode.Failed, "resource %s not found", p)
		}
		return p, nil
	}
	var tried []string
	for _, base := range e.ResourceRoots() {
		cand := filepath.Join(base, filepath.FromSlash(p))
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
		tried = append(tried, cand)
	}
	return "", axxerr.New(CodeResource, exitcode.Failed, "resource %s not found", p).
		WithHint("looked in: %s (configure `resources` in axx.yaml)", strings.Join(tried, ", "))
}

// ResourceRoots are the directories ResolvePath looks in, in order: the
// `resources` roots, then the config directory.
func (e *Engine) ResourceRoots() []string {
	var out []string
	for _, root := range append(append([]string{}, e.Config.Resources...), ".") {
		if !filepath.IsAbs(root) {
			root = filepath.Join(e.Config.Dir, root)
		}
		out = append(out, root)
	}
	return out
}

// Invoke runs a step by text inside a scenario, for steps that are built
// from other steps.
//
// A leading Gherkin keyword is ignored.
func (e *Engine) Invoke(sc *core.Scenario, text string, table *core.Table, doc *core.DocString) error {
	text = strings.TrimSpace(text)
	for _, kw := range []string{"Given ", "When ", "Then ", "And ", "But ", "* "} {
		if strings.HasPrefix(text, kw) {
			text = strings.TrimSpace(text[len(kw):])
			break
		}
	}
	ms := e.Registry.Match(text)
	switch len(ms) {
	case 0:
		return fmt.Errorf("undefined step %q", text)
	case 1:
	default:
		return fmt.Errorf("ambiguous step %q (%d definitions match)", text, len(ms))
	}
	args, err := e.Registry.Resolve(sc, ms[0], text, table, doc)
	if err != nil {
		return err
	}
	if ms[0].Def().Step.Run == nil {
		return fmt.Errorf("step %s has no implementation", ms[0].Def().Step.ID)
	}
	return ms[0].Def().Step.Run(sc, args)
}

// Interpolate expands ${env:}/${sys:} references like configuration values.
func (e *Engine) Interpolate(s string) string { return e.resolver.MustExpand(s) }

// FeaturePaths resolves the feature paths to load: explicit CLI args
// (relative to the working directory) or run.paths (relative to the config
// directory, default "features").
func (e *Engine) FeaturePaths(args []string) ([]string, map[string][]int, error) {
	if len(args) > 0 {
		wd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		abs := make([]string, len(args))
		for i, a := range args {
			if filepath.IsAbs(a) {
				abs[i] = a
			} else {
				abs[i] = filepath.Join(wd, a)
			}
		}
		paths, lines := feature.ParseLineSpecs(abs, e.Config.Dir)
		return paths, lines, nil
	}
	paths := e.Config.Run.Paths
	if len(paths) == 0 {
		paths = []string{"features"}
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		if filepath.IsAbs(p) {
			out[i] = p
		} else {
			out[i] = filepath.Join(e.Config.Dir, p)
		}
	}
	return out, nil, nil
}

// LoadFeatures parses features under paths (URIs relative to the config dir).
func (e *Engine) LoadFeatures(paths []string) (*feature.Set, error) {
	return feature.Load(paths, e.Config.Dir, messages.UUID{}.NewId)
}

// Manifests returns each pack's manifest.
func (e *Engine) Manifests() map[string]core.Manifest {
	out := make(map[string]core.Manifest, len(e.Packs))
	for _, p := range e.Packs {
		out[p.Name] = p.Pack.Manifest()
	}
	return out
}

// PackNames returns registered pack names in order.
func (e *Engine) PackNames() []string {
	out := make([]string, len(e.Packs))
	for i, p := range e.Packs {
		out[i] = p.Name
	}
	return out
}

// String identifies the engine for debugging.
func (e *Engine) String() string {
	return fmt.Sprintf("engine(%d packs, %d steps)", len(e.Packs), len(e.Registry.Defs()))
}
