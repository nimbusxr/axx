package istanbul

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Measured says which of the metrics a coverage measures: a format, or a
// runtime, may measure no branches or no functions, which then count
// neither for nor against it.
type Measured struct {
	Branches  bool `json:"branches"`
	Functions bool `json:"functions"`
}

// ReadLcov reads an lcov tracefile, as coverage tools of every language
// write one: each line it lists is a statement, each of its branches
// (BRDA) a branch, each function (FN, FNDA) a function. A file the
// tracefile lists twice is merged.
func ReadLcov(r io.Reader) ([]*FileCoverage, Measured, error) {
	var (
		files  []*FileCoverage
		byPath = map[string]*FileCoverage{}
		m      Measured
		fc     *FileCoverage
		fnHits map[string][]int // a function's hits by name, for its FNs in order
		blocks map[string]int   // a branch's index by line and block
	)
	end := func() {
		if fc == nil {
			return
		}
		for i, fn := range fc.FnMap {
			if h := fnHits[fn.Name]; len(h) > 0 {
				fc.F[i], fnHits[fn.Name] = h[0], h[1:]
			}
		}
		if prev, ok := byPath[fc.Path]; ok {
			prev.Merge(fc)
		} else {
			byPath[fc.Path] = fc
			files = append(files, fc)
		}
		fc = nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		key, val, _ := strings.Cut(line, ":")
		if key != "SF" && key != "end_of_record" && key != "TN" && key != "" && fc == nil {
			return nil, m, fmt.Errorf("lcov line %d: %s before any SF", n, key)
		}
		f := strings.Split(val, ",")
		switch key {
		case "SF":
			end()
			fc = &FileCoverage{Path: val}
			fnHits, blocks = map[string][]int{}, map[string]int{}
		case "end_of_record":
			end()
		case "DA":
			l, hits, err := ints(f, 0, 1)
			if err != nil {
				return nil, m, fmt.Errorf("lcov line %d: DA: %w", n, err)
			}
			fc.StatementMap = append(fc.StatementMap, Span(l, 0, l, 0))
			fc.S = append(fc.S, hits)
		case "FN":
			// FN:line,name, or lcov 2's FN:line,end line,name.
			m.Functions = true
			l, err := strconv.Atoi(f[0])
			if err != nil || len(f) < 2 {
				return nil, m, fmt.Errorf("lcov line %d: FN %q", n, val)
			}
			name := strings.Join(f[1:], ",")
			if len(f) > 2 {
				if _, err := strconv.Atoi(f[1]); err == nil {
					name = strings.Join(f[2:], ",")
				}
			}
			loc := Span(l, 0, l, 0)
			fc.FnMap = append(fc.FnMap, FnMeta{Name: name, Decl: loc, Loc: loc, Line: l})
			fc.F = append(fc.F, 0)
		case "FNDA":
			hits, err := strconv.Atoi(f[0])
			if err != nil || len(f) < 2 {
				return nil, m, fmt.Errorf("lcov line %d: FNDA %q", n, val)
			}
			name := strings.Join(f[1:], ",")
			fnHits[name] = append(fnHits[name], hits)
		case "FNF", "FNH":
			m.Functions = true
		case "BRDA":
			// BRDA:line,block,branch,taken; "-" is a branch whose block never ran.
			m.Branches = true
			if len(f) != 4 {
				return nil, m, fmt.Errorf("lcov line %d: BRDA %q", n, val)
			}
			l, err := strconv.Atoi(f[0])
			if err != nil {
				return nil, m, fmt.Errorf("lcov line %d: BRDA %q", n, val)
			}
			taken := 0
			if f[3] != "-" {
				if taken, err = strconv.Atoi(f[3]); err != nil {
					return nil, m, fmt.Errorf("lcov line %d: BRDA %q", n, val)
				}
			}
			k := f[0] + "," + f[1]
			i, ok := blocks[k]
			if !ok {
				i = len(fc.BranchMap)
				blocks[k] = i
				loc := Span(l, 0, l, 0)
				fc.BranchMap = append(fc.BranchMap, BranchMeta{Type: "branch", Line: l, Loc: loc})
				fc.B = append(fc.B, nil)
			}
			fc.BranchMap[i].Locations = append(fc.BranchMap[i].Locations, Span(l, 0, l, 0))
			fc.B[i] = append(fc.B[i], taken)
		case "BRF", "BRH":
			m.Branches = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, m, err
	}
	end()
	return files, m, nil
}

func ints(f []string, i, j int) (int, int, error) {
	if len(f) <= j {
		return 0, 0, fmt.Errorf("%q", strings.Join(f, ","))
	}
	a, err := strconv.Atoi(f[i])
	if err != nil {
		return 0, 0, err
	}
	b, err := strconv.Atoi(f[j])
	return a, b, err
}

// conditions is Cobertura's condition-coverage, "50% (1/2)".
var conditions = regexp.MustCompile(`\((\d+)/(\d+)\)`)

type coberturaLine struct {
	Number     int    `xml:"number,attr"`
	Hits       int    `xml:"hits,attr"`
	Branch     bool   `xml:"branch,attr"`
	Conditions string `xml:"condition-coverage,attr"`
}

type coberturaDoc struct {
	Sources  []string `xml:"sources>source"`
	Packages []struct {
		Classes []struct {
			Filename   string            `xml:"filename,attr"`
			BranchRate *string           `xml:"branch-rate,attr"`
			Lines      []coberturaLine   `xml:"lines>line"`
			Methods    *coberturaMethods `xml:"methods"`
		} `xml:"classes>class"`
	} `xml:"packages>package"`
}

type coberturaMethods struct {
	Methods []struct {
		Name  string          `xml:"name,attr"`
		Lines []coberturaLine `xml:"lines>line"`
	} `xml:"method"`
}

// ReadCobertura reads a Cobertura report, as coverage.py, coverlet,
// gcovr and other tools write one: each line a statement, a line's
// conditions its branches (which of them ran is not said: the first ones
// count as covered), each method a function at its first line. A file's
// name is joined to the report's first source.
func ReadCobertura(r io.Reader) ([]*FileCoverage, Measured, error) {
	var doc coberturaDoc
	var m Measured
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, m, fmt.Errorf("not a Cobertura report: %w", err)
	}
	byPath := map[string]*FileCoverage{}
	var files []*FileCoverage
	for _, p := range doc.Packages {
		for _, c := range p.Classes {
			name := c.Filename
			if len(doc.Sources) > 0 && !path.IsAbs(name) && !strings.Contains(name, ":") {
				name = path.Join(strings.TrimSpace(doc.Sources[0]), name)
			}
			fc := &FileCoverage{Path: name}
			for _, l := range c.Lines {
				fc.StatementMap = append(fc.StatementMap, Span(l.Number, 0, l.Number, 0))
				fc.S = append(fc.S, l.Hits)
				if !l.Branch {
					continue
				}
				mm := conditions.FindStringSubmatch(l.Conditions)
				if mm == nil {
					continue
				}
				covered, _ := strconv.Atoi(mm[1])
				total, _ := strconv.Atoi(mm[2])
				loc := Span(l.Number, 0, l.Number, 0)
				b := BranchMeta{Type: "branch", Line: l.Number, Loc: loc}
				hits := make([]int, total)
				for i := range total {
					b.Locations = append(b.Locations, loc)
					if i < covered {
						hits[i] = 1
					}
				}
				fc.BranchMap, fc.B = append(fc.BranchMap, b), append(fc.B, hits)
			}
			if c.BranchRate != nil {
				m.Branches = true
			}
			if c.Methods != nil {
				m.Functions = true
				for _, meth := range c.Methods.Methods {
					if len(meth.Lines) == 0 {
						continue
					}
					first := meth.Lines[0]
					loc := Span(first.Number, 0, first.Number, 0)
					fc.FnMap = append(fc.FnMap, FnMeta{Name: meth.Name, Decl: loc, Loc: loc, Line: first.Number})
					fc.F = append(fc.F, first.Hits)
				}
			}
			if prev, ok := byPath[fc.Path]; ok {
				prev.Merge(fc)
				continue
			}
			byPath[fc.Path] = fc
			files = append(files, fc)
		}
	}
	return files, m, nil
}
