package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/report"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// coveredProject is a project whose service writes its own coverage (the
// report runtime) when cover is set: an lcov file of two lines of
// src/parcels.go, one of which ran. settings are coverage-core's.
func coveredProject(t *testing.T, cover bool, settings string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(helperEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	command := "[" + q(exe) + ", parcels, api, ready]"
	if cover {
		command = "[" + q(exe) + ", cover, lcov.info, " + q(`TN:\nSF:src/parcels.go\nDA:1,3\nDA:2,0\nend_of_record\n`) + ", parcels, api, ready]"
	}
	yaml := "services:\n  api:\n    command: " + command + "\n" +
		"    ready:\n      log: parcels api ready\n      timeout: 20s\n" +
		"    coverage:\n      runtime: report\n"
	if settings != "" {
		yaml += "packs:\n  coverage-core:\n" + settings
	}
	scenario := func(name string) string {
		return "  Scenario: " + name + "\n" +
			"    Given the console log with the following properties:\n      | url | file://.axx/logs/services.log |\n" +
			"    Then within 5s the console log has an entry matching \"parcels api serving\"\n\n"
	}
	writeFiles(t, dir, map[string]string{
		"axx.yaml":                 yaml,
		"axx-packs.yaml":           "packs: [logs, coverage-core]\n",
		"features/parcels.feature": "Feature: Parcels\n\n" + scenario("the first parcel") + scenario("the second parcel"),
	})
	return dir
}

// runReport runs axx with --json and reads the run report of its envelope.
func runReport(t *testing.T, args ...string) (report.AgentReport, bool, string, int) {
	t.Helper()
	stdout, stderr, code := run(t, append(args, "--json")...)
	var env struct {
		OK   bool               `json:"ok"`
		Data report.AgentReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("exit %d, no envelope (%v):\n%s\n%s", code, err, stdout, stderr)
	}
	return env.Data, env.OK, stderr, code
}

func TestARunWithCoverage(t *testing.T) {
	dir := coveredProject(t, true, "")
	rep, ok, stderr, code := runReport(t, "run", "--coverage")
	if code != int(exitcode.OK) || !ok {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	c := rep.Coverage
	if c == nil || c.Report != ".axx/coverage/lcov.info" || c.Check != "" || len(c.Parts) != 1 {
		t.Fatalf("coverage: %+v", c)
	}
	if p := c.Parts[0]; p.Name != "api" || p.Runtime != "report" || p.Totals.Lines == nil || p.Totals.Lines.Covered != 1 || p.Totals.Lines.Total != 2 || p.Totals.Lines.Pct != 50 {
		t.Errorf("the service's part: %+v %+v", p, p.Totals.Lines)
	}
	if !strings.Contains(stderr, "api (report): lines 50%") {
		t.Errorf("the run says its coverage:\n%s", stderr)
	}
	lcov, err := os.ReadFile(filepath.Join(dir, ".axx", "coverage", "lcov.info"))
	if err != nil || !strings.Contains(string(lcov), "SF:src/parcels.go\n") {
		t.Errorf("the run's lcov names the project's file: %s %v", lcov, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "coverage", "run.json")); err != nil {
		t.Error(err)
	}

	// Without --coverage, the service runs unmeasured, and the report has
	// no coverage.
	if rep, _, stderr, code := runReport(t, "run"); code != int(exitcode.OK) || rep.Coverage != nil {
		t.Errorf("a run without coverage: exit %d, %+v\n%s", code, rep.Coverage, stderr)
	}
}

// Under a minimum, a run whose scenarios passed exits 6, its result passed;
// a run of part of the scenarios checks no minimum.
func TestARunUnderACoverageMinimum(t *testing.T) {
	coveredProject(t, true, "    minimum: {lines: 80}\n    parts:\n      api: {minimum: {lines: 40}}\n")
	rep, ok, stderr, code := runReport(t, "run", "--coverage")
	if code != int(exitcode.Coverage) || ok || rep.Result != "passed" {
		t.Fatalf("exit %d, ok %v, result %s:\n%s", code, ok, rep.Result, stderr)
	}
	if c := rep.Coverage; c.Check != "unmet" || len(c.Unmet) != 1 || c.Unmet[0].Part != "" || c.Unmet[0].Metric != "lines" || c.Unmet[0].Minimum != 80 || c.Unmet[0].Actual != 50 {
		t.Errorf("coverage: %+v", c)
	}
	if !strings.Contains(stderr, "coverage under the minimum: the run's lines 50% (minimum 80%)") {
		t.Errorf("the run says so:\n%s", stderr)
	}

	rep, _, stderr, code = runReport(t, "run", "--coverage", "features/parcels.feature:3")
	if code != int(exitcode.OK) || rep.Coverage.Check != "skipped" || !strings.Contains(stderr, "it runs the paths it names") {
		t.Errorf("a run of one scenario: exit %d, %+v\n%s", code, rep.Coverage, stderr)
	}
}

// A measured service that wrote nothing is no 0%: where the minimums
// apply, the run says why, and exits 4.
func TestAServiceThatWroteNoCoverage(t *testing.T) {
	coveredProject(t, false, "    minimum: {lines: 10}\n")
	_, stderr, code := run(t, "run", "--coverage")
	if code != int(exitcode.Environment) || !strings.Contains(stderr, "AXX-E1002") || !strings.Contains(stderr, "api (report) has no coverage") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
}

// axx coverage check merges the shards of one run and checks them together.
func TestCoverageCheckMergesTheShards(t *testing.T) {
	dir := coveredProject(t, true, "    minimum: {lines: 80}\n")
	for _, shard := range []string{"1/2", "2/2"} {
		folder := "shard-" + strings.Replace(shard, "/", "-of-", 1)
		rep, _, stderr, code := runReport(t, "run", "--coverage", "--shard", shard, "--set", "packs.coverage-core.folder="+folder)
		if code != int(exitcode.OK) || rep.Coverage.Check != "skipped" {
			t.Fatalf("shard %s: exit %d, %+v\n%s", shard, code, rep.Coverage, stderr)
		}
	}
	_, stderr, code := run(t, "coverage", "check", "shard-1-of-2", "shard-2-of-2")
	if code != int(exitcode.Coverage) || !strings.Contains(stderr, "AXX-E1006") || !strings.Contains(stderr, "the run's lines 50% (minimum 80%)") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "coverage", "lcov.info")); err != nil {
		t.Errorf("the merged report: %v", err)
	}
	for name, folders := range map[string][]string{
		"a shard missing": {"shard-1-of-2"},
		"a shard twice":   {"shard-1-of-2", "shard-1-of-2", "shard-2-of-2"},
	} {
		if _, stderr, code := run(t, append([]string{"coverage", "check"}, folders...)...); code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E1005") {
			t.Errorf("%s: exit %d:\n%s", name, code, stderr)
		}
	}
}

