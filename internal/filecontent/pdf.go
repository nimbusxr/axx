package filecontent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// pdfText is the text of a PDF's pages, in the order the pages draw it: a
// line per line of text, and a space where the page leaves one between
// words. The pdf package reads the document (cross-reference streams,
// object streams, compression, standard encryption, fonts); the pages'
// content is read here, to place the text.
func pdfText(b []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("cannot read the PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		if errors.Is(err, pdf.ErrInvalidPassword) {
			return "", errors.New("cannot read the PDF: it is protected by a password")
		}
		return "", fmt.Errorf("cannot read the PDF: %w", err)
	}
	var out strings.Builder
	var failed error
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		t, err := pageText(p)
		if err != nil {
			failed = fmt.Errorf("cannot read page %d of the PDF: %w", i, err)
			continue
		}
		if t != "" {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(t)
		}
	}
	if out.Len() == 0 && failed != nil {
		return "", failed
	}
	return out.String(), nil
}

// pageText is the text of a page. A page whose content breaks off has the
// text before the break.
func pageText(p pdf.Page) (text string, err error) {
	c := &canvas{}
	defer func() {
		if r := recover(); r != nil {
			if text = c.text(); text == "" {
				err = fmt.Errorf("%v", r)
			}
		}
	}()
	c.draw(p.V.Key("Contents"), p.Resources(), identity, 0)
	return c.text(), nil
}

