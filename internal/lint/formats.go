package lint

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// The files a `type: jsonpath` rule reads, by extension: .yaml and .yml are
// YAML, .xml is XML, .csv is a CSV table, anything else is JSON. Each becomes
// the same tree of objects, arrays and scalars, with every value's position:
//
//   - YAML values are typed as Jackson's YAML reader types them (yes is
//     true, 0x1F is 31); every document of the file is a root.
//   - An XML document whose root is <dataset> is a flat XML dataset (DBUnit):
//     each child element is a row of the table it names, and its attributes
//     are the row's columns ({"public.closures": [{"id": ...}]}).
//   - Any other XML document reads as the xml fixture family writes it: the
//     root element's content is the tree, an attribute is an "@name" member,
//     a child element a member of its name (repeated elements an array), an
//     element with only text a string, and an element's text beside its
//     attributes "#text". A single element also matches an indexed path
//     (line[*].sku finds the one <line> as well as each of several).
//   - A CSV file is a dataset table in DBUnit's layout: the table is named
//     after the file (public.closures.csv), the first line names the columns,
//     and every other line is a row; values may be double-quoted, with \" and
//     \\ escaped, and an unquoted null is null.

// FormatError is a file a jsonpath rule cannot read: not valid YAML, XML or
// CSV, with the position when the reader knows it.
type FormatError struct {
	Format       string
	Line, Column int
	Message      string
}

func (e *FormatError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("invalid %s at line %d, column %d: %s", e.Format, e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("invalid %s: %s", e.Format, e.Message)
}

// document is a file read for a jsonpath rule.
type document struct {
	roots []*jsonNode
	// value is the first root as jsonx values, for the Jayway port.
	value func() (any, error)
	// xml reports a tree read from XML, where a lone element matches an
	// indexed path segment.
	xml bool
}

// readDocument reads content by the extension of name.
func readDocument(name, content string) (*document, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml":
		return readYAML(content)
	case ".xml":
		return readXML(content)
	case ".csv":
		return readCSV(name, content)
	}
	roots, err := parseJSON(content)
	if err != nil {
		return nil, err
	}
	return &document{roots: roots, value: func() (any, error) {
		return jsonx.Parse(strings.TrimPrefix(content, "\ufeff"))
	}}, nil
}

// ---- YAML ----

func readYAML(content string) (*document, error) {
	nodes, err := jyaml.UnmarshalNodes([]byte(content))
	if err != nil {
		var ye *jyaml.Error
		if errors.As(err, &ye) {
			return nil, &FormatError{Format: "YAML", Line: ye.Line, Column: ye.Column, Message: ye.Message}
		}
		return nil, &FormatError{Format: "YAML", Message: err.Error()}
	}
	d := &document{value: func() (any, error) { return jyaml.Unmarshal([]byte(content)) }}
	for _, n := range nodes {
		d.roots = append(d.roots, fromYAML(n))
	}
	return d, nil
}

func fromYAML(n *jyaml.Node) *jsonNode {
	out := &jsonNode{line: n.Line, col: n.Column}
	switch n.Kind {
	case jyaml.MappingNode:
		out.kind, out.keys = jsonObject, n.Keys
		for _, k := range n.Kids {
			out.kids = append(out.kids, fromYAML(k))
		}
	case jyaml.SequenceNode:
		out.kind = jsonArray
		for _, k := range n.Kids {
			out.kids = append(out.kids, fromYAML(k))
		}
	default:
		switch v := n.Value.(type) {
		case string:
			out.kind, out.text = jsonString, v
		case bool:
			out.kind, out.text = jsonBool, fmt.Sprint(v)
		case jsonx.Number:
			out.kind, out.text = jsonNumber, v.Text()
		default: // null, and !!binary, which is no value to compare
			out.kind = jsonNull
		}
	}
	return out
}

// ---- positions in text read by offset ----

// offsets turns byte offsets of a text into 1-based lines and columns,
// columns counted in characters like the JSON scanner's.
type offsets struct {
	s      string
	starts []int
}

func newOffsets(s string) *offsets {
	o := &offsets{s: s, starts: []int{0}}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			o.starts = append(o.starts, i+1)
		}
	}
	return o
}

