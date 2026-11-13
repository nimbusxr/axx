// Package coveragecore is the coverage-core pack: the coverage of the system
// under test, as a run exercises it (axx run --coverage). It readies the
// services whose coverage section names a runtime, through the packs of
// those runtimes (coverage-jvm, coverage-go, coverage-python,
// coverage-node), which register with it; once they stop, it reads what
// they wrote, adds what packs measured during the run (web-coverage), writes
// the run's reports (lcov, Istanbul's JSON) and compares them with the
// minimums axx.yaml sets. It has no steps.
package coveragecore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/nimbusxr/axx/core"
)

// Name is the pack's name.
const Name = "coverage-core"

// Pack returns the coverage-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		ConfigSchema: []byte(configSchema), Steps: []core.StepDef{},
		Commands: []core.Command{coverageCommand()},
	}
}

const packDoc = `Measure the coverage of the system under test as the scenarios exercise it: ` + "`axx run --coverage`" + ` (or ` + "`run.coverage`" + `) measures each service whose ` + "`coverage`" + ` section in axx.yaml names a runtime, with the pack of that runtime (` + "`coverage-jvm`, `coverage-go`, `coverage-python`, `coverage-node`" + `, which bring this one), and the web apps' JavaScript with ` + "`web-coverage`" + `. Once the services stop, it writes the run's reports, ` + "`lcov.info`" + ` for editors and coverage services and Istanbul's ` + "`coverage-final.json`" + ` and ` + "`coverage-summary.json`" + `, in ` + "`.axx/coverage`" + ` (` + "`folder`" + `), prints the totals of each part, and compares them with the minimums.

- **Minimums.** ` + "`minimum`" + ` sets the whole run's, and ` + "`parts.<name>.minimum`" + ` a part's, in percent of ` + "`lines`, `branches` and `functions`" + `. Only a run of every scenario its configuration selects checks them; under one, a run whose scenarios all passed exits 6. ` + "`axx coverage check`" + ` checks the shards of a run together.
- **Any language.** A service whose coverage section names the runtime ` + "`report`" + ` writes lcov (` + "`*.info`" + `) or Cobertura (` + "`*.xml`" + `) into the folder ` + "`$AXX_COVERAGE_DIR`" + ` names, by its own tools, and the run reads it once it stops.
- **Paths.** A service in a container names its files as they are in it: ` + "`parts.<name>.paths`" + ` maps where they were to where they are in the project.

` + "```yaml" + `
services:
  orders:
    command: java -jar build/libs/orders.jar
    coverage:
      runtime: jvm
packs:
  coverage-core:
    minimum: {lines: 80, branches: 70}
    parts:
      orders: {minimum: {lines: 85}}
` + "```"

// CoverageRuntimes are the runtimes the registered packs measure, and
// report.
func (pack) CoverageRuntimes() []string {
	return slices.Sorted(slices.Values(append(runtimes(), reportRuntime{}.Name())))
}

// LaunchCoverage readies a service of a runtime as it starts.
func (pack) LaunchCoverage(ctx context.Context, settings func(pack string, v any) error, s core.CoverageService) ([]string, error) {
	r, ok := runtimeFor(s.Runtime)
	if !ok {
		return nil, nil
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	vars, err := r.Launch(ctx, LaunchCall{Service: s, Settings: settings})
	if err != nil {
		return nil, err
	}
	if s.Mount != "" {
		if err := writableByAll(s.Dir); err != nil {
			return nil, err
		}
	}
	return vars, nil
}

// writableByAll makes a container's coverage folder, and the folders its
// runtime made in it, writable by every user: the folder is this machine's,
// mounted into the container, whose processes often run as a user of its own
// (distroless's nonroot, say), which may write only into a folder that lets
// every user. The files they write stay theirs, in folders that are this
// user's, which it can remove.
func writableByAll(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return os.Chmod(p, 0o777) //nolint:gosec // the container's user writes its coverage here
	})
}

// runtimeFor is a runtime by its name, report included.
func runtimeFor(name string) (Runtime, bool) {
	if name == (reportRuntime{}).Name() {
		return reportRuntime{}, true
	}
	return runtimeOf(name)
}

// KeepsCoverage reports whether a runtime's services can be read while they
// run (Keeper).
func (pack) KeepsCoverage(runtime string) bool {
	r, ok := runtimeFor(runtime)
	if !ok {
		return false
	}
	_, keeps := r.(Keeper)
	return keeps
}

// ResetCoverage starts a kept service's counts afresh.
func (p pack) ResetCoverage(ctx context.Context, settings func(pack string, v any) error, s core.CoverageService) error {
	k, err := keeper(s)
	if err != nil {
		return err
	}
	return k.Reset(ctx, LaunchCall{Service: s, Settings: settings})
}

// TakeCoverage writes what ran of a kept service since it was reset into
// into.
func (p pack) TakeCoverage(ctx context.Context, settings func(pack string, v any) error, s core.CoverageService, into string) error {
	k, err := keeper(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(into, 0o755); err != nil {
		return err
	}
	return k.Take(ctx, LaunchCall{Service: s, Settings: settings}, into)
}

func keeper(s core.CoverageService) (Keeper, error) {
	r, ok := runtimeFor(s.Runtime)
	if !ok {
		return nil, fmt.Errorf("service %s: no coverage pack measures its runtime, %s", s.Name, s.Runtime)
	}
	k, ok := r.(Keeper)
	if !ok {
		return nil, fmt.Errorf("service %s: %s is read only when it exits", s.Name, s.Runtime)
	}
	return k, nil
}
