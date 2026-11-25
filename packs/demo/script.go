package demo

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	messages "github.com/cucumber/messages/go/v35"

	"github.com/nimbusxr/axx/internal/feature"
)

// A demo's script is what its feature file says to the people who watch it,
// in comments that start with "demo:". A comment's text is a subtitle, and
// what it has in square brackets directs the video:
//
//	# demo: [speed 4x] The depot's database has the hold at once.
//	#       Nobody had to tell it.
//	Then ...
//
// A cue belongs to the line of the feature below it: a step, or a Scenario or
// Feature line, where it plays before the first step. A comment line right
// below a cue, with its text indented to the cue's, goes on with its text.
// Any other comment is the author's own, and no part of the demo.

// Cue is one "# demo:" comment, with the comment lines that continue it.
type Cue struct {
	// Line is the comment's line, and Anchor the line of the feature it
	// belongs to: a step's, a Scenario's, a Feature's. Anchor is 0 for a cue
	// below the file's last step, which plays at the end.
	Line   int `json:"line"`
	Anchor int `json:"anchor"`
	// Directions are what it has in square brackets, in their order.
	Directions []Direction `json:"directions,omitempty"`
	// Text is what it says: a subtitle, or the text of a direction that
	// takes it (a card's).
	Text string `json:"text,omitempty"`
}

// Direction is one direction of a cue: "[speed 4x]" is the direction
// "speed" with the argument "4x".
type Direction struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
	// Column is where its bracket opens in the comment's line, from 1.
	Column int `json:"-"`
}

// Problem is something wrong in a script, where it is.
type Problem struct {
	Line    int
	Column  int
	Message string
}

// Script is the cues of a feature file, in the file's order.
type Script struct {
	Cues     []Cue
	Problems []Problem
	// FeatureLine is the line of the file's Feature: a cue that belongs to
	// it is the whole video's, and plays once, whatever rows a Scenario
	// Outline has.
	FeatureLine int
	// Rows are the values of each row of the Scenario Outlines' Examples,
	// by the row's line and its column's name: a cue's <name> is the row's
	// value, as a step's is.
	Rows map[int]map[string]string
	// Scenarios are the file's scenarios, each with the lines of its steps
	// as the file has them now: a take made before the file was edited has
	// the lines of then, and its steps are found by their order.
	Scenarios []ScriptScenario
}

// ScriptScenario is a scenario of a demo's file.
type ScriptScenario struct {
	Name string
	Line int
	// Rows are the lines of the rows of its Examples, in order: none for a
	// scenario that is no outline.
	Rows []int
	// Steps are the lines of the steps a run of it runs, in order: its
	// Backgrounds' first.
	Steps []int
}

// Placed says where a take's steps are in the file as it is now: each step's
// line, and the values of the take's row, for a row of an outline. Row is
// which row the take is of, from 0, when its own line no longer says. It
// reports false when the file has no scenario of the take's steps: the
// scenario changed since the take.
func (s *Script) Placed(take *Take, row int) (lines []int, values map[string]string, ok bool) {
	var found *ScriptScenario
	// The scenario the take's line is in, when the file's lines are as they
	// were; otherwise the one of its name; otherwise the file's only one.
	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		if at := slices.Index(sc.Rows, take.Line); at >= 0 {
			found, row = sc, at
			break
		}
		if sc.Line == take.Line && len(sc.Rows) == 0 {
			found = sc
			break
		}
	}
	if found == nil || len(found.Steps) != len(take.Steps) {
		found = nil
		for i := range s.Scenarios {
			sc := &s.Scenarios[i]
			name := sc.Name
			if row < len(sc.Rows) {
				name = fill(name, s.Rows[sc.Rows[row]])
			}
			if name == take.Scenario && len(sc.Steps) == len(take.Steps) {
				found = sc
				break
			}
		}
	}
	if found == nil && len(s.Scenarios) == 1 && len(s.Scenarios[0].Steps) == len(take.Steps) {
		found = &s.Scenarios[0]
	}
	if found == nil {
		return nil, nil, false
	}
	if row < len(found.Rows) {
		values = s.Rows[found.Rows[row]]
	}
	return found.Steps, values, true
}

const marker = "demo:"

// Known is what a project has that a cue can name: its own directions, and
// its diagrams.
type Known struct {
	Directions []string
	Diagrams   []string
	// Surfaces are what [show ...] can name beside the pack's own: the
	// modules', and the project's.
	Surfaces []string
}

// known is what the project's settings, and the modules, say a cue can name.
func (s Settings) known() Known {
	k := Known{Directions: slices.Clone(s.Directions), Diagrams: s.Diagrams.Names, Surfaces: slices.Clone(s.Surfaces)}
	for _, m := range Modules() {
		k.Surfaces = append(k.Surfaces, m.Surfaces...)
		k.Directions = append(k.Directions, m.Directions...)
	}
	return k
}

