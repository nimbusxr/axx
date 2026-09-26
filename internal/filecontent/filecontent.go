// Package filecontent reads what a file holds, by its type: the text of a
// plain text, CSV, TSV, JSON, XML, HTML, PDF, Word (.docx) or Excel (.xlsx)
// file, and the table of a CSV, TSV or Excel file. The content checks of
// the storage and files packs use it, so the text of a PDF or of a
// workbook counts as the file's text.
package filecontent

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"path"
	"slices"
	"strings"
)

// Readable lists the files whose text the package reads, for messages.
const Readable = "text, CSV, TSV, JSON, XML, HTML, PDF, Word .docx, Excel .xlsx"

// Kind is a type of file.
type Kind string

// The kinds of file the package reads, and Binary for the others.
const (
	Text   Kind = "text"
	CSV    Kind = "CSV"
	TSV    Kind = "TSV"
	JSON   Kind = "JSON"
	XML    Kind = "XML"
	HTML   Kind = "HTML"
	PDF    Kind = "PDF"
	Word   Kind = "Word"
	Excel  Kind = "Excel"
	Binary Kind = "binary"
)

// Noun names a file of the kind, for messages: "a PDF", "an Excel workbook".
func (k Kind) Noun() string {
	switch k {
	case Text:
		return "a text file"
	case CSV, TSV:
		return "a " + string(k) + " file"
	case JSON, XML, HTML:
		return "an " + string(k) + " file"
	case PDF:
		return "a PDF"
	case Word:
		return "a Word document"
	case Excel:
		return "an Excel workbook"
	}
	return "a binary file"
}

// File is a file's content and what tells its type: its name (by its
// extension) and, when it is known, its media type.
type File struct {
	Name      string
	MediaType string
	Body      []byte
}

var extensions = map[string]Kind{
	".txt": Text, ".text": Text, ".log": Text, ".md": Text,
	".csv": CSV, ".tsv": TSV, ".tab": TSV,
	".json": JSON,
	".xml":  XML,
	".html": HTML, ".htm": HTML, ".xhtml": HTML,
	".pdf":  PDF,
	".docx": Word, ".docm": Word,
	".xlsx": Excel, ".xlsm": Excel,
}

var mediaTypes = map[string]Kind{
	"text/plain":                Text,
	"text/csv":                  CSV,
	"application/csv":           CSV,
	"text/tab-separated-values": TSV,
	"application/json":          JSON,
	"text/json":                 JSON,
	"application/xml":           XML,
	"text/xml":                  XML,
	"text/html":                 HTML,
	"application/xhtml+xml":     HTML,
	"application/pdf":           PDF,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": Word,
	"application/vnd.ms-word.document.macroEnabled.12":                        Word,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       Excel,
	"application/vnd.ms-excel.sheet.macroEnabled.12":                          Excel,
}

// Kind is the file's type: by its extension, else by its media type, else
// by its content.
func (f File) Kind() Kind {
	if k, ok := extensions[strings.ToLower(path.Ext(f.Name))]; ok {
		return k
	}
	if mt, _, err := mime.ParseMediaType(f.MediaType); err == nil {
		if k, ok := mediaTypes[mt]; ok {
			return k
		}
		switch {
		case strings.HasSuffix(mt, "+json"):
			return JSON
		case strings.HasSuffix(mt, "+xml"):
			return XML
		case strings.HasPrefix(mt, "text/"):
			return Text
		}
	}
	return sniff(f.Body)
}

// sniff tells a file's type by its content.
func sniff(b []byte) Kind {
	if bytes.Contains(b[:min(len(b), 1024)], []byte("%PDF-")) {
		return PDF
	}
	if bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		if zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err == nil {
			for _, f := range zr.File {
				switch f.Name {
				case "word/document.xml":
					return Word
				case "xl/workbook.xml":
					return Excel
				}
			}
		}
		return Binary
	}
	if !isText(b) {
		return Binary
	}
	s := strings.TrimSpace(decode(b))
	switch {
	case (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) && json.Valid([]byte(s)):
		return JSON
	case strings.HasPrefix(s, "<"):
		head := strings.ToLower(s[:min(len(s), 512)])
		if strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html") {
			return HTML
		}
		return XML
	}
	return Text
}

