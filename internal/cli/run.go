package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	messages "github.com/cucumber/messages/go/v34"
	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lifecycle"
	"github.com/nimbusxr/axx/internal/report"
	"github.com/nimbusxr/axx/internal/runner"
	"github.com/nimbusxr/axx/internal/version"
)

type runFlags struct {
	cf       configFlags
	tags     string
	names    []string
	workers  int
	failFast bool
	dryRun   bool
	formats  []string
	noStart  bool
	attach   []string
	debug    string
	// debugSteps is the port for --debug-steps ("" when not debugging steps).
	debugSteps string
	order      string
	rerunFile  string
	// pauseAt are the steps (file:line) the run pauses before.
	pauseAt []string
	// watch shows what the scenarios do on screen, slowdown slows it down.
	watch    bool
	slowdown string
}

func newRunCmd(app *App) *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run [paths[:line]...]",
		Short: "Start the apps, run feature files, and report",
		Long: `Run Gherkin scenarios against your services.

axx starts the applications declared in axx.yaml (only those needed by the
selected scenarios when active.enabled is set), waits until they are ready,
runs scenarios in parallel, stops the apps and runs their cleanups.

Select scenarios with paths (features/x.feature:14 selects the scenario on
line 14, or the scenario containing that step line), --tags and --name.

Exit codes: 0 passed, 1 failures, 2 usage/config, 3 undefined/ambiguous steps,
4 an app failed to start, 130 interrupted.`,
		Example: `  axx run
  axx run features/register-parcels.feature:17
  axx run --tags "@smoke and not @wip" --format junit:build/axx/junit.xml
  axx run --attach api        # you run the api from your IDE; axx waits for it
  axx run --debug=api         # start api with its debug command
  axx run --debug-steps features/register-parcels.feature:17   # stop at breakpoints in step code
  axx run features/shop-portal.feature --pause-at features/shop-portal.feature:24   # pause before that step, where packs can show it`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.run(cmd.Context(), &f, args)
		},
	}
	f.cf.register(cmd)
	fl := cmd.Flags()
	fl.StringVarP(&f.tags, "tags", "t", "", `tag expression, e.g. "@smoke and not @wip"`)
	fl.StringArrayVarP(&f.names, "name", "n", nil, "only scenarios whose name matches this regular expression (repeatable)")
	fl.IntVarP(&f.workers, "workers", "w", 0, "parallel scenarios (default: run.workers, or number of CPUs)")
	fl.BoolVar(&f.failFast, "fail-fast", false, "stop scheduling scenarios after the first failure")
	fl.BoolVar(&f.dryRun, "dry-run", false, "match steps without executing them or starting apps")
	fl.StringArrayVarP(&f.formats, "format", "f", nil, "reporter NAME or NAME:FILE (repeatable): "+strings.Join(report.Names(), ", "))
	fl.BoolVar(&f.noStart, "no-start", false, "do not start, stop or clean up any app")
	fl.StringSliceVar(&f.attach, "attach", nil, "apps you run yourself; axx waits for their readiness instead of starting them")
	fl.StringVar(&f.debug, "debug", "", "start apps with their debug command (all, or a comma-separated list)")
	fl.Lookup("debug").NoOptDefVal = "*"
	fl.StringVar(&f.debugSteps, "debug-steps", "", "run under Delve so a Go debugger can stop at breakpoints in step code (port, default "+defaultStepsPort+")")
	fl.Lookup("debug-steps").NoOptDefVal = defaultStepsPort
	fl.StringVar(&f.order, "order", "", `scenario order: "defined" or "random[:seed]"`)
	fl.BoolVar(&f.watch, "watch", false, "show what the scenarios do as they do it: browsers and devices in their windows, one scenario at a time unless --workers says otherwise (run.watch)")
	fl.StringVar(&f.slowdown, "slowdown", "", `pause after each action of a watched run, e.g. "500ms" (run.slowdown)`)
	fl.StringVar(&f.rerunFile, "rerun-file", "", "write failed scenario locations (file:line) to this file")
	fl.StringArrayVar(&f.pauseAt, "pause-at", nil, "pause before the step at this file:line, with no timeouts, for the packs that can show a person what the scenario does (repeatable)")
	return cmd
}

