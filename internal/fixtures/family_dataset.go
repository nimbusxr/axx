package fixtures

import (
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// datasetFamily is family: dataset: tabular documents (named tables of
// homogeneous rows), the shape DBUnit calls a dataset and the SQL pack
// seeds from. options.format selects the rendering: yaml (default, the
// Database Rider dialect), xml (DBUnit flat XML) or csv (DBUnit's directory
// layout: <table>.csv files plus table-ordering.txt per fixture).
//
// A prototype entry is a per-table row template deep-merged under every row
// of that table; identities are table.column paths enforced per row, deriving
// prefix + fixtureKey for the first row and prefix + fixtureKey-N for later
// ones. factory.schema is optional committed DDL: with it, tables and
// columns are typo-guarded, NOT NULL columns without a database default are
// required, and tables emit in DDL order.
type datasetFamily struct{}

func (datasetFamily) Name() string { return "dataset" }

var datasetFormats = []string{"csv", "xml", "yaml"}

func datasetFormat(spec *Spec) (string, error) {
	declared := get(spec.Options, "format")
	if declared == nil {
		return "yaml", nil
	}
	format := strings.ToLower(valueOf(declared))
	for _, f := range datasetFormats {
		if f == format {
			return format, nil
		}
	}
	return "", specError("%s: options.format must be one of %s, got '%s'", spec.SourceName, javaListString(datasetFormats), valueOf(declared))
}

func datasetDDL(spec *Spec, baseDir string) (*ddlSchema, error) {
	ref := spec.SchemaRef()
	if strings.TrimSpace(ref) == "" {
		return nil, nil
	}
	if err := checkRef(ref); err != nil {
		return nil, err
	}
	return parseDDL(filepath.Join(baseDir, filepath.FromSlash(ref)))
}

func (d datasetFamily) Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	if spec.Options.Has("table") {
		return nil, specError("%s: options.table was removed - a dataset fixture is a whole document ({table: [rows...]}); declare tables in the fixture data", spec.SourceName)
	}
	format, err := datasetFormat(spec)
	if err != nil {
		return nil, err
	}
	schema, err := datasetDDL(spec, baseDir)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]byte{}
	var unresolved []string
	for _, key := range spec.Fixtures.SortedKeys() {
		fx := spec.Fixtures.Get(key)
		templates := spec.PrototypeFor(fx.Dir)
		w := where(spec, key)
		resolved := jsonx.NewObject()
		for _, rawTable := range fx.Data.Keys() {
			table := strings.ToLower(rawTable)
			var columns *ddlTable
			if schema != nil {
				if columns = schema.table(table); columns == nil {
					return nil, genError("%s: table '%s' not found in DDL (tables: %s)", w, rawTable, schema.tableNames())
				}
			}
			rows, ok := get(fx.Data, rawTable).([]any)
			if !ok {
				return nil, genError("%s: table '%s' must map to a list of rows", w, table)
			}
			template, _ := get(templates, rawTable).(*jsonx.Object)
			var resolvedRows []any
			for i, raw := range rows {
				rowMap, ok := raw.(*jsonx.Object)
				if !ok {
					return nil, genError("%s: table '%s' row %d must be a map", w, table, i+1)
				}
				merged := deepMerge(template, rowMap)
				if err := ctx.eval.evaluateTree(merged, w+" "+table+"["+itoa(i)+"]", nil); err != nil {
					return nil, err
				}
				if err := d.applyIdentities(spec, ctx, key, table, merged, i); err != nil {
					return nil, err
				}
				var row *jsonx.Object
				if columns == nil {
					row, err = resolveRowSchemaFree(spec, table, merged, key)
				} else {
					row, err = resolveRow(spec, columns, table, merged, key, &unresolved)
				}
				if err != nil {
					return nil, err
				}
				resolvedRows = append(resolvedRows, row)
			}
			if resolvedRows == nil {
				resolvedRows = []any{}
			}
			resolved.Set(table, resolvedRows)
		}
		if len(unresolved) == 0 {
			rendered, err := renderDataset(format, key, orderedTables(schema, resolved))
			if err != nil {
				return nil, err
			}
			out[key] = rendered
		}
	}
	if len(unresolved) > 0 {
		return nil, genError("unresolved required column(s):\n  %s\nfix: add the column to defaults:, the prototype row template, or the row in %s",
			joinLines(unresolved), spec.SourceName)
	}
	return out, nil
}