// Text is the text the file holds: the text itself for plain text, CSV,
// TSV and JSON; the text content of XML and HTML; the text of a PDF's
// pages; the paragraphs and tables of a Word document; the cells of every
// sheet of an Excel workbook, as it shows them.
func (f File) Text() (string, error) {
	switch k := f.Kind(); k {
	case XML:
		return xmlText(f.Body)
	case HTML:
		return htmlText(f.Body)
	case PDF:
		return pdfText(f.Body)
	case Word:
		return wordText(f.Body)
	case Excel:
		return excelText(f.Body)
	case Binary:
		return "", fmt.Errorf("it is %s (%d bytes), not a file whose text axx reads (%s)", k.Noun(), len(f.Body), Readable)
	}
	return decode(f.Body), nil
}

// Contains reports whether the file's text holds want, and returns the
// text. Runs of spaces and line breaks count as one space, in the text and
// in want; case matters. The markup of XML and HTML counts too.
func (f File) Contains(want string) (bool, string, error) {
	text, err := f.Text()
	if err != nil {
		return false, "", err
	}
	w := Collapse(want)
	if strings.Contains(Collapse(text), w) {
		return true, text, nil
	}
	if k := f.Kind(); k == XML || k == HTML {
		return strings.Contains(Collapse(decode(f.Body)), w), text, nil
	}
	return false, text, nil
}

// Collapse turns every run of spaces and line breaks into one space and
// trims the ends.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Nearest explains, for a failure report, what text holds instead of want:
// the (up to 3) lines that have the most in common with it, or else the
// start of the text.
func Nearest(text, want string) string {
	var lines []string
	for l := range strings.SplitSeq(text, "\n") {
		if l = Collapse(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return "It has no text."
	}
	all := len(lines)
	lines = lines[:min(all, 10000)] // enough to find the nearest in
	if near := nearest(lines, Collapse(want)); len(near) > 0 {
		return "Its text nearest that:\n  " + strings.Join(near, "\n  ")
	}
	var b strings.Builder
	b.WriteString("Its text begins:")
	for i, l := range lines {
		if i == 10 || b.Len() > 600 {
			fmt.Fprintf(&b, "\n  … (%d lines in all)", all)
			break
		}
		b.WriteString("\n  " + shorten(l, 200))
	}
	return b.String()
}

// nearest are the (up to 3) lines that have the most in common with want:
// the longest run of the same text, at least a third of it.
func nearest(lines []string, want string) []string {
	type scored struct {
		line  string
		score int
	}
	var found []scored
	least := max(4, len([]rune(want))/3)
	for _, l := range lines {
		if n := commonRun(l, want); n >= least {
			found = append(found, scored{l, n})
		}
	}
	// Equally near lines keep the file's order.
	slices.SortStableFunc(found, func(a, b scored) int { return b.score - a.score })
	var out []string
	for _, f := range found {
		if len(out) == 3 {
			break
		}
		if l := shorten(f.line, 200); !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// commonRun is the length of the longest text a and b have in common.
func commonRun(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	ra, rb = ra[:min(len(ra), 500)], rb[:min(len(rb), 200)]
	prev, cur := make([]int, len(rb)+1), make([]int, len(rb)+1)
	best := 0
	for i := range ra {
		for j := range rb {
			if ra[i] == rb[j] {
				cur[j+1] = prev[j] + 1
				best = max(best, cur[j+1])
			} else {
				cur[j+1] = 0
			}
		}
		prev, cur = cur, prev
	}
	return best
}

// shorten cuts s to n runes.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