func (o *offsets) position(off int) (line, col int) {
	lo, hi := 0, len(o.starts)-1
	for lo < hi { // the last line start <= off
		mid := (lo + hi + 1) / 2
		if o.starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1, utf8.RuneCountInString(o.s[o.starts[lo]:off]) + 1
}

func (o *offsets) node(kind jsonKind, text string, off int) *jsonNode {
	line, col := o.position(off)
	return &jsonNode{kind: kind, text: text, line: line, col: col}
}

// ---- XML ----

type xmlAttr struct {
	name, value string
	off         int
}

type xmlElem struct {
	name    string
	off     int
	attrs   []xmlAttr
	kids    []*xmlElem
	text    strings.Builder
	textOff int // where its first non-space text starts, -1 if none
}

func readXML(content string) (*document, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	pos := newOffsets(content)
	fail := func(off int, format string, args ...any) error {
		line, col := pos.position(off)
		return &FormatError{Format: "XML", Line: line, Column: col, Message: fmt.Sprintf(format, args...)}
	}
	dec := xml.NewDecoder(strings.NewReader(content))
	var root *xmlElem
	var stack []*xmlElem
	for {
		off := int(dec.InputOffset())
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var se *xml.SyntaxError
			if errors.As(err, &se) {
				return nil, &FormatError{Format: "XML", Line: se.Line, Message: se.Msg}
			}
			return nil, fail(off, "%v", err)
		}
		end := int(dec.InputOffset())
		switch t := tok.(type) {
		case xml.StartElement:
			e := &xmlElem{name: rawName(t.Name), off: off, textOff: -1}
			raw := content[off:end]
			for _, a := range t.Attr {
				e.attrs = append(e.attrs, xmlAttr{name: rawName(a.Name), value: a.Value, off: off + attrValueOffset(raw, rawName(a.Name))})
			}
			switch {
			case len(stack) > 0:
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, e)
			case root != nil:
				return nil, fail(off, "a second root element <%s>", e.name)
			default:
				root = e
			}
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != rawName(t.Name) {
				return nil, fail(off, "unexpected </%s>", rawName(t.Name))
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			e := stack[len(stack)-1]
			if e.textOff < 0 {
				if lead := len(content[off:end]) - len(strings.TrimLeft(content[off:end], " \t\r\n")); lead < end-off {
					e.textOff = off + lead
				}
			}
			e.text.Write(t)
		}
	}
	if len(stack) > 0 {
		return nil, fail(len(content), "unexpected end of input in <%s>", stack[len(stack)-1].name)
	}
	d := &document{xml: true}
	if root != nil {
		if root.name == "dataset" {
			d.roots = []*jsonNode{flatDataset(root, pos)}
		} else {
			d.roots = []*jsonNode{xmlContent(root, pos)}
		}
	}
	d.value = func() (any, error) {
		if len(d.roots) == 0 {
			return nil, nil
		}
		return toJSONX(d.roots[0]), nil
	}
	return d, nil
}

