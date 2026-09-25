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
	"time"

	"github.com/nimbusxr/axx/internal/config"
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

// stateApp records one running app.
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
		if old, err := readState(path); err == nil && len(old.Apps) > 0 {
			m.inherited = old.Apps
			// Apps this run attaches to, such as the ones `axx up` keeps
			// running, are expected here; only the others are leftovers.
			stale := 0
			for _, a := range old.Apps {
				if !m.opts.Attach[a.Name] {
					stale++
				}
			}
			if stale > 0 {
				m.log.Warn("apps from an earlier run may still be running; `axx down` stops them",
					"stateFile", path, "apps", stale)
			}
		}
	}
	st := runState{Version: stateVersion, PID: os.Getpid(), Apps: append([]stateApp(nil), m.inherited...)}
	for _, a := range m.up {
		if a.proc == nil || a.cleaned {
			continue
		}
		pid, pgid := a.proc.group.ids()
		st.Apps = append(st.Apps, stateApp{
			Name: a.cfg.Name, PID: pid, PGID: pgid, StartedAt: a.proc.startedAt,
			Dir: a.dir, StopSignal: a.cfg.Stop.Signal, Grace: a.cfg.Stop.Grace,
			Cleanup: a.cleanup, Env: a.cfg.Env,
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
// app's prefix. The state file is removed. A missing state file means there
// is nothing to do.
func Reap(stateFile string, stdout, stderr io.Writer) error {
	st, err := readState(stateFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return envErr(CodeStateFile, "cannot read the state file %s: %v", stateFile, err).
			WithHint("stop leftover apps by hand, then delete %s", stateFile)
	}
	con := newConsole(stdout, stderr)
	ctx := context.Background()
	var errs []error
	for i := len(st.Apps) - 1; i >= 0; i-- {
		sa := st.Apps[i]
		g, err := openProcGroup(sa.PID, sa.PGID)
		if err != nil {
			errs = append(errs, envErr(CodeStateFile, "state file %s: app %s: %v", stateFile, sa.Name, err))
			continue
		}
		if g.alive() {
			con.Println(fmt.Sprintf("stopping app %s left over from an earlier run (pid %d)", sa.Name, sa.PID))
			if err := terminate(ctx, g, sa.StopSignal, sa.Grace.Or(defaultGrace)); err != nil {
				errs = append(errs, envErr(CodeStopFailed, "app %s could not be stopped: %v", sa.Name, err).
					WithHint("stop its processes by hand (process group %d)", sa.PGID))
			}
		}
		g.release()
		if len(sa.Cleanup) > 0 {
			con.Println(fmt.Sprintf("running cleanup of app %s: %s", sa.Name, displayArgv(sa.Cleanup)))
			if err := runCleanup(ctx, con, nil, sa.Name, sa.Cleanup, sa.Dir, appEnv(os.Environ(), sa.Env)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := os.Remove(stateFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, envErr(CodeStateFile, "cannot remove the state file %s: %v", stateFile, err))
	}
	return joinErrs(errs)
}

// ProcessAlive reports whether the process with this id is running (and,
// on Unix, not a zombie).
func ProcessAlive(pid int) bool { return processAlive(pid) }