// ParseScript reads the cues of a feature file. A file with none has an
// empty script.
func ParseScript(uri string, src []byte, custom Known) (*Script, error) {
	n := 0
	doc, _, err := feature.ParseSource(uri, src, func() string { n++; return strconv.Itoa(n) })
	if err != nil {
		return nil, err
	}
	return scriptOf(doc.AST, custom), nil
}

func scriptOf(ast *messages.GherkinDocument, custom Known) *Script {
	s := &Script{Rows: map[int]map[string]string{}}
	if ast.Feature != nil {
		s.FeatureLine = int(ast.Feature.Location.Line)
		rowsOf(ast.Feature.Children, s.Rows)
		s.Scenarios = scenariosOf(ast.Feature.Children, nil)
	}
	anchors := anchorLines(ast)
	// The comments by line, to find the ones that go on below a cue.
	byLine := map[int]*messages.Comment{}
	var lines []int
	for _, c := range ast.Comments {
		byLine[int(c.Location.Line)] = c
		lines = append(lines, int(c.Location.Line))
	}
	sort.Ints(lines)
	taken := map[int]bool{}
	for _, line := range lines {
		if taken[line] {
			continue
		}
		body, column, ok := cueBody(byLine[line].Text)
		if !ok {
			continue
		}
		// The lines that go on with it: comments right below, indented to
		// its text, that start no cue of their own.
		last := line
		for next := line + 1; byLine[next] != nil; next++ {
			more, indent, isCue := continuation(byLine[next].Text)
			if isCue || indent < column {
				break
			}
			body += " " + more
			taken[next], last = true, next
		}
		cue := Cue{Line: line}
		// Where a mistake is, in the line: the comment's indent, then the
		// cue's text from its "#".
		raw := byLine[line].Text
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
		cue.Directions, cue.Text, s.Problems = parseCue(body, line, indent+column, custom, s.Problems)
		// What it belongs to: the first line of the feature below it.
		i := sort.SearchInts(anchors, last+1)
		if i < len(anchors) {
			cue.Anchor = anchors[i]
		}
		s.Cues = append(s.Cues, cue)
	}
	return s
}

// rowsOf collects the rows of the Examples of every Scenario Outline.
func rowsOf(children []*messages.FeatureChild, rows map[int]map[string]string) {
	outline := func(sc *messages.Scenario) {
		for _, ex := range sc.Examples {
			if ex.TableHeader == nil {
				continue
			}
			for _, row := range ex.TableBody {
				values := map[string]string{}
				for i, cell := range row.Cells {
					if i < len(ex.TableHeader.Cells) {
						values[ex.TableHeader.Cells[i].Value] = cell.Value
					}
				}
				rows[int(row.Location.Line)] = values
			}
		}
	}
	for _, c := range children {
		switch {
		case c.Scenario != nil:
			outline(c.Scenario)
		case c.Rule != nil:
			for _, rc := range c.Rule.Children {
				if rc.Scenario != nil {
					outline(rc.Scenario)
				}
			}
		}
	}
}

// scenariosOf are the scenarios among a feature's or a rule's children, each
// with the steps a run of it runs: before are the steps of the Backgrounds
// above.
func scenariosOf(children []*messages.FeatureChild, before []int) []ScriptScenario {
	var out []ScriptScenario
	lines := func(steps []*messages.Step) []int {
		var l []int
		for _, st := range steps {
			l = append(l, int(st.Location.Line))
		}
		return l
	}
	scenario := func(sc *messages.Scenario, before []int) ScriptScenario {
		s := ScriptScenario{Name: sc.Name, Line: int(sc.Location.Line), Steps: append(slices.Clone(before), lines(sc.Steps)...)}
		for _, ex := range sc.Examples {
			for _, row := range ex.TableBody {
				s.Rows = append(s.Rows, int(row.Location.Line))
			}
		}
		return s
	}
	for _, c := range children {
		switch {
		case c.Background != nil:
			before = append(slices.Clone(before), lines(c.Background.Steps)...)
		case c.Scenario != nil:
			out = append(out, scenario(c.Scenario, before))
		case c.Rule != nil:
			inRule := before
			for _, rc := range c.Rule.Children {
				switch {
				case rc.Background != nil:
					inRule = append(slices.Clone(inRule), lines(rc.Background.Steps)...)
				case rc.Scenario != nil:
					out = append(out, scenario(rc.Scenario, inRule))
				}
			}
		}
	}
	return out
}

// fill puts a row's values in the place of a text's <name>s.
func fill(text string, row map[string]string) string {
	for name, value := range row {
		text = strings.ReplaceAll(text, "<"+name+">", value)
	}
	return text
}

