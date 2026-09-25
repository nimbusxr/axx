// Package feature loads Gherkin feature files and compiles them to pickles.
package feature

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
	tagexpr "github.com/cucumber/tag-expressions/go/v11"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes produced while loading features.
const (
	CodeParse    = "AXX-E0200"
	CodeNotFound = "AXX-E0201"
	CodeTags     = "AXX-E0202"
	CodeName     = "AXX-E0203"
)

// Document is a parsed feature file.
type Document struct {
	URI    string
	Path   string
	Source []byte
	AST    *messages.GherkinDocument

	steps     map[string]*messages.Step
	scenarios map[string]*messages.Scenario
	rows      map[string]*messages.TableRow
	examples  map[string]*messages.Examples
	rules     map[string]*messages.Rule // scenario/background id -> enclosing rule
}

// Pickle is an executable scenario.
type Pickle struct {
	*messages.Pickle
	Doc *Document
	// Line is the scenario line, or the example row line for outlines.
	Line int
	// ScenarioLine is the line of the Scenario/Scenario Outline keyword.
	ScenarioLine int
	Keyword      string
	TagNames     []string
}

// Step describes a pickle step's source.
type Step struct {
	Keyword string
	Line    int
	// Background is true for steps inherited from a Background.
	Background bool
}

// StepSource returns the keyword and line of a pickle step.
func (p *Pickle) StepSource(ps *messages.PickleStep) Step {
	for _, id := range ps.AstNodeIds {
		if st, ok := p.Doc.steps[id]; ok {
			return Step{Keyword: st.Keyword, Line: int(st.Location.Line), Background: p.isBackground(id)}
		}
	}
	return Step{}
}

func (p *Pickle) isBackground(stepID string) bool {
	sc := p.Doc.scenarios[p.AstNodeIds[0]]
	if sc == nil {
		return false
	}
	for _, s := range sc.Steps {
		if s.Id == stepID {
			return false
		}
	}
	return true
}

// Set is a collection of loaded documents.
type Set struct {
	Docs    []*Document
	Pickles []*Pickle
}

// Load parses every feature file under paths (files or directories). URIs
// are made relative to base with forward slashes. Parse errors from all
// files are reported together.
func Load(paths []string, base string, newID func() string) (*Set, error) {
	files, err := collect(paths, base)
	if err != nil {
		return nil, err
	}
	set := &Set{}
	var errs []string
	for _, f := range files {
		doc, err := parse(f, base, newID)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		set.Docs = append(set.Docs, doc)
		for _, pk := range gherkin.Pickles(*doc.AST, doc.URI, newID) {
			set.Pickles = append(set.Pickles, newPickle(doc, pk))
		}
	}
	if len(errs) > 0 {
		return set, axxerr.New(CodeParse, exitcode.Usage, "invalid feature file%s:\n  %s", plural(len(errs)), strings.Join(errs, "\n  ")).
			WithHint("fix the Gherkin syntax; see https://cucumber.io/docs/gherkin/reference/")
	}
	return set, nil
}

// ParseSource parses a single document from memory (used by validate/MCP).
func ParseSource(uri string, src []byte, newID func() string) (*Document, []*Pickle, error) {
	doc, err := parseBytes(uri, uri, src, newID)
	if err != nil {
		return nil, nil, err
	}
	var pickles []*Pickle
	for _, pk := range gherkin.Pickles(*doc.AST, doc.URI, newID) {
		pickles = append(pickles, newPickle(doc, pk))
	}
	return doc, pickles, nil
}

func collect(paths []string, base string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	for _, p := range paths {
		p, _ = splitLineSpec(p)
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(base, p)
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, axxerr.New(CodeNotFound, exitcode.Usage, "feature path %s not found", p).
				WithHint("check run.paths in axx.yaml or the paths passed to `axx run`")
		}
		if !st.IsDir() {
			if !seen[abs] {
				seen[abs] = true
				files = append(files, abs)
			}
			continue
		}
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && path != abs) {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".feature") && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func parse(path, base string, newID func() string) (*Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseBytes(uriFor(path, base), path, src, newID)
}

func parseBytes(uri, path string, src []byte, newID func() string) (*Document, error) {
	ast, err := gherkin.ParseGherkinDocument(bytes.NewReader(src), newID)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", uri, strings.ReplaceAll(err.Error(), "\n", "\n  "+uri+": "))
	}
	ast.Uri = uri
	doc := &Document{
		URI: uri, Path: path, Source: src, AST: ast,
		steps: map[string]*messages.Step{}, scenarios: map[string]*messages.Scenario{},
		rows: map[string]*messages.TableRow{}, examples: map[string]*messages.Examples{},
		rules: map[string]*messages.Rule{},
	}
	if ast.Feature != nil {
		doc.index(ast.Feature.Children, nil)
	}
	return doc, nil
}

func (d *Document) index(children []*messages.FeatureChild, rule *messages.Rule) {
	for _, c := range children {
		switch {
		case c.Background != nil:
			for _, s := range c.Background.Steps {
				d.steps[s.Id] = s
			}
		case c.Scenario != nil:
			sc := c.Scenario
			d.scenarios[sc.Id] = sc
			if rule != nil {
				d.rules[sc.Id] = rule
			}
			for _, s := range sc.Steps {
				d.steps[s.Id] = s
			}
			for _, ex := range sc.Examples {
				for _, row := range ex.TableBody {
					d.rows[row.Id] = row
					d.examples[row.Id] = ex
				}
			}
		case c.Rule != nil:
			rc := make([]*messages.FeatureChild, 0, len(c.Rule.Children))
			for _, r := range c.Rule.Children {
				rc = append(rc, &messages.FeatureChild{Background: r.Background, Scenario: r.Scenario})
			}
			d.index(rc, c.Rule)
		}
	}
}

