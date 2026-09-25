package sql

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/xuri/excelize/v2"
)

// Dataset is an ordered set of tables to insert (DbUnit/Database Rider
// compatible).
type Dataset struct {
	Tables []DatasetTable
}

// DatasetTable is one table's rows. Each row lists its own columns, so rows
// may omit columns (their database defaults apply).
type DatasetTable struct {
	Name string
	Rows []DatasetRow
}

// DatasetRow is an ordered column -> value mapping. A nil value is NULL.
type DatasetRow struct {
	Columns []string
	Values  []any
}

// LoadDataset reads a dataset file, choosing the format by extension.
func LoadDataset(path string) (*Dataset, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return loadYAMLDataset(path)
	case ".xml":
		return loadXMLDataset(path)
	case ".json":
		return loadJSONDataset(path)
	case ".csv":
		// Like Database Rider, a CSV file selects the DbUnit CSV dataset in
		// its directory (one <table>.csv per table, ordered by
		// table-ordering.txt when present).
		return loadCSVDataset(filepath.Dir(path))
	case ".xlsx":
		return loadXLSXDataset(path)
	case ".xls":
		return nil, fmt.Errorf("legacy .xls datasets are not supported; save %s as .xlsx, YAML or CSV", filepath.Base(path))
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return loadCSVDataset(path)
	}
	return nil, fmt.Errorf("unknown dataset format %q (use .yaml, .xml, .json, .csv or .xlsx)", filepath.Ext(path))
}

func loadYAMLDataset(path string) (*Dataset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(b, &root, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("%s: %s", filepath.Base(path), yaml.FormatError(err, false, true))
	}
	ds := &Dataset{}
	for _, t := range root {
		table := DatasetTable{Name: fmt.Sprint(t.Key)}
		rows, ok := t.Value.([]any)
		if t.Value != nil && !ok {
			return nil, fmt.Errorf("%s: table %s must be a list of rows", filepath.Base(path), table.Name)
		}
		for i, r := range rows {
			m, ok := r.(yaml.MapSlice)
			if !ok {
				return nil, fmt.Errorf("%s: row %d of %s must be a mapping of column: value", filepath.Base(path), i+1, table.Name)
			}
			var row DatasetRow
			for _, c := range m {
				row.Columns = append(row.Columns, fmt.Sprint(c.Key))
				row.Values = append(row.Values, c.Value)
			}
			table.Rows = append(table.Rows, row)
		}
		ds.Tables = append(ds.Tables, table)
	}
	return ds, nil
}

// loadXMLDataset reads a DbUnit flat XML dataset:
// <dataset><schema.table col="v" .../></dataset>
func loadXMLDataset(path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	ds := &Dataset{}
	index := map[string]int{}
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth != 2 {
				continue
			}
			name := t.Name.Local
			if t.Name.Space != "" {
				name = t.Name.Space + ":" + name
			}
			i, ok := index[name]
			if !ok {
				i = len(ds.Tables)
				index[name] = i
				ds.Tables = append(ds.Tables, DatasetTable{Name: name})
			}
			var row DatasetRow
			for _, a := range t.Attr {
				row.Columns = append(row.Columns, a.Name.Local)
				row.Values = append(row.Values, a.Value)
			}
			if len(row.Columns) > 0 { // an empty element only declares the table
				ds.Tables[i].Rows = append(ds.Tables[i].Rows, row)
			}
		case xml.EndElement:
			depth--
		}
	}
	return ds, nil
}

