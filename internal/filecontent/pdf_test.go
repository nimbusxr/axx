package filecontent

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"reflect"
	"testing"
)

func TestLexer(t *testing.T) {
	l := newLexer([]byte(`% a comment
/F1 12 Tf [(Par\(cel\)\n) -250.5 <50 58 2>] TJ /Span <</MCID 3 /Alt (a >> b)>> BDC
/A#20B 1 0 0 1 .5 -2 Tm (a\
b\101) ' true null EMC`))
	type op struct {
		name string
		args []any
	}
	var got []op
	for {
		name, args, ok := l.next()
		if !ok {
			break
		}
		got = append(got, op{name, args})
	}
	want := []op{
		{"Tf", []any{pdfName("F1"), 12.0}},
		{"TJ", []any{[]any{pdfString("Par(cel)\n"), -250.5, pdfString("PX ")}}},
		{"BDC", []any{pdfName("Span"), nil}},
		{"Tm", []any{pdfName("A B"), 1.0, 0.0, 0.0, 1.0, 0.5, -2.0}},
		{"'", []any{pdfString("abA")}},
		{"EMC", []any{nil, nil}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

// pdfFile is a PDF whose pages draw the given content streams, the second
// one compressed. The pages inherit their resources: Helvetica (F1) and
// Courier (F2) without widths, and a form (X1) that draws form.
func pdfFile(t *testing.T, pages []string, form string) []byte {
	t.Helper()
	var objs []string
	add := func(s string) int {
		objs = append(objs, s)
		return len(objs)
	}
	stream := func(dict, content string, compress bool) string {
		if compress {
			var z bytes.Buffer
			w := zlib.NewWriter(&z)
			_, _ = w.Write([]byte(content))
			_ = w.Close()
			content = z.String()
			dict += " /Filter /FlateDecode"
		}
		return fmt.Sprintf("<<%s /Length %d>>\nstream\n%s\nendstream", dict, len(content), content)
	}
	add("<< /Type /Catalog /Pages 2 0 R >>")
	add("") // the page tree, below
	helvetica := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	courier := add("<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>")
	x := add(stream(fmt.Sprintf(" /Type /XObject /Subtype /Form /BBox [0 0 500 100] /Resources << /Font << /F2 %d 0 R >> >>", courier), form, false))
	var kids string
	for i, p := range pages {
		c := add(stream("", p, i == 1))
		kids += fmt.Sprintf("%d 0 R ", add(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R >>", c)))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> /XObject << /X1 %d 0 R >> >> >>",
		kids, len(pages), helvetica, courier, x)
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}