// A matrix is a PDF transformation [a b c d e f].
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// mul is m×n: m applied first.
func (m matrix) mul(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func translate(x, y float64) matrix { return matrix{1, 0, 0, 1, x, y} }

// run is a piece of text a page shows: where it starts and ends (in
// device space) and its font size there.
type run struct {
	x, y, end, size float64
	s               string
}

// canvas collects the runs a page shows.
type canvas struct {
	runs []run
}

// state is the graphics state the text depends on.
type state struct {
	ctm                  matrix
	font                 *font
	size                 float64 // Tfs
	charSpace, wordSpace float64 // Tc, Tw
	scale, leading, rise float64 // Th, TL, Ts
	fonts, xobjects      pdf.Value
}

// draw interprets a content stream (or an array of them) with its
// resources. Form XObjects are drawn too, up to a few levels deep.
func (c *canvas) draw(content, resources pdf.Value, ctm matrix, depth int) {
	if depth > 8 {
		return
	}
	g := state{ctm: ctm, scale: 1, fonts: resources.Key("Font"), xobjects: resources.Key("XObject")}
	var saved []state
	fonts := map[string]*font{}
	var tm, tlm matrix
	ops := newLexer(streamBytes(content))
	for {
		op, args, ok := ops.next()
		if !ok {
			return
		}
		num := func(i int) float64 {
			if i < len(args) {
				if f, ok := args[i].(float64); ok {
					return f
				}
			}
			return 0
		}
		switch op {
		case "q":
			saved = append(saved, g)
		case "Q":
			if n := len(saved); n > 0 {
				g, saved = saved[n-1], saved[:n-1]
			}
		case "cm":
			g.ctm = matrix{num(0), num(1), num(2), num(3), num(4), num(5)}.mul(g.ctm)
		case "BT":
			tm, tlm = identity, identity
		case "Tf":
			n := name(args)
			f, ok := fonts[n]
			if !ok {
				f = newFont(g.fonts.Key(n))
				fonts[n] = f
			}
			g.font, g.size = f, num(1)
		case "Tc":
			g.charSpace = num(0)
		case "Tw":
			g.wordSpace = num(0)
		case "Tz":
			g.scale = num(0) / 100
		case "TL":
			g.leading = num(0)
		case "Ts":
			g.rise = num(0)
		case "Td":
			tlm = translate(num(0), num(1)).mul(tlm)
			tm = tlm
		case "TD":
			g.leading = -num(1)
			tlm = translate(num(0), num(1)).mul(tlm)
			tm = tlm
		case "Tm":
			tlm = matrix{num(0), num(1), num(2), num(3), num(4), num(5)}
			tm = tlm
		case "T*":
			tlm = translate(0, -g.leading).mul(tlm)
			tm = tlm
		case "Tj", "'", "\"":
			switch op {
			case "\"":
				g.wordSpace, g.charSpace = num(0), num(1)
				fallthrough
			case "'":
				tlm = translate(0, -g.leading).mul(tlm)
				tm = tlm
			}
			if len(args) > 0 {
				if s, ok := args[len(args)-1].(pdfString); ok {
					tm = c.show(&g, tm, s)
				}
			}
		case "TJ":
			if len(args) == 0 {
				continue
			}
			arr, _ := args[0].([]any)
			for _, x := range arr {
				switch v := x.(type) {
				case pdfString:
					tm = c.show(&g, tm, v)
				case float64:
					tm = translate(-v/1000*g.size*g.scale, 0).mul(tm)
				}
			}
		case "Do":
			x := g.xobjects.Key(name(args))
			if x.Key("Subtype").Name() != "Form" {
				continue
			}
			m := identity
			if fm := x.Key("Matrix"); fm.Len() == 6 {
				for i := range m {
					m[i] = fm.Index(i).Float64()
				}
			}
			res := x.Key("Resources")
			if res.IsNull() {
				res = resources
			}
			c.draw(x, res, m.mul(g.ctm), depth+1)
		}
	}
}

// name is the first operand, a name.
func name(args []any) string {
	if len(args) == 0 {
		return ""
	}
	n, _ := args[0].(pdfName)
	return string(n)
}

// show records the text of a string and returns the text matrix after it.
func (c *canvas) show(g *state, tm matrix, s pdfString) matrix {
	f := g.font
	if f == nil {
		f = &font{}
	}
	trm := matrix{g.size * g.scale, 0, 0, g.size, 0, g.rise}.mul(tm).mul(g.ctm)
	x, y := trm[4], trm[5]
	size := math.Hypot(trm[2], trm[3])
	text := f.decode(string(s))
	for _, code := range f.codes(s) {
		adv := f.width(code)*g.size + g.charSpace
		if code == 32 && f.bytes == 1 {
			adv += g.wordSpace
		}
		tm = translate(adv*g.scale, 0).mul(tm)
	}
	end := matrix{1, 0, 0, 1, 0, g.rise}.mul(tm).mul(g.ctm)[4]
	if text != "" {
		c.runs = append(c.runs, run{x: x, y: y, end: end, size: size, s: text})
	}
	return tm
}

// text lays out the runs in the order the page draws them: a new line
// where a run is on another line than the one before, a space where it
// leaves a gap after it. A run drawn again over itself (a shadow, bold
// made by drawing twice) counts once.
func (c *canvas) text() string {
	var b strings.Builder
	var prev *run
	for i := range c.runs {
		r := &c.runs[i]
		if prev != nil {
			size := max(prev.size, r.size, 1)
			if r.s == prev.s && math.Abs(r.x-prev.x) < size/4 && math.Abs(r.y-prev.y) < size/4 {
				continue
			}
			last, _ := utf8.DecodeLastRuneInString(b.String())
			first, _ := utf8.DecodeRuneInString(r.s)
			switch {
			case math.Abs(r.y-prev.y) > size/2:
				b.WriteByte('\n')
			case (r.x-prev.end > size*0.15 || prev.end-r.x > size) && !unicode.IsSpace(last) && !unicode.IsSpace(first):
				b.WriteByte(' ')
			}
		}
		b.WriteString(r.s)
		prev = r
	}
	return tidyLines(b.String())
}

// font is what the text needs of a font: how its codes decode, how many
// bytes a code has, and how wide each glyph is (in text space units per
// unit of font size).
type font struct {
	enc     pdf.TextEncoding
	bytes   int
	widths  map[int]float64
	ranges  []widthRange
	missing float64 // the width of a glyph the font does not list
	mono    bool
}

// widthRange gives the codes first to last one width.
type widthRange struct {
	first, last int
	width       float64
}

func newFont(v pdf.Value) *font {
	f := &font{bytes: 1, widths: map[int]float64{}}
	if v.IsNull() {
		return f
	}
	pf := pdf.Font{V: v}
	f.enc = pf.Encoder()
	switch v.Key("Subtype").Name() {
	case "Type0":
		f.bytes = 2
		d := v.Key("DescendantFonts").Index(0)
		f.missing = 1
		if dw := d.Key("DW"); !dw.IsNull() {
			f.missing = dw.Float64() / 1000
		}
		w := d.Key("W")
		for i := 0; i < w.Len(); {
			first := int(w.Index(i).Int64())
			switch next := w.Index(i + 1); next.Kind() {
			case pdf.Array:
				for j := 0; j < next.Len(); j++ {
					f.widths[first+j] = next.Index(j).Float64() / 1000
				}
				i += 2
			default:
				f.ranges = append(f.ranges, widthRange{first, int(next.Int64()), w.Index(i+2).Float64() / 1000})
				i += 3
			}
		}
	default:
		scale := 1.0 / 1000
		if fm := v.Key("FontMatrix"); fm.Len() == 6 { // Type3 glyph space
			scale = fm.Index(0).Float64()
		}
		first := int(v.Key("FirstChar").Int64())
		ws := v.Key("Widths")
		for i := 0; i < ws.Len(); i++ {
			f.widths[first+i] = ws.Index(i).Float64() * scale
		}
		if mw := v.Key("FontDescriptor").Key("MissingWidth"); !mw.IsNull() {
			f.missing = mw.Float64() * scale
		}
		f.mono = strings.Contains(v.Key("BaseFont").Name(), "Courier")
	}
	return f
}

func (f *font) decode(raw string) string {
	if f.enc == nil {
		return raw
	}
	return f.enc.Decode(raw)
}

func (f *font) codes(s pdfString) []int {
	out := make([]int, 0, len(s)/f.bytes)
	for i := 0; i+f.bytes <= len(s); i += f.bytes {
		c := 0
		for _, b := range s[i : i+f.bytes] {
			c = c<<8 | int(b)
		}
		out = append(out, c)
	}
	return out
}

// width is a glyph's width. Fonts that do not list their widths (the
// standard fonts often do not) get an estimate from the character.
func (f *font) width(code int) float64 {
	if w, ok := f.widths[code]; ok {
		return w
	}
	for _, r := range f.ranges {
		if code >= r.first && code <= r.last {
			return r.width
		}
	}
	if f.missing > 0 || f.bytes == 2 {
		return f.missing
	}
	if f.mono {
		return 0.6
	}
	r := rune(code)
	switch {
	case r == ' ':
		return 0.278
	case strings.ContainsRune("il.,:;|!'`jI", r):
		return 0.25
	case strings.ContainsRune("frt()[]-/", r):
		return 0.333
	case strings.ContainsRune("mwMW", r):
		return 0.833
	case r >= 'A' && r <= 'Z':
		return 0.667
	case r >= '0' && r <= '9':
		return 0.556
	}
	return 0.5
}

// streamBytes is the decoded content of a stream, or of an array of
// streams, joined.
func streamBytes(v pdf.Value) []byte {
	var out []byte
	read := func(s pdf.Value) {
		rc := s.Reader()
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		out = append(out, b...)
		out = append(out, '\n')
	}
	switch v.Kind() {
	case pdf.Array:
		for i := 0; i < v.Len(); i++ {
			read(v.Index(i))
		}
	case pdf.Stream:
		read(v)
	}
	return out
}