// orderedTables puts tables in DDL declaration order when a schema is
// declared; authored order otherwise.
func orderedTables(schema *ddlSchema, resolved *jsonx.Object) *jsonx.Object {
	if schema == nil {
		return resolved
	}
	out := jsonx.NewObject()
	for _, t := range schema.order {
		if resolved.Has(t) {
			out.Set(t, get(resolved, t))
		}
	}
	return out
}

func (datasetFamily) applyIdentities(spec *Spec, ctx *ExpansionContext, key, table string, row *jsonx.Object, index int) error {
	for _, id := range spec.Identity {
		dot := strings.LastIndexByte(id.Path, '.')
		if dot < 0 || strings.ToLower(id.Path[:dot]) != table {
			continue
		}
		column := id.Path[dot+1:]
		present := get(row, column)
		var value string
		switch {
		case present != nil:
			value = valueOf(present)
		case id.Derive == "authored":
			return genError("identity %s is derive: authored but fixtures.%s %s[%d] does not provide a value (%s)", id.Path, key, table, index, spec.SourceName)
		default:
			name := key
			if index > 0 {
				name = key + "-" + itoa(index+1)
			}
			value = ctx.identities.derive(id, name)
		}
		if err := ctx.identities.claim(value, spec.SourceName, key+" "+table+"["+itoa(index)+"]", id.Path); err != nil {
			return err
		}
		row.Set(column, value)
	}
	return nil
}

func resolveRow(spec *Spec, columns *ddlTable, table string, merged *jsonx.Object, key string, unresolved *[]string) (*jsonx.Object, error) {
	remaining := copyObjectShallow(merged)
	row := jsonx.NewObject()
	for _, name := range columns.order {
		col := columns.columns[name]
		present := remaining.Has(name)
		value := get(remaining, name)
		remaining.Delete(name)
		if !present {
			dotted := table + "." + name
			switch {
			case spec.Defaults.Has(dotted):
				value, present = get(spec.Defaults, dotted), true
			case spec.Defaults.Has(name):
				value, present = get(spec.Defaults, name), true
			}
		}
		if present {
			if err := scalarOnly(spec, name, value, key); err != nil {
				return nil, err
			}
			row.Set(name, value)
		} else if col.required {
			*unresolved = append(*unresolved, "column '"+table+"."+name+"' (NOT NULL, no database default) in fixtures."+key+" ("+spec.SourceName+")")
		}
	}
	if remaining.Len() > 0 {
		return nil, genError("unknown column(s) %s in fixtures.%s - table %s has %s (%s)", javaSetString(remaining), key, table, columns.names(), spec.SourceName)
	}
	return row, nil
}

func resolveRowSchemaFree(spec *Spec, table string, merged *jsonx.Object, key string) (*jsonx.Object, error) {
	row := jsonx.NewObject()
	for _, col := range merged.Keys() {
		v := get(merged, col)
		if err := scalarOnly(spec, col, v, key); err != nil {
			return nil, err
		}
		row.Set(col, v)
	}
	prefix := table + "."
	for _, k := range spec.Defaults.Keys() {
		switch {
		case strings.HasPrefix(k, prefix):
			if c := k[len(prefix):]; !row.Has(c) || get(row, c) == nil {
				row.Set(c, get(spec.Defaults, k))
			}
		case !strings.Contains(k, "."):
			return nil, specError("%s: bare-name default '%s' needs a declared factory.schema (DDL) to know which tables carry the column - use a dotted table.column default, or declare the schema", spec.SourceName, k)
		}
	}
	return row, nil
}

func scalarOnly(spec *Spec, column string, v any, key string) error {
	switch v.(type) {
	case *jsonx.Object, []any:
		return genError("column '%s' in fixtures.%s must be a scalar (encode JSON columns as strings) (%s)", column, key, spec.SourceName)
	}
	return nil
}

