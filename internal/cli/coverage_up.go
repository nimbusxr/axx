package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// codeKeptUnmeasured: a run with coverage, against services axx up keeps
// without it.
const codeKeptUnmeasured = "AXX-E1007"

// upCoverageDir is the coverage folder of the services axx up keeps: theirs
// until axx down.
func upCoverageDir(cfg *config.Config) string {
	return filepath.Join(cfg.Dir, ".axx", "coverage", "raw", "up")
}

// upCoverage readies the services axx up keeps for coverage (axx up
// --coverage): each gets what its runtime adds, the agent of one a run reads
// while it runs (core.CoverageService.Kept). measured is each one's
// runtime.
func upCoverage(ctx context.Context, e *engine.Engine, names []string) (measured map[string]string, env map[string][]string, err error) {
	cfg := e.Config
	l := coverageLauncher(e)
	if l == nil {
		return nil, nil, axxerr.New(codeNoCoveragePacks, exitcode.Usage, "axx up --coverage measures the services with a coverage section, with coverage-core and the pack of each one's runtime, and the project lists none of them").
			WithHint("add the pack of each service's runtime: `axx pack add coverage-jvm` (or coverage-go, coverage-python, coverage-node)")
	}
	if err := checkCoverageRuntimes(cfg, l); err != nil {
		return nil, nil, err
	}
	dir := upCoverageDir(cfg)
	_ = os.RemoveAll(dir) // an earlier axx up's
	measured, env = map[string]string{}, map[string][]string{}
	for _, s := range cfg.Services {
		if s.Coverage == nil || !slices.Contains(names, s.Name) {
			continue
		}
		cs := coverageService(cfg, s, dir)
		cs.Kept = true
		if err := os.MkdirAll(cs.Dir, 0o755); err != nil {
			return nil, nil, err
		}
		vars, err := l.LaunchCoverage(ctx, e.Suite.PackConfig, cs)
		if err != nil {
			return nil, nil, axxerr.Wrap(err, codeNoCoveragePacks, exitcode.Environment, "cannot ready service %s to measure its coverage", s.Name)
		}
		measured[s.Name], env[s.Name] = s.Coverage.Runtime, vars
	}
	return measured, env, nil
}

// kept are the services axx up keeps that a run with coverage measures:
// those whose runtime it reads while they run, reset as it starts and taken
// as it ends; and the others, which write their coverage only as axx down
// stops them.
type kept struct {
	keeper   core.CoverageKeeper
	read     []core.CoverageService
	atDown   map[string]string // service → runtime
	release  func()
	settings func(pack string, v any) error
}

// keepCoverage readies a run with coverage against the services axx up
// keeps (st): resets those it reads while they run, under a lock, so that
// no other run resets them meanwhile.
func (c *runCoverage) keepCoverage(ctx context.Context, e *engine.Engine, st upState) (*kept, error) {
	cfg := e.Config
	var unmeasured []string
	for _, n := range st.Services {
		if s, ok := cfg.Services.Get(n); ok && s.Coverage != nil && st.Coverage[n] == "" {
			unmeasured = append(unmeasured, n)
		}
	}
	if len(unmeasured) > 0 {
		return nil, axxerr.New(codeKeptUnmeasured, exitcode.Usage, "axx up keeps %s, without coverage: a run with coverage cannot measure them", strings.Join(unmeasured, ", ")).
			WithHint("`axx down`, then `axx up --coverage`; or let the run start them")
	}
	k := &kept{atDown: map[string]string{}, settings: e.Suite.PackConfig, release: func() {}}
	k.keeper, _ = c.launcher.(core.CoverageKeeper)
	for _, n := range st.Services {
		runtime, ok := st.Coverage[n]
		s, found := cfg.Services.Get(n)
		if !ok || !found {
			continue
		}
		if k.keeper == nil || !k.keeper.KeepsCoverage(runtime) {
			k.atDown[n] = runtime
			continue
		}
		cs := coverageService(cfg, s, upCoverageDir(cfg))
		cs.Kept = true
		k.read = append(k.read, cs)
	}
	if len(k.read) == 0 {
		return k, nil
	}
	release, err := lockCoverage(cfg)
	if err != nil {
		return nil, err
	}
	k.release = release
	for _, cs := range k.read {
		if err := k.keeper.ResetCoverage(ctx, k.settings, cs); err != nil {
			release()
			return nil, axxerr.Wrap(err, codeNoCoveragePacks, exitcode.Environment, "cannot reset the coverage of service %s, which axx up keeps", cs.Name)
		}
	}
	return k, nil
}

