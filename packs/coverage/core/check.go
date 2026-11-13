package coveragecore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

func coverageCommand() core.Command {
	return core.Command{
		Name:     "coverage",
		Short:    "The coverage of the system under test, as runs with coverage measured it",
		Commands: []core.Command{checkCommand()},
	}
}

func checkCommand() core.Command {
	return core.Command{
		Name: "check", Args: "[folder...]", MinArgs: 0, MaxArgs: -1,
		Short: "Merge the coverage of a run's shards and check it against the minimums",
		Long: `Read the coverage that runs with coverage wrote (run.json, in the folder each
wrote its reports to), merge it, write the merged reports, and check them against
the minimums axx.yaml sets (packs.coverage-core): exit 6 under one.

The runs are the shards of one run (axx run --coverage --shard i/n), each once, or
one whole run: every shard of the same scenarios, written by the same axx. A
shard that ran part of its share (--tags, a path) is refused. Without folders, it
reads the project's (packs.coverage-core.folder, default .axx/coverage).`,
		Flags: []core.Flag{{Name: "out", Usage: "where the merged reports go (default: packs.coverage-core.folder)"}},
		Run:   runCheck,
	}
}

// checkResult is what axx coverage check reports.
type checkResult struct {
	Report string     `json:"report"`
	Check  string     `json:"check,omitempty"`
	Totals jsonTotals `json:"totals"`
	Parts  []jsonPart `json:"parts"`
	Unmet  []jsonMiss `json:"unmet,omitempty"`
}

type jsonTotals struct {
	Lines     *jsonTotal `json:"lines,omitempty"`
	Branches  *jsonTotal `json:"branches,omitempty"`
	Functions *jsonTotal `json:"functions,omitempty"`
}

type jsonTotal struct {
	Covered int     `json:"covered"`
	Total   int     `json:"total"`
	Pct     float64 `json:"pct"`
}

type jsonPart struct {
	Name    string     `json:"name"`
	Runtime string     `json:"runtime"`
	Totals  jsonTotals `json:"totals"`
	Missing string     `json:"missing,omitempty"`
}

type jsonMiss struct {
	Part    string  `json:"part,omitempty"`
	Metric  string  `json:"metric"`
	Minimum float64 `json:"minimum"`
	Actual  float64 `json:"actual"`
}

func runCheck(call *core.CommandCall) error {
	cfg, err := readConfig(func(_ string, v any) error { return call.Settings(v) })
	if err != nil {
		return err
	}
	project := call.ProjectDir
	if project == "" {
		project = "."
	}
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(project, p)
	}
	folders := call.Args
	if len(folders) == 0 {
		folders = []string{cfg.Folder}
	}
	var runs []runData
	var from []string
	for _, f := range folders {
		p := abs(f)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			p = filepath.Join(p, "run.json")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return axxerr.New(CodeShards, exitcode.Usage, "cannot read %s: %v", p, err).
				WithHint("name the folders runs with coverage wrote their reports to, each with its run.json")
		}
		var d runData
		if err := json.Unmarshal(b, &d); err != nil || d.Version != dataVersion {
			return axxerr.New(CodeShards, exitcode.Usage, "%s is no run.json of this axx (version %d): %v", p, d.Version, err)
		}
		runs, from = append(runs, d), append(from, p)
	}
	data, err := mergeShards(runs, from)
	if err != nil {
		return err
	}
	out := cfg.Folder
	if o := call.Flags["out"]; o != "" {
		out = o
	}
	out = abs(out)
	if err := write(out, data); err != nil {
		return err
	}
	cov, err := evaluate(cfg, data)
	if err != nil {
		return err
	}
	cov.Report = filepath.ToSlash(rel(project, filepath.Join(out, "lcov.info")))
	if missing := missingParts(cfg, data, cov); missing != "" {
		return axxerr.New(CodeMissing, exitcode.Environment, "the minimums apply to the shards together, and %s", missing)
	}
	res := checkResult{Report: cov.Report, Check: string(cov.Check), Totals: jsonTotalsOf(cov.Totals), Parts: []jsonPart{}}
	for _, p := range cov.Parts {
		res.Parts = append(res.Parts, jsonPart{Name: p.Name, Runtime: p.Runtime, Totals: jsonTotalsOf(p.Totals), Missing: p.Missing})
	}
	for _, u := range cov.Unmet {
		res.Unmet = append(res.Unmet, jsonMiss(u))
	}
	if err := call.Emit(res, func(w io.Writer) error {
		fmt.Fprintf(w, "coverage: %s, in %s\n", describe(cov.Totals), cov.Report)
		for _, p := range cov.Parts {
			if p.Missing != "" {
				fmt.Fprintf(w, "  %s (%s): no coverage: %s\n", p.Name, p.Runtime, p.Missing)
				continue
			}
			fmt.Fprintf(w, "  %s (%s): %s\n", p.Name, p.Runtime, describe(p.Totals))
		}
		return nil
	}); err != nil {
		return err
	}
	if cov.Check == core.CoverageUnder {
		var under []string
		for _, u := range cov.Unmet {
			name := u.Part
			if name == "" {
				name = "the run's"
			}
			under = append(under, fmt.Sprintf("%s %s %s%% (minimum %s%%)", name, u.Metric, pct(u.Actual), pct(u.Minimum)))
		}
		return axxerr.New(CodeUnder, exitcode.Coverage, "the coverage is under the minimum: %s", strings.Join(under, "; ")).
			WithHint("scenarios for the code it shows no run reached, or lower minimums (packs.coverage-core)")
	}
	return nil
}