func (a *App) run(ctx context.Context, f *runFlags, args []string) error {
	if f.debugSteps != "" && os.Getenv(envDebuggee) == "" {
		return a.debugSteps(ctx, f)
	}
	if f.watch {
		f.cf.settings = append(f.cf.settings, "run.watch=true")
	}
	if f.slowdown != "" {
		f.cf.settings = append(f.cf.settings, "run.slowdown="+f.slowdown)
	}
	e, err := a.loadEngine(&f.cf)
	if err != nil {
		return err
	}
	cfg := e.Config
	pauses, err := a.pauseAt(e, f.pauseAt)
	if err != nil {
		return err
	}
	paths, lines, err := e.FeaturePaths(args)
	if err != nil {
		return err
	}
	set, err := e.LoadFeatures(paths)
	if err != nil {
		return err
	}
	filter := feature.Filter{Tags: f.tags, DefaultTags: cfg.Run.Tags, Names: f.names, Lines: lines}
	if len(args) > 0 {
		// A feature file named on the command line (or clicked in an editor) runs
		// whatever run.tags leaves out of a whole run.
		filter.Named = feature.NamedFiles(paths, cfg.Dir)
	}
	pickles, err := set.Apply(filter)
	if err != nil {
		return err
	}
	if len(cfg.Run.Uses) > 0 {
		// The scenarios that use the packs run.uses lists: a profile per
		// platform, with no tags.
		if err := knownPacks(e, "run.uses", cfg.Run.Uses); err != nil {
			return err
		}
		packs := packsOf(e, pickles)
		pickles = feature.ByUses(pickles, cfg.Run.Uses, filter.Named, func(p *feature.Pickle) []string { return packs[p] })
	}
	e.Suite.PauseAt(a.pausesOnSteps(pauses, pickles))
	if len(pickles) == 0 {
		fmt.Fprintln(a.Stderr, "axx: no scenarios matched the given paths and filters")
		return a.EmitResult(map[string]any{"scenarios": 0}, true, nil)
	}

	reporters, agentBuf, needsMsgs, closeReporters, err := a.reporters(cfg, f)
	if err != nil {
		return err
	}
	defer closeReporters()

	// Packs set up what the apps need before they start (core.Preparer);
	// what they open is released when the engine closes, after the apps stop.
	defer func() { _ = e.Close(context.WithoutCancel(ctx)) }()
	if !f.dryRun {
		if err := e.Prepare(ctx, pickles); err != nil {
			return err
		}
	}

	// Applications.
	var mgr *lifecycle.Manager
	if !f.dryRun && len(cfg.Apps) > 0 {
		mgr, err = a.startApps(ctx, e, f, pickles)
		if mgr != nil {
			defer func() {
				stopCtx := context.WithoutCancel(ctx)
				if serr := mgr.Stop(stopCtx); serr != nil {
					fmt.Fprintln(a.Stderr, "axx: while stopping apps:", serr)
				}
			}()
		}
		if err != nil {
			return err
		}
	}

	if !f.dryRun {
		if err := e.Init(ctx); err != nil {
			return err
		}
	}

	workers := f.workers
	if workers == 0 && cfg.Run.Watch && cfg.Run.Workers == 0 {
		workers = 1 // a person follows one scenario at a time
	}
	if workers == 0 {
		workers = int(cfg.Run.Workers)
	}
	order := f.order
	if order == "" {
		order = cfg.Run.Order
	}
	stepTimeout, scenarioTimeout, hookTimeout := cfg.Run.Timeouts.Step.D(), cfg.Run.Timeouts.Scenario.D(), cfg.Run.Timeouts.Hook.D()
	if os.Getenv(envDebuggee) != "" || e.Suite.Pausing() {
		// Stopped at a breakpoint, or paused, a step can take as long as it takes.
		stepTimeout, scenarioTimeout, hookTimeout = noTimeout, 0, noTimeout
	}
	r, err := runner.New(runner.Options{
		Registry:        e.Registry,
		Hooks:           e.Hooks,
		Suite:           e.Suite,
		Workers:         workers,
		Exclusive:       cfg.Run.Exclusive,
		FailFast:        f.failFast,
		DryRun:          f.dryRun,
		Order:           order,
		StepTimeout:     stepTimeout,
		ScenarioTimeout: scenarioTimeout,
		HookTimeout:     hookTimeout,
		AfterRun:        e.AfterRun,
		Reporters:       reporters,
		Messages:        needsMsgs,
		Meta:            &messages.Meta{Implementation: &messages.Product{Name: "axx", Version: version.Get().Version}},
		Docs:            set.Docs,
	})
	if err != nil {
		return axxerr.Wrap(err, "AXX-E0004", exitcode.Usage, "cannot start the run")
	}
	res := r.Run(ctx, pickles)

	if f.rerunFile != "" {
		if err := writeRerun(f.rerunFile, res); err != nil {
			fmt.Fprintln(a.Stderr, "axx: cannot write rerun file:", err)
		}
	}
	code := runExitCode(res)
	if code == exitcode.Undefined {
		a.hintNoPacks(e)
	}
	if a.JSON && agentBuf != nil {
		closeReporters()
		if err := a.EmitResult(json.RawMessage(bytes.TrimSpace(agentBuf.Bytes())), code == exitcode.OK, nil); err != nil {
			return err
		}
	}
	if code != exitcode.OK {
		return silentExit{code: code}
	}
	return nil
}

