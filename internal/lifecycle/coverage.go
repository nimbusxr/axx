package lifecycle

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// CoverageEnv names a service's coverage folder, on every run of a service
// with a coverage section: where it writes its coverage when the run
// measures it (axx run --coverage), and an empty folder otherwise, so that a
// compose file that mounts it works in either.
const CoverageEnv = "AXX_COVERAGE_DIR"

// CoverageRunDir is a new folder for one run's coverage, under the project's
// .axx/coverage/raw: each run's own, so what one run's containers wrote (as
// root, on Linux) never mixes with the next's.
func CoverageRunDir(configDir string) string {
	return filepath.Join(configDir, ".axx", "coverage", "raw", strconv.FormatInt(time.Now().UnixNano(), 36))
}

// CoverageFolder is a service's folder in a run's coverage folder.
func CoverageFolder(runDir, service string) string {
	return filepath.Join(runDir, service)
}

// coverageFolder is a's coverage folder: the run's own when the run
// measures it, and otherwise one that stays empty, the same on every run.
func (m *Manager) coverageFolder(a *service) string {
	if ok, _ := m.measured(a); ok {
		return CoverageFolder(m.coverageDir, a.cfg.Name)
	}
	return CoverageFolder(filepath.Join(m.opts.ConfigDir, ".axx", "coverage", "raw", "unmeasured"), a.cfg.Name)
}

// Exit is how a service the run started ended, for what a service writes as
// it exits (its coverage) and for saying why it wrote nothing.
type Exit struct {
	Service string `json:"service"`
	// Status describes how its main process ended: "exited with code 0",
	// "was killed (signal: killed)".
	Status string `json:"status,omitempty"`
	// Killed is set when axx killed it after its grace: it had no chance to
	// write what it writes as it exits. NoStopSignal, when axx could give it
	// no stop signal first, and killed it at once: Windows has none for a
	// process.
	Killed       bool `json:"killed,omitempty"`
	NoStopSignal bool `json:"noStopSignal,omitempty"`
	// OnItsOwn is set when it exited before axx stopped it.
	OnItsOwn bool `json:"onItsOwn,omitempty"`
	// Measured is set when the run measured its coverage; CoverageDir is
	// its coverage folder, for a service with a coverage section.
	Measured    bool   `json:"measured,omitempty"`
	CoverageDir string `json:"coverageDir,omitempty"`
	// Unmeasured says why a service the run was to measure was not: it ran
	// in debug mode, or someone else started it.
	Unmeasured string `json:"unmeasured,omitempty"`
}

// Exits are how the services this run started ended, once Stop has stopped
// them, in the order they started; and the attached ones, measured by no
// one.
func (m *Manager) Exits() []Exit {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.exits)
}

// measured reports whether the run measures a's coverage, and if it was to
// but does not, why.
func (m *Manager) measured(a *service) (bool, string) {
	name := a.cfg.Name
	if a.cfg.Coverage == nil || !m.opts.Coverage[name] {
		return false, ""
	}
	switch {
	case m.opts.Attach[name]:
		return false, "it is attached: someone else starts it"
	case a.debug != nil && (m.opts.DebugAll || m.opts.Debug[name]):
		return false, "it runs in debug mode, whose command is debug.command's"
	}
	return true, ""
}

// coverageEnv makes a's coverage folder and returns the variables a gets for
// it: AXX_COVERAGE_DIR on every run, and when the run measures it, its
// coverage section's env and what its runtime adds (Options.CoverageEnv).
func (m *Manager) coverageEnv(a *service) ([]string, error) {
	if a.cfg.Coverage == nil {
		return nil, nil
	}
	name := a.cfg.Name
	dir := m.coverageFolder(a)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, envErr(CodeStateFile, "service %s: cannot make its coverage folder %s: %v", name, dir, err)
	}
	env := []string{CoverageEnv + "=" + dir}
	if ok, _ := m.measured(a); ok {
		if c := a.run.Coverage; c != nil {
			for _, k := range slices.Sorted(maps.Keys(c.Env)) {
				env = append(env, k+"="+c.Env[k])
			}
		}
		env = append(env, m.opts.CoverageEnv[name]...)
	}
	return env, nil
}

// recordExit keeps how a ended (under m.mu).
func (m *Manager) recordExit(a *service, killed, noStopSignal bool) {
	e := Exit{Service: a.cfg.Name}
	if a.cfg.Coverage != nil {
		e.CoverageDir = m.coverageFolder(a)
	}
	e.Measured, e.Unmeasured = m.measured(a)
	if p := a.proc; p != nil {
		e.Status, e.Killed, e.NoStopSignal = describeExit(p.state), killed, killed && noStopSignal
		e.OnItsOwn = p.exitedOnItsOwn.Load()
	}
	m.exits = append(m.exits, e)
}
