package main

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

var heron = []documentLine{
	{
		ID: "ML-HER-0501-1", Reference: "PX-HER-5001", Sender: "heron-prints", Status: "IMPORTED", WeightGrams: 850, ServiceLevel: "STANDARD",
		Recipient: Recipient{Name: "Emmy Noether", Postcode: "37073", City: "Göttingen"},
	},
	{ID: "ML-HER-0501-2", Reference: "PX-HER-5002", Sender: "heron-prints", Status: "REJECTED", Reason: "unknown service level"},
}

func TestManifestDocuments(t *testing.T) {
	d := &documents{dir: t.TempDir()}
	if err := d.write("M-HERON-0501", heron); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d.dir, "manifests", "M-HERON-0501")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	if got, want := string(read("report.csv")), "line,reference,status,reason\nML-HER-0501-1,PX-HER-5001,IMPORTED,\nML-HER-0501-2,PX-HER-5002,REJECTED,unknown service level\n"; got != want {
		t.Errorf("report.csv:\n%s\nwant:\n%s", got, want)
	}

	wb, err := excelize.OpenReader(bytes.NewReader(read("report.xlsx")))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := wb.GetRows("Report")
	if err != nil || len(rows) != 3 || strings.Join(rows[2], "|") != "ML-HER-0501-2|PX-HER-5002|REJECTED|unknown service level" {
		t.Errorf("report.xlsx: %v %v", rows, err)
	}

	var summary map[string]any
	if err := json.Unmarshal(read("summary.json"), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["manifest"] != "M-HERON-0501" || summary["shop"] != "heron-prints" || summary["imported"] != 1.0 || summary["rejected"] != 1.0 {
		t.Errorf("summary.json: %v", summary)
	}

	pdf := read("handover.pdf")
	for _, want := range []string{"(Parcels to collect: 1) Tj", "(PX-HER-5001) Tj", "(Emmy Noether, 37073 G\xf6ttingen) Tj", "(850 g) Tj"} {
		if !strings.Contains(pdfContent(t, pdf), want) {
			t.Errorf("the handover note lacks %q:\n%s", want, pdfContent(t, pdf))
		}
	}
	if strings.Contains(pdfContent(t, pdf), "PX-HER-5002") {
		t.Error("a rejected line is not collected")
	}
	label, err := png.Decode(bytes.NewReader(read("labels/PX-HER-5001.png")))
	if err != nil {
		t.Fatal(err)
	}
	if b := label.Bounds(); b.Dx() != 400 || b.Dy() != 600 {
		t.Errorf("the label is %v, not 4 by 6 inches at 100 dots an inch", b)
	}
	if r, g, b, _ := label.At(390, 50).RGBA(); r|g|b != 0 {
		t.Error("the label's service level band is not black")
	}
	if _, err := os.Stat(filepath.Join(dir, "labels", "PX-HER-5002.png")); !os.IsNotExist(err) {
		t.Error("a rejected line has a label")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 5 {
		t.Errorf("the manifest's folder has %d entries", len(entries))
	}
}

// pdfContent is the decompressed content stream of a document writePDF
// wrote, after checking its cross-reference table.
func pdfContent(t *testing.T, pdf []byte) string {
	t.Helper()
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(pdf)
	if m == nil {
		t.Fatal("no startxref")
	}
	xref, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(pdf[xref:], []byte("xref\n")) {
		t.Fatalf("startxref %d does not point at the table", xref)
	}
	for i, off := range regexp.MustCompile(`(\d{10}) 00000 n`).FindAllSubmatch(pdf[xref:], -1) {
		o, _ := strconv.Atoi(string(off[1]))
		if !bytes.HasPrefix(pdf[o:], []byte(strconv.Itoa(i+1)+" 0 obj")) {
			t.Fatalf("object %d is not at %d", i+1, o)
		}
	}
	start := bytes.Index(pdf, []byte("stream\n")) + len("stream\n")
	end := bytes.Index(pdf, []byte("\nendstream"))
	zr, err := zlib.NewReader(bytes.NewReader(pdf[start:end]))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
