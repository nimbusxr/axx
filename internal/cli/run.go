package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
  axx run features/register-parcels.feature:14
  axx run --tags "@smoke and not @wip" --format junit:build/axx/junit.xml
  axx run --attach api        # you run the api from your IDE; axx waits for it
  axx run --debug=api         # start api with its debug command
  axx run --debug-steps features/register-parcels.feature:14   # stop at breakpoints in step code`,
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
	fl.StringVar(&f.rerunFile, "rerun-file", "", "write failed scenario locations (file:line) to this file")
	return cmd
}

func (a *App) run(ctx context.Context, f *runFlags, args []string) error {
	if f.debugSteps != "" && os.Getenv(envDebuggee) == "" {
		return a.debugSteps(ctx, f)
	}
	e, err := a.loadEngine(&f.cf)
	if err != nil {
		return err
	}
	cfg := e.Config
	paths, lines, err := e.FeaturePaths(args)
	if err != nil {
		return err
	}
	set, err := e.LoadFeatures(paths)
	if err != nil {
		return err
	}
	tags := f.tags
	if tags == "" {
		tags = cfg.Run.Tags
	}
	pickles, err := set.Apply(feature.Filter{Tags: tags, Names: f.names, Lines: lines})
	if err != nil {
		return err
	}
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
	if workers == 0 {
		workers = int(cfg.Run.Workers)
	}
	order := f.order
	if order == "" {
		order = cfg.Run.Order
	}
	stepTimeout, scenarioTimeout, hookTimeout := cfg.Run.Timeouts.Step.D(), cfg.Run.Timeouts.Scenario.D(), cfg.Run.Timeouts.Hook.D()
	if os.Getenv(envDebuggee) != "" {
		// Stopped at a breakpoint, a step can take as long as it takes.
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
		names, err = lifecycle.SelectActive(cfg.Apps, cfg.Active, tags)
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
		StateFile: filepath.Join(cfg.Dir, ".axx", "run", "state.json"),
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
	p := filepath.Join(cfg.Dir, filepath.FromSlash(r.Pickle.Doc.URI))
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
