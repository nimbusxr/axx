// Package featurefmt lays out Gherkin feature files the one way axx does,
// for `axx fmt` and the language server's formatting. It changes where
// things sit (indentation, the columns of tables, blank lines, the spaces
// between words), never what a feature says: it parses what it writes with
// Cucumber's parser, and refuses a layout whose document differs from the
// one it read.
//
// The layout is the one axx's own features have: the Feature at the left
// edge; a Background, Scenario or Rule two spaces in, and what a Rule holds
// two more; steps and Examples two spaces in from their scenario; a step's
// table or doc string, and an Examples table, two more. Tags sit on their own
// lines at the indent of what they tag, a description two spaces in from
// its keyword, and a comment at the indent of the line it is above (or, with
// a blank line after it, of the line it follows). A table's columns line up
// by how wide their text shows, wide characters counting twice. One blank
// line comes before each Background, Scenario, Rule and Examples (and the
// tags and comments above it); the author's single blank lines stay
// elsewhere, but never after tags or before a table or doc string, and the
// file ends with one line break. Keywords stay as written, in any language,
// and so does the text of steps, names, cells (with their escapes), comments
// and doc strings, whose content moves only with its delimiter.
package featurefmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
	"golang.org/x/text/width"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
)

// Codes of the errors Format returns.
const (
	// CodeParse is a feature that does not parse: it is not laid out.
	CodeParse = feature.CodeParse
	// CodeChanged is a layout that would read differently from the
	// feature, which Format refuses: a bug of axx's.
	CodeChanged = "AXX-E0208"
)

// Options are a project's choices in the layout: axx.yaml's fmt section.
// The zero value is axx's layout.
type Options struct {
	// Indent is the spaces of one level of indentation: 2 when it is 0.
	Indent int
}

// unit is one level of indentation.
func (o Options) unit() int {
	if o.Indent > 0 {
		return o.Indent
	}
	return 2
}

// Format returns src laid out as axx lays out features, with o's choices.
// name is the file's name in errors. A feature that does not parse is an
// *axxerr.Error naming its line (CodeParse), and so is a layout that would
// not read the same as src (CodeChanged).
func Format(name string, src []byte, o Options) ([]byte, error) {
	doc, err := parse(src)
	if err != nil {
		return nil, parseError(name, err)
	}
	out, err := layout(src, doc, o.unit())
	if err != nil {
		return nil, changed(name, err.Error())
	}
	outDoc, err := parse(out)
	if err != nil {
		return nil, changed(name, "what it would write does not parse: "+firstLine(err.Error()))
	}
	if diff := compare(doc, outDoc); diff != "" {
		return nil, changed(name, diff)
	}
	again, err := layout(out, outDoc, o.unit())
	if err != nil || !bytes.Equal(again, out) {
		return nil, changed(name, "laying it out again would change it again")
	}
	return out, nil
}

func parse(src []byte) (*messages.GherkinDocument, error) {
	ids := &messages.Incrementing{}
	return gherkin.ParseGherkinDocument(bytes.NewReader(src), ids.NewId)
}

var parserError = regexp.MustCompile(`^\((\d+):(\d+)\): (.*)$`)

// parseError names the lines of what the parser could not read: the first
// is the error's location.
func parseError(name string, err error) error {
	var msgs []string
	line, col := 0, 0
	for _, l := range strings.Split(err.Error(), "\n") {
		m := parserError.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		ln, _ := strconv.Atoi(m[1])
		cn, _ := strconv.Atoi(m[2])
		if line == 0 {
			line, col = ln, cn
		}
		msgs = append(msgs, fmt.Sprintf("line %d: %s", ln, m[3]))
	}
	const hint = "fix the Gherkin syntax at the line named (`axx validate` reports the same); see https://cucumber.io/docs/gherkin/reference/"
	switch len(msgs) {
	case 0:
		return axxerr.New(CodeParse, exitcode.Usage, "%s does not parse, so it is not formatted: %s", name, err.Error()).WithHint(hint)
	case 1:
		_, msg, _ := strings.Cut(msgs[0], ": ")
		return axxerr.New(CodeParse, exitcode.Usage, "%s (the feature does not parse, so it is not formatted)", msg).
			At(name, line, col).WithHint(hint)
	}
	return axxerr.New(CodeParse, exitcode.Usage, "the feature does not parse, so it is not formatted:\n  %s", strings.Join(msgs, "\n  ")).
		At(name, line, col).WithHint(hint)
}

