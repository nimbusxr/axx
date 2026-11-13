package core

import (
	"context"
	"fmt"
	"strings"
)

// Coverage is what a run with coverage (axx run --coverage) measured of the
// system under test, and how it compares with the minimums axx.yaml sets: the
// outcome a coverage pack concludes once the services have stopped, which the
// run's result carries and its exit code follows (6 when it is under a
// minimum and every scenario passed).
type Coverage struct {
	// Report is the run's merged lcov report, relative to the project.
	Report string
	// Check is how the run compares with the minimums.
	Check CoverageCheck
	// Totals are the whole run's.
	Totals CoverageTotals
	// Parts are what was measured, one each: a service, or a web app.
	Parts []CoveragePart
	// Unmet are the minimums the run is under, when Check is CoverageUnder.
	Unmet []CoverageUnmet
	// Skipped says why the minimums do not apply, when Check is
	// CoverageSkipped: the run ran part of the scenarios, or one shard.
	Skipped string
}

// Summary is what a person reads of the coverage: the run's totals and its
// report, each part's, and why the minimums do not apply, when they do not.
func (c *Coverage) Summary() []string {
	lines := []string{fmt.Sprintf("coverage: %s, in %s", c.Totals.describe(), c.Report)}
	for _, p := range c.Parts {
		if p.Missing != "" {
			lines = append(lines, fmt.Sprintf("  %s (%s): no coverage: %s", p.Name, p.Runtime, p.Missing))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s (%s): %s", p.Name, p.Runtime, p.Totals.describe()))
	}
	if c.Check == CoverageSkipped {
		lines = append(lines, "the coverage minimums apply to a run of every scenario, and this run is not one: "+c.Skipped)
	}
	return lines
}

// Under says which minimums the coverage is under, when it is: "the run's
// lines 71.5% (minimum 80%); orders branches 60% (minimum 70%)".
func (c *Coverage) Under() string {
	var under []string
	for _, u := range c.Unmet {
		name := u.Part
		if name == "" {
			name = "the run's"
		}
		under = append(under, fmt.Sprintf("%s %s %s%% (minimum %s%%)", name, u.Metric, Percent(u.Actual), Percent(u.Minimum)))
	}
	return strings.Join(under, "; ")
}

// Percent writes a percentage with at most two decimals: 71.5, 80.
func Percent(f float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
}

func (t CoverageTotals) describe() string {
	var parts []string
	for _, m := range []struct {
		name string
		t    *CoverageTotal
	}{{"lines", t.Lines}, {"branches", t.Branches}, {"functions", t.Functions}} {
		if m.t != nil {
			parts = append(parts, fmt.Sprintf("%s %s%%", m.name, Percent(m.t.Pct)))
		}
	}
	if len(parts) == 0 {
		return "nothing measured"
	}
	return strings.Join(parts, ", ")
}

// CoverageCheck is how a run compares with the minimums axx.yaml sets.
type CoverageCheck string

const (
	// CoverageUnchecked: axx.yaml sets no minimum.
	CoverageUnchecked CoverageCheck = ""
	// CoverageMet: every minimum is met.
	CoverageMet CoverageCheck = "met"
	// CoverageUnder: the run is under a minimum.
	CoverageUnder CoverageCheck = "unmet"
	// CoverageSkipped: the run ran part of the scenarios (paths, tags, a
	// shard, fail-fast...), whose coverage the minimums do not apply to.
	CoverageSkipped CoverageCheck = "skipped"
)

// CoverageTotals are the totals of what was measured, by metric; nil is a
// metric the runtime does not measure (Go measures no branches).
type CoverageTotals struct {
	Lines     *CoverageTotal
	Branches  *CoverageTotal
	Functions *CoverageTotal
}

// CoverageTotal is how many of a metric's items ran, of how many.
type CoverageTotal struct {
	Covered int
	Total   int
	// Pct is the percentage covered, rounded to two decimals as Istanbul
	// rounds it: 100 when there is nothing to cover.
	Pct float64
}

// CoveragePart is one measured part of the system under test.
type CoveragePart struct {
	// Name is the service's name in axx.yaml, or the web app's.
	Name string
	// Runtime is what measured it: jvm, go, python, node, report, web.
	Runtime string
	Totals  CoverageTotals
	// Missing says why the part has no coverage, when it has none: a
	// service killed before it could write it, say.
	Missing string
}

// CoverageUnmet is a minimum the run is under.
type CoverageUnmet struct {
	// Part is the service or web app the minimum is for, or "" for the
	// whole run.
	Part string
	// Metric is lines, branches or functions.
	Metric  string
	Minimum float64
	Actual  float64
}

// CoverageService is a service a run measures, as the runtime that measures
// it sees it.
type CoverageService struct {
	// Name is the service's name in axx.yaml; Runtime, its coverage
	// section's runtime.
	Name    string
	Runtime string
	// Dir is the service's coverage folder on this machine, which
	// AXX_COVERAGE_DIR names; Mount, the same folder inside its container
	// (its coverage section's mount), "" for a service on this machine.
	Dir   string
	Mount string
	// WorkDir is the service's working directory (its dir), where its
	// sources are unless the runtime's settings say otherwise.
	WorkDir string
	// Kept is a service axx up keeps running across runs: each run resets
	// its coverage as it starts and takes it as it ends, for the runtimes
	// that can (CoverageKeeper).
	Kept bool
}

// CoverageEnd is how a service the run measured, or was to measure, ended:
// what its runtime needs to read what it wrote, and to say why it wrote
// nothing.
type CoverageEnd struct {
	CoverageService
	// Status describes how its main process ended ("exited with code 0");
	// Killed is set when axx killed it after its grace, before it could
	// write what it writes as it exits; OnItsOwn, when it exited before axx
	// stopped it.
	Status   string
	Killed   bool
	OnItsOwn bool
	// Unmeasured says why a service the run was to measure was not: it ran
	// in debug mode, say.
	Unmeasured string
}

// CoverageRun is what a run with coverage hands the pack that concludes it,
// once its services have stopped.
type CoverageRun struct {
	// Services are the services the run measured, or was to measure.
	Services []CoverageEnd
	// Partial says why the run is not a run of every scenario its
	// configuration selects ("" when it is): the minimums apply to whole
	// runs only.
	Partial string
	// Shard is the share of a run the run is ("2/3"), "" for a whole one;
	// Selected identifies the scenarios the run's configuration selects,
	// before sharding, so the shards of one run can be told from another's.
	Shard    string
	Selected string
}

// CoverageLauncher is implemented by the pack that measures the services'
// coverage (coverage-core): it knows the runtimes the project's coverage
// packs measure, and readies a measured service as it starts. The axx host
// calls it too, for the services it starts, so it has no Suite: settings
// decodes a pack's settings from axx.yaml (Suite.PackConfig).
type CoverageLauncher interface {
	// CoverageRuntimes are the runtimes a service's coverage section can
	// name.
	CoverageRuntimes() []string
	// LaunchCoverage readies s's coverage folder (an agent, a preload) and
	// returns the variables s gets, NAME=value.
	LaunchCoverage(ctx context.Context, settings func(pack string, v any) error, s CoverageService) ([]string, error)
}

// Concluder is implemented by the pack that concludes a run's coverage
// (coverage-core): once the services have stopped, and before the run's
// result is written, it reads what the services wrote, adds what packs
// measured during the run (web-coverage), writes the run's reports, and
// compares them with the minimums axx.yaml sets.
type Concluder interface {
	Conclude(ctx context.Context, s *Suite, run CoverageRun) (*Coverage, error)
}

// CoverageKeeper is implemented by the pack that measures the services axx
// up keeps (coverage-core): a run resets their coverage as it starts and
// takes it as it ends, while they keep running, for the runtimes that can
// be read so (JVM, Node); the others write theirs when axx down stops them.
type CoverageKeeper interface {
	// KeepsCoverage reports whether a runtime's services can be read while
	// they run.
	KeepsCoverage(runtime string) bool
	// ResetCoverage starts s's counts afresh.
	ResetCoverage(ctx context.Context, settings func(pack string, v any) error, s CoverageService) error
	// TakeCoverage writes what ran of s since it was reset into the folder
	// into, which the run concludes from as from a stopped service's.
	TakeCoverage(ctx context.Context, settings func(pack string, v any) error, s CoverageService, into string) error
}
