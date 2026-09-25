package lsp

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/match"
)

// The files that steps name: a step's {filepath} parameters, the file
// properties of its data table (its TableTypes), and the Examples cells that
// fill either in a Scenario Outline. Editors link them to the file, warn
// when a file is missing, and complete their paths.

// Value types that name files: a {filepath}, which the step reads, and a
// url, whose file: form names a file that may appear only during the run
// (a log the services write).
const (
	typeFilepath = "filepath"
	typeURL      = "url"
)

func isFileType(t string) bool { return t == typeFilepath || t == typeURL }

// fileValue is a value in a feature file that names a file.
type fileValue struct {
	line, start int // start is a byte offset in the line
	text        string
	typ         string
}

// fileValues finds the values of a document that name files, in document
// order, and the types of the Examples columns that fill such values, by
// block.
func (d *document) fileValues(reg *match.Registry) ([]fileValue, map[int]map[string]string) {
	columns := map[int]map[string]string{}
	if reg == nil {
		return nil, columns
	}
	fill := func(block int, placeholder, typ string) {
		if columns[block] == nil {
			columns[block] = map[string]string{}
		}
		columns[block][placeholder[1:len(placeholder)-1]] = typ
	}
	var out []fileValue
	seen := map[[2]int]bool{}
	add := func(v fileValue) {
		if at := [2]int{v.line, v.start}; !seen[at] {
			seen[at] = true
			out = append(out, v)
		}
	}
	for _, st := range d.steps {
		for _, m := range reg.Match(st.text) {
			for _, a := range m.Args {
				switch {
				case !a.Present || a.Param != typeFilepath:
				case isPlaceholder(a.Raw):
					fill(st.block, a.Raw, a.Param)
				default:
					add(fileValue{st.line, st.textStart + a.Start, a.Raw, a.Param})
				}
			}
		}
	}
	for _, t := range d.tables {
		st, ok := d.stepAt(t.step)
		if !ok {
			continue
		}
		for _, m := range reg.Match(st.text) {
			for _, r := range t.rows {
				c, typ := propertyValue(r, m.Def().Step.TableTypes)
				switch {
				case typ == "" || c.open || c.escaped || c.text == "":
				case isPlaceholder(c.text):
					fill(st.block, c.text, typ)
				default:
					add(fileValue{r.line, c.from, c.text, typ})
				}
			}
		}
	}
	for _, t := range d.tables {
		if t.step >= 0 || len(t.rows) < 2 {
			continue
		}
		for i, h := range t.rows[0].cells {
			typ := columns[t.block][h.text]
			if typ == "" || h.open {
				continue
			}
			for _, r := range t.rows[1:] {
				if i < len(r.cells) {
					if c := r.cells[i]; !c.open && !c.escaped && c.text != "" {
						add(fileValue{r.line, c.from, c.text, typ})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].start < out[j].start
	})
	return out, columns
}

// propertyValue is the value cell of a key/value row and its type, when the
// step's TableTypes give its key a type that names files.
func propertyValue(r row, types map[string]string) (cell, string) {
	if len(r.cells) < 2 || r.cells[0].open {
		return cell{}, ""
	}
	typ := types[r.cells[0].text]
	if !isFileType(typ) {
		return cell{}, ""
	}
	return r.cells[1], typ
}

// isPlaceholder reports whether a value is a whole Scenario Outline
// <placeholder>.
func isPlaceholder(v string) bool {
	return len(v) > 2 && v[0] == '<' && v[len(v)-1] == '>' && !strings.ContainsAny(v[1:len(v)-1], "<>")
}

// candidates are the paths a value may name, in the order the steps look:
// an absolute path, or the value under each resource root. A value that is
// another URL, or that is interpolated when the step runs, has none.
func (p *project) candidates(v string) []string {
	for _, prefix := range []string{"file://", "file:", "classpath:"} {
		if s, ok := strings.CutPrefix(v, prefix); ok {
			v = s
			break
		}
	}
	if v == "" || strings.Contains(v, "://") || strings.Contains(v, "${") {
		return nil
	}
	if filepath.IsAbs(v) {
		return []string{filepath.Clean(v)}
	}
	out := make([]string, 0, len(p.roots))
	for _, root := range p.roots {
		out = append(out, filepath.Join(root, filepath.FromSlash(v)))
	}
	return out
}

// find returns the first candidate that exists, as a step finds it.
func find(candidates []string) (string, os.FileInfo) {
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil {
			return c, info
		}
	}
	return "", nil
}

// rel is path relative to the project, when it is inside it.
func (p *project) rel(path string) string {
	if r, err := filepath.Rel(p.dir, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return path
}

// fileLink is a value that names an existing file.
type fileLink struct {
	rng  textRange
	path string
}

// fileLinks finds the values on line (every line, when line is negative)
// that name an existing file.
func (d *document) fileLinks(pr *project, line int) []fileLink {
	if pr == nil {
		return nil
	}
	values, _ := d.fileValues(pr.reg)
	var out []fileLink
	for _, v := range values {
		if line >= 0 && v.line != line {
			continue
		}
		if path, info := find(pr.candidates(v.text)); info != nil && info.Mode().IsRegular() {
			out = append(out, fileLink{rng: span(d.lines, v.line, v.start, v.start+len(v.text)), path: path})
		}
	}
	return out
}

// documentLinks are the links to the files a document names.
func (d *document) documentLinks(pr *project) []documentLink {
	links := d.fileLinks(pr, -1)
	out := make([]documentLink, 0, len(links))
	for _, l := range links {
		out = append(out, documentLink{Range: l.rng, Target: fileLocation(l.path, 0).URI, Tooltip: pr.rel(l.path)})
	}
	return out
}

// fileDiagnostics warn about the {filepath} values that name nothing: the
// step would fail. A url may name a file the run has yet to write, so it is
// not checked.
func (d *document) fileDiagnostics(p *project) []diagnostic {
	values, _ := d.fileValues(p.reg)
	var out []diagnostic
	for _, v := range values {
		if v.typ != typeFilepath {
			continue
		}
		cands := p.candidates(v.text)
		if len(cands) == 0 {
			continue
		}
		if path, _ := find(cands); path != "" {
			continue
		}
		msg := ""
		for _, c := range cands {
			if p.generated[c] {
				msg = fmt.Sprintf("%s does not exist yet: `axx fixtures generate` writes it.", v.text)
				break
			}
		}
		if msg == "" {
			looked := make([]string, len(cands))
			for i, c := range cands {
				looked[i] = p.rel(c)
			}
			msg = fmt.Sprintf("No file %s. Looked for %s.", v.text, strings.Join(looked, ", "))
		}
		out = append(out, diagnostic{
			Range: span(d.lines, v.line, v.start, v.start+len(v.text)), Severity: severityWarning,
			Code: "missing-file", Source: "axx", Message: msg,
		})
	}
	return out
}

// names reports whether a document names path: whether a file appearing or
// disappearing there changes what the document's values name.
func (d *document) names(p *project, path string) bool {
	values, _ := d.fileValues(p.reg)
	for _, v := range values {
		for _, c := range p.candidates(v.text) {
			if c == path {
				return true
			}
		}
	}
	return false
}

// pathCompletions completes the path of a value that names a file at the
// cursor: in a step, the {filepath} parameter being typed; in a table, a
// file property's value or an Examples cell that fills one. It returns false
// when the cursor is in no such value.
func (d *document) pathCompletions(pr *project, pos position) ([]completionItem, bool) {
	if pr.reg == nil || pos.Line >= len(d.lines) {
		return nil, false
	}
	l := d.lines[pos.Line]
	cursor := byteOffset(l, pos.Character)
	if st, ok := d.stepAt(pos.Line); ok {
		if cursor < st.textStart || !inFileSlot(pr.reg, l[st.textStart:cursor]) {
			return nil, false
		}
		from := st.textStart + strings.LastIndexAny(l[st.textStart:cursor], " \t") + 1
		end := cursor
		for end < len(l) && l[end] != ' ' && l[end] != '\t' {
			end++
		}
		// The edit covers the step text, as step completions do, so editors
		// filter both kinds the same way.
		return d.paths(pr, typeFilepath, pos.Line, st.textStart, from, cursor, end), true
	}
	t, r, i := d.cellAt(pos.Line, cursor)
	if t == nil {
		return nil, false
	}
	typ := d.cellType(pr.reg, t, r, i)
	if typ == "" {
		return nil, false
	}
	c := r.cells[i]
	from, end := cursor, cursor
	switch {
	case c.text == "" || cursor < c.from:
	case cursor <= c.to:
		from, end = c.from, c.to
	default:
		return nil, false // after the value, in the cell's trailing space
	}
	return d.paths(pr, typ, pos.Line, from, from, cursor, end), true
}

// inFileSlot reports whether the word being typed at the end of typed step
// text is a {filepath} parameter of a step it can become.
func inFileSlot(reg *match.Registry, typed string) bool {
	got := typedWords(typed)
	at := len(got)
	if typed != "" && !strings.HasSuffix(typed, " ") && !strings.HasSuffix(typed, "\t") {
		at--
	}
	for _, v := range reg.Variants() {
		if ok, _ := continues(v.Expr, typed); !ok {
			continue
		}
		if words := strings.Fields(v.Expr); at >= 0 && at < len(words) && words[at] == "{"+typeFilepath+"}" {
			return true
		}
	}
	return false
}

// cellAt finds the table cell at the cursor.
func (d *document) cellAt(line, cursor int) (*table, *row, int) {
	for ti := range d.tables {
		t := &d.tables[ti]
		for ri := range t.rows {
			r := &t.rows[ri]
			if r.line != line {
				continue
			}
			for i, c := range r.cells {
				if c.innerFrom <= cursor && cursor <= c.innerTo {
					return t, r, i
				}
			}
		}
	}
	return nil, nil, 0
}

// cellType is the type of a cell's value when it names files: a file
// property's value, or an Examples cell of a column that fills a file value.
func (d *document) cellType(reg *match.Registry, t *table, r *row, i int) string {
	if t.step >= 0 {
		st, ok := d.stepAt(t.step)
		if i != 1 || !ok {
			return ""
		}
		for _, m := range reg.Match(st.text) {
			if _, typ := propertyValue(*r, m.Def().Step.TableTypes); typ != "" {
				return typ
			}
		}
		return ""
	}
	header := t.rows[0]
	if r.line == header.line || i >= len(header.cells) || header.cells[i].open {
		return ""
	}
	_, columns := d.fileValues(reg)
	return columns[t.block][header.cells[i].text]
}

// paths lists the files and directories that complete the value from
// valueFrom to the cursor: those in the directory typed so far, under each
// resource root, whose names start with the rest. Each item replaces the
// line from editFrom to end.
func (d *document) paths(pr *project, typ string, line, editFrom, valueFrom, cursor, end int) []completionItem {
	l := d.lines[line]
	typed := l[valueFrom:cursor]
	scheme := ""
	for _, s := range []string{"file://", "file:", "classpath:"} {
		if strings.HasPrefix(typed, s) {
			scheme = s
			break
		}
	}
	if typ == typeURL && !strings.HasPrefix(scheme, "file:") {
		return []completionItem{} // a url names a file only as a file: URL
	}
	dir, partial := path.Split(typed[len(scheme):])
	if strings.Contains(dir, "://") || strings.Contains(typed, "${") {
		return []completionItem{}
	}
	var bases []string
	if filepath.IsAbs(filepath.FromSlash(dir)) {
		bases = []string{filepath.FromSlash(dir)}
	} else {
		for _, root := range pr.roots {
			bases = append(bases, filepath.Join(root, filepath.FromSlash(dir)))
		}
	}
	edit := span(d.lines, line, editFrom, end)
	before := l[editFrom:valueFrom] + scheme + dir
	items := []completionItem{}
	seen := map[string]bool{}
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			hidden := strings.HasPrefix(name, ".") && !strings.HasPrefix(partial, ".")
			if seen[name] || hidden || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(partial)) {
				continue
			}
			seen[name] = true
			isDir := e.IsDir()
			if e.Type()&os.ModeSymlink != 0 {
				if info, err := os.Stat(filepath.Join(base, name)); err == nil {
					isDir = info.IsDir()
				}
			}
			label, kind, order := name, completionKindFile, "1"
			if isDir {
				label, kind, order = name+"/", completionKindFolder, "0"
			}
			items = append(items, completionItem{
				Label: label, Kind: kind, Detail: pr.rel(filepath.Join(base, name)),
				FilterText: before + label, SortText: order + name, InsertTextFormat: insertFormatPlain,
				TextEdit: textEdit{Range: edit, NewText: before + label},
			})
			if len(items) == maxCompletions {
				return items
			}
		}
	}
	return items
}
