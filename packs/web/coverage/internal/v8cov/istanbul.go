package v8cov

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"slices"
	"strings"
)

// Converting a script's V8 coverage to Istanbul's file coverage, as
// v8-to-istanbul does, source maps included. Lines are Istanbul's
// statements; each V8 range is a branch; named functions are functions.

// Pos is an Istanbul position (1-based line, 0-based column).
type Pos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Loc is an Istanbul location.
type Loc struct {
	Start Pos `json:"start"`
	End   Pos `json:"end"`
}

// FnMeta is a function of fnMap.
type FnMeta struct {
	Name string `json:"name"`
	Decl Loc    `json:"decl"`
	Loc  Loc    `json:"loc"`
	Line int    `json:"line"`
}

// BranchMeta is a branch of branchMap.
type BranchMeta struct {
	Type      string `json:"type"`
	Line      int    `json:"line"`
	Loc       Loc    `json:"loc"`
	Locations []Loc  `json:"locations"`
}

// FileCoverage is Istanbul's coverage of a file. Istanbul keys its items
// "0", "1"...: here they are the indexes of the slices.
type FileCoverage struct {
	Path         string
	All          bool
	StatementMap []Loc
	S            []int
	BranchMap    []BranchMeta
	B            [][]int
	FnMap        []FnMeta
	F            []int
}

type covBranch struct {
	startLine, startCol, endLine, endCol, count int
}

type covFunction struct {
	name string
	covBranch
}

type pathSource struct {
	source *covSource
	path   string
}

// Converter converts one script's coverage.
type Converter struct {
	path       string
	covSources []pathSource
	transpiled *covSource
	sm         *SourceMap
	multi      bool
	branches   map[string][]covBranch
	functions  map[string][]covFunction
}

// NewConverter prepares a script, at a slash-separated path the original
// files of its source map (nil without one) resolve against, with its
// source. read reads an original file the source map has not the content
// of, by its resolved path; it may be nil.
func NewConverter(scriptPath, source string, sm *SourceMap, read func(string) (string, error)) (*Converter, error) {
	c := &Converter{path: scriptPath, sm: sm, branches: map[string][]covBranch{}, functions: map[string][]covFunction{}}
	if sm == nil {
		c.covSources = []pathSource{{newCovSource(source), scriptPath}}
		return c, nil
	}
	content := func(i int, resolved string) (string, error) {
		if i < len(sm.SourcesContent) && sm.SourcesContent[i] != nil {
			return *sm.SourcesContent[i], nil
		}
		if read == nil {
			return "", fmt.Errorf("its source map has not the content of %s", resolved)
		}
		return read(resolved)
	}
	c.transpiled = newCovSource(source)
	if len(sm.Sources) > 1 {
		c.multi = true
		for i, s := range sm.Sources {
			text, err := content(i, c.resolveSource(s))
			if err != nil {
				return nil, err
			}
			c.covSources = append(c.covSources, pathSource{newCovSource(text), s})
		}
		return c, nil
	}
	candidate := sm.File
	if len(sm.Sources) == 1 {
		candidate = sm.Sources[0]
	}
	if candidate == "" {
		candidate = scriptPath
	}
	c.path = c.resolveSource(candidate)
	text, err := content(0, c.path)
	if err != nil {
		return nil, err
	}
	c.covSources = []pathSource{{newCovSource(text), c.path}}
	return c, nil
}

// resolveSource is where an original file of the source map is: against
// the source map's sourceRoot, then the script's folder.
func (c *Converter) resolveSource(p string) string {
	if strings.HasPrefix(p, "file://") {
		if u, err := url.Parse(p); err == nil {
			return u.Path
		}
		return strings.TrimPrefix(p, "file://")
	}
	p = strings.TrimPrefix(p, "webpack://")
	root := strings.Replace(c.sm.SourceRoot, "file://", "", 1)
	candidate := path.Join(root, p)
	if path.IsAbs(candidate) {
		return candidate
	}
	return path.Join(path.Dir(c.path), candidate)
}