func jsonTotalsOf(t core.CoverageTotals) jsonTotals {
	one := func(t *core.CoverageTotal) *jsonTotal {
		if t == nil {
			return nil
		}
		return &jsonTotal{Covered: t.Covered, Total: t.Total, Pct: t.Pct}
	}
	return jsonTotals{Lines: one(t.Lines), Branches: one(t.Branches), Functions: one(t.Functions)}
}

// mergeShards merges the runs into one, when they are the shards of one
// run, each once, or one whole run.
func mergeShards(runs []runData, from []string) (runData, error) {
	refuse := func(format string, a ...any) error {
		return axxerr.New(CodeShards, exitcode.Usage, format, a...).
			WithHint("check the shards of one run together: those of axx run --coverage --shard i/n, for each i of the same n, each once")
	}
	first := runs[0]
	n := 0
	seen := map[int]string{}
	for i, r := range runs {
		switch {
		case r.Selected != first.Selected:
			return runData{}, refuse("%s and %s are of runs of different scenarios", from[0], from[i])
		case r.Axx != first.Axx:
			return runData{}, refuse("%s was written by axx %s, and %s by axx %s", from[0], first.Axx, from[i], r.Axx)
		case r.Partial != "":
			return runData{}, refuse("%s is of a run of part of its scenarios: %s", from[i], r.Partial)
		}
		if r.Shard == "" {
			if len(runs) > 1 {
				return runData{}, refuse("%s is a whole run, which no shard of another adds to", from[i])
			}
			return r, nil
		}
		k, of, ok := shard(r.Shard)
		if !ok || (n != 0 && of != n) {
			return runData{}, refuse("%s is the shard %s, which is no shard of the others' %d", from[i], r.Shard, n)
		}
		n = of
		if prev, dup := seen[k]; dup {
			return runData{}, refuse("%s and %s are both the shard %s", prev, from[i], r.Shard)
		}
		seen[k] = from[i]
	}
	if len(seen) != n {
		var missing []string
		for k := 1; k <= n; k++ {
			if _, ok := seen[k]; !ok {
				missing = append(missing, fmt.Sprintf("%d/%d", k, n))
			}
		}
		return runData{}, refuse("the shards %s are missing", strings.Join(missing, ", "))
	}
	out := runData{Version: dataVersion, Axx: first.Axx, Selected: first.Selected}
	byName := map[string]int{}
	for _, r := range runs {
		for _, p := range r.Parts {
			i, ok := byName[p.Name]
			if !ok {
				byName[p.Name] = len(out.Parts)
				p.Files = slices.Clone(p.Files)
				if p.Missing != "" {
					p.Missing = "in the shard " + r.Shard + ": " + p.Missing
				}
				out.Parts = append(out.Parts, p)
				continue
			}
			q := &out.Parts[i]
			q.Measured.Branches = q.Measured.Branches || p.Measured.Branches
			q.Measured.Functions = q.Measured.Functions || p.Measured.Functions
			q.Files = merged(append(q.Files, p.Files...))
			if p.Missing != "" && q.Missing == "" {
				q.Missing = "in the shard " + r.Shard + ": " + p.Missing
			}
		}
	}
	return out, nil
}

// shard reads "i/n".
func shard(s string) (i, n int, ok bool) {
	a, b, found := strings.Cut(s, "/")
	i, err1 := strconv.Atoi(a)
	n, err2 := strconv.Atoi(b)
	return i, n, found && err1 == nil && err2 == nil && i >= 1 && i <= n
}
