package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// upState is written by the supervisor started by `axx up`.
type upState struct {
	PID     int       `json:"pid"`
	Apps    []string  `json:"apps"`
	Started time.Time `json:"started"`
	Profile string    `json:"profile,omitempty"`
}

const (
	readyMarker = "AXX_SUPERVISOR_READY"
	errorMarker = "AXX_SUPERVISOR_ERROR "
)

// supervisorError reports a startup failure to the waiting `axx up` with its
// code, so the user sees the same error `axx run` would give.
func supervisorError(err error) error {
	e := supervisorFailure{Message: err.Error(), Exit: int(exitcode.Environment)}
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		e.Code, e.Message, e.Hint = ae.Code, ae.Error(), ae.Hint
		if ae.Exit != 0 {
			e.Exit = int(ae.Exit)
		}
	}
	b, _ := json.Marshal(e)
	fmt.Println(errorMarker + string(b))
	return err
}

type supervisorFailure struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Exit    int    `json:"exit"`
}

func parseSupervisorError(line string) (*axxerr.Error, bool) {
	rest, ok := strings.CutPrefix(line, errorMarker)
	if !ok {
		return nil, false
	}
	var e supervisorFailure
	if err := json.Unmarshal([]byte(rest), &e); err != nil {
		return nil, false //nolint:nilerr // not a supervisor error line after all
	}
	if e.Code == "" {
		e.Code = lifecycle.CodeExitedEarly
	}
	out := axxerr.New(e.Code, exitcode.Code(e.Exit), "%s", e.Message)
	if e.Hint != "" {
		out = out.WithHint("%s", e.Hint)
	}
	return out, true
}

func runDir(cfg *config.Config) string   { return filepath.Join(cfg.Dir, ".axx", "run") }
func upFile(cfg *config.Config) string   { return filepath.Join(runDir(cfg), "up.json") }
func stopFile(cfg *config.Config) string { return filepath.Join(runDir(cfg), "up.stop") }

