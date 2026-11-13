package coveragecore

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/version"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// The pack's error codes.
const (
	// CodeMissing: a part the minimums apply to has no coverage.
	CodeMissing = "AXX-E1002"
	// CodeConvert: what a service wrote cannot be read.
	CodeConvert = "AXX-E1003"
	// CodeSettings: a minimum the runtime cannot measure, or of no part.
	CodeSettings = "AXX-E1004"
	// CodeShards: axx coverage check's runs are no shards of one run.
	CodeShards = "AXX-E1005"
	// CodeUnder: axx coverage check finds the coverage under a minimum.
	CodeUnder = "AXX-E1006"
)

// dataVersion is run.json's version.
const dataVersion = 1

// runData is what a run with coverage measured (run.json): the parts it
// measured, and which run it is, for axx coverage check to merge the shards
// of one run.
type runData struct {
	Version int    `json:"version"`
	Axx     string `json:"axx"`
	// Shard is "2/3" for a shard of a run, "" for a whole one; Selected
	// identifies the scenarios the run selects before sharding; Partial,
	// why it ran part of them otherwise.
	Shard    string `json:"shard,omitempty"`
	Selected string `json:"selected"`
	Partial  string `json:"partial,omitempty"`
	Parts    []part `json:"parts"`
}

// part is a measured part of the system under test: a service, or what a
// pack measured during the run (web).
type part struct {
	Name     string            `json:"name"`
	Runtime  string            `json:"runtime"`
	Measured istanbul.Measured `json:"measured"`
	// Missing says why the part has no coverage.
	Missing string `json:"missing,omitempty"`
	// Files' paths are relative to the project, slash-separated.
	Files []*istanbul.FileCoverage `json:"files"`
}

// Part is what a pack measured during a run, for the run's coverage:
// web-coverage's, of the web apps' JavaScript. Its files' paths are
// relative to the project, slash-separated.
type Part struct {
	Name     string
	Runtime  string
	Measured istanbul.Measured
	Files    []*istanbul.FileCoverage
}

type concluding struct {
	mu  sync.Mutex
	fns []func(context.Context) (Part, error)
}

// OnConclude has fn hand a pack's part of the coverage when a run with
// coverage concludes, once its services have stopped.
func OnConclude(s *core.Suite, fn func(context.Context) (Part, error)) {
	c, _ := core.Cached(s, Name+"/concluding", func() (*concluding, error) { return &concluding{}, nil })
	c.mu.Lock()
	c.fns = append(c.fns, fn)
	c.mu.Unlock()
}

// Conclude reads what the services wrote and the packs measured, writes the
// run's reports and compares them with the minimums.
func (pack) Conclude(ctx context.Context, s *core.Suite, run core.CoverageRun) (*core.Coverage, error) {
	cfg, err := readConfig(s.PackConfig)
	if err != nil {
		return nil, err
	}
	project := s.ProjectDir()
	data := runData{Version: dataVersion, Axx: version.Get().Version, Shard: run.Shard, Selected: run.Selected, Partial: run.Partial}
	runDirs := map[string]bool{}
	for _, end := range run.Services {
		pt := part{Name: end.Name, Runtime: end.Runtime}
		r, ok := runtimeFor(end.Runtime)
		switch {
		case !ok:
			pt.Missing = "no coverage pack of the project measures its runtime, " + end.Runtime
		case end.Unmeasured != "":
			pt.Missing, pt.Measured = end.Unmeasured, r.Measured()
		default:
			runDirs[filepath.Dir(end.Dir)] = true
			files, m, err := r.Convert(ctx, ConvertCall{
				End: end, Settings: s.PackConfig, Logger: s.Logger(), Project: project,
				Path: func(p string) string { return cfg.pathOf(project, end.CoverageService, p) },
			})
			if err != nil {
				return nil, axxerr.Wrap(err, CodeConvert, exitcode.Environment, "cannot read the coverage service %s wrote, in %s", end.Name, end.Dir)
			}
			pt.Measured, pt.Files = m, relative(project, files)
			if len(pt.Files) == 0 {
				pt.Missing = silence(end)
			}
		}
		data.Parts = append(data.Parts, pt)
	}
	if c, err := core.Cached(s, Name+"/concluding", func() (*concluding, error) { return &concluding{}, nil }); err == nil {
		c.mu.Lock()
		fns := slices.Clone(c.fns)
		c.mu.Unlock()
		for _, fn := range fns {
			p, err := fn(ctx)
			if err != nil {
				return nil, err
			}
			if len(p.Files) == 0 {
				continue // the pack measured nothing: no page in Chromium, say
			}
			files := make([]*istanbul.FileCoverage, len(p.Files))
			for i, fc := range p.Files {
				files[i] = fc.Clone()
			}
			data.Parts = append(data.Parts, part{Name: p.Name, Runtime: p.Runtime, Measured: p.Measured, Files: merged(files)})
		}
	}

	folder := cfg.Folder
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(project, folder)
	}
	if err := write(folder, data); err != nil {
		return nil, err
	}
	keepOnly(filepath.Join(project, ".axx", "coverage", "raw"), runDirs)
	cov, err := evaluate(cfg, data)
	if err != nil {
		return nil, err
	}
	cov.Report = filepath.ToSlash(rel(project, filepath.Join(folder, "lcov.info")))
	s.Announce("coverage", "path", filepath.Join(folder, "lcov.info"))
	if missing := missingParts(cfg, data, cov); missing != "" {
		return cov, axxerr.New(CodeMissing, exitcode.Environment, "the minimums apply to this run, and %s", missing).
			WithHint("a service writes its coverage as it exits: give it the grace to (stop.grace), and check its coverage section (a -cover build for Go, the command under coverage.py)")
	}
	return cov, nil
}

