package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/config"

	"github.com/nimbusxr/axx/internal/proc"
)

// stateVersion is the schema version of the state file.
const stateVersion = 1

// runState is the state file: the apps a run has started and not yet
// cleaned up, so that `axx down` can reap them if the run was killed.
type runState struct {
	Version int `json:"version"`
	// PID is the axx process that started the apps.
	PID  int        `json:"pid"`
	Apps []stateApp `json:"apps"`
}

// stateApp records one app that is running, or stopped but not cleaned up.
type stateApp struct {
	Name       string            `json:"name"`
	PID        int               `json:"pid"`
	PGID       int               `json:"pgid"`
	StartedAt  time.Time         `json:"startedAt"`
	Dir        string            `json:"dir"`
	StopSignal string            `json:"stopSignal,omitempty"`
	Grace      config.Duration   `json:"grace,omitzero"`
	Cleanup    []string          `json:"cleanup,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	// Owner is the axx process that started the app (earlier files: the
	// file's PID).
	Owner int `json:"owner,omitempty"`
	// CleanupFailed is set once the app is stopped but its cleanup failed:
	// `axx down` runs the cleanup again.
	CleanupFailed bool `json:"cleanupFailed,omitempty"`
}

// StateFile is where a project's runs record the apps they start.
func StateFile(projectDir string) string {
	return filepath.Join(projectDir, ".axx", "run", "state.json")
}

// saveStateLocked rewrites the state file with the apps that are running or
// not cleaned up yet (and entries inherited from an earlier run), or removes
// it when there are none. Failures are logged: the file is a safety net and
// must not fail the run. Callers hold m.mu.
func (m *Manager) saveStateLocked() {
	path := m.opts.StateFile
	if path == "" {
		return
	}
	if !m.stateLoaded {
		m.stateLoaded = true
		if old, err := readState(path); err == nil {
			for _, a := range old.Apps {
				if a.Owner == 0 && !a.CleanupFailed {
					a.Owner = old.PID
				}
				m.inherited = append(m.inherited, a)
			}
		}
	}
	st := runState{Version: stateVersion, PID: os.Getpid(), Apps: slices.Concat(m.inherited, m.unclean)}
	for _, a := range m.up {
		if a.proc == nil || a.cleaned {
			continue
		}
		pid, pgid := a.proc.group.IDs()
		st.Apps = append(st.Apps, stateApp{
			Name: a.cfg.Name, PID: pid, PGID: pgid, StartedAt: a.proc.startedAt,
			Dir: a.dir, StopSignal: a.cfg.Stop.Signal, Grace: a.cfg.Stop.Grace,
			Cleanup: a.cleanup, Env: a.cfg.Env, Owner: os.Getpid(),
		})
	}
	if len(st.Apps) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			m.log.Warn("could not remove the state file", "stateFile", path, "error", err)
		}
		return
	}
	if err := writeState(path, st); err != nil {
		m.log.Warn("could not write the state file; `axx down` will not know about these apps",
			"stateFile", path, "error", err)
	}
}

// writeState writes st to path atomically, readable only by the user (the
// cleanup environment may hold secrets).
func writeState(path string, st runState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// readState reads a state file.
func readState(path string) (runState, error) {
	var st runState
	data, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	if st.Version != stateVersion {
		return st, fmt.Errorf("unsupported state file version %d", st.Version)
	}
	return st, nil
}

// Reap stops the apps recorded in stateFile by a run that did not stop them
// (it was killed, or crashed): process groups still alive get their stop
// signal, a grace period and then SIGKILL, in reverse start order. Each
// app's cleanup then runs, its output going to stdout and stderr with the
// app's prefix, as does a cleanup that failed before. It returns the apps it
// stopped or cleaned up. An app that cannot be stopped, or whose cleanup
// fails, stays in the state file for the next Reap; the file is removed
// once nothing is left. A missing state file means there is nothing to do.
func Reap(stateFile string, stdout, stderr io.Writer) ([]string, error) {
	st, err := readState(stateFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, envErr(CodeStateFile, "cannot read the state file %s: %v", stateFile, err).
			WithHint("stop leftover apps by hand, then delete %s", stateFile)
	}
	con := newConsole(stdout, stderr)
	ctx := context.Background()
	var (
		errs []error
		done []string
		kept []stateApp
	)
	for i := len(st.Apps) - 1; i >= 0; i-- {
		sa := st.Apps[i]
		if !sa.CleanupFailed {
			g, err := proc.OpenGroup(sa.PID, sa.PGID)
			if err != nil {
				errs = append(errs, envErr(CodeStateFile, "state file %s: app %s: %v", stateFile, sa.Name, err))
				continue
			}
			alive := g.Alive()
			if alive {
				con.Println(fmt.Sprintf("stopping app %s left over from an earlier run (pid %d)", sa.Name, sa.PID))
				if err := terminate(ctx, g, sa.StopSignal, sa.Grace.Or(defaultGrace)); err != nil {
					errs = append(errs, envErr(CodeStopFailed, "app %s could not be stopped: %v", sa.Name, err).
						WithHint("stop its processes by hand (process group %d)", sa.PGID))
					g.Release()
					sa.Owner = 0
					kept = append([]stateApp{sa}, kept...)
					continue
				}
			}
			g.Release()
			if !alive && len(sa.Cleanup) == 0 {
				continue // gone, with nothing to clean up
			}
		}
		if len(sa.Cleanup) > 0 {
			con.Println(fmt.Sprintf("running cleanup of app %s: %s", sa.Name, displayArgv(sa.Cleanup)))
			if err := runCleanup(ctx, con, nil, sa.Name, sa.Cleanup, sa.Dir, appEnv(os.Environ(), sa.Env)); err != nil {
				errs = append(errs, err)
				kept = append([]stateApp{uncleaned(sa)}, kept...)
				continue
			}
		}
		done = append(done, sa.Name)
	}
	if len(kept) > 0 {
		if err := writeState(stateFile, runState{Version: stateVersion, Apps: kept}); err != nil {
			errs = append(errs, envErr(CodeStateFile, "cannot write the state file %s: %v", stateFile, err))
		}
	} else if err := os.Remove(stateFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, envErr(CodeStateFile, "cannot remove the state file %s: %v", stateFile, err))
	}
	return done, joinErrs(errs)
}

// uncleaned is the state entry of a stopped app whose cleanup failed.
func uncleaned(sa stateApp) stateApp {
	return stateApp{Name: sa.Name, Dir: sa.Dir, Cleanup: sa.Cleanup, Env: sa.Env, CleanupFailed: true}
}

// AppStatus is an app a project's state file records.
type AppStatus struct {
	Name string `json:"name"`
	// State is "running" (the axx process that started it, `axx up` or a
	// run, is still running), "left over" (it runs, but what started it
	// does not: `axx down` stops it and runs its cleanup), or "not cleaned
	// up" (it stopped, and its cleanup failed or never ran: `axx down` runs
	// it).
	State string `json:"state"`
	PID   int    `json:"pid,omitempty"`
	// Cleanup is the app's cleanup command.
	Cleanup string `json:"cleanup,omitempty"`
	// CleanupFailed: the cleanup ran and failed.
	CleanupFailed bool `json:"cleanupFailed,omitempty"`
}

// The states of AppStatus.
const (
	AppRunning    = "running"
	AppLeftOver   = "left over"
	AppNotCleaned = "not cleaned up"
)

// Status returns the apps stateFile records, with their state; none when
// there is no state file.
func Status(stateFile string) ([]AppStatus, error) {
	st, err := readState(stateFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, envErr(CodeStateFile, "cannot read the state file %s: %v", stateFile, err)
	}
	return statusOf(st), nil
}

func statusOf(st runState) []AppStatus {
	var out []AppStatus
	for _, sa := range st.Apps {
		owner := sa.Owner
		if owner == 0 && !sa.CleanupFailed {
			owner = st.PID
		}
		as := AppStatus{Name: sa.Name, CleanupFailed: sa.CleanupFailed}
		if len(sa.Cleanup) > 0 {
			as.Cleanup = displayArgv(sa.Cleanup)
		}
		switch {
		case sa.CleanupFailed:
			as.State = AppNotCleaned
		case owner > 0 && proc.ProcessAlive(owner):
			as.State, as.PID = AppRunning, sa.PID
		case sa.PID > 0 && proc.ProcessAlive(sa.PID):
			as.State, as.PID = AppLeftOver, sa.PID
		case len(sa.Cleanup) > 0:
			as.State = AppNotCleaned
		default:
			continue // gone, with nothing to clean up
		}
		out = append(out, as)
	}
	return out
}

// checkEarlierRuns deals with apps an earlier run left behind (still running
// with nothing to stop them, or stopped without their cleanup), whose data
// would be what this run starts from. When nothing recorded belongs to a run
// still going, or to an app this run attaches to, all of it was left by runs
// that ended without stopping it (stopped by force, or crashed): it is
// cleaned up as `axx down` does, and the run starts from a clean slate.
// Otherwise the run refuses to start.
func (m *Manager) checkEarlierRuns() error {
	if m.opts.StateFile == "" {
		return nil
	}
	var apps []AppStatus
	if st, err := readState(m.opts.StateFile); err == nil { // none, or unreadable: `axx down` reports that
		apps = statusOf(st)
	}
	var left []string
	inUse := false
	for _, as := range apps {
		if m.opts.Attach[as.Name] || as.State == AppRunning {
			inUse = true
			continue
		}
		switch {
		case as.State == AppLeftOver:
			left = append(left, fmt.Sprintf("app %s is still running (pid %d)", as.Name, as.PID))
		case as.CleanupFailed:
			left = append(left, fmt.Sprintf("the cleanup of app %s failed (%s)", as.Name, as.Cleanup))
		default:
			left = append(left, fmt.Sprintf("the cleanup of app %s never ran (%s)", as.Name, as.Cleanup))
		}
	}
	if len(left) == 0 {
		return nil
	}
	if !inUse {
		m.log.Warn("an earlier run was not cleaned up; cleaning up as `axx down` does", "left", strings.Join(left, "; "))
		if _, err := Reap(m.opts.StateFile, m.opts.Stdout, m.opts.Stderr); err != nil {
			return envErr(CodeNotCleanedUp, "an earlier run was not cleaned up, and cleaning it up failed: %v", err).
				WithHint("run `axx down` to see what is left, and stop it by hand if it cannot")
		}
		return nil
	}
	return envErr(CodeNotCleanedUp, "an earlier run was not cleaned up: %s", strings.Join(left, "; ")).
		WithHint("run `axx down`: it stops what is left and runs the cleanup again")
}

// ProcessAlive reports whether the process with this id is running (and,
// on Unix, not a zombie).
func ProcessAlive(pid int) bool { return proc.ProcessAlive(pid) }