// loadJSONDataset reads {"schema.table": [{"col": value}, ...]} in key order.
func loadJSONDataset(path string) (*Dataset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.MapSlice // YAML is a superset of JSON and keeps key order
	if err := yaml.UnmarshalWithOptions(b, &root, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON dataset: %w", filepath.Base(path), err)
	}
	ds := &Dataset{}
	for _, t := range root {
		table := DatasetTable{Name: fmt.Sprint(t.Key)}
		rows, _ := t.Value.([]any)
		for _, r := range rows {
			m, ok := r.(yaml.MapSlice)
			if !ok {
				return nil, fmt.Errorf("%s: rows of %s must be objects", filepath.Base(path), table.Name)
			}
			var row DatasetRow
			for _, c := range m {
				row.Columns = append(row.Columns, fmt.Sprint(c.Key))
				v := c.Value
				if nested, ok := v.(yaml.MapSlice); ok { // JSON object value -> JSON text
					v = mapSliceJSON(nested)
				}
				row.Values = append(row.Values, v)
			}
			table.Rows = append(table.Rows, row)
		}
		ds.Tables = append(ds.Tables, table)
	}
	return ds, nil
}

func mapSliceJSON(m yaml.MapSlice) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, it := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(fmt.Sprint(it.Key))
		b.Write(k)
		b.WriteByte(':')
		if nested, ok := it.Value.(yaml.MapSlice); ok {
			b.WriteString(mapSliceJSON(nested))
			continue
		}
		v, _ := json.Marshal(it.Value)
		b.Write(v)
	}
	b.WriteByte('}')
	return b.String()
}

// loadCSVDataset reads a DbUnit CSV dataset directory.
func loadCSVDataset(dir string) (*Dataset, error) {
	var tables []string
	if b, err := os.ReadFile(filepath.Join(dir, "table-ordering.txt")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				tables = append(tables, line)
			}
		}
	} else {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.csv"))
		sort.Strings(matches)
		for _, m := range matches {
			tables = append(tables, strings.TrimSuffix(filepath.Base(m), ".csv"))
		}
	}
	ds := &Dataset{}
	for _, t := range tables {
		f, err := os.Open(filepath.Join(dir, t+".csv"))
		if err != nil {
			return nil, fmt.Errorf("CSV dataset: table %s listed but %s.csv not found", t, t)
		}
		records, err := readDbUnitCSV(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s.csv: %w", t, err)
		}
		table := DatasetTable{Name: t}
		if len(records) > 0 {
			header := records[0]
			for _, rec := range records[1:] {
				var row DatasetRow
				for i, col := range header {
					if i >= len(rec) {
						break
					}
					row.Columns = append(row.Columns, strings.TrimSpace(col))
					if rec[i] == "null" {
						row.Values = append(row.Values, nil)
					} else {
						row.Values = append(row.Values, rec[i])
					}
				}
				table.Rows = append(table.Rows, row)
			}
		}
		ds.Tables = append(ds.Tables, table)
	}
	return ds, nil
}

// loadXLSXDataset reads one sheet per table; the first row holds column names.
func loadXLSXDataset(path string) (*Dataset, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ds := &Dataset{}
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		table := DatasetTable{Name: sheet}
		if len(rows) > 0 {
			header := rows[0]
			for _, rec := range rows[1:] {
				var row DatasetRow
				for i, col := range header {
					row.Columns = append(row.Columns, col)
					if i < len(rec) && rec[i] != "" {
						row.Values = append(row.Values, rec[i])
					} else {
						row.Values = append(row.Values, nil)
					}
				}
				table.Rows = append(table.Rows, row)
			}
		}
		ds.Tables = append(ds.Tables, table)
	}
	return ds, nil
}

var replacer = regexp.MustCompile(`^\[(DAY|HOUR|MIN|SEC),\s*(NOW|PLUS|MINUS)\s*(?:,\s*(\d+))?\]$`)