// Apply adds a script's coverage: its takes merged first, when there are
// several (see Merge).
func (c *Converter) Apply(functions []Function) {
	for _, block := range functions {
		for i, r := range block.Ranges {
			startCol, endCol, path, cs := c.remap(r)
			lines := sliceRange(cs.lines, startCol, endCol, false)
			if len(lines) == 0 {
				continue
			}
			first, last := lines[0], lines[len(lines)-1]
			b := covBranch{first.line, startCol - first.startCol, last.line, endCol - last.startCol, r.Count}
			if block.IsBlockCoverage {
				c.branches[path] = append(c.branches[path], b)
				if block.FunctionName != "" && i == 0 {
					c.functions[path] = append(c.functions[path], covFunction{block.FunctionName, b})
				}
			} else if block.FunctionName != "" {
				c.functions[path] = append(c.functions[path], covFunction{block.FunctionName, b})
			}
			// A line takes the count of the last (innermost) range that spans
			// all of it; every line starts at 1.
			for _, l := range lines {
				if startCol <= l.startCol && endCol >= l.endCol && !l.ignore {
					l.count = r.Count
				}
			}
		}
	}
}

func (c *Converter) remap(r Range) (startCol, endCol int, path string, cs *covSource) {
	cs = c.covSources[0].source
	startCol = max(0, r.StartOffset)
	endCol = min(cs.eof, r.EndOffset)
	path = c.path
	if c.sm == nil {
		return startCol, endCol, path, cs
	}
	startCol = max(0, r.StartOffset)
	endCol = min(c.transpiled.eof, r.EndOffset)
	o, ok := c.transpiled.offsetToOriginalRelative(c.sm, startCol, endCol)
	match := -1
	if ok {
		for i, s := range c.covSources {
			if s.path == o.source {
				match = i
				break
			}
		}
	}
	if match >= 0 {
		cs, path = c.covSources[match].source, c.covSources[match].path
	} else {
		cs, path = c.covSources[0].source, c.covSources[0].path
	}
	startCol = cs.relativeToOffset(o.startLine, o.relStart, ok)
	endCol = cs.relativeToOffset(o.endLine, o.relEnd, ok)
	return startCol, endCol, path, cs
}

type originalRange struct {
	source              string
	startLine, relStart int
	endLine, relEnd     int
}

// infinity is JavaScript's Infinity as a column: past the end of any line.
const infinity = math.MaxInt32

func (s *covSource) offsetToOriginalRelative(sm *SourceMap, startCol, endCol int) (originalRange, bool) {
	lines := sliceRange(s.lines, startCol, endCol, true)
	if len(lines) == 0 {
		return originalRange{}, false
	}
	first, last := lines[0], lines[len(lines)-1]
	start := originalPositionTryBoth(sm, first.line, max(0, startCol-first.startCol))
	if !start.ok || start.source == "" {
		return originalRange{}, false
	}
	end, ok := originalEndPositionFor(sm, last.line, endCol-last.startCol)
	if !ok || end.source == "" {
		return originalRange{}, false
	}
	if start.source != end.source {
		return originalRange{}, false
	}
	if start.line == end.line && start.column == end.column {
		end = sm.originalPositionFor(last.line, endCol-last.startCol, biasLUB)
		// trace-mapping's nulls: a null line is 0 and a null column 0 here.
		end.column--
	}
	return originalRange{start.source, start.line, start.column, end.line, end.column}, true
}

func originalEndPositionFor(sm *SourceMap, line, column int) (omapping, bool) {
	before := originalPositionTryBoth(sm, line, max(column-1, 1))
	if !before.ok {
		return omapping{}, false
	}
	after := sm.generatedPositionFor(before.source, before.line, before.column+1, biasLUB)
	if !after.ok || lineOf(sm.originalPositionFor(after.line, after.column, biasGLB)) != before.line {
		return omapping{source: before.source, line: before.line, column: infinity, ok: true}, true
	}
	return sm.originalPositionFor(after.line, after.column, biasGLB), true
}

func lineOf(m omapping) int {
	if !m.ok {
		return -1 // null equals no line
	}
	return m.line
}

func originalPositionTryBoth(sm *SourceMap, line, column int) omapping {
	o := sm.originalPositionFor(line, column, biasGLB)
	if !o.ok {
		o = sm.originalPositionFor(line, column, biasLUB)
	}
	// A mapping mid-line may map to an earlier line than one at column 0:
	// v8-to-istanbul takes column 0's (null compares as 0).
	m := sm.originalPositionFor(line, 0, biasGLB)
	if m.ok && m.line > o.line {
		o = m
	}
	return o
}

