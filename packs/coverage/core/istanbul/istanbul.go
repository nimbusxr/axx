// Package istanbul is coverage as Istanbul models it: each file's
// statements, branches and functions with their places and hits. axx's
// coverage packs convert what a runtime measured into it, merge it, and write
// the reports Istanbul's tools write (lcov, coverage-final.json,
// coverage-summary.json), byte for byte.
package istanbul

import (
	"fmt"
	"math"
	"slices"
)

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

// Span is the location from one position to another.
func Span(startLine, startColumn, endLine, endColumn int) Loc {
	return Loc{Start: Pos{Line: startLine, Column: startColumn}, End: Pos{Line: endLine, Column: endColumn}}
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
	Path         string       `json:"path"`
	All          bool         `json:"all,omitempty"`
	StatementMap []Loc        `json:"statementMap"`
	S            []int        `json:"s"`
	BranchMap    []BranchMeta `json:"branchMap"`
	B            [][]int      `json:"b"`
	FnMap        []FnMeta     `json:"fnMap"`
	F            []int        `json:"f"`
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