// resolveValue applies Database Rider replacers and renders a value as a SQL
// literal (or NULL).
func resolveValue(d Dialect, v any, now time.Time) (string, error) {
	switch t := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return d.Literal(strconv.FormatBool(t)), nil
	case uint64:
		return d.Literal(strconv.FormatUint(t, 10)), nil
	case int, int64, int32:
		return d.Literal(fmt.Sprint(t)), nil
	case float64:
		return d.Literal(strconv.FormatFloat(t, 'f', -1, 64)), nil
	case time.Time:
		return d.Literal(t.Format("2006-01-02 15:04:05.999999")), nil
	case string:
		s := t
		switch s {
		case "[null]":
			return "NULL", nil
		case "[UNIX_TIMESTAMP]":
			return d.Literal(strconv.FormatInt(now.Unix(), 10)), nil
		}
		if m := replacer.FindStringSubmatch(s); m != nil {
			return d.Literal(shift(now, m[1], m[2], m[3]).Format("2006-01-02 15:04:05")), nil
		}
		if strings.HasPrefix(s, "groovy:") || strings.HasPrefix(s, "js:") {
			return "", fmt.Errorf("scripted value %q is not supported; use a literal or a [DAY,NOW]-style replacer", s)
		}
		return d.Literal(s), nil
	case []any, yaml.MapSlice, map[string]any:
		b, err := json.Marshal(normalizeYAML(t))
		if err != nil {
			return "", err
		}
		return d.Literal(string(b)), nil
	}
	return d.Literal(fmt.Sprint(v)), nil
}

func normalizeYAML(v any) any {
	switch t := v.(type) {
	case yaml.MapSlice:
		m := map[string]any{}
		for _, it := range t {
			m[fmt.Sprint(it.Key)] = normalizeYAML(it.Value)
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeYAML(e)
		}
		return out
	}
	return v
}

func shift(now time.Time, unit, op, n string) time.Time {
	if op == "NOW" {
		return now
	}
	k, _ := strconv.Atoi(n)
	if op == "MINUS" {
		k = -k
	}
	switch unit {
	case "DAY":
		return now.AddDate(0, 0, k)
	case "HOUR":
		return now.Add(time.Duration(k) * time.Hour)
	case "MIN":
		return now.Add(time.Duration(k) * time.Minute)
	default:
		return now.Add(time.Duration(k) * time.Second)
	}
}

// insert writes the dataset in one transaction, one INSERT per row, in order.
func (svc *Service) insert(ctx context.Context, ds *Dataset, now time.Time) error {
	d := svc.Target.Dialect
	tx, err := svc.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range ds.Tables {
		table, err := Ident(t.Name)
		if err != nil {
			return err
		}
		for i, r := range t.Rows {
			cols := make([]string, len(r.Columns))
			vals := make([]string, len(r.Values))
			for j, c := range r.Columns {
				if cols[j], err = Ident(c); err != nil {
					return err
				}
				if vals[j], err = resolveValue(d, r.Values[j], now); err != nil {
					return fmt.Errorf("%s row %d column %s: %w", t.Name, i+1, c, err)
				}
			}
			q := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(vals, ", ") + ")"
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("insert into %s (row %d): %w", t.Name, i+1, err)
			}
		}
	}
	return tx.Commit()
}

// readDbUnitCSV reads a CSV file the way DbUnit's CSV dataset reader did:
// fields are separated by commas, a field may be quoted with double quotes
// (commas and line breaks inside are literal), and a backslash escapes the
// next character inside or outside quotes (\" is a quote, \\ a backslash).
func readDbUnitCSV(r io.Reader) ([][]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var (
		records [][]string
		record  []string
		field   strings.Builder
		quoted  bool
		line    = 1
	)
	endField := func() {
		record = append(record, field.String())
		field.Reset()
	}
	endRecord := func() {
		endField()
		if len(record) != 1 || record[0] != "" { // skip blank lines
			records = append(records, record)
		}
		record = nil
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '\\' && i+1 < len(data):
			i++
			field.WriteByte(data[i])
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			endField()
		case (c == '\n' || c == '\r') && !quoted:
			if c == '\r' && i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
			endRecord()
			line++
		default:
			if c == '\n' {
				line++
			}
			field.WriteByte(c)
		}
	}
	if quoted {
		return nil, fmt.Errorf("line %d: a quoted field is not closed", line)
	}
	if field.Len() > 0 || len(record) > 0 {
		endRecord()
	}
	return records, nil
}