// Istanbul returns the coverage of the script's files, by path: the script,
// or the original files of its source map.
func (c *Converter) Istanbul() map[string]*FileCoverage {
	out := map[string]*FileCoverage{}
	for _, ps := range c.covSources {
		p := ps.path
		if c.multi {
			p = c.resolveSource(ps.path)
		}
		fc := &FileCoverage{Path: p}
		for _, l := range ps.source.lines {
			fc.StatementMap = append(fc.StatementMap, Loc{Pos{l.line, 0}, Pos{l.line, l.endCol - l.startCol}})
			count := l.count
			if l.ignore {
				count = 1
			}
			fc.S = append(fc.S, count)
		}
		ignored := func(line int) bool {
			if line-1 < 0 || line-1 >= len(ps.source.lines) {
				return true
			}
			return ps.source.lines[line-1].ignore
		}
		for _, b := range c.branches[ps.path] {
			loc := Loc{Pos{b.startLine, b.startCol}, Pos{b.endLine, b.endCol}}
			fc.BranchMap = append(fc.BranchMap, BranchMeta{Type: "branch", Line: b.startLine, Loc: loc, Locations: []Loc{loc}})
			count := b.count
			if ignored(b.startLine) {
				count = 1
			}
			fc.B = append(fc.B, []int{count})
		}
		for _, f := range c.functions[ps.path] {
			loc := Loc{Pos{f.startLine, f.startCol}, Pos{f.endLine, f.endCol}}
			fc.FnMap = append(fc.FnMap, FnMeta{Name: f.name, Decl: loc, Loc: loc, Line: f.startLine})
			count := f.count
			if ignored(f.startLine) {
				count = 1
			}
			fc.F = append(fc.F, count)
		}
		out[p] = fc
	}
	return out
}

// Merge adds another coverage of the same file, as istanbul-lib-coverage's
// FileCoverage.merge does: items at the same place add up; an item only one
// side has takes the hits of the other side's nearest item around it.
func (fc *FileCoverage) Merge(o *FileCoverage) {
	if o.All {
		return
	}
	if fc.All {
		*fc = *o
		return
	}
	var s [][]int
	s, fc.StatementMap = mergeProp(single(fc.S), fc.StatementMap, single(o.S), o.StatementMap,
		func(l Loc) Loc { return l }, func(l Loc) Loc { return l })
	fc.S = unsingle(s)
	var f [][]int
	f, fc.FnMap = mergeProp(single(fc.F), fc.FnMap, single(o.F), o.FnMap,
		func(m FnMeta) Loc { return m.Loc }, func(m FnMeta) Loc { return m.Loc })
	fc.F = unsingle(f)
	fc.B, fc.BranchMap = mergeProp(fc.B, fc.BranchMap, o.B, o.BranchMap,
		func(m BranchMeta) Loc { return m.Locations[0] }, func(m BranchMeta) Loc { return m.Loc })
}

func single(hits []int) [][]int {
	out := make([][]int, len(hits))
	for i, h := range hits {
		out[i] = []int{h}
	}
	return out
}

func unsingle(hits [][]int) []int {
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h[0]
	}
	return out
}

func locKey(l Loc) string {
	return fmt.Sprintf("%d|%d|%d|%d", l.Start.Line, l.Start.Column, l.End.Line, l.End.Column)
}

// mergeProp merges the items of one kind; an item is known by the location
// key gives, and contained in those around it by the location of within.
func mergeProp[M any](aHits [][]int, aMap []M, bHits [][]int, bMap []M, key, within func(M) Loc) ([][]int, []M) {
	type item struct {
		hits []int
		meta M
	}
	index := func(hits [][]int, metas []M) ([]string, map[string]item) {
		var order []string
		items := map[string]item{}
		for i, h := range hits {
			k := locKey(key(metas[i]))
			if _, ok := items[k]; !ok {
				order = append(order, k)
			}
			items[k] = item{h, metas[i]}
		}
		return order, items
	}
	aOrder, aItems := index(aHits, aMap)
	bOrder, bItems := index(bHits, bMap)
	var order []string
	merged := map[string]item{}
	for _, k := range aOrder {
		it := aItems[k]
		h := it.hits
		if bi, ok := bItems[k]; ok {
			h = addHits(h, bi.hits)
		} else if c := nearestContainer(within(it.meta), bMap, within); c >= 0 {
			h = addHits(h, bHits[c])
		}
		merged[k] = item{h, it.meta}
		order = append(order, k)
	}
	for _, k := range bOrder {
		if _, ok := merged[k]; ok {
			continue
		}
		it := bItems[k]
		h := it.hits
		if c := nearestContainer(within(it.meta), aMap, within); c >= 0 {
			h = addHits(h, aHits[c])
		}
		merged[k] = item{h, it.meta}
		order = append(order, k)
	}
	hits := make([][]int, len(order))
	metas := make([]M, len(order))
	for i, k := range order {
		hits[i], metas[i] = merged[k].hits, merged[k].meta
	}
	return hits, metas
}

