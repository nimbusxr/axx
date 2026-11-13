package cli

import (
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/runner"
)

// A run under a coverage minimum exits 6 only when every scenario passed: a
// failing scenario, an undefined step or an interruption says more.
func TestRunExitCodeOfCoverage(t *testing.T) {
	under := &core.Coverage{Check: core.CoverageUnder}
	for name, c := range map[string]struct {
		res  runner.RunResult
		want exitcode.Code
	}{
		"passed, under a minimum":   {runner.RunResult{Coverage: under}, exitcode.Coverage},
		"passed, minimums met":      {runner.RunResult{Coverage: &core.Coverage{Check: core.CoverageMet}}, exitcode.OK},
		"passed, a partial run":     {runner.RunResult{Coverage: &core.Coverage{Check: core.CoverageSkipped}}, exitcode.OK},
		"passed, no coverage":       {runner.RunResult{}, exitcode.OK},
		"failed, under a minimum":   {runner.RunResult{Coverage: under, Scenarios: []*runner.ScenarioResult{{Status: runner.Failed}}}, exitcode.Failed},
		"undefined, under":          {runner.RunResult{Coverage: under, Scenarios: []*runner.ScenarioResult{{Status: runner.Undefined}}}, exitcode.Undefined},
		"interrupted, under":        {runner.RunResult{Coverage: under, Interrupted: true}, exitcode.Interrupted},
		"a pack's run error, under": {runner.RunResult{Coverage: under, RunErrors: []runner.RunError{{Source: "mock", Message: "x"}}}, exitcode.Failed},
	} {
		if got := runExitCode(&c.res); got != c.want {
			t.Errorf("%s: exit %d, want %d", name, got, c.want)
		}
	}
	if exitcode.Coverage != 6 || exitcode.Coverage.String() != "coverage" {
		t.Errorf("exit code 6 is %d %q", exitcode.Coverage, exitcode.Coverage)
	}
}
