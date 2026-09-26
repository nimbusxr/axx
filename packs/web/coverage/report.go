package coverage

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nimbusxr/axx/packs/web/coverage/internal/v8cov"
)

// The reports, at the end of the run.
const (
	lcovFile    = "lcov.info"
	finalFile   = "coverage-final.json"
	summaryFile = "coverage-summary.json"
)

// report writes the run's coverage, when it has some: a run with no page in
// Chromium, Chrome or Edge writes nothing.
func (r *run) report(context.Context) error {
	waited := make(chan struct{})
	go func() {
		r.fetching.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(settleWait):
	}
	r.mu.Lock()
	collected := len(r.scripts) > 0
	r.mu.Unlock()
	if !collected {
		return nil
	}
	files, problems, lost := r.files()
	log := r.suite.Logger()
	for i, p := range problems {
		if i == 5 {
			log.Warn(fmt.Sprintf("JavaScript coverage: and %d more scripts like these", len(problems)-5))
			break
		}
		log.Warn("JavaScript coverage: " + p)
	}
	for _, u := range lost {
		log.Debug("JavaScript coverage: the source of a script could not be read before its page closed: it is left out", "script", u)
	}
	if len(files) == 0 {
		if len(r.cfg.sources) > 0 {
			log.Warn(fmt.Sprintf("JavaScript coverage: none of the scripts the pages loaded is under packs.%s.sources (%s)", Name, r.cfg.prefixes()))
		}
		return nil
	}
	if err := os.MkdirAll(r.cfg.folder, 0o755); err != nil {
		return fmt.Errorf("cannot write the JavaScript coverage: %w", err)
	}
	lcov := filepath.Join(r.cfg.folder, lcovFile)
	for name, b := range map[string][]byte{
		lcovFile:    v8cov.Lcov(files),
		finalFile:   v8cov.FinalJSON(files),
		summaryFile: v8cov.SummaryJSON(files),
	} {
		if err := os.WriteFile(filepath.Join(r.cfg.folder, name), b, 0o644); err != nil {
			return fmt.Errorf("cannot write the JavaScript coverage: %w", err)
		}
	}
	t := v8cov.Total(files)
	log.Warn(fmt.Sprintf("the web apps' JavaScript coverage: lines %s, statements %s, functions %s, branches %s, in %s",
		pct(t.Lines), pct(t.Statements), pct(t.Functions), pct(t.Branches), r.relative(lcov)))
	if abs, err := filepath.Abs(lcov); err == nil {
		lcov = abs
	}
	r.suite.Announce("coverage", "path", lcov)
	return nil
}

func pct(t v8cov.Totals) string {
	if t.Pct == nil {
		return "-"
	}
	return strconv.FormatFloat(*t.Pct, 'f', -1, 64) + "%"
}

// relative is a path of the project, relative to it where it can be.
func (r *run) relative(p string) string {
	if rel, err := filepath.Rel(r.cfg.projectDir, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return p
}

func (st *settings) prefixes() string {
	var out []string
	for _, sf := range st.sources {
		out = append(out, sf.prefix)
	}
	slices.Sort(out)
	return strings.Join(out, ", ")
}

// files merges each script's takes, converts them and names the files that
// count, in the order of the reports; problems are the scripts whose source
// map or original files could not be read, lost those whose source could
// not be.
func (r *run) files() (list []*v8cov.FileCoverage, problems, lost []string) {
	r.mu.Lock()
	keys := slices.SortedFunc(maps.Keys(r.scripts), func(a, b scriptKey) int {
		return cmp.Or(strings.Compare(a.url, b.url), strings.Compare(a.hash, b.hash))
	})
	type job struct {
		key       scriptKey
		src       source
		takes     [][]v8cov.Function
		times     []int
		hasSource bool
	}
	var jobs []job
	for _, k := range keys {
		s := r.scripts[k]
		j := job{key: k, takes: s.takes, times: s.times}
		if src, ok := r.sources[k.hash]; ok && src.ok {
			j.src, j.hasSource = *src, true
		}
		jobs = append(jobs, j)
	}
	r.mu.Unlock()
	files := map[string]*v8cov.FileCoverage{}
	for _, j := range jobs {
		if !j.hasSource {
			lost = append(lost, j.key.url)
			continue
		}
		takes, times := fitting(j.takes, j.times, utf16Len(j.src.text))
		if len(takes) == 0 {
			continue
		}
		converted, problem := r.convert(j.key.url, j.src, v8cov.MergeCounted(takes, times))
		if problem != "" {
			problems = append(problems, problem)
		}
		for _, fc := range converted {
			name, ok := r.cfg.name(fc.Path)
			if !ok {
				continue
			}
			fc.Path = name
			if prev, ok := files[name]; ok {
				prev.Merge(fc)
			} else {
				files[name] = fc
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		list = append(list, files[name])
	}
	return list, problems, lost
}

// convert converts a script's merged takes to the coverage of its files, by
// their URL path: the script's own, or its source map's original files.
func (r *run) convert(scriptURL string, src source, functions []v8cov.Function) (files []*v8cov.FileCoverage, problem string) {
	defer func() {
		if e := recover(); e != nil {
			files, problem = nil, fmt.Sprintf("the coverage of %s could not be worked out (%v): it is left out", scriptURL, e)
		}
	}()
	scriptPath := "/"
	if u, err := url.Parse(scriptURL); err == nil && u.Path != "" {
		scriptPath = u.Path
	}
	var sm *v8cov.SourceMap
	switch {
	case src.mapErr != nil:
		problem = fmt.Sprintf("the source map of %s could not be read (%v): the coverage is the script's own", scriptURL, src.mapErr)
	case src.sourceMap != nil:
		m, err := v8cov.ParseSourceMap(src.sourceMap)
		if err != nil {
			problem = fmt.Sprintf("the source map of %s is not one (%v): the coverage is the script's own", scriptURL, err)
		} else {
			sm = m
		}
	}
	c, err := v8cov.NewConverter(scriptPath, src.text, sm, r.cfg.read)
	if err != nil {
		problem = fmt.Sprintf("the original files of %s cannot be read (%v): the coverage is the script's own", scriptURL, err)
		c, _ = v8cov.NewConverter(scriptPath, src.text, nil, nil)
	}
	c.Apply(functions)
	converted := c.Istanbul()
	for _, p := range slices.Sorted(maps.Keys(converted)) {
		files = append(files, converted[p])
	}
	return files, problem
}

// fitting are the takes whose ranges are in a source of n UTF-16 code units:
// a take of another script with the same id and address is not.
func fitting(takes [][]v8cov.Function, times []int, n int) ([][]v8cov.Function, []int) {
	var ft [][]v8cov.Function
	var fn []int
next:
	for i, take := range takes {
		for _, f := range take {
			for _, rg := range f.Ranges {
				if rg.EndOffset > n || rg.StartOffset < 0 {
					continue next
				}
			}
		}
		ft = append(ft, take)
		fn = append(fn, times[i])
	}
	return ft, fn
}

// utf16Len is a string's length in UTF-16 code units, JavaScript's.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 && r <= utf8.MaxRune {
			n += 2
		} else {
			n++
		}
	}
	return n
}
