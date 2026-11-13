package core

import (
	"slices"
	"testing"
)

// What a person reads of a run's coverage: its totals, each part's (or why
// it has none), why the minimums do not apply, and which it is under.
func TestCoverageSummary(t *testing.T) {
	c := &Coverage{
		Report: ".axx/coverage/lcov.info",
		Check:  CoverageSkipped, Skipped: "it runs the paths it names",
		Totals: CoverageTotals{Lines: &CoverageTotal{Covered: 143, Total: 200, Pct: 71.5}, Functions: &CoverageTotal{Covered: 8, Total: 10, Pct: 80}},
		Parts: []CoveragePart{
			{Name: "orders", Runtime: "jvm", Totals: CoverageTotals{Lines: &CoverageTotal{Pct: 66.67}, Branches: &CoverageTotal{Pct: 50}}},
			{Name: "batch", Runtime: "go", Missing: "it was killed after its grace (stop.grace)"},
		},
	}
	want := []string{
		"coverage: lines 71.5%, functions 80%, in .axx/coverage/lcov.info",
		"  orders (jvm): lines 66.67%, branches 50%",
		"  batch (go): no coverage: it was killed after its grace (stop.grace)",
		"the coverage minimums apply to a run of every scenario, and this run is not one: it runs the paths it names",
	}
	if got := c.Summary(); !slices.Equal(got, want) {
		t.Errorf("summary:\n%q\nwant\n%q", got, want)
	}
	c.Check, c.Unmet = CoverageUnder, []CoverageUnmet{{Metric: "lines", Minimum: 80, Actual: 71.5}, {Part: "orders", Metric: "branches", Minimum: 70, Actual: 50}}
	if got, want := c.Under(), "the run's lines 71.5% (minimum 80%); orders branches 50% (minimum 70%)"; got != want {
		t.Errorf("under: %q, want %q", got, want)
	}
}