func changed(name, why string) error {
	return axxerr.New(CodeChanged, exitcode.Usage, "formatting %s would change what it says (%s), so it is left as it is", name, why).
		WithHint("this is a bug in axx: please report it at https://github.com/nimbusxr/axx/issues with the feature file; until it is fixed, format the file by hand")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

type kind int

const (
	unset kind = iota
	blank
	comment
	language
	tagLine
	header
	step
	row
	docOpen
	docBody
	docClose
	desc
)

// line is one line of the feature, as it is laid out.
type line struct {
	kind   kind
	indent int
	// text is the line without its indent; a row's is its cells.
	text  string
	cells []string
	table int // a row's table, from 1
	// block marks the line of a Background, Scenario, Rule or Examples,
	// which has a blank line before it and what comes with it.
	block bool
	// first is a header's first line: its first tag's (from 1), or its own.
	first int
}

type builder struct {
	// unit is one level of indentation.
	unit   int
	src    []string
	lines  []line
	tables int
	err    error
}

// layout lays out src, whose document is doc, unit spaces to a level.
func layout(src []byte, doc *messages.GherkinDocument, unit int) ([]byte, error) {
	eol := "\n"
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		eol = "\r\n"
	}
	b := &builder{unit: unit, src: splitLines(src)}
	b.lines = make([]line, len(b.src))
	for _, c := range doc.Comments {
		l := int(c.Location.Line)
		b.mark(l, line{kind: comment, text: trimText(b.at(l))})
	}
	if doc.Feature != nil {
		b.feature(doc.Feature)
	}
	if b.err != nil {
		return nil, b.err
	}
	for i := range b.lines {
		if b.lines[i].kind != unset {
			continue
		}
		t := strings.TrimLeft(b.src[i], " \t")
		switch {
		case t == "":
			b.lines[i].kind = blank
		case languageLine.MatchString(t):
			b.lines[i] = line{kind: language, text: trimText(t)}
		default:
			return nil, fmt.Errorf("line %d is not read as any part of the feature", i+1)
		}
	}
	b.indentComments()
	return b.emit(eol), nil
}

