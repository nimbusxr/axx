package filecontent

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/xuri/excelize/v2"
)

// wordText is the text of a Word document: its paragraphs, a line each,
// and its tables, a line per row with the cells separated by tabs; then
// the text of its headers and footers.
func wordText(b []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", fmt.Errorf("cannot read the Word document: %w", err)
	}
	var body *zip.File
	var headers, footers []*zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			body = f
		case strings.HasPrefix(f.Name, "word/header") && strings.HasSuffix(f.Name, ".xml"):
			headers = append(headers, f)
		case strings.HasPrefix(f.Name, "word/footer") && strings.HasSuffix(f.Name, ".xml"):
			footers = append(footers, f)
		}
	}
	if body == nil {
		return "", errors.New("cannot read the Word document: it has no word/document.xml")
	}
	byName := func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(headers, byName)
	slices.SortFunc(footers, byName)
	var parts []string
	for _, f := range slices.Concat([]*zip.File{body}, headers, footers) {
		t, err := wordPartText(f)
		if err != nil {
			return "", fmt.Errorf("cannot read the Word document: %s: %w", f.Name, err)
		}
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// wordPartText reads the text of a part of a Word document (its body, a
// header, a footer). Only text runs (w:t) count: not deleted text or field
// instructions. Of alternate content, the first choice counts.
func wordPartText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	d := xml.NewDecoder(rc)
	var out strings.Builder
	inText := false
	cells := 0 // how deep in table cells
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Fallback":
				if err := d.Skip(); err != nil {
					return "", err
				}
			case "t":
				inText = true
			case "tc":
				cells++
			case "tab":
				out.WriteByte('\t')
			case "br", "cr":
				out.WriteByte('\n')
			case "noBreakHyphen":
				out.WriteByte('-')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if cells > 0 {
					out.WriteByte(' ') // a paragraph of a table cell
				} else {
					out.WriteByte('\n')
				}
			case "tr":
				out.WriteByte('\n')
			case "tc":
				cells--
				out.WriteByte('\t')
			}
		case xml.CharData:
			if inText {
				out.Write(t)
			}
		}
	}
	return tidyLines(strings.ReplaceAll(out.String(), " \t", "\t")), nil
}

// openWorkbook opens an Excel workbook.
func openWorkbook(b []byte) (*excelize.File, error) {
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("cannot read the Excel workbook: %w", err)
	}
	return f, nil
}

// excelText is the text of every sheet of a workbook, as the sheets show
// their cells: a line per row, the cells separated by tabs.
func excelText(b []byte) (string, error) {
	f, err := openWorkbook(b)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var lines []string
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("cannot read the %q sheet: %w", sheet, err)
		}
		for _, r := range rows {
			if l := strings.TrimSpace(strings.Join(r, "\t")); l != "" {
				lines = append(lines, l)
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

// excelTable is the table of a sheet of a workbook (the first one, unless
// sheet names another).
func excelTable(b []byte, sheet string) (*Table, error) {
	f, err := openWorkbook(b)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sheets := f.GetSheetList()
	switch {
	case len(sheets) == 0:
		return nil, errors.New("the workbook has no sheets")
	case sheet == "":
		sheet = sheets[0]
	case !slices.Contains(sheets, sheet):
		return nil, fmt.Errorf("the workbook has no %q sheet; its sheets are: %s", sheet, strings.Join(sheets, ", "))
	}
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("cannot read the %q sheet: %w", sheet, err)
	}
	return newTable(rows), nil
}
