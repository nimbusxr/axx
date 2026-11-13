package core

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
