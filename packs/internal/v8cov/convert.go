package v8cov

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"strings"

	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// Converting a script's V8 coverage to Istanbul's file coverage, as
// v8-to-istanbul does, source maps included. Lines are Istanbul's
// statements; each V8 range is a branch; named functions are functions.

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
func (c *Converter) Istanbul() map[string]*istanbul.FileCoverage {
	out := map[string]*istanbul.FileCoverage{}
	for _, ps := range c.covSources {
		p := ps.path
		if c.multi {
			p = c.resolveSource(ps.path)
		}
		fc := &istanbul.FileCoverage{Path: p}
		for _, l := range ps.source.lines {
			fc.StatementMap = append(fc.StatementMap, istanbul.Span(l.line, 0, l.line, l.endCol-l.startCol))
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
			loc := istanbul.Span(b.startLine, b.startCol, b.endLine, b.endCol)
			fc.BranchMap = append(fc.BranchMap, istanbul.BranchMeta{Type: "branch", Line: b.startLine, Loc: loc, Locations: []istanbul.Loc{loc}})
			count := b.count
			if ignored(b.startLine) {
				count = 1
			}
			fc.B = append(fc.B, []int{count})
		}
		for _, f := range c.functions[ps.path] {
			loc := istanbul.Span(f.startLine, f.startCol, f.endLine, f.endCol)
			fc.FnMap = append(fc.FnMap, istanbul.FnMeta{Name: f.name, Decl: loc, Loc: loc, Line: f.startLine})
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