func newPickle(doc *Document, pk *messages.Pickle) *Pickle {
	p := &Pickle{Pickle: pk, Doc: doc}
	if sc := doc.scenarios[pk.AstNodeIds[0]]; sc != nil {
		p.ScenarioLine = int(sc.Location.Line)
		p.Line = p.ScenarioLine
		p.Keyword = sc.Keyword
	}
	if len(pk.AstNodeIds) > 1 {
		if row := doc.rows[pk.AstNodeIds[1]]; row != nil {
			p.Line = int(row.Location.Line)
		}
	}
	for _, t := range pk.Tags {
		p.TagNames = append(p.TagNames, t.Name)
	}
	return p
}

// Filter selects pickles.
type Filter struct {
	// Tags is a Cucumber tag expression, e.g. "@smoke and not @wip".
	Tags string
	// Names are regular expressions matched against scenario names (any).
	Names []string
	// Lines selects pickles by file:line; keys are URIs.
	Lines map[string][]int
}

// Apply returns the pickles selected by f, in load order.
func (s *Set) Apply(f Filter) ([]*Pickle, error) {
	var tagEval tagexpr.Evaluatable
	if strings.TrimSpace(f.Tags) != "" {
		ev, err := tagexpr.Parse(f.Tags)
		if err != nil {
			return nil, axxerr.Wrap(err, CodeTags, exitcode.Usage, "invalid tag expression %q", f.Tags).
				WithHint(`use expressions like "@smoke and not @wip"`)
		}
		tagEval = ev
	}
	var names []*regexp.Regexp
	for _, n := range f.Names {
		re, err := regexp.Compile(n)
		if err != nil {
			return nil, axxerr.Wrap(err, CodeName, exitcode.Usage, "invalid --name pattern %q", n)
		}
		names = append(names, re)
	}
	var out []*Pickle
	for _, p := range s.Pickles {
		if tagEval != nil && !tagEval.Evaluate(p.TagNames) {
			continue
		}
		if len(names) > 0 && !anyMatch(names, p.Name) {
			continue
		}
		if lines, ok := f.Lines[p.Doc.URI]; ok && len(lines) > 0 && !p.coversAny(lines) {
			continue
		}
		if len(f.Lines) > 0 {
			if _, ok := f.Lines[p.Doc.URI]; !ok {
				continue
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// coversAny reports whether any line identifies this pickle: its scenario
// line, its example row, the Examples block it belongs to, or any line inside
// the scenario (so the file:line of a failing step reruns that scenario).
func (p *Pickle) coversAny(lines []int) bool {
	sc := p.Doc.scenarios[p.AstNodeIds[0]]
	end := p.scenarioEnd(sc)
	for _, l := range lines {
		if l == p.Line {
			return true
		}
		if len(p.AstNodeIds) > 1 {
			// outline: the header/step lines select every row; a row line selects only its row
			if ex := p.Doc.examples[p.AstNodeIds[1]]; ex != nil && l == int(ex.Location.Line) {
				return true
			}
			if l == p.ScenarioLine || (l > p.ScenarioLine && l < firstExamplesLine(sc)) {
				return true
			}
			continue
		}
		if l >= p.ScenarioLine && l <= end {
			return true
		}
	}
	return false
}

func firstExamplesLine(sc *messages.Scenario) int {
	if sc == nil || len(sc.Examples) == 0 {
		return 1 << 30
	}
	return int(sc.Examples[0].Location.Line)
}

// scenarioEnd estimates the last line of a scenario (the line before the next
// scenario, rule or end of file).
func (p *Pickle) scenarioEnd(sc *messages.Scenario) int {
	if sc == nil {
		return p.ScenarioLine
	}
	next := 1 << 30
	for _, other := range p.Doc.scenarios {
		if l := int(other.Location.Line); l > p.ScenarioLine && l < next {
			next = l
		}
	}
	return next - 1
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// ParseLineSpecs extracts "path:line[:line]" suffixes from CLI paths,
// returning the plain paths and a URI -> lines map.
func ParseLineSpecs(args []string, base string) (paths []string, lines map[string][]int) {
	lines = map[string][]int{}
	for _, a := range args {
		p, ls := splitLineSpec(a)
		paths = append(paths, p)
		if len(ls) > 0 {
			abs := p
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(base, p)
			}
			uri := uriFor(abs, base)
			lines[uri] = append(lines[uri], ls...)
		}
	}
	return paths, lines
}

func splitLineSpec(arg string) (string, []int) {
	parts := strings.Split(arg, ":")
	var lines []int
	i := len(parts)
	for i > 1 {
		n, err := strconv.Atoi(parts[i-1])
		if err != nil {
			break
		}
		lines = append([]int{n}, lines...)
		i--
	}
	if len(lines) == 0 {
		return arg, nil
	}
	return strings.Join(parts[:i], ":"), lines
}

func uriFor(path, base string) string {
	if rel, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