func runExitCode(res *runner.RunResult) exitcode.Code {
	if res.Interrupted {
		return exitcode.Interrupted
	}
	switch res.Worst() {
	case runner.Failed, runner.Pending:
		return exitcode.Failed
	case runner.Undefined, runner.Ambiguous:
		return exitcode.Undefined
	}
	return exitcode.OK
}

func (a *App) startApps(ctx context.Context, e *engine.Engine, f *runFlags, pickles []*feature.Pickle) (*lifecycle.Manager, error) {
	cfg := e.Config
	var names []string
	if !f.noStart {
		tags := make([][]string, len(pickles))
		for i, p := range pickles {
			tags[i] = p.TagNames
		}
		var err error
		var used []string
		for _, packs := range packsOf(e, pickles) {
			for _, p := range packs {
				if !slices.Contains(used, p) {
					used = append(used, p)
				}
			}
		}
		names, err = lifecycle.SelectActive(cfg.Apps, cfg.Active, tags, used)
		if err != nil {
			return nil, err
		}
	}
	logDir := filepath.Join(cfg.Dir, ".axx", "logs")
	_ = os.MkdirAll(logDir, 0o755)
	logFile, err := os.Create(filepath.Join(logDir, "apps.log"))
	if err != nil {
		return nil, err
	}
	var out io.Writer = logFile
	if a.Verbose > 0 || f.debug != "" {
		out = io.MultiWriter(logFile, a.Stderr)
	}
	attach := set(f.attach)
	if st, ok := liveUp(cfg); ok {
		// Apps kept running by `axx up` are reused: axx only waits for them.
		for _, n := range st.Apps {
			attach[n] = true
		}
		fmt.Fprintf(a.Stderr, "axx: reusing apps started by `axx up`: %s\n", strings.Join(st.Apps, ", "))
	}
	opts := lifecycle.Options{
		ConfigDir: cfg.Dir,
		Stdout:    out,
		Stderr:    out,
		Logger:    a.logger(),
		NoStart:   f.noStart,
		Attach:    attach,
		StateFile: lifecycle.StateFile(cfg.Dir),
	}
	switch f.debug {
	case "":
	case "*":
		opts.DebugAll = true
	default:
		opts.Debug = set(strings.Split(f.debug, ","))
	}
	mgr, err := lifecycle.New(cfg.Apps, opts)
	if err != nil {
		return nil, err
	}
	if len(names) > len(attach) {
		fmt.Fprintf(a.Stderr, "axx: starting %s (logs: %s)\n", strings.Join(names, ", "), relPath(logFile.Name()))
	}
	if err := mgr.Start(ctx, names); err != nil {
		return mgr, err
	}
	return mgr, nil
}

func (a *App) reporters(cfg *config.Config, f *runFlags) ([]runner.Reporter, *bytes.Buffer, bool, func(), error) {
	specs := cfg.Run.Reporters
	if len(f.formats) > 0 {
		specs = nil
		for _, s := range f.formats {
			name, path, _ := strings.Cut(s, ":")
			specs = append(specs, config.Reporter{Name: name, Path: path})
		}
	}
	hasStdout := false
	for _, s := range specs {
		if s.Path == "" {
			hasStdout = true
		}
	}
	if !hasStdout && !a.JSON {
		name := "pretty"
		if a.Compact {
			name = "compact"
		}
		specs = append([]config.Reporter{{Name: name}}, specs...)
	}
	opts := report.Options{
		Color:        a.color(),
		Version:      version.Get().Version,
		BaseDir:      cfg.Dir,
		RerunCommand: func(r *runner.ScenarioResult) string { return "axx run " + scenarioLocation(cfg, r) },
	}
	var reps []runner.Reporter
	var files []*os.File
	closeAll := func() {
		for _, f := range files {
			_ = f.Close()
		}
		files = nil
	}
	var agentBuf *bytes.Buffer
	needsMsgs := false
	for _, s := range specs {
		var w io.Writer
		switch {
		case s.Path != "":
			p := s.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(cfg.Dir, p)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				closeAll()
				return nil, nil, false, nil, err
			}
			file, err := os.Create(p)
			if err != nil {
				closeAll()
				return nil, nil, false, nil, err
			}
			files = append(files, file)
			w = file
		case a.JSON:
			continue // stdout carries the JSON envelope
		default:
			w = a.Stdout
		}
		rep, err := report.New(s.Name, w, opts)
		if err != nil {
			closeAll()
			return nil, nil, false, nil, err
		}
		needsMsgs = needsMsgs || report.NeedsMessages(s.Name)
		reps = append(reps, rep)
	}
	if a.JSON {
		agentBuf = &bytes.Buffer{}
		rep, err := report.New("agent", agentBuf, opts)
		if err != nil {
			closeAll()
			return nil, nil, false, nil, err
		}
		reps = append(reps, rep)
	}
	return reps, agentBuf, needsMsgs, closeAll, nil
}