// silence says why a measured service wrote no coverage, from how it ended.
func silence(end core.CoverageEnd) string {
	switch {
	case end.NoStopSignal:
		return "axx could give it no stop signal, and ended it at once, before it wrote its coverage (" + end.Status + "): Windows has none for a process. Run it in a container, which Docker stops with one, to measure it"
	case end.Killed:
		return "axx killed it after its stop grace, before it wrote its coverage (" + end.Status + "): give it more (stop.grace), or make it exit on " + "its stop signal"
	case end.OnItsOwn:
		return "it exited on its own (" + end.Status + ") and wrote no coverage"
	case end.Status == "":
		return "it wrote no coverage"
	}
	return "it wrote no coverage into its folder (" + end.Status + "): check how its coverage section runs it"
}

// relative names files by their paths relative to the project.
func relative(project string, files []*istanbul.FileCoverage) []*istanbul.FileCoverage {
	for _, fc := range files {
		fc.Path = filepath.ToSlash(rel(project, fc.Path))
	}
	return merged(files)
}

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}

// merged is files with those of one path merged, by path.
func merged(files []*istanbul.FileCoverage) []*istanbul.FileCoverage {
	byPath := map[string]*istanbul.FileCoverage{}
	for _, fc := range files {
		if prev, ok := byPath[fc.Path]; ok {
			prev.Merge(fc)
			continue
		}
		byPath[fc.Path] = fc
	}
	out := make([]*istanbul.FileCoverage, 0, len(byPath))
	for _, p := range slices.Sorted(maps.Keys(byPath)) {
		out = append(out, byPath[p])
	}
	return out
}

