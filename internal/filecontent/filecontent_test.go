package filecontent

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nimbusxr/axx/core"
)

// The files in testdata come from real producers, with their metadata
// removed:
//
//   - customs-invoice.pdf: an HTML invoice printed to PDF by Chromium (Skia;
//     Type0 fonts with ToUnicode maps, kerning moves), then saved with
//     object streams and AES-128 encryption (no user password) by qpdf.
//   - manifest.pdf: two pages from ReportLab (standard fonts, no widths).
//   - customs-declaration.pdf: from pdf-lib (hex strings).
//   - manifest-locked.pdf: manifest.pdf with a user password.
//   - packing-list.docx: HTML converted by macOS textutil.

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func text(t *testing.T, f File) string {
	t.Helper()
	s, err := f.Text()
	if err != nil {
		t.Fatalf("%s: %v", f.Name, err)
	}
	return s
}

func TestKind(t *testing.T) {
	docx := wordFile(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`)
	xlsx := workbook(t)
	for _, c := range []struct {
		f    File
		want Kind
	}{
		{File{Name: "report.CSV"}, CSV},
		{File{Name: "manifests/M-1/lines.tsv"}, TSV},
		{File{Name: "summary.json", MediaType: "text/plain"}, JSON},
		{File{Name: "label", MediaType: "application/pdf"}, PDF},
		{File{Name: "report", MediaType: "text/csv; charset=utf-8"}, CSV},
		{File{Name: "event", MediaType: "application/cloudevents+json"}, JSON},
		{File{Name: "a.bin", MediaType: "text/markdown"}, Text},
		{File{Name: "invoice", Body: read(t, "manifest.pdf")}, PDF},
		{File{Name: "packing-list", MediaType: "application/octet-stream", Body: docx}, Word},
		{File{Name: "report", Body: xlsx}, Excel},
		{File{Name: "summary", Body: []byte(` {"imported": 2}`)}, JSON},
		{File{Name: "declaration", Body: []byte(`<?xml version="1.0"?><declaration/>`)}, XML},
		{File{Name: "page", Body: []byte("<!DOCTYPE html>\n<html><body>Parcel</body></html>")}, HTML},
		{File{Name: "notes", Body: []byte("line ML-1 rejected\n")}, Text},
		{File{Name: "photo", Body: []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")}, Binary},
		{File{Name: "archive", Body: []byte("PK\x03\x04garbage")}, Binary},
	} {
		if got := c.f.Kind(); got != c.want {
			t.Errorf("%s (%s): kind %s, want %s", c.f.Name, c.f.MediaType, got, c.want)
		}
	}
}

func TestPlainText(t *testing.T) {
	for name, c := range map[string]struct {
		body []byte
		want string
	}{
		"utf-8 with a byte order mark": {[]byte("\xEF\xBB\xBFZürich"), "Zürich"},
		"utf-16":                       {[]byte{0xFF, 0xFE, 'Z', 0, 0xFC, 0, 'r', 0}, "Zür"},
		"windows-1252":                 {[]byte("Stra\xDFe \x80"), "Straße €"},
	} {
		if got := text(t, File{Name: "x.txt", Body: c.body}); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestMarkupText(t *testing.T) {
	xml := File{Name: "declaration.xml", Body: []byte(`<?xml version="1.0" encoding="ISO-8859-1"?>
<declaration id="CN23-6001"><parcel ref="PX-CUS-6001">
  <recipient>Luca M` + "\xFC" + `ller &amp; family</recipient><value currency="EUR">153.00</value>
</parcel></declaration>`)}
	if got, want := text(t, xml), "Luca Müller & family\n153.00"; got != want {
		t.Errorf("XML text %q, want %q", got, want)
	}
	for _, want := range []string{"Luca Müller & family 153.00", `currency="EUR"`} {
		if ok, _, err := xml.Contains(want); err != nil || !ok {
			t.Errorf("the XML should contain %q (%v)", want, err)
		}
	}

	page := File{Name: "label.html", Body: []byte(`<html><head><title>Label PX-4101</title><style>p{color:red}</style>
<script>var hidden = "not text";</script></head><body><h1>Parcel <b>PX-</b>4101</h1><p>To:&nbsp;Anna Weber</p>
<table><tr><th>Service</th><th>Weight</th></tr><tr><td>EXPRESS</td><td>1200 g</td></tr></table></body></html>`)}
	got := text(t, page)
	want := "Label PX-4101\nParcel PX-4101\nTo: Anna Weber\nService\tWeight\nEXPRESS\t1200 g"
	if got != want {
		t.Errorf("HTML text %q, want %q", got, want)
	}
	if strings.Contains(got, "not text") {
		t.Error("a script is not the page's text")
	}
	if ok, _, _ := page.Contains(`<b>PX-</b>4101`); !ok {
		t.Error("the markup should count")
	}
	if ok, _, _ := page.Contains("To: Anna Weber"); !ok {
		t.Error("a no-break space should count as a space")
	}
}

func TestPDFText(t *testing.T) {
	for _, c := range []struct {
		file string
		want []string
	}{
		{"customs-invoice.pdf", []string{
			"Commercial invoice",
			"Parcel: PX-CUS-6001",
			"Sender: Kestrel Outdoor GmbH, Invalidenstraße 116, 10115 Berlin, Germany",
			"Recipient: Luca Müller, Bahnhofstrasse 1, 8001 Zürich, Switzerland",
			"Description HS code Quantity Value",
			"Hiking boots 6403.91 1 129.00 EUR",
			"Total value: 153.00 EUR — Reason for export: sale of goods",
		}},
		{"manifest.pdf", []string{
			"Daily manifest M-KESTREL-0412",
			"PX-KES-0412-1 Anna Weber 1200 g",
			"PX-KES-0412-2 Jonas Keller 800 g",
			"Page two: signed by the driver",
		}},
		{"customs-declaration.pdf", []string{
			"Customs declaration CN23 for parcel PX-CUS-6003",
			"Contents: Hiking boots (1), value 129.00 EUR",
		}},
	} {
		got := strings.Split(text(t, File{Name: c.file, Body: read(t, c.file)}), "\n")
		for _, w := range c.want {
			found := false
			for _, l := range got {
				found = found || strings.TrimSpace(l) == w
			}
			if !found {
				t.Errorf("%s: no line %q in:\n%s", c.file, w, strings.Join(got, "\n"))
			}
		}
	}
}

// TestPDFLayout draws text the ways PDF producers do and checks where the
// lines and spaces go.
func TestPDFLayout(t *testing.T) {
	body := pdfFile(t, []string{
		// Lines by T* and Td, words spaced by TJ, a kerning pair, columns by Td.
		`BT /F1 12 Tf 14 TL 72 700 Td (Shipping label) Tj T* (Parcel PX-LBL-7003) Tj
0 -14 Td [(Recipient:) -278 (Anna) -250 (W) 80 (eber)] TJ ET
BT /F1 10 Tf 72 600 Td (PX-4101) Tj 100 0 Td (IN_TRANSIT) Tj ET
BT /F2 10 Tf 1 0 0 1 72 580 Tm (Invalidenstra\337e 116 \(rear\)) Tj ET
BI /W 13 /H 1 /BPC 8 /CS /G ID (Hidden) Tj x
EI
q 1 0 0 1 72 500 cm /X1 Do Q
BT /F1 12 Tf 72 480 Td (Weight:) Tj ( 1200 g) Tj ET
BT /F1 12 Tf 72 460 Td (Fragile) Tj ET BT /F1 12 Tf 72.4 460 Td (Fragile) Tj ET`,
		// A second page, compressed.
		"BT /F1 12 Tf 72 700 Td (Page two) Tj ET",
	}, "BT /F2 10 Tf 0 0 Td (Signed by the driver) Tj ET")
	got := text(t, File{Name: "label.pdf", Body: body})
	want := strings.Join([]string{
		"Shipping label",
		"Parcel PX-LBL-7003",
		"Recipient: Anna Weber",
		"PX-4101 IN_TRANSIT",
		"Invalidenstraße 116 (rear)",
		"Signed by the driver",
		"Weight: 1200 g",
		"Fragile",
		"Page two",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPDFErrors(t *testing.T) {
	for name, c := range map[string]struct {
		body []byte
		want string
	}{
		"password":  {read(t, "manifest-locked.pdf"), "cannot read the PDF: it is protected by a password"},
		"truncated": {read(t, "manifest.pdf")[:900], "cannot read the PDF"},
	} {
		_, err := File{Name: "manifest.pdf", Body: c.body}.Text()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func TestWordText(t *testing.T) {
	got := text(t, File{Name: "packing-list.docx", Body: read(t, "packing-list.docx")})
	for _, want := range []string{"Packing list PX-CUS-6001", "Parcel PX-CUS-6001 for Luca Müller, Zürich", "Total value: 153.00 EUR"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}

	body := wordFile(t, `<w:p><w:r><w:t>Packing list </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>PX-CUS-</w:t></w:r><w:r><w:t>6001</w:t></w:r></w:p>
<w:p><w:r><w:t>Shop</w:t><w:tab/><w:t>Kestrel Outdoor</w:t><w:br/><w:t>Berlin</w:t></w:r></w:p>
<w:p><w:del><w:r><w:delText>withdrawn</w:delText></w:r></w:del><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:t>Page 1</w:t></w:r></w:p>
<w:p><w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><w:txbxContent><w:p><w:r><w:t>Fragile</w:t></w:r></w:p></w:txbxContent></w:drawing></mc:Choice><mc:Fallback><w:pict><w:txbxContent><w:p><w:r><w:t>Fragile</w:t></w:r></w:p></w:txbxContent></w:pict></mc:Fallback></mc:AlternateContent></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Item</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Value</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>Hiking boots</w:t></w:r></w:p><w:p><w:r><w:t>size 42</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>129.00 EUR</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`)
	got = text(t, File{Name: "packing-list.docx", Body: body})
	want := strings.Join([]string{
		"Packing list PX-CUS-6001",
		"Shop\tKestrel Outdoor",
		"Berlin",
		"Page 1",
		"Fragile",
		"Item\tValue",
		"Hiking boots size 42\t129.00 EUR",
		"Kestrel Outdoor GmbH",
		"Page footer",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if ok, _, _ := (File{Name: "p.docx", Body: body}).Contains("Hiking boots size 42 129.00 EUR"); !ok {
		t.Error("a table row should read as one line")
	}
}

func TestExcel(t *testing.T) {
	f := File{Name: "report.xlsx", Body: workbook(t)}
	got := text(t, f)
	want := "line\treference\tstatus\tprice\nML-HER-0501-1\tPX-HER-5001\tIMPORTED\t6.90\nML-HER-0501-2\t\tREJECTED\nlines\t2"
	if got != want {
		t.Errorf("text %q, want %q", got, want)
	}
	tbl, err := f.Table("")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tbl.Columns, "|"); got != "line|reference|status|price" {
		t.Errorf("columns %s", got)
	}
	if ok, why := tbl.FindRow(pairs("line", "ML-HER-0501-1", "price", "6.90")); !ok {
		t.Error(why)
	}
	if ok, why := tbl.FindRow(pairs("line", "ML-HER-0501-2", "reference", "")); !ok {
		t.Errorf("an empty value should match an empty cell: %s", why)
	}
	sum, err := f.Table("Summary")
	if err != nil || len(sum.Rows) != 0 || sum.Columns[0] != "lines" {
		t.Errorf("the Summary sheet: %+v, %v", sum, err)
	}
	if _, err := f.Table("Totals"); err == nil || err.Error() != `the workbook has no "Totals" sheet; its sheets are: Report, Summary` {
		t.Errorf("a missing sheet: %v", err)
	}
	if _, err := (File{Name: "report.xlsx", Body: []byte("PK\x03\x04")}).Text(); err == nil || !strings.Contains(err.Error(), "cannot read the Excel workbook") {
		t.Errorf("a broken workbook: %v", err)
	}
}

func TestCSVTable(t *testing.T) {
	for name, c := range map[string]struct {
		file File
		cols string
		rows int
	}{
		"comma":        {File{Name: "r.csv", Body: []byte("\xEF\xBB\xBFline,status,reason\r\nML-1,IMPORTED,\r\nML-2,REJECTED,\"weight exceeds\n30000 g\"\r\n\r\n")}, "line|status|reason", 2},
		"semicolon":    {File{Name: "r.csv", Body: []byte("line;status;reason\nML-1;IMPORTED;\n")}, "line|status|reason", 1},
		"excel sep":    {File{Name: "r.csv", Body: []byte("sep=|\nline|status\nML-1|IMPORTED\n")}, "line|status", 1},
		"tsv":          {File{Name: "r.tsv", Body: []byte("line\tstatus\tnote\nML-1\tIMPORTED\t\"fragile\"\n")}, "line|status|note", 1},
		"text as csv":  {File{Name: "report", Body: []byte("line\tstatus\nML-1\tIMPORTED\n")}, "line|status", 1},
		"blank header": {File{Name: "r.csv", Body: []byte("\n,,\n line , status \nML-1,IMPORTED\n")}, "line|status", 1},
	} {
		tbl, err := c.file.Table("")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := strings.Join(tbl.Columns, "|"); got != c.cols || len(tbl.Rows) != c.rows {
			t.Errorf("%s: columns %s, %d rows; want %s, %d", name, got, len(tbl.Rows), c.cols, c.rows)
		}
	}
	tbl, _ := File{Name: "r.csv", Body: []byte("line,status,reason\nML-2,REJECTED,\"weight exceeds\n30000 g\"\n")}.Table("")
	if ok, why := tbl.FindRow(pairs("reason", "weight exceeds 30000 g")); !ok {
		t.Errorf("a line break in a cell should count as a space: %s", why)
	}
	for _, f := range []File{{Name: "r.pdf", Body: read(t, "manifest.pdf")}, {Name: "r.csv"}} {
		_, err := f.Table("Report")
		if err == nil || !strings.Contains(err.Error(), "which has no sheets") {
			t.Errorf("%s with a sheet: %v", f.Name, err)
		}
	}
	if _, err := (File{Name: "invoice.pdf"}).Table(""); err == nil || err.Error() != "it is a PDF, not a table axx reads (CSV, TSV, Excel .xlsx)" {
		t.Errorf("a PDF: %v", err)
	}
}

func TestFindRow(t *testing.T) {
	var rows strings.Builder
	rows.WriteString("line,reference,status,reason\n")
	for i := 1; i <= 12; i++ {
		rows.WriteString("ML-" + string(rune('A'+i-1)) + ",PX-" + string(rune('A'+i-1)) + ",IMPORTED,\n")
	}
	tbl, err := File{Name: "report.csv", Body: []byte(rows.String())}.Table("")
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := tbl.FindRow(pairs("line", "ML-C", "status", " IMPORTED ")); !ok {
		t.Error(why)
	}
	for _, c := range []struct {
		want []core.Pair
		why  string
	}{
		{pairs("parcel", "PX-A"), `its table has no "parcel" column; its columns are: line, reference, status, reason`},
		{pairs("parcel", "PX-A", "zone", "DE-1"), `its table has no "parcel", "zone" columns; its columns are: line, reference, status, reason`},
		{pairs("line", "ML-A", "status", "REJECTED"), "no row has line=ML-A, status=REJECTED. Its 12 rows are:\n  line=ML-A, status=IMPORTED\n  line=ML-B, status=IMPORTED\n"},
		{pairs("line", "ML-Z"), "  line=ML-J\n  … and 2 more"},
	} {
		ok, why := tbl.FindRow(c.want)
		if ok || !strings.Contains(why, c.why) {
			t.Errorf("%v: %v %q, want %q", c.want, ok, why, c.why)
		}
	}
	one, _ := File{Name: "r.csv", Body: []byte("line,status\nML-1,REJECTED\n")}.Table("")
	if _, why := one.FindRow(pairs("status", "IMPORTED")); why != "no row has status=IMPORTED. Its 1 row is:\n  status=REJECTED" {
		t.Errorf("one row: %q", why)
	}
	empty, _ := File{Name: "r.csv", Body: []byte("line,status\n")}.Table("")
	if _, why := empty.FindRow(pairs("status", "IMPORTED")); why != "its table has no rows, only the columns line, status" {
		t.Errorf("no rows: %q", why)
	}
	none, _ := File{Name: "r.csv"}.Table("")
	if _, why := none.FindRow(pairs("status", "IMPORTED")); why != "it holds no table: it is empty" {
		t.Errorf("an empty file: %q", why)
	}
}

func TestContainsAndNearest(t *testing.T) {
	f := File{Name: "handover.pdf", Body: read(t, "manifest.pdf")}
	ok, got, err := f.Contains("PX-KES-0412-1   Anna\nWeber")
	if err != nil || !ok {
		t.Fatalf("spaces and line breaks should count as one space: %v %v", ok, err)
	}
	if ok, _, _ := f.Contains("anna weber"); ok {
		t.Error("case matters")
	}
	if n := Nearest(got, "PX-KES-0412-3 Anna Weber"); n != "Its text nearest that:\n  PX-KES-0412-1 Anna Weber 1200 g\n  PX-KES-0412-2 Jonas Keller 800 g" {
		t.Errorf("nearest: %q", n)
	}
	if n := Nearest(got, "Customs"); !strings.HasPrefix(n, "Its text begins:\n  Daily manifest M-KESTREL-0412\n") {
		t.Errorf("begins: %q", n)
	}
	if n := Nearest(" \n", "x"); n != "It has no text." {
		t.Errorf("no text: %q", n)
	}
	var long strings.Builder
	for range 30 {
		long.WriteString("parcel line\n")
	}
	if n := Nearest(long.String(), "Customs"); !strings.HasSuffix(n, "\n  … (30 lines in all)") {
		t.Errorf("a long text is shortened: %q", n)
	}
	_, err = File{Name: "photo.png", Body: []byte("\x89PNG\r\n\x1a\n\x00\x00")}.Text()
	if err == nil || err.Error() != "it is a binary file (10 bytes), not a file whose text axx reads (text, CSV, TSV, JSON, XML, HTML, PDF, Word .docx, Excel .xlsx)" {
		t.Errorf("binary: %v", err)
	}
}

func pairs(kv ...string) []core.Pair {
	var out []core.Pair
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, core.Pair{Key: kv[i], Value: kv[i+1], Null: kv[i+1] == ""})
	}
	return out
}

// workbook is an Excel workbook of an import report: a Report sheet (the
// first) and a Summary sheet.
func workbook(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", "Report"); err != nil {
		t.Fatal(err)
	}
	rows := [][]any{
		{"line", "reference", "status", "price"},
		{"ML-HER-0501-1", "PX-HER-5001", "IMPORTED", 6.9},
		{"ML-HER-0501-2", nil, "REJECTED"},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Report", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	style, err := f.NewStyle(&excelize.Style{NumFmt: 2}) // 0.00
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle("Report", "D2", "D2", style); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("Summary"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Summary", "A1", &[]any{"lines", 2}); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// wordFile is a Word document whose body is the given WordprocessingML,
// with a header and a footer.
func wordFile(t *testing.T, body string) []byte {
	t.Helper()
	const ns = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, content := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document ` + ns + `><w:body>` + body + `</w:body></w:document>`,
		"word/header1.xml":    `<?xml version="1.0"?><w:hdr ` + ns + `><w:p><w:r><w:t>Kestrel Outdoor GmbH</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":    `<?xml version="1.0"?><w:ftr ` + ns + `><w:p><w:r><w:t>Page footer</w:t></w:r></w:p></w:ftr>`,
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// FuzzText reads broken files: a reader may fail, not panic or hang.
func FuzzText(f *testing.F) {
	for _, name := range []string{"customs-invoice.pdf", "manifest.pdf", "customs-declaration.pdf", "packing-list.docx"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(name, b[:len(b)*2/3])
	}
	f.Fuzz(func(_ *testing.T, name string, b []byte) {
		file := File{Name: name, Body: b}
		_, _ = file.Text()
		_, _ = file.Table("")
	})
}