func renderDataset(format, key string, doc *jsonx.Object) (map[string][]byte, error) {
	switch format {
	case "xml":
		return map[string][]byte{key + ".xml": writeFlatXML(doc)}, nil
	case "csv":
		return writeDatasetCSV(key, doc), nil
	}
	out, err := jyaml.Marshal(doc, yamlFactory)
	if err != nil {
		return nil, genError("cannot serialize dataset: %v", err)
	}
	return map[string][]byte{key + ".yaml": out}, nil
}

// writeFlatXML is DBUnit flat XML: one element per row, columns as
// attributes, null columns omitted.
func writeFlatXML(doc *jsonx.Object) []byte {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<dataset>\n")
	for _, table := range doc.Keys() {
		rows, _ := get(doc, table).([]any)
		for _, r := range rows {
			row, _ := r.(*jsonx.Object)
			b.WriteString("  <" + table)
			for _, col := range keys(row) {
				if v := get(row, col); v != nil {
					b.WriteString(" " + col + "=\"" + escapeXMLAttr(valueOf(v)) + "\"")
				}
			}
			b.WriteString("/>\n")
		}
	}
	b.WriteString("</dataset>\n")
	return []byte(b.String())
}

func escapeXMLText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func escapeXMLAttr(s string) string {
	return strings.ReplaceAll(escapeXMLText(s), "\"", "&quot;")
}

// writeDatasetCSV is DBUnit's CSV layout: <key>/<table>.csv with a header
// row (first-appearance column order) plus <key>/table-ordering.txt. Nulls
// are the literal null.
func writeDatasetCSV(key string, doc *jsonx.Object) map[string][]byte {
	out := map[string][]byte{}
	var ordering strings.Builder
	for _, table := range doc.Keys() {
		ordering.WriteString(table + "\n")
		rawRows, _ := get(doc, table).([]any)
		var columns []string
		seen := map[string]bool{}
		for _, r := range rawRows {
			for _, c := range keys(r.(*jsonx.Object)) {
				if !seen[c] {
					seen[c] = true
					columns = append(columns, c)
				}
			}
		}
		var csv strings.Builder
		csv.WriteString(strings.Join(columns, ",") + "\n")
		for _, r := range rawRows {
			row := r.(*jsonx.Object)
			cells := make([]string, len(columns))
			for i, c := range columns {
				if v := get(row, c); v == nil {
					cells[i] = "null"
				} else {
					cells[i] = escapeCSV(valueOf(v))
				}
			}
			csv.WriteString(strings.Join(cells, ",") + "\n")
		}
		out[key+"/"+table+".csv"] = []byte(csv.String())
	}
	out[key+"/table-ordering.txt"] = []byte(ordering.String())
	return out
}

// escapeCSV follows DBUnit's CSV dialect: quotes inside quoted fields are
// escaped with a backslash, not doubled.
func escapeCSV(v string) string {
	if strings.ContainsAny(v, ",\"\n\r") || v == "null" {
		return "\"" + strings.ReplaceAll(strings.ReplaceAll(v, "\\", "\\\\"), "\"", "\\\"") + "\""
	}
	return v
}

// LintFilePatterns: datasets are not JSON; structural rules do not apply.
func (datasetFamily) LintFilePatterns(*Spec) []string { return nil }

// LintJSONPath is unused (no patterns).
func (datasetFamily) LintJSONPath(_ *Spec, p string) string { return p }

// Validate lints a dataset file (managed or hand-written, format detected
// from its name) against the DDL: tables must exist and every row's columns
// must exist.
func (datasetFamily) Validate(data []byte, baseDir, schemaRef, fixtureName string) error {
	if err := checkRef(schemaRef); err != nil {
		return err
	}
	schema, err := parseDDL(filepath.Join(baseDir, filepath.FromSlash(schemaRef)))
	if err != nil {
		return err
	}
	doc, err := readDatasetByName(data, fixtureName, schema)
	if err != nil || doc == nil {
		return err
	}
	var problems []string
	for _, table := range doc.Keys() {
		columns := schema.table(table)
		if columns == nil {
			problems = append(problems, "table '"+table+"' not found in DDL (tables: "+schema.tableNames()+")")
			continue
		}
		rows, ok := get(doc, table).([]any)
		if !ok {
			problems = append(problems, "table '"+table+"' must map to a list of rows")
			continue
		}
		for _, r := range rows {
			row, ok := r.(*jsonx.Object)
			if !ok {
				continue
			}
			for _, col := range row.Keys() {
				if !columns.has(strings.ToLower(col)) {
					problems = append(problems, "table '"+table+"' has no column '"+col+"' (columns: "+columns.names()+")")
				}
			}
		}
	}
	if len(problems) > 0 {
		return genError("%s does not match the DDL:\n  %s", fixtureName, joinLines(problems))
	}
	return nil
}