func newUpCmd(app *App) *cobra.Command {
	var cf configFlags
	var debug string
	cmd := &cobra.Command{
		Use:   "up [apps...]",
		Short: "Start the apps from axx.yaml and keep them running between `axx run`s",
		Long: `Start applications (all enabled apps, or the ones named) in the background and
wait until they are ready. Later ` + "`axx run`" + ` invocations reuse them instead of
starting and stopping them each time, which is the fastest local loop.
Stop them with ` + "`axx down`" + `. App output goes to .axx/logs/apps.log.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			if len(cfg.Apps) == 0 {
				return app.Emit(upState{Apps: []string{}}, func(w io.Writer) error {
					_, err := fmt.Fprintln(w, "nothing to start: axx.yaml declares no apps")
					return err
				})
			}
			if st, ok := liveUp(cfg); ok {
				return app.Emit(st, func(w io.Writer) error {
					_, err := fmt.Fprintf(w, "already up (pid %d): %s\nrun `axx down` first to restart\n", st.PID, strings.Join(st.Apps, ", "))
					return err
				})
			}
			self, err := os.Executable()
			if err != nil {
				return err
			}
			sargs := []string{"__supervise"}
			if app.Config != "" {
				sargs = append(sargs, "--config", app.Config)
			}
			if cf.profile != "" {
				sargs = append(sargs, "--profile", cf.profile)
			}
			for _, d := range cf.defines {
				sargs = append(sargs, "-D", d)
			}
			if debug != "" {
				sargs = append(sargs, "--debug", debug)
			}
			sargs = append(sargs, args...)
			if err := os.MkdirAll(filepath.Join(cfg.Dir, ".axx", "logs"), 0o755); err != nil {
				return err
			}
			logf, err := os.OpenFile(filepath.Join(cfg.Dir, ".axx", "logs", "supervisor.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			defer logf.Close()
			sup := exec.Command(self, sargs...) //nolint:noctx // the supervisor must outlive this command
			sup.Dir = cfg.Dir
			sup.Stderr = logf
			detach(sup)
			out, err := sup.StdoutPipe()
			if err != nil {
				return err
			}
			if err := sup.Start(); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "axx: starting apps in the background (logs: %s)\n", relPath(filepath.Join(cfg.Dir, ".axx", "logs")))
			ready := make(chan error, 1)
			go func() {
				sc := bufio.NewScanner(out)
				var last []string
				for sc.Scan() {
					line := sc.Text()
					if line == readyMarker {
						ready <- nil
						return
					}
					if e, ok := parseSupervisorError(line); ok {
						ready <- e
						return
					}
					last = append(last, line)
				}
				ready <- axxerr.New(lifecycle.CodeExitedEarly, exitcode.Environment, "axx up failed: %s", strings.TrimSpace(strings.Join(last, "\n"))).
					WithHint("see %s", relPath(logf.Name()))
			}()
			select {
			case err := <-ready:
				if err != nil {
					_ = sup.Wait()
					return err
				}
			case <-cmd.Context().Done():
				_ = sup.Process.Signal(os.Interrupt)
				return cmd.Context().Err()
			}
			_ = sup.Process.Release()
			st, _ := readUp(cfg)
			return app.Emit(st, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "up: %s (stop with `axx down`)\n", strings.Join(st.Apps, ", "))
				return err
			})
		},
	}
	cf.register(cmd)
	cmd.Flags().StringVar(&debug, "debug", "", "start apps with their debug command (all, or a comma-separated list)")
	cmd.Flags().Lookup("debug").NoOptDefVal = "*"
	return cmd
}

func newSuperviseCmd(app *App) *cobra.Command {
	var cf configFlags
	var debug string
	cmd := &cobra.Command{
		Use:    "__supervise [apps...]",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return supervisorError(err)
			}
			_ = os.Remove(stopFile(cfg))
			logw, err := os.OpenFile(filepath.Join(cfg.Dir, ".axx", "logs", "apps.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return supervisorError(err)
			}
			defer logw.Close()
			opts := lifecycle.Options{
				ConfigDir: cfg.Dir, Stdout: logw, Stderr: logw, Logger: app.logger(),
				StateFile: filepath.Join(runDir(cfg), "state.json"),
			}
			switch debug {
			case "":
			case "*":
				opts.DebugAll = true
			default:
				opts.Debug = set(strings.Split(debug, ","))
			}
			names := args
			if len(names) == 0 {
				for _, a := range cfg.Apps {
					if a.IsEnabled() {
						names = append(names, a.Name)
					}
				}
			}
			mgr, err := lifecycle.New(cfg.Apps, opts)
			if err != nil {
				return supervisorError(err)
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// Packs set up what the apps need before they start (core.Preparer),
			// for every scenario of the configured features, and keep it until
			// `axx down`: later runs reuse it.
			e, err := engine.New(engine.Options{Config: cfg, Logger: app.logger()})
			if err != nil {
				return supervisorError(err)
			}
			defer func() { _ = e.Close(context.WithoutCancel(ctx)) }()
			if pickles, err := allScenarios(e); err != nil {
				fmt.Fprintf(os.Stderr, "axx: the features cannot be read, so no pack prepared them: %v\n", err)
			} else if err := e.Prepare(ctx, pickles); err != nil {
				return supervisorError(err)
			}
			if err := mgr.Start(ctx, names); err != nil {
				return supervisorError(err)
			}
			st := upState{PID: os.Getpid(), Apps: mgr.Started(), Started: time.Now(), Profile: cf.profile}
			b, _ := json.MarshalIndent(st, "", "  ")
			err = os.MkdirAll(runDir(cfg), 0o755)
			if err == nil {
				err = os.WriteFile(upFile(cfg), b, 0o644)
			}
			if err != nil {
				_ = mgr.Stop(context.Background())
				return supervisorError(err)
			}
			fmt.Println(readyMarker)
			_ = os.Stdout.Close() // detach from the parent's pipe

			// Wait for `axx down` (stop file or signal) or for every app to exit.
			tick := time.NewTicker(500 * time.Millisecond)
			defer tick.Stop()
		wait:
			for {
				select {
				case <-ctx.Done():
					break wait
				case <-tick.C:
					if _, err := os.Stat(stopFile(cfg)); err == nil {
						break wait
					}
				}
			}
			err = mgr.Stop(context.Background())
			_ = os.Remove(upFile(cfg))
			_ = os.Remove(stopFile(cfg))
			return err
		},
	}
	cf.register(cmd)
	cmd.Flags().StringVar(&debug, "debug", "", "")
	return cmd
}

func newDownCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop apps started by `axx up` (and any left behind by an interrupted run)",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			var stopped []string
			if st, ok := liveUp(cfg); ok {
				_ = os.WriteFile(stopFile(cfg), []byte("stop\n"), 0o644)
				signalTerm(st.PID)
				deadline := time.Now().Add(3 * time.Minute)
				for processAlive(st.PID) && time.Now().Before(deadline) {
					select {
					case <-cmd.Context().Done():
						return cmd.Context().Err()
					case <-time.After(200 * time.Millisecond):
					}
				}
				stopped = st.Apps
			}
			_ = os.Remove(upFile(cfg))
			_ = os.Remove(stopFile(cfg))
			// Reap anything a killed run or supervisor left behind.
			if err := lifecycle.Reap(filepath.Join(runDir(cfg), "state.json"), app.Stderr, app.Stderr); err != nil {
				return err
			}
			return app.Emit(map[string]any{"stopped": stopped}, func(w io.Writer) error {
				if len(stopped) == 0 {
					_, err := fmt.Fprintln(w, "nothing was running")
					return err
				}
				_, err := fmt.Fprintf(w, "stopped: %s\n", strings.Join(stopped, ", "))
				return err
			})
		},
	}
	cf.register(cmd)
	return cmd
}

// allScenarios returns every scenario of the configured feature paths.
func allScenarios(e *engine.Engine) ([]*feature.Pickle, error) {
	paths, _, err := e.FeaturePaths(nil)
	if err != nil {
		return nil, err
	}
	set, err := e.LoadFeatures(paths)
	if err != nil {
		return nil, err
	}
	return set.Apply(feature.Filter{})
}

func readUp(cfg *config.Config) (upState, error) {
	var st upState
	b, err := os.ReadFile(upFile(cfg))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

// liveUp returns the `axx up` state when its supervisor is still running.
func liveUp(cfg *config.Config) (upState, bool) {
	st, err := readUp(cfg)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			_ = os.Remove(upFile(cfg))
		}
		return st, false
	}
	if st.PID <= 0 || !processAlive(st.PID) {
		_ = os.Remove(upFile(cfg))
		return st, false
	}
	return st, true
}
