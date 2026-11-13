package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

const (
	// codeNoCoveragePacks: a run with coverage, in a project without the
	// packs that measure it.
	codeNoCoveragePacks = "AXX-E1000"
	// codeCoverageRuntime: a coverage section names a runtime no pack of
	// the project measures.
	codeCoverageRuntime = "AXX-E1001"
)

// runCoverage is a run with coverage (axx run --coverage, run.coverage): the
// pack that readies the measured services as they start, the one that
// concludes the run once they stop, and what the minimums need to know of
// the run.
type runCoverage struct {
	launcher  core.CoverageLauncher
	concluder core.Concluder
	// dir is the run's coverage folder (lifecycle.CoverageRunDir).
	dir string
	// partial says why the run is not a run of every scenario its
	// configuration selects; shard and selected, which share of which run
	// it is.
	partial, shard, selected string
}

// coverageLauncher is the pack that readies measured services, if the
// project has one.
func coverageLauncher(e *engine.Engine) core.CoverageLauncher {
	for _, p := range e.Packs {
		if l, ok := p.Pack.(core.CoverageLauncher); ok {
			return l
		}
	}
	return nil
}

// checkCoverageRuntimes checks the runtimes the services' coverage sections
// name against those the project's coverage packs measure: on every run
// once the project has them, so a misspelt one shows before the run that
// measures it.
func checkCoverageRuntimes(cfg *config.Config, l core.CoverageLauncher) error {
	if l == nil {
		return nil
	}
	known := l.CoverageRuntimes()
	for _, s := range cfg.Services {
		if s.Coverage == nil || slices.Contains(known, s.Coverage.Runtime) {
			continue
		}
		return axxerr.New(codeCoverageRuntime, exitcode.Usage, "services.%s.coverage.runtime is %q, which none of the project's coverage packs measures (they measure %s)",
			s.Name, s.Coverage.Runtime, strings.Join(known, ", ")).
			WithHint("add the pack of its runtime (`axx pack add coverage-%s`), or name one of those", s.Coverage.Runtime)
	}
	return nil
}

// newRunCoverage readies a run with coverage, nil without: selected are the
// scenarios the configuration selects before sharding.
func (a *App) newRunCoverage(e *engine.Engine, f *runFlags, args []string, selected []*feature.Pickle) (*runCoverage, error) {
	cfg := e.Config
	l := coverageLauncher(e)
	if err := checkCoverageRuntimes(cfg, l); err != nil {
		return nil, err
	}
	if !cfg.Run.Coverage || f.dryRun {
		return nil, nil
	}
	var c core.Concluder
	for _, p := range e.Packs {
		if cc, ok := p.Pack.(core.Concluder); ok {
			c = cc
			break
		}
	}
	if l == nil || c == nil {
		return nil, axxerr.New(codeNoCoveragePacks, exitcode.Usage, "a run with coverage measures the services with a coverage section, with coverage-core and the pack of each one's runtime, and the project lists none of them").
			WithHint("add the pack of each service's runtime, which brings coverage-core: `axx pack add coverage-jvm` (or coverage-go, coverage-python, coverage-node)")
	}
	return &runCoverage{
		launcher:  l,
		concluder: c,
		dir:       lifecycle.CoverageRunDir(cfg.Dir),
		partial:   partialRun(f, args),
		shard:     f.shard,
		selected:  selection(selected),
	}, nil
}

// partialRun says why a run is not a run of every scenario its
// configuration selects (run.tags and run.uses), "" when it is: the
// minimums apply to whole runs only.
func partialRun(f *runFlags, args []string) string {
	switch {
	case len(args) > 0:
		return "it runs the paths it names"
	case strings.TrimSpace(f.tags) != "":
		return "it runs the scenarios --tags selects"
	case len(f.names) > 0:
		return "it runs the scenarios --name selects"
	case f.shard != "":
		return "it is the shard " + f.shard + " of a run: axx coverage check checks the shards together"
	case f.failFast:
		return "--fail-fast can stop it before every scenario ran"
	case f.noStart:
		return "--no-start leaves the services to someone else"
	}
	return ""
}

// selection identifies a set of scenarios by their places.
func selection(pickles []*feature.Pickle) string {
	places := make([]string, len(pickles))
	for i, p := range pickles {
		places[i] = fmt.Sprintf("%s:%d", p.Uri, p.Line)
	}
	slices.Sort(places)
	h := sha256.Sum256([]byte(strings.Join(places, "\n")))
	return hex.EncodeToString(h[:8])
}

// launch readies the measured services among those the run starts (names),
// returning the variables each gets for its runtime.
func (c *runCoverage) launch(ctx context.Context, e *engine.Engine, names []string) (map[string]bool, map[string][]string, error) {
	cfg := e.Config
	measured, env := map[string]bool{}, map[string][]string{}
	for _, s := range cfg.Services {
		if s.Coverage == nil || !slices.Contains(names, s.Name) {
			continue
		}
		cs := coverageService(cfg, s, c.dir)
		if err := os.MkdirAll(cs.Dir, 0o755); err != nil {
			return nil, nil, err
		}
		vars, err := c.launcher.LaunchCoverage(ctx, e.Suite.PackConfig, cs)
		if err != nil {
			return nil, nil, axxerr.Wrap(err, codeNoCoveragePacks, exitcode.Environment, "cannot ready service %s to measure its coverage", s.Name)
		}
		measured[s.Name], env[s.Name] = true, vars
	}
	return measured, env, nil
}

// coverageService is s as the runtime that measures it sees it.
func coverageService(cfg *config.Config, s config.Service, runDir string) core.CoverageService {
	work := cfg.Dir
	if s.Dir != "" {
		work = s.Dir
		if !filepath.IsAbs(work) {
			work = filepath.Join(cfg.Dir, work)
		}
	}
	return core.CoverageService{
		Name: s.Name, Runtime: s.Coverage.Runtime,
		Dir: lifecycle.CoverageFolder(runDir, s.Name), Mount: s.Coverage.Mount, WorkDir: work,
	}
}

// conclude has the coverage pack conclude the run, once its services have
// stopped (exits, how they ended).
func (c *runCoverage) conclude(ctx context.Context, e *engine.Engine, exits []lifecycle.Exit, interrupted bool) (*core.Coverage, error) {
	run := core.CoverageRun{Partial: c.partial, Shard: c.shard, Selected: c.selected}
	if interrupted {
		run.Partial = "it was interrupted"
	}
	byName := map[string]config.Service{}
	for _, s := range e.Config.Services {
		byName[s.Name] = s
	}
	for _, x := range exits {
		s, ok := byName[x.Service]
		if !ok || s.Coverage == nil || (!x.Measured && x.Unmeasured == "") {
			continue
		}
		end := core.CoverageEnd{
			CoverageService: coverageService(e.Config, s, c.dir),
			Status:          x.Status, Killed: x.Killed, OnItsOwn: x.OnItsOwn, Unmeasured: x.Unmeasured,
		}
		end.Dir = x.CoverageDir
		if x.Unmeasured != "" && run.Partial == "" {
			run.Partial = "service " + x.Service + " was not measured: " + x.Unmeasured
		}
		run.Services = append(run.Services, end)
	}
	return c.concluder.Conclude(ctx, e.Suite, run)
}