// readDatasetByName reads any of the three renderings back into the tabular
// document; table-ordering.txt is validated inline (nil document).
func readDatasetByName(data []byte, name string, schema *ddlSchema) (*jsonx.Object, error) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "table-ordering.txt"):
		var problems []string
		for _, t := range strings.Split(string(data), "\n") {
			t = strings.TrimFunc(t, isJavaWhitespace)
			if t != "" && schema.table(strings.ToLower(t)) == nil {
				problems = append(problems, "table '"+t+"' not found in DDL")
			}
		}
		if len(problems) > 0 {
			return nil, genError("%s does not match the DDL:\n  %s", name, joinLines(problems))
		}
		return nil, nil
	case strings.HasSuffix(lower, ".xml"):
		return readFlatXML(data, name)
	case strings.HasSuffix(lower, ".csv"):
		table := name[strings.LastIndexByte(name, '/')+1:]
		table = table[:len(table)-len(".csv")]
		header := strings.SplitN(string(data), "\n", 2)[0]
		row := jsonx.NewObject()
		if strings.TrimFunc(header, isJavaWhitespace) != "" {
			for _, c := range strings.Split(strings.TrimFunc(header, isJavaWhitespace), ",") {
				row.Set(strings.TrimFunc(c, isJavaWhitespace), nil)
			}
		}
		return newObject(table, []any{row}), nil
	}
	return readDatasetDocument(data, name)
}

func readDatasetDocument(data []byte, where string) (*jsonx.Object, error) {
	v, err := jyaml.Unmarshal(data)
	if err != nil {
		return nil, genError("%s is not valid YAML: %v", where, err)
	}
	if v == nil {
		return jsonx.NewObject(), nil
	}
	doc, ok := v.(*jsonx.Object)
	if !ok {
		return nil, genError("%s is not valid YAML: expected a mapping of tables", where)
	}
	return doc, nil
}

func readFlatXML(data []byte, where string) (*jsonx.Object, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	doc := jsonx.NewObject()
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, genError("%s is not valid flat XML: %v", where, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := qualified(t.Name)
			if depth == 1 {
				if name != "dataset" {
					return nil, genError("%s: flat XML root must be <dataset>", where)
				}
				continue
			}
			if depth == 2 {
				row := jsonx.NewObject()
				for _, a := range t.Attr {
					row.Set(qualified(a.Name), a.Value)
				}
				rows, _ := get(doc, name).([]any)
				doc.Set(name, append(rows, row))
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return nil, genError("%s is not valid flat XML: DOCTYPE is not allowed", where)
		}
	}
	return doc, nil
}