// unmatchedRuntime is a runtime whose services write coverage of none of
// the project's files, as a Python service's coverage.py does whose files
// paths map nowhere.
type unmatchedRuntime struct{}

func init() { coveragecore.Register(unmatchedRuntime{}) }

func (unmatchedRuntime) Name() string                { return "unmatched-test" }
func (unmatchedRuntime) Measured() istanbul.Measured { return istanbul.Measured{Functions: true} }
func (unmatchedRuntime) Launch(context.Context, coveragecore.LaunchCall) ([]string, error) {
	return nil, nil
}

func (unmatchedRuntime) Convert(context.Context, coveragecore.ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	return nil, istanbul.Measured{Functions: true}, &coveragecore.Unmatched{Why: "it measured 3 files, like /app/api.py, and none of them is in the project"}
}

// A service whose coverage names none of the project's files is no error
// and no 0%: the run says why, of that service.
func TestAServiceWhoseCoverageIsOfNoFileOfTheProject(t *testing.T) {
	coveredProject(t, false, "")
	b, err := os.ReadFile("axx.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, ".", map[string]string{"axx.yaml": strings.Replace(string(b), "runtime: report", "runtime: unmatched-test", 1)})
	_, stderr, code := run(t, "run", "--coverage")
	if code != int(exitcode.OK) || !strings.Contains(stderr, "api (unmatched-test)") || !strings.Contains(stderr, "none of them is in the project") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
}