// splitLines splits src into lines as the parser reads them: at line feeds,
// without a carriage return before one, and with no line after a last line
// feed.
func splitLines(src []byte) []string {
	s := string(src)
	s = strings.TrimSuffix(s, "\n")
	if s == "" && len(src) == 0 {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

var languageLine = regexp.MustCompile(`^\s*#\s*language\s*:\s*[a-zA-Z\-_]+\s*$`)

func (b *builder) at(n int) string {
	if n < 1 || n > len(b.src) {
		return ""
	}
	return b.src[n-1]
}

func (b *builder) fail(format string, args ...any) {
	if b.err == nil {
		b.err = fmt.Errorf(format, args...)
	}
}

// mark says what line n (from 1) is.
func (b *builder) mark(n int, l line) {
	if n < 1 || n > len(b.lines) {
		b.fail("line %d is past the end of the file", n)
		return
	}
	if b.lines[n-1].kind != unset {
		b.fail("line %d is read as two parts of the feature", n)
		return
	}
	b.lines[n-1] = l
}

func (b *builder) feature(f *messages.Feature) {
	b.tags(f.Tags, 0)
	b.header(f.Location, 0, f.Keyword, f.Name, false, f.Tags)
	b.description(f.Location, f.Description, 0)
	for _, c := range f.Children {
		b.child(c.Background, c.Scenario, c.Rule, b.unit)
	}
}

func (b *builder) child(bg *messages.Background, sc *messages.Scenario, r *messages.Rule, indent int) {
	switch {
	case bg != nil:
		b.header(bg.Location, indent, bg.Keyword, bg.Name, true, nil)
		b.description(bg.Location, bg.Description, indent)
		b.steps(bg.Steps, indent+b.unit)
	case sc != nil:
		b.tags(sc.Tags, indent)
		b.header(sc.Location, indent, sc.Keyword, sc.Name, true, sc.Tags)
		b.description(sc.Location, sc.Description, indent)
		b.steps(sc.Steps, indent+b.unit)
		for _, ex := range sc.Examples {
			in := indent + b.unit
			b.tags(ex.Tags, in)
			b.header(ex.Location, in, ex.Keyword, ex.Name, true, ex.Tags)
			b.description(ex.Location, ex.Description, in)
			var rows []*messages.TableRow
			if ex.TableHeader != nil {
				rows = append(rows, ex.TableHeader)
			}
			b.table(append(rows, ex.TableBody...), in+b.unit)
		}
	case r != nil:
		b.tags(r.Tags, indent)
		b.header(r.Location, indent, r.Keyword, r.Name, true, r.Tags)
		b.description(r.Location, r.Description, indent)
		for _, c := range r.Children {
			b.child(c.Background, c.Scenario, nil, indent+b.unit)
		}
	}
}

func (b *builder) header(loc *messages.Location, indent int, keyword, name string, block bool, tags []*messages.Tag) {
	text := keyword + ":"
	if name != "" {
		text += " " + name
	}
	first := int(loc.Line)
	for _, t := range tags {
		first = min(first, int(t.Location.Line))
	}
	b.mark(int(loc.Line), line{kind: header, indent: indent, text: text, block: block, first: first})
}

var tagComment = regexp.MustCompile(`\s+#`)

// tags lays out an element's tags, each line of them as one line, with
// the comment one may end with.
func (b *builder) tags(tags []*messages.Tag, indent int) {
	var lines []int
	names := map[int][]string{}
	for _, t := range tags {
		l := int(t.Location.Line)
		if _, ok := names[l]; !ok {
			lines = append(lines, l)
		}
		names[l] = append(names[l], t.Name)
	}
	for _, l := range lines {
		text := strings.Join(names[l], " ")
		src := strings.TrimLeft(b.at(l), " \t")
		if loc := tagComment.FindStringIndex(src); loc != nil {
			text += " " + trimText(src[loc[0]:])
		}
		b.mark(l, line{kind: tagLine, indent: indent, text: text})
	}
}

// description lays out the description below the line at loc: its lines
// keep their indentation among themselves, two spaces in from the keyword.
func (b *builder) description(loc *messages.Location, d string, indent int) {
	if d == "" {
		return
	}
	want := strings.Split(d, "\n")
	i := int(loc.Line) // the index of the line after the keyword's
	for i < len(b.src) && isEmpty(b.src[i]) {
		i++
	}
	for j, w := range want {
		if i+j >= len(b.src) || b.src[i+j] != w {
			b.fail("the description below line %d is not where the parser read it", loc.Line)
			return
		}
	}
	for j, t := range dedent(want) {
		b.mark(i+j+1, line{kind: desc, indent: indent + b.unit, text: t})
	}
}

func isEmpty(s string) bool { return strings.TrimLeft(s, " \t") == "" }

// dedent takes the indentation all lines share off them, and the spaces at
// their ends.
func dedent(lines []string) []string {
	prefix, first := "", true
	for _, l := range lines {
		if isEmpty(l) {
			continue
		}
		ws := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			prefix, first = ws, false
			continue
		}
		n := 0
		for n < len(prefix) && n < len(ws) && prefix[n] == ws[n] {
			n++
		}
		prefix = prefix[:n]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if isEmpty(l) {
			continue
		}
		out[i] = strings.TrimRight(l[len(prefix):], " \t")
	}
	return out
}

func (b *builder) steps(steps []*messages.Step, indent int) {
	for _, st := range steps {
		// A step's keyword ends with its space (in the languages that
		// have one), so a step with no text keeps it.
		b.mark(int(st.Location.Line), line{kind: step, indent: indent, text: st.Keyword + st.Text})
		if st.DataTable != nil {
			b.table(st.DataTable.Rows, indent+b.unit)
		}
		if st.DocString != nil {
			b.docString(st.DocString, indent+b.unit)
		}
	}
}

func (b *builder) table(rows []*messages.TableRow, indent int) {
	if len(rows) == 0 {
		return
	}
	b.tables++
	for _, r := range rows {
		l := int(r.Location.Line)
		cells := rawCells(b.at(l))
		if len(cells) != len(r.Cells) {
			b.fail("the row on line %d has %d cells, not the %d the parser read", l, len(cells), len(r.Cells))
			return
		}
		b.mark(l, line{kind: row, indent: indent, cells: cells, table: b.tables})
	}
}

// rawCells returns a row's cells as written, escapes and all, without the
// spaces the parser trims from them.
func rawCells(s string) []string {
	t := strings.TrimLeft(s, " \t")
	if !strings.HasPrefix(t, "|") {
		return nil
	}
	var cells []string
	var cell strings.Builder
	for i := 1; i < len(t); {
		r, w := utf8.DecodeRuneInString(t[i:])
		i += w
		switch r {
		case '|':
			cells = append(cells, trimCell(cell.String()))
			cell.Reset()
		case '\\':
			cell.WriteRune(r)
			if i < len(t) {
				r, w = utf8.DecodeRuneInString(t[i:])
				i += w
				cell.WriteRune(r)
			}
		default:
			cell.WriteRune(r)
		}
	}
	return cells
}

// trimCell trims a cell as the parser does: every space but a line feed.
func trimCell(s string) string {
	space := func(r rune) bool { return unicode.IsSpace(r) && r != '\n' }
	return strings.TrimRightFunc(strings.TrimLeftFunc(s, space), space)
}

// docString lays out a doc string: its delimiters at indent, and its
// content as the parser reads it, moved with them.
func (b *builder) docString(ds *messages.DocString, indent int) {
	o := int(ds.Location.Line) - 1
	if o < 0 || o >= len(b.src) {
		b.fail("the doc string of line %d is past the end of the file", ds.Location.Line)
		return
	}
	open := strings.TrimLeft(b.src[o], " \t")
	if !strings.HasPrefix(open, ds.Delimiter) {
		b.fail("the doc string of line %d does not start there", ds.Location.Line)
		return
	}
	// The parser removes, from each line of the content, the spaces before
	// the delimiter that opens it.
	strip := len(b.src[o]) - len(open)
	c := o + 1
	for c < len(b.src) && !strings.HasPrefix(strings.TrimLeft(b.src[c], " \t"), ds.Delimiter) {
		c++
	}
	if c == len(b.src) {
		b.fail("the doc string of line %d does not end", ds.Location.Line)
		return
	}
	b.mark(o+1, line{kind: docOpen, indent: indent, text: ds.Delimiter + strings.TrimRight(open[len(ds.Delimiter):], " \t")})
	for k := o + 1; k < c; k++ {
		l := b.src[k]
		var text string
		if lead := len(l) - len(strings.TrimLeft(l, " ")); lead > strip {
			text = l[strip:]
		} else {
			text = strings.TrimLeft(l, " ")
		}
		b.mark(k+1, line{kind: docBody, indent: indent, text: text})
	}
	b.mark(c+1, line{kind: docClose, indent: indent, text: trimText(b.src[c])})
}

func trimText(s string) string { return strings.Trim(s, " \t") }

// structural is a line that is part of the feature's structure: not blank,
// not a comment.
func structural(k kind) bool { return k != blank && k != comment && k != language }

// indentComments puts each run of comments at the indent of the line below
// it, or, when a blank line or the end of the file follows the run, of the
// line above it.
func (b *builder) indentComments() {
	for i := 0; i < len(b.lines); {
		if b.lines[i].kind != comment {
			i++
			continue
		}
		end := i
		for end < len(b.lines) && b.lines[end].kind == comment {
			end++
		}
		indent := -1
		if end < len(b.lines) && structural(b.lines[end].kind) {
			indent = b.lines[end].indent
		} else {
			for j := i - 1; j >= 0; j-- {
				if structural(b.lines[j].kind) {
					indent = b.lines[j].indent
					break
				}
			}
		}
		if indent < 0 {
			indent = 0
		}
		for j := i; j < end; j++ {
			b.lines[j].indent = indent
		}
		i = end
	}
}

// emit writes the lines out, with the blank lines the layout has.
func (b *builder) emit(eol string) []byte {
	n := len(b.lines)
	// One blank line before each block, and its tags and the comments
	// right above them; none among its tags and comments.
	blankBefore := make([]bool, n)
	glued := make([]bool, n)
	for i, l := range b.lines {
		if l.kind != header {
			continue
		}
		p := l.first - 1
		for j := p + 1; j <= i; j++ {
			glued[j] = true
		}
		for p > 0 && (b.lines[p-1].kind == tagLine || b.lines[p-1].kind == comment) {
			p--
		}
		blankBefore[p] = l.block
	}
	// next[i] is the first structural line from i on, or n.
	next := make([]int, n+1)
	next[n] = n
	for i := n - 1; i >= 0; i-- {
		if structural(b.lines[i].kind) {
			next[i] = i
		} else {
			next[i] = next[i+1]
		}
	}
	widths := b.widths()
	var out []string
	last := unset // the last structural line written
	pending := false
	for i, l := range b.lines {
		if l.kind == blank {
			pending = len(out) > 0
			continue
		}
		need := blankBefore[i] && len(out) > 0
		if pending {
			// The author's blank line stays, but never after tags, nor
			// before a table or a doc string, which go with what is above.
			nk := unset
			if next[i] < n {
				nk = b.lines[next[i]].kind
			}
			if last != tagLine && nk != row && nk != docOpen && !glued[i] {
				need = true
			}
			pending = false
		}
		if need && (len(out) == 0 || out[len(out)-1] != "") {
			out = append(out, "")
		}
		out = append(out, b.render(l, widths))
		if structural(l.kind) {
			last = l.kind
		}
	}
	if len(out) == 0 {
		return nil
	}
	return []byte(strings.Join(out, eol) + eol)
}

func (b *builder) widths() map[int][]int {
	w := map[int][]int{}
	for _, l := range b.lines {
		if l.kind != row {
			continue
		}
		cols := w[l.table]
		for len(cols) < len(l.cells) {
			cols = append(cols, 0)
		}
		for c, cell := range l.cells {
			cols[c] = max(cols[c], Width(cell))
		}
		w[l.table] = cols
	}
	return w
}

func (b *builder) render(l line, widths map[int][]int) string {
	pad := strings.Repeat(" ", l.indent)
	if l.kind == row {
		if len(l.cells) == 0 {
			return pad + "|"
		}
		var s strings.Builder
		s.WriteString(pad + "|")
		for c, cell := range l.cells {
			s.WriteString(" " + cell + strings.Repeat(" ", widths[l.table][c]-Width(cell)) + " |")
		}
		return s.String()
	}
	if l.text == "" {
		return ""
	}
	return pad + l.text
}

// Width is how many columns s takes in a monospaced font: two for a wide
// or full-width character (CJK, most emoji), none for a combining mark or
// a zero-width one.
func Width(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		case width.LookupRune(r).Kind() == width.EastAsianWide || width.LookupRune(r).Kind() == width.EastAsianFullwidth:
			n += 2
		default:
			n++
		}
	}
	return n
}