func qualified(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// Adoption of YAML datasets: documents in, per-table row templates out.
func (d datasetFamily) Adoption(baseDir, schemaRef string) (Adoption, error) {
	return &datasetAdoption{family: d, baseDir: baseDir, schemaRef: schemaRef}, nil
}

type datasetAdoption struct {
	family    datasetFamily
	baseDir   string
	schemaRef string
}

func (a *datasetAdoption) Decode(data []byte, where string) (any, error) {
	if err := a.family.Validate(data, a.baseDir, a.schemaRef, where); err != nil {
		return nil, err
	}
	return readDatasetDocument(data, where)
}

func (a *datasetAdoption) AuthoringTree(data []byte, where string) (*jsonx.Object, error) {
	return readDatasetDocument(data, where)
}

func (a *datasetAdoption) Shape() (*FieldShape, error) { return shapeRoot(nil), nil }

func (a *datasetAdoption) Analyze(fixtureKeys []string, trees map[string]*jsonx.Object) (*AdoptionAnalysis, error) {
	// Every row per table across every document, tables in sorted order.
	rowsByTable := map[string][]*jsonx.Object{}
	var tables []string
	for _, k := range fixtureKeys {
		doc := trees[k]
		for _, t := range doc.Keys() {
			list, ok := get(doc, t).([]any)
			if !ok {
				continue
			}
			lt := strings.ToLower(t)
			for _, r := range list {
				if row, ok := r.(*jsonx.Object); ok {
					if _, seen := rowsByTable[lt]; !seen {
						tables = append(tables, lt)
					}
					rowsByTable[lt] = append(rowsByTable[lt], row)
				}
			}
		}
	}
	sortStrings(tables)

	// Identity candidates: string columns present in every row of a table,
	// all distinct.
	var identityPaths []string
	for _, t := range tables {
		rows := rowsByTable[t]
		if len(rows) < 2 {
			continue
		}
		for _, col := range rows[0].Keys() {
			seen := map[string]bool{}
			everywhere := true
			for _, row := range rows {
				s, ok := get(row, col).(string)
				if !ok {
					everywhere = false
					break
				}
				seen[s] = true
			}
			if everywhere && len(seen) == len(rows) {
				identityPaths = append(identityPaths, t+"."+col)
			}
		}
	}
	// Identity values share one namespace: a column echoing an earlier
	// candidate's values cannot be an identity too.
	claimed := map[string]bool{}
	var kept []string
	for _, p := range identityPaths {
		dot := strings.LastIndexByte(p, '.')
		t, col := p[:dot], p[dot+1:]
		var values []string
		overlap := false
		for _, row := range rowsByTable[t] {
			v := valueOf(get(row, col))
			values = append(values, v)
			overlap = overlap || claimed[v]
		}
		if overlap {
			continue
		}
		for _, v := range values {
			claimed[v] = true
		}
		kept = append(kept, p)
	}
	identityPaths = kept
	isIdentity := func(p string) bool {
		for _, q := range identityPaths {
			if q == p {
				return true
			}
		}
		return false
	}

	// Row template per table: columns present in every row, modal value
	// shared by more than one row.
	prototype := jsonx.NewObject()
	for _, t := range tables {
		rows := rowsByTable[t]
		template := jsonx.NewObject()
		var columns []string
		seen := map[string]bool{}
		for _, row := range rows {
			for _, c := range row.Keys() {
				if !seen[c] {
					seen[c] = true
					columns = append(columns, c)
				}
			}
		}
		for _, c := range columns {
			if isIdentity(t + "." + c) {
				continue
			}
			everywhere := true
			var values []any
			for _, row := range rows {
				everywhere = everywhere && row.Has(c)
				values = append(values, get(row, c))
			}
			if !everywhere {
				continue
			}
			best, count := modal(values)
			if count > 1 {
				template.Set(c, best)
			}
		}
		if template.Len() > 0 {
			prototype.Set(t, template)
		}
	}

	// Per-fixture documents: rows keep their deltas plus pinned identities.
	fixtures := map[string]*jsonx.Object{}
	for _, k := range fixtureKeys {
		doc := trees[k]
		data := jsonx.NewObject()
		for _, rawTable := range doc.Keys() {
			t := strings.ToLower(rawTable)
			template, _ := get(prototype, t).(*jsonx.Object)
			deltas := []any{}
			if list, ok := get(doc, rawTable).([]any); ok {
				for _, r := range list {
					row := r.(*jsonx.Object)
					delta := jsonx.NewObject()
					for _, col := range row.Keys() {
						v := get(row, col)
						covered := has(template, col) && deepEquals(get(template, col), v)
						if isIdentity(t+"."+col) || !covered {
							delta.Set(col, v)
						}
					}
					deltas = append(deltas, delta)
				}
			}
			data.Set(t, deltas)
		}
		fixtures[k] = data
	}
	return &AdoptionAnalysis{IdentityPaths: identityPaths, Prototype: prototype, Fixtures: fixtures}, nil
}