// take takes the coverage of the kept services the run read, into its own
// folder, and says how each ended for the run, as lifecycle does of those it
// stops; it releases the lock.
func (k *kept) take(ctx context.Context, runDir string) ([]lifecycle.Exit, error) {
	defer k.release()
	var exits []lifecycle.Exit
	for _, cs := range k.read {
		into := lifecycle.CoverageFolder(runDir, cs.Name)
		if err := k.keeper.TakeCoverage(ctx, k.settings, cs, into); err != nil {
			return nil, axxerr.Wrap(err, codeNoCoveragePacks, exitcode.Environment, "cannot take the coverage of service %s, which axx up keeps", cs.Name)
		}
		exits = append(exits, lifecycle.Exit{Service: cs.Name, Status: "kept running by axx up", Measured: true, CoverageDir: into})
	}
	for _, n := range slices.Sorted(func(yield func(string) bool) {
		for n := range k.atDown {
			if !yield(n) {
				return
			}
		}
	}) {
		exits = append(exits, lifecycle.Exit{Service: n, Unmeasured: "axx up keeps it, and " + k.atDown[n] + " writes its coverage only as it exits: axx down writes it"})
	}
	return exits, nil
}

// lockCoverage takes the project's lock of the coverage of the services axx
// up keeps, for a run that resets and reads them: a run that holds it
// shares them with no other. A lock whose run has gone is taken over.
func lockCoverage(cfg *config.Config) (func(), error) {
	p := filepath.Join(runDir(cfg), "coverage.lock")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	for range 2 {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintln(f, os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		b, _ := os.ReadFile(p)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		if pid > 0 && processAlive(pid) {
			return nil, axxerr.New(codeKeptUnmeasured, exitcode.Environment, "another run (pid %d) measures the coverage of the services axx up keeps", pid).
				WithHint("run again once it is done: two runs would reset each other's coverage")
		}
		_ = os.Remove(p) // its run has gone
	}
	return nil, fmt.Errorf("cannot take %s", p)
}

// concludeAtDown writes, once axx down stopped the services axx up kept,
// the coverage of those that write theirs only as they exit (Go, Python):
// every run's since axx up, whose summary it prints to w. exits are how they
// ended.
func concludeAtDown(ctx context.Context, w io.Writer, e *engine.Engine, exits []lifecycle.Exit) error {
	l := coverageLauncher(e)
	var c core.Concluder
	for _, p := range e.Packs {
		if cc, ok := p.Pack.(core.Concluder); ok {
			c = cc
		}
	}
	if l == nil || c == nil {
		return nil
	}
	keeper, _ := l.(core.CoverageKeeper)
	var atExit []lifecycle.Exit
	for _, x := range exits {
		s, ok := e.Config.Services.Get(x.Service)
		if !ok || s.Coverage == nil || !x.Measured || keeper != nil && keeper.KeepsCoverage(s.Coverage.Runtime) {
			continue
		}
		atExit = append(atExit, x)
	}
	if len(atExit) == 0 {
		return nil
	}
	rc := &runCoverage{
		launcher: l, concluder: c, dir: upCoverageDir(e.Config),
		partial: "it is the coverage the services axx up kept wrote as axx down stopped them, of every run since axx up",
	}
	cov, err := rc.conclude(ctx, e, atExit, false)
	printCoverage(w, cov)
	return err
}
