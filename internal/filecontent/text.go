package filecontent

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"
)

// decode returns the text of b: UTF-8 (with or without a byte order mark),
// UTF-16 with a byte order mark, or else Windows-1252, the way spreadsheet
// programs often write CSV files.
func decode(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return utf16Text(b[2:], false)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return utf16Text(b[2:], true)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	s, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(s)
}

func utf16Text(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(u))
}

// isText reports whether b looks like text rather than binary content: it
// has a UTF-16 byte order mark, or no NUL bytes and few control characters
// in its first kilobytes.
func isText(b []byte) bool {
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		return true
	}
	head := b[:min(len(b), 8192)]
	control := 0
	for _, c := range head {
		switch {
		case c == 0:
			return false
		case c < 0x20 && c != '\n' && c != '\r' && c != '\t' && c != '\f':
			control++
		}
	}
	return control*20 <= len(head)
}

// xmlText is the text content of an XML document: its character data, a
// line per element.
func xmlText(b []byte) (string, error) {
	d := xml.NewDecoder(strings.NewReader(decode(b)))
	d.Strict = false
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var out strings.Builder
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if out.Len() > 0 {
				break // what was read of a document still being written
			}
			return "", errors.New("cannot read the XML: " + err.Error())
		}
		switch t := tok.(type) {
		case xml.CharData:
			out.Write(t)
		case xml.StartElement, xml.EndElement:
			out.WriteByte('\n')
		}
	}
	return tidyLines(out.String()), nil
}

// blocks are the HTML elements that start a line of their own.
var blocks = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "br": true, "caption": true, "dd": true,
	"div": true, "dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "header": true,
	"hr": true, "li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true, "section": true,
	"table": true, "title": true, "tr": true, "ul": true,
}

// hidden are the HTML elements whose content is not text a reader sees.
var hidden = map[string]bool{"script": true, "style": true, "noscript": true, "template": true}

// htmlText is the text of an HTML page: what its elements say, a line per
// block, table cells separated by tabs.
func htmlText(b []byte) (string, error) {
	z := html.NewTokenizer(strings.NewReader(decode(b)))
	var out strings.Builder
	skip := ""
	for {
		switch z.Next() {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return tidyLines(out.String()), nil
			}
			return "", errors.New("cannot read the HTML: " + z.Err().Error())
		case html.TextToken:
			if skip == "" {
				out.Write(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case skip != "":
			case hidden[tag]:
				skip = tag
			case blocks[tag]:
				out.WriteByte('\n')
			case tag == "td" || tag == "th":
				out.WriteByte('\t')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case skip == tag:
				skip = ""
			case skip == "" && blocks[tag]:
				out.WriteByte('\n')
			}
		}
	}
}

// tidyLines trims every line and drops the empty ones.
func tidyLines(s string) string {
	var lines []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
