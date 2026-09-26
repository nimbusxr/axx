package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// A pdfLine is a line of a PDF document: its cells, each at its distance
// from the left margin, in Helvetica of that size.
type pdfLine struct {
	size  float64
	bold  bool
	cells []pdfCell
}

type pdfCell struct {
	x    float64
	text string
}

// writePDF writes a one-page A4 document of text lines.
func writePDF(title string, lines []pdfLine) []byte {
	var content bytes.Buffer
	y := 800.0
	for _, l := range lines {
		y -= l.size * 1.8
		font := "F1"
		if l.bold {
			font = "F2"
		}
		for _, c := range l.cells {
			fmt.Fprintf(&content, "BT /%s %g Tf %g %g Td (%s) Tj ET\n", font, l.size, 56+c.x, y, pdfText(c.text))
		}
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(content.Bytes())
	_ = zw.Close()

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", z.Len(), z.Bytes()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Title (%s) /Producer (parcels) >>", pdfText(title)),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, len(objects), xref)
	return b.Bytes()
}

// pdfText is text as a PDF string holds it: in the fonts' encoding
// (Windows-1252), with the string's delimiters escaped.
func pdfText(s string) string {
	b, err := charmap.Windows1252.NewEncoder().String(s)
	if err != nil {
		b = strings.Map(func(r rune) rune {
			if r > 0x7e {
				return '?'
			}
			return r
		}, s)
	}
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(b)
}