// compare says where two documents read differently, or "" when they read
// the same: the same keywords, names, steps, tags, tables, doc strings and
// comments, wherever they are.
func compare(a, b *messages.GherkinDocument) string {
	na, nb := normalize(a), normalize(b)
	if reflect.DeepEqual(na, nb) {
		return ""
	}
	if p := firstDifference("", na, nb); p != "" {
		return "they differ at " + p
	}
	return "they differ"
}

// normalize is a document without what layout changes: locations, ids,
// a description's shared indentation and line ends, and spaces after a doc
// string's media type and around a comment.
func normalize(doc *messages.GherkinDocument) any {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err.Error()
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err.Error()
	}
	return clean(v)
}

func clean(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			switch k {
			case "location", "id", "uri":
				delete(t, k)
			case "description":
				if s, ok := x.(string); ok {
					t[k] = strings.Join(dedent(strings.Split(s, "\n")), "\n")
				}
			case "mediaType":
				if s, ok := x.(string); ok {
					t[k] = strings.TrimRight(s, " \t")
				}
			case "comments":
				var texts []string
				if list, ok := x.([]any); ok {
					for _, c := range list {
						if m, ok := c.(map[string]any); ok {
							s, _ := m["text"].(string)
							texts = append(texts, trimText(s))
						}
					}
				}
				t[k] = texts
			default:
				t[k] = clean(x)
			}
		}
	case []any:
		for i := range t {
			t[i] = clean(t[i])
		}
	}
	return v
}

func firstDifference(path string, a, b any) string {
	switch ta := a.(type) {
	case map[string]any:
		tb, ok := b.(map[string]any)
		if !ok {
			return orRoot(path)
		}
		keys := map[string]bool{}
		for k := range ta {
			keys[k] = true
		}
		for k := range tb {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		slices.Sort(sorted)
		for _, k := range sorted {
			if p := firstDifference(path+"."+k, ta[k], tb[k]); p != "" {
				return p
			}
		}
		return ""
	case []any:
		tb, ok := b.([]any)
		if !ok || len(ta) != len(tb) {
			return orRoot(path)
		}
		for i := range ta {
			if p := firstDifference(fmt.Sprintf("%s[%d]", path, i), ta[i], tb[i]); p != "" {
				return p
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(a, b) {
			return orRoot(path)
		}
		return ""
	}
}

func orRoot(p string) string {
	if p == "" {
		return "the document"
	}
	return strings.TrimPrefix(p, ".")
}
