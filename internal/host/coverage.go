package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// Launch readies a service a run with coverage measures, for its runtime:
// the variables it gets (core.CoverageLauncher, with cfg's pack settings).
type Launch func(ctx context.Context, cfg *config.Config, s core.CoverageService) ([]string, error)

// hostCoverage is what a run with coverage asks of the host: the run's
// coverage folder, the services it measures, and what each gets for its
// runtime.
type hostCoverage struct {
	dir      string
	measured map[string]bool
	env      map[string][]string
}

// coverage readies the services a run with coverage (req.Coverage) measures,
// through the coverage packs this axx has (Options.Launch). Their coverage
// goes to the project's folder the run names, never the host's own: the
// run reads it, and a container's mount must reach it.
func (s *Server) coverage(ctx context.Context, cfg *config.Config, req Request) (*hostCoverage, error) {
	if !req.Coverage {
		return nil, nil
	}
	rel := filepath.FromSlash(req.CoverageDir)
	raw := filepath.Join(".axx", "coverage", "raw") + string(filepath.Separator)
	if !filepath.IsLocal(rel) || !strings.HasPrefix(rel, raw) {
		return nil, hostErr(CodeRefused, "the run's coverage folder %s is not under the project's .axx/coverage/raw", req.CoverageDir)
	}
	if s.opts.Launch == nil {
		return nil, hostErr(CodeRefused, "the axx host measures no coverage: it runs without the project's coverage packs").
			WithHint("start `axx host` again in the project, whose axx-packs.yaml lists them")
	}
	c := &hostCoverage{dir: filepath.Join(cfg.Dir, rel), measured: map[string]bool{}, env: map[string][]string{}}
	for _, svc := range cfg.Services {
		if svc.Coverage == nil || !svc.IsEnabled() || (req.Services != nil && !slices.Contains(req.Services, svc.Name)) {
			continue
		}
		work := cfg.Dir
		if svc.Dir != "" {
			if work = svc.Dir; !filepath.IsAbs(work) {
				work = filepath.Join(cfg.Dir, work)
			}
		}
		cs := core.CoverageService{
			Name: svc.Name, Runtime: svc.Coverage.Runtime,
			Dir: lifecycle.CoverageFolder(c.dir, svc.Name), Mount: svc.Coverage.Mount, WorkDir: work,
			Kept: req.CoverageKept,
		}
		if err := os.MkdirAll(cs.Dir, 0o755); err != nil {
			return nil, err
		}
		vars, err := s.opts.Launch(ctx, cfg, cs)
		if err != nil {
			return nil, axxerr.Wrap(err, CodeDefinition, exitcode.Environment, "cannot ready service %s to measure its coverage", svc.Name)
		}
		c.measured[svc.Name], c.env[svc.Name] = true, vars
	}
	return c, nil
}

// text is what the person approves of a run with coverage: what each
// measured service gets for its runtime, its folder written
// ${AXX_COVERAGE_DIR}, so that the run's next has the same.
func (c *hostCoverage) text() string {
	var b strings.Builder
	b.WriteString("\n# what the services get when a run measures their coverage (axx run --coverage)\n")
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for n := range c.env {
			if !yield(n) {
				return
			}
		}
	}) {
		fmt.Fprintf(&b, "%s:\n", name)
		folder := lifecycle.CoverageFolder(c.dir, name)
		for _, v := range c.env[name] {
			fmt.Fprintf(&b, "  %s\n", strings.ReplaceAll(v, folder, "${"+lifecycle.CoverageEnv+"}"))
		}
	}
	return b.String()
}

// relativeExits are exits with their coverage folders relative to dir, the
// project: the run, which may see the project elsewhere, joins them to its
// own.
func relativeExits(dir string, exits []lifecycle.Exit) []lifecycle.Exit {
	out := slices.Clone(exits)
	for i := range out {
		if out[i].CoverageDir == "" {
			continue
		}
		if rel, err := filepath.Rel(dir, out[i].CoverageDir); err == nil && filepath.IsLocal(rel) {
			out[i].CoverageDir = filepath.ToSlash(rel)
		} else {
			out[i].CoverageDir = ""
		}
	}
	return out
}