// cueBody is what a comment says after "# demo:", and the column its text
// starts in, counted from the "#"; ok is false for a comment that is no cue.
func cueBody(comment string) (body string, column int, ok bool) {
	t := strings.TrimLeft(comment, " \t")
	if !strings.HasPrefix(t, "#") {
		return "", 0, false
	}
	rest := strings.TrimLeft(t[1:], " \t")
	if !strings.HasPrefix(rest, marker) {
		return "", 0, false
	}
	after := rest[len(marker):]
	text := strings.TrimLeft(after, " \t")
	return strings.TrimRight(text, " \t"), len(t) - len(text), true
}

// continuation is what a comment line says, how far its text is indented
// from its "#", and whether it starts a cue of its own.
func continuation(comment string) (text string, indent int, isCue bool) {
	if _, _, ok := cueBody(comment); ok {
		return "", 0, true
	}
	t := strings.TrimLeft(comment, " \t")
	text = strings.TrimLeft(t[1:], " \t")
	return strings.TrimRight(text, " \t"), len(t) - len(text), false
}

// parseCue reads a cue's directions, which come first, and its text.
func parseCue(body string, line, column int, custom Known, problems []Problem) ([]Direction, string, []Problem) {
	var dirs []Direction
	rest, col := body, column+1
	for strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			problems = append(problems, Problem{line, col, "a direction's bracket is not closed: write it as [speed 2x], or start the subtitle with another character"})
			break
		}
		fields := strings.Fields(rest[1:end])
		d := Direction{Column: col}
		switch {
		case strings.Contains(rest[1:end], "->"):
			// An arrow lights a line of the diagram: [parcels --> addresses].
			d.Name, d.Args = "flow", arrowOf(rest[1:end])
		case len(fields) == 0:
			problems = append(problems, Problem{line, col, "an empty direction: say what to do between the brackets, like [pause 2s]"})
		case isDuration(fields[0]):
			// A time alone says how long the step below lasts in the video.
			d.Name, d.Args = "lasts", fields
		default:
			d.Name, d.Args = fields[0], fields[1:]
		}
		if d.Name != "" {
			if msg := checkDirection(d, custom); msg != "" {
				problems = append(problems, Problem{line, col, msg})
			} else {
				dirs = append(dirs, d)
			}
		}
		trimmed := strings.TrimLeft(rest[end+1:], " \t")
		col += len(rest) - len(trimmed)
		rest = trimmed
	}
	return dirs, rest, problems
}

// arrow splits what an arrow joins: the names on either side of each "-->"
// or "->", so that a --> b --> c is a, b and c.
var arrow = regexp.MustCompile(`\s*-+>\s*`)

func arrowOf(text string) []string {
	var out []string
	for _, name := range arrow.Split(strings.TrimSpace(text), -1) {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// anchorLines are the lines a cue can belong to, in order: every step, and
// every Feature, Rule, Background, Scenario and Examples line.
func anchorLines(ast *messages.GherkinDocument) []int {
	var out []int
	if ast.Feature == nil {
		return out
	}
	add := func(l *messages.Location) { out = append(out, int(l.Line)) }
	steps := func(ss []*messages.Step) {
		for _, st := range ss {
			add(st.Location)
		}
	}
	var children func(cs []*messages.FeatureChild)
	scenario := func(sc *messages.Scenario) {
		add(sc.Location)
		steps(sc.Steps)
		for _, ex := range sc.Examples {
			add(ex.Location)
		}
	}
	children = func(cs []*messages.FeatureChild) {
		for _, c := range cs {
			switch {
			case c.Background != nil:
				add(c.Background.Location)
				steps(c.Background.Steps)
			case c.Scenario != nil:
				scenario(c.Scenario)
			case c.Rule != nil:
				add(c.Rule.Location)
				for _, rc := range c.Rule.Children {
					switch {
					case rc.Background != nil:
						add(rc.Background.Location)
						steps(rc.Background.Steps)
					case rc.Scenario != nil:
						scenario(rc.Scenario)
					}
				}
			}
		}
	}
	add(ast.Feature.Location)
	children(ast.Feature.Children)
	sort.Ints(out)
	return out
}

// isDuration reports whether a word is a time, like 3s, 0s, 1.5s or 500ms.
func isDuration(word string) bool {
	_, err := parseDuration(word)
	return err == nil
}

// parseDuration reads a time of a direction: a number with ms, s or m.
func parseDuration(word string) (time.Duration, error) {
	if word == "" || (word[0] < '0' || word[0] > '9') && word[0] != '.' {
		return 0, fmt.Errorf("%q is no time", word)
	}
	d, err := time.ParseDuration(word)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is no time: write one like 2s, 1.5s or 500ms", word)
	}
	return d, nil
}

// parseSpeed reads a speed: 4x is four times as fast, 0.5x half as fast.
func parseSpeed(word string) (float64, error) {
	if !strings.HasSuffix(word, "x") {
		return 0, fmt.Errorf("%q is no speed: write one like 2x or 0.5x", word)
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(word, "x"), 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%q is no speed: write one like 2x or 0.5x", word)
	}
	return v, nil
}