// write writes a run's reports into folder: lcov.info, Istanbul's JSON and
// run.json.
func write(folder string, data runData) error {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	var all []*istanbul.FileCoverage
	for _, p := range data.Parts {
		for _, fc := range p.Files {
			all = append(all, fc.Clone())
		}
	}
	all = merged(all)
	b, err := json.MarshalIndent(data, "", " ")
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{
		"lcov.info":             istanbul.Lcov(all),
		"coverage-final.json":   istanbul.FinalJSON(all),
		"coverage-summary.json": istanbul.SummaryJSON(all),
		"run.json":              append(b, '\n'),
	} {
		if err := os.WriteFile(filepath.Join(folder, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// keepOnly removes the raw folders of earlier runs, keeping the runs' own
// (keep) and the one of services no run measures. What a container wrote as
// root may stay.
func keepOnly(raw string, keep map[string]bool) {
	entries, err := os.ReadDir(raw)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(raw, e.Name())
		if e.Name() == "unmeasured" || keep[p] {
			continue
		}
		_ = os.RemoveAll(p)
	}
}

// evaluate is the coverage of data, compared with cfg's minimums.
func evaluate(cfg config, data runData) (*core.Coverage, error) {
	cov := &core.Coverage{Parts: []core.CoveragePart{}}
	byName := map[string]part{}
	var lines, branches, functions []*istanbul.FileCoverage
	for _, p := range data.Parts {
		byName[p.Name] = p
		cp := core.CoveragePart{Name: p.Name, Runtime: p.Runtime, Missing: p.Missing}
		if p.Missing == "" {
			cp.Totals = totals(p.Files, p.Measured)
		}
		cov.Parts = append(cov.Parts, cp)
		lines = append(lines, p.Files...)
		if p.Measured.Branches {
			branches = append(branches, p.Files...)
		}
		if p.Measured.Functions {
			functions = append(functions, p.Files...)
		}
	}
	whole := istanbul.Total(copies(lines))
	cov.Totals.Lines = total(whole.Lines)
	if len(branches) > 0 {
		cov.Totals.Branches = total(istanbul.Total(copies(branches)).Branches)
	}
	if len(functions) > 0 {
		cov.Totals.Functions = total(istanbul.Total(copies(functions)).Functions)
	}

	// Minimums the parts cannot meet are the configuration's mistake,
	// whatever the run.
	if err := checkSettings(cfg, data, byName); err != nil {
		return nil, err
	}
	switch {
	case !cfg.minimums():
		cov.Check = core.CoverageUnchecked
		return cov, nil
	case data.Partial != "" || data.Shard != "":
		cov.Check, cov.Skipped = core.CoverageSkipped, data.Partial
		if cov.Skipped == "" {
			cov.Skipped = "it is the shard " + data.Shard + " of a run: `axx coverage check` checks the shards together"
		}
		return cov, nil
	}
	under := func(partName, metric string, minimum *float64, t *core.CoverageTotal) {
		if minimum != nil && t != nil && t.Pct < *minimum {
			cov.Unmet = append(cov.Unmet, core.CoverageUnmet{Part: partName, Metric: metric, Minimum: *minimum, Actual: t.Pct})
		}
	}
	check := func(partName string, m minimum, t core.CoverageTotals) {
		under(partName, "lines", m.Lines, t.Lines)
		under(partName, "branches", m.Branches, t.Branches)
		under(partName, "functions", m.Functions, t.Functions)
	}
	check("", cfg.Minimum, cov.Totals)
	for _, cp := range cov.Parts {
		check(cp.Name, cfg.Parts[cp.Name].Minimum, cp.Totals)
	}
	cov.Check = core.CoverageMet
	if len(cov.Unmet) > 0 {
		cov.Check = core.CoverageUnder
	}
	return cov, nil
}

// checkSettings refuses minimums of a metric a part does not measure, or of
// a part that is none.
func checkSettings(cfg config, data runData, byName map[string]part) error {
	measures := func(metric string, m istanbul.Measured) bool {
		return metric == "lines" || metric == "branches" && m.Branches || metric == "functions" && m.Functions
	}
	set := func(m minimum) []string {
		var out []string
		for metric, v := range map[string]*float64{"lines": m.Lines, "branches": m.Branches, "functions": m.Functions} {
			if v != nil {
				out = append(out, metric)
			}
		}
		slices.Sort(out)
		return out
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Parts)) {
		p, ok := byName[name]
		if !ok {
			continue // missingParts says so, where the minimums apply
		}
		for _, metric := range set(cfg.Parts[name].Minimum) {
			if !measures(metric, p.Measured) {
				return axxerr.New(CodeSettings, exitcode.Usage, "packs.coverage-core.parts.%s.minimum.%s: %s (%s) measures no %s", name, metric, name, p.Runtime, metric).
					WithHint("set minimums of what its runtime measures: lines, for every runtime")
			}
		}
	}
	for _, metric := range set(cfg.Minimum) {
		if !slices.ContainsFunc(data.Parts, func(p part) bool { return measures(metric, p.Measured) }) {
			return axxerr.New(CodeSettings, exitcode.Usage, "packs.coverage-core.minimum.%s: no part the run measures has %s", metric, metric).
				WithHint("set minimums of what the parts' runtimes measure")
		}
	}
	return nil
}

// missingParts says which parts the minimums apply to have no coverage, ""
// when none or when they do not apply.
func missingParts(cfg config, data runData, cov *core.Coverage) string {
	if cov.Check != core.CoverageMet && cov.Check != core.CoverageUnder {
		return ""
	}
	var out []string
	measured := map[string]bool{}
	for _, p := range data.Parts {
		measured[p.Name] = true
		if p.Missing != "" {
			out = append(out, fmt.Sprintf("%s (%s) has no coverage: %s", p.Name, p.Runtime, p.Missing))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Parts)) {
		if !measured[name] && cfg.Parts[name].Minimum.set() {
			out = append(out, fmt.Sprintf("%s, which packs.coverage-core.parts sets minimums of, was not measured", name))
		}
	}
	return strings.Join(out, "; ")
}

func copies(files []*istanbul.FileCoverage) []*istanbul.FileCoverage {
	out := make([]*istanbul.FileCoverage, len(files))
	for i, fc := range files {
		out[i] = fc.Clone()
	}
	return merged(out)
}

func totals(files []*istanbul.FileCoverage, m istanbul.Measured) core.CoverageTotals {
	s := istanbul.Total(copies(files))
	t := core.CoverageTotals{Lines: total(s.Lines)}
	if m.Branches {
		t.Branches = total(s.Branches)
	}
	if m.Functions {
		t.Functions = total(s.Functions)
	}
	return t
}

func total(t istanbul.Totals) *core.CoverageTotal {
	out := &core.CoverageTotal{Covered: t.Covered, Total: t.Total, Pct: 100}
	if t.Pct != nil {
		out.Pct = *t.Pct
	}
	return out
}