// scenarioLocation renders "path:line" relative to the working directory.
func scenarioLocation(cfg *config.Config, r *runner.ScenarioResult) string {
	// A file outside the configuration's directory has an absolute URI.
	p := filepath.FromSlash(r.Pickle.Doc.URI)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cfg.Dir, p)
	}
	return fmt.Sprintf("%s:%d", relPath(p), r.Pickle.Line)
}

func writeRerun(path string, res *runner.RunResult) error {
	var b strings.Builder
	for _, s := range res.Scenarios {
		if s.Status > runner.Skipped {
			fmt.Fprintf(&b, "%s:%d\n", s.Pickle.Doc.URI, s.Pickle.Line)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// pauseAt reads where the run is to pause, from --pause-at file:line
// values: the lines of each file, by URI.
func (a *App) pauseAt(e *engine.Engine, specs []string) (map[string][]int, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	for _, spec := range specs {
		_, line, ok := strings.Cut(spec[max(0, strings.LastIndexByte(spec, ':')):], ":")
		if n, err := strconv.Atoi(line); !ok || err != nil || n < 1 {
			return nil, axxerr.New("AXX-E0001", exitcode.Usage, "invalid usage: --pause-at takes the file:line of a step, got %q", spec).
				WithHint("use --pause-at features/<file>.feature:<line>, the line of the step to pause before")
		}
	}
	_, lines, err := e.FeaturePaths(specs)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

// pausesOnSteps are the pauses on a step of the run, the ones the run
// pauses at. It tells of the others: a line of a scenario that is not a
// step, or a file the run leaves out.
func (a *App) pausesOnSteps(pauses map[string][]int, pickles []*feature.Pickle) map[string][]int {
	if len(pauses) == 0 {
		return nil
	}
	steps := map[string]map[int]bool{}
	for _, p := range pickles {
		if steps[p.Doc.URI] == nil {
			steps[p.Doc.URI] = map[int]bool{}
		}
		for _, ps := range p.Steps {
			steps[p.Doc.URI][p.StepSource(ps).Line] = true
		}
	}
	out := map[string][]int{}
	for _, uri := range slices.Sorted(maps.Keys(pauses)) {
		for _, line := range slices.Compact(slices.Sorted(slices.Values(pauses[uri]))) {
			if !steps[uri][line] {
				fmt.Fprintf(a.Stderr, "axx: not pausing at %s:%d: no step of this run is on that line\n", uri, line)
				continue
			}
			out[uri] = append(out[uri], line)
		}
	}
	return out
}

// packsOf is the packs whose steps each scenario uses, Backgrounds included.
func packsOf(e *engine.Engine, pickles []*feature.Pickle) map[*feature.Pickle][]string {
	out := make(map[*feature.Pickle][]string, len(pickles))
	for i, sc := range e.Plan(pickles).Scenarios {
		for _, st := range sc.Steps {
			if st.Pack != "" && !slices.Contains(out[pickles[i]], st.Pack) {
				out[pickles[i]] = append(out[pickles[i]], st.Pack)
			}
		}
	}
	return out
}

// knownPacks reports a pack the project does not load in a list of packs.
func knownPacks(e *engine.Engine, key string, packs []string) error {
	for _, want := range packs {
		if !slices.ContainsFunc(e.Packs, func(p engine.NamedPack) bool { return p.Name == want }) {
			var names []string
			for _, p := range e.Packs {
				names = append(names, p.Name)
			}
			return axxerr.New(feature.CodeUses, exitcode.Usage, "%s lists %s, which is not one of this project's packs", key, want).
				WithHint("list packs of axx-packs.yaml (%s), or add it with `axx pack add %s`", strings.Join(names, ", "), want)
		}
	}
	return nil
}
