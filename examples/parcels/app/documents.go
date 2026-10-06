package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

// Once every line of a manifest is imported or rejected, the service writes
// the manifest's documents to its export folder, where the shop's system
// collects them:
//
//	manifests/<manifest>/report.csv    what became of every line, for the shop's system
//	manifests/<manifest>/report.xlsx   the same report as a workbook, for the shop's staff
//	manifests/<manifest>/summary.json  how many lines were imported and rejected
//	manifests/<manifest>/handover.pdf  the parcels the driver collects, signed on pickup
//	manifests/<manifest>/labels/<reference>.png
//	                                   each imported parcel's label, for the shop's staff
//	                                   to check before printing
//
// It writes them again when more lines of the manifest are processed.
type documents struct {
	store *store
	dir   string
	log   *slog.Logger
	// written holds, by manifest, when its last line was processed when
	// its documents were written.
	written map[string]time.Time
}

// documentLine is a manifest line as the documents show it.
type documentLine struct {
	ID, Reference, Sender, Status, Reason, ServiceLevel string
	WeightGrams                                         int
	Recipient                                           Recipient
}

var manifestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (d *documents) run(ctx context.Context, every time.Duration) {
	d.written = map[string]time.Time{}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := d.writeProcessed(ctx); err != nil && ctx.Err() == nil {
			d.log.Warn("writing manifest documents failed", "err", err)
		}
	}
}

// writeProcessed writes the documents of the manifests whose lines are all
// processed, unless they are written already.
func (d *documents) writeProcessed(ctx context.Context) error {
	rows, err := d.store.pool.Query(ctx, `
SELECT manifest_id, max(processed_at) FROM parcels.manifest_lines
GROUP BY manifest_id HAVING count(*) FILTER (WHERE status = 'PENDING') = 0`)
	if err != nil {
		return err
	}
	type manifest struct {
		id   string
		last time.Time
	}
	ms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (manifest, error) {
		var m manifest
		err := row.Scan(&m.id, &m.last)
		return m, err
	})
	if err != nil {
		return err
	}
	for _, m := range ms {
		if last, ok := d.written[m.id]; ok && last.Equal(m.last) {
			continue
		}
		if !manifestIDPattern.MatchString(m.id) {
			d.log.Warn("manifest documents not written: the manifest id is not a folder name", "manifest", m.id)
			d.written[m.id] = m.last
			continue
		}
		lines, err := d.lines(ctx, m.id)
		if err != nil {
			return err
		}
		if err := d.write(m.id, lines); err != nil {
			return fmt.Errorf("manifest %s: %w", m.id, err)
		}
		d.written[m.id] = m.last
		d.log.Info("manifest documents written", "manifest", m.id, "lines", len(lines))
	}
	return nil
}

func (d *documents) lines(ctx context.Context, manifestID string) ([]documentLine, error) {
	rows, err := d.store.pool.Query(ctx, `
SELECT id, reference, sender, status, coalesce(error, ''), weight_grams, service_level, recipient
FROM parcels.manifest_lines WHERE manifest_id = $1 ORDER BY id`, manifestID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (documentLine, error) {
		var l documentLine
		var rec []byte
		if err := row.Scan(&l.ID, &l.Reference, &l.Sender, &l.Status, &l.Reason, &l.WeightGrams, &l.ServiceLevel, &rec); err != nil {
			return l, err
		}
		_ = json.Unmarshal(rec, &l.Recipient)
		return l, nil
	})
}

// write writes a manifest's documents. Each file appears whole: it is
// written next to its place and renamed.
func (d *documents) write(manifestID string, lines []documentLine) error {
	dir := filepath.Join(d.dir, "manifests", manifestID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	report, err := reportCSV(lines)
	if err != nil {
		return err
	}
	workbook, err := reportWorkbook(lines)
	if err != nil {
		return err
	}
	summary, err := manifestSummary(manifestID, lines)
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"report.csv":   report,
		"report.xlsx":  workbook,
		"summary.json": summary,
		"handover.pdf": handoverNote(manifestID, lines),
	}
	for _, l := range lines {
		if l.Status != "IMPORTED" || !manifestIDPattern.MatchString(l.Reference) {
			continue
		}
		label, err := labelPreview(l)
		if err != nil {
			return err
		}
		files[filepath.Join("labels", l.Reference+".png")] = label
	}
	if err := os.MkdirAll(filepath.Join(dir, "labels"), 0o755); err != nil {
		return err
	}
	for name, body := range files {
		if err := writeFile(filepath.Join(dir, name), body); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, body []byte) error {
	tmp := path + ".part"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var reportColumns = []string{"line", "reference", "status", "reason"}

func reportRow(l documentLine) []string {
	return []string{l.ID, l.Reference, l.Status, l.Reason}
}

func reportCSV(lines []documentLine) ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write(reportColumns)
	for _, l := range lines {
		_ = w.Write(reportRow(l))
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

func reportWorkbook(lines []documentLine) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Report"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	rows := [][]string{reportColumns}
	for _, l := range lines {
		rows = append(rows, reportRow(l))
	}
	for i, r := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			return nil, err
		}
		if err := f.SetSheetRow(sheet, cell, &r); err != nil {
			return nil, err
		}
	}
	if err := f.SetColWidth(sheet, "A", "D", 22); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func manifestSummary(manifestID string, lines []documentLine) ([]byte, error) {
	s := struct {
		Manifest string `json:"manifest"`
		Shop     string `json:"shop"`
		Lines    int    `json:"lines"`
		Imported int    `json:"imported"`
		Rejected int    `json:"rejected"`
	}{Manifest: manifestID, Lines: len(lines)}
	for _, l := range lines {
		s.Shop = l.Sender
		switch l.Status {
		case "IMPORTED":
			s.Imported++
		case "REJECTED":
			s.Rejected++
		}
	}
	return json.MarshalIndent(s, "", "  ")
}

// handoverNote is the note the driver signs when collecting the manifest's
// parcels: the imported ones.
func handoverNote(manifestID string, lines []documentLine) []byte {
	var parcels []documentLine
	shop := ""
	for _, l := range lines {
		shop = l.Sender
		if l.Status == "IMPORTED" {
			parcels = append(parcels, l)
		}
	}
	doc := []pdfLine{
		{size: 18, bold: true, cells: []pdfCell{{0, "Handover note"}}},
		{size: 11, cells: []pdfCell{{0, fmt.Sprintf("Manifest %s from %s", manifestID, shop)}}},
		{size: 11, cells: []pdfCell{{0, fmt.Sprintf("Parcels to collect: %d", len(parcels))}}},
		{size: 11, bold: true, cells: []pdfCell{{0, "Parcel"}, {110, "Recipient"}, {330, "Weight"}, {400, "Service"}}},
	}
	for _, p := range parcels {
		to := p.Recipient.Name
		if p.Recipient.Postcode != "" || p.Recipient.City != "" {
			to += ", " + p.Recipient.Postcode + " " + p.Recipient.City
		}
		doc = append(doc, pdfLine{size: 11, cells: []pdfCell{
			{0, p.Reference}, {110, to}, {330, fmt.Sprintf("%d g", p.WeightGrams)}, {400, p.ServiceLevel},
		}})
	}
	doc = append(doc,
		pdfLine{size: 11, cells: []pdfCell{{0, "Collected by (driver's signature): ______________________"}}},
	)
	return writePDF("Handover note "+manifestID, doc)
}