func rawName(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

// attrValueOffset is where the value of the attribute name starts in the raw
// start tag, or the tag's start when it cannot be found.
func attrValueOffset(raw, name string) int {
	for from := 0; ; {
		i := strings.Index(raw[from:], name)
		if i < 0 {
			return 0
		}
		i += from
		from = i + len(name)
		if i == 0 || !isXMLSpace(raw[i-1]) {
			continue
		}
		k := skipXMLSpace(raw, from)
		if k < len(raw) && raw[k] == '=' {
			if k = skipXMLSpace(raw, k+1); k < len(raw) && (raw[k] == '"' || raw[k] == '\'') {
				return k + 1
			}
		}
	}
}

func isXMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func skipXMLSpace(s string, i int) int {
	for i < len(s) && isXMLSpace(s[i]) {
		i++
	}
	return i
}

// flatDataset reads a DBUnit flat XML dataset: tables of rows.
func flatDataset(root *xmlElem, pos *offsets) *jsonNode {
	line, col := pos.position(root.off)
	out := &jsonNode{kind: jsonObject, line: line, col: col}
	tables := map[string]*jsonNode{}
	for _, rowElem := range root.kids {
		t, ok := tables[rowElem.name]
		if !ok {
			line, col := pos.position(rowElem.off)
			t = &jsonNode{kind: jsonArray, line: line, col: col}
			tables[rowElem.name] = t
			out.keys = append(out.keys, rowElem.name)
			out.kids = append(out.kids, t)
		}
		line, col := pos.position(rowElem.off)
		row := &jsonNode{kind: jsonObject, line: line, col: col}
		for _, a := range rowElem.attrs {
			row.keys = append(row.keys, a.name)
			row.kids = append(row.kids, pos.node(jsonString, a.value, a.off))
		}
		t.kids = append(t.kids, row)
	}
	return out
}

// xmlContent reads an element as the xml fixture family writes it.
func xmlContent(e *xmlElem, pos *offsets) *jsonNode {
	text := strings.TrimSpace(e.text.String())
	if len(e.attrs) == 0 && len(e.kids) == 0 {
		if text == "" {
			return pos.node(jsonNull, "", e.off)
		}
		return pos.node(jsonString, text, e.textOff)
	}
	line, col := pos.position(e.off)
	out := &jsonNode{kind: jsonObject, line: line, col: col}
	for _, a := range e.attrs {
		out.keys = append(out.keys, "@"+a.name)
		out.kids = append(out.kids, pos.node(jsonString, a.value, a.off))
	}
	var order []string
	byName := map[string][]*xmlElem{}
	for _, k := range e.kids {
		if _, ok := byName[k.name]; !ok {
			order = append(order, k.name)
		}
		byName[k.name] = append(byName[k.name], k)
	}
	for _, name := range order {
		elems := byName[name]
		out.keys = append(out.keys, name)
		if len(elems) == 1 {
			n := xmlContent(elems[0], pos)
			n.lone = true
			out.kids = append(out.kids, n)
			continue
		}
		line, col := pos.position(elems[0].off)
		list := &jsonNode{kind: jsonArray, line: line, col: col}
		for _, k := range elems {
			list.kids = append(list.kids, xmlContent(k, pos))
		}
		out.kids = append(out.kids, list)
	}
	if len(e.kids) == 0 && text != "" {
		out.keys = append(out.keys, "#text")
		out.kids = append(out.kids, pos.node(jsonString, text, e.textOff))
	}
	return out
}

// toJSONX is a tree of strings, objects and arrays as jsonx values.
func toJSONX(n *jsonNode) any {
	switch n.kind {
	case jsonObject:
		o := jsonx.NewObject()
		for i, k := range n.keys {
			o.Set(k, toJSONX(n.kids[i]))
		}
		return o
	case jsonArray:
		list := make([]any, len(n.kids))
		for i, k := range n.kids {
			list[i] = toJSONX(k)
		}
		return list
	case jsonString:
		return n.text
	}
	return nil
}

// ---- CSV ----

func readCSV(name, content string) (*document, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	pos := newOffsets(content)
	records, err := csvRecords(content, pos)
	if err != nil {
		return nil, err
	}
	table := filepath.Base(name)
	table = table[:len(table)-len(filepath.Ext(table))]
	rows := &jsonNode{kind: jsonArray, line: 1, col: 1}
	d := &document{roots: []*jsonNode{{kind: jsonObject, line: 1, col: 1, keys: []string{table}, kids: []*jsonNode{rows}}}}
	d.value = func() (any, error) { return toJSONX(d.roots[0]), nil }
	if len(records) == 0 {
		return d, nil
	}
	var header []string
	for _, c := range records[0] {
		header = append(header, strings.TrimSpace(c.text))
	}
	for _, rec := range records[1:] {
		if len(rec) != len(header) {
			line, _ := pos.position(rec[0].off)
			return nil, &FormatError{
				Format: "CSV", Line: line, Column: 1,
				Message: fmt.Sprintf("the row has %d values, the header names %d columns", len(rec), len(header)),
			}
		}
		line, col := pos.position(rec[0].off)
		row := &jsonNode{kind: jsonObject, line: line, col: col, keys: header}
		for _, c := range rec {
			kind := jsonString
			if c.null {
				kind = jsonNull
			}
			row.kids = append(row.kids, pos.node(kind, c.text, c.off))
		}
		rows.kids = append(rows.kids, row)
	}
	return d, nil
}

type csvCell struct {
	text string
	off  int
	null bool
}

// csvRecords splits DBUnit's CSV dialect into records, leaving out blank
// lines: comma-separated values, double-quoted values (across lines too)
// with \" and \\ escaped, and an unquoted null.
func csvRecords(s string, pos *offsets) ([][]csvCell, error) {
	var records [][]csvCell
	var rec []csvCell
	i := 0
	for i <= len(s) {
		if len(rec) == 0 && (i == len(s) || s[i] == '\n' || s[i] == '\r') {
			if i == len(s) {
				break
			}
			i++ // a blank line
			continue
		}
		cell := csvCell{off: i}
		if i < len(s) && s[i] == '"' {
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(s) {
					line, col := pos.position(i)
					return nil, &FormatError{Format: "CSV", Line: line, Column: col, Message: "a quoted value is never closed"}
				}
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == '"' {
					break
				}
				b.WriteByte(s[j])
				j++
			}
			cell.text, cell.off = b.String(), i+1
			i = j + 1
			for i < len(s) && s[i] != ',' && s[i] != '\n' && s[i] != '\r' {
				i++ // text after the closing quote
			}
		} else {
			j := i
			for j < len(s) && s[j] != ',' && s[j] != '\n' && s[j] != '\r' {
				j++
			}
			cell.text = s[i:j]
			cell.null = cell.text == "null"
			i = j
		}
		rec = append(rec, cell)
		if i < len(s) && s[i] == ',' {
			i++
			continue
		}
		records = append(records, rec) // the end of a line, or of the file
		rec = nil
		if i < len(s) && s[i] == '\r' {
			i++
		}
		if i < len(s) && s[i] == '\n' {
			i++
		}
		if i == len(s) {
			break
		}
	}
	return records, nil
}