func addHits(a, b []int) []int {
	out := make([]int, len(a))
	for i := range a {
		out[i] = a[i]
		if i < len(b) {
			out[i] += b[i]
		}
	}
	return out
}

// nearestContainer is the index of the narrowest item of m around l, or -1.
func nearestContainer[M any](l Loc, m []M, within func(M) Loc) int {
	best := -1
	var bd [4]int
	for i, meta := range m {
		ml := within(meta)
		d := [4]int{l.Start.Line - ml.Start.Line, l.Start.Column - ml.Start.Column, ml.End.Line - l.End.Line, ml.End.Column - l.End.Column}
		if d[0] < 0 || d[2] < 0 || (d[0] == 0 && d[1] < 0) || (d[2] == 0 && d[3] < 0) {
			continue
		}
		if best < 0 || d[0] < bd[0] || (d[0] == 0 && d[1] < bd[1]) || d[2] < bd[2] || (d[2] == 0 && d[3] < bd[3]) {
			best, bd = i, d
		}
	}
	return best
}

// LineCoverage is Istanbul's getLineCoverage: each statement's first line
// takes its highest count; lines in order.
func (fc *FileCoverage) LineCoverage() (lines []int, hits map[int]int) {
	hits = map[int]int{}
	for i, count := range fc.S {
		l := fc.StatementMap[i].Start.Line
		if prev, ok := hits[l]; !ok || prev < count {
			if !ok {
				lines = append(lines, l)
			}
			hits[l] = count
		}
	}
	slices.Sort(lines)
	return lines, hits
}

// Totals are Istanbul's totals of a kind of item. Pct is nil where
// Istanbul has "Unknown".
type Totals struct {
	Total, Covered, Skipped int
	Pct                     *float64
}

// Summary is Istanbul's coverage summary of a file, or of files.
type Summary struct {
	Lines, Statements, Functions, Branches Totals
}

func percent(covered, total int) float64 {
	if total == 0 {
		return 100
	}
	tmp := float64(1000*100*covered) / float64(total)
	return math.Floor(tmp/10) / 100
}

func totals(covered, total int) Totals {
	p := percent(covered, total)
	return Totals{Total: total, Covered: covered, Pct: &p}
}

func covered(vs []int) int {
	n := 0
	for _, v := range vs {
		if v != 0 {
			n++
		}
	}
	return n
}

// Summary sums up the file's coverage.
func (fc *FileCoverage) Summary() Summary {
	lines, hits := fc.LineCoverage()
	var lv, bv []int
	for _, l := range lines {
		lv = append(lv, hits[l])
	}
	branches := 0
	for _, b := range fc.B {
		bv = append(bv, b...)
		branches += len(b)
	}
	bc := 0
	for _, v := range bv {
		if v > 0 {
			bc++
		}
	}
	return Summary{
		Lines:      totals(covered(lv), len(lv)),
		Statements: totals(covered(fc.S), len(fc.S)),
		Functions:  totals(covered(fc.F), len(fc.F)),
		Branches:   totals(bc, branches),
	}
}

// Total sums up the summaries of files, as Istanbul sums up a report's.
func Total(files []*FileCoverage) Summary {
	var sum Summary
	for _, fc := range files {
		s := fc.Summary()
		add := func(a *Totals, b Totals) { *a = totals(a.Covered+b.Covered, a.Total+b.Total) }
		add(&sum.Lines, s.Lines)
		add(&sum.Statements, s.Statements)
		add(&sum.Functions, s.Functions)
		add(&sum.Branches, s.Branches)
	}
	return sum
}
