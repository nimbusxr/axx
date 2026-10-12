package fixtures

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ddlSchema is statically parsed DDL: table names to ordered columns, from
// committed SQL, never a live database. It supports the CREATE TABLE /
// ALTER TABLE (ADD, DROP, RENAME COLUMN) subset real migrations use; every
// other statement is skipped.
//
// A column is required when it is NOT NULL (or PRIMARY KEY) with no DEFAULT
// and is not generated: the seed row must provide it.
type ddlSchema struct {
	order  []string
	tables map[string]*ddlTable
}

type ddlColumn struct {
	name     string
	required bool
}

type ddlTable struct {
	order   []string
	columns map[string]ddlColumn
	// refs are its foreign keys: the columns, and the table they reference as the DDL names it.
	refs []ddlRef
}

type ddlRef struct {
	columns []string
	table   string
}

func newDDLTable() *ddlTable { return &ddlTable{columns: map[string]ddlColumn{}} }

// reference records the foreign key def has, if it has one: a table's FOREIGN KEY constraint,
// or a column's REFERENCES (def is then the column's definition).
func (t *ddlTable) reference(def string) {
	if m := ddlForeignKey.FindStringSubmatch(def); m != nil {
		var cols []string
		for _, c := range strings.Split(m[1], ",") {
			cols = append(cols, normalizeIdentifier(strings.TrimFunc(c, isJavaWhitespace)))
		}
		t.refs = append(t.refs, ddlRef{columns: cols, table: normalizeIdentifier(m[2])})
		return
	}
	if m := ddlReferences.FindStringSubmatch(def); m != nil {
		column := regexp.MustCompile(`\s+`).Split(strings.TrimFunc(def, isJavaWhitespace), 2)[0]
		t.refs = append(t.refs, ddlRef{columns: []string{normalizeIdentifier(column)}, table: normalizeIdentifier(m[1])})
	}
}

func (t *ddlTable) put(c ddlColumn) {
	if _, ok := t.columns[c.name]; !ok {
		t.order = append(t.order, c.name)
	}
	t.columns[c.name] = c
}

func (t *ddlTable) remove(name string) (ddlColumn, bool) {
	c, ok := t.columns[name]
	if !ok {
		return c, false
	}
	delete(t.columns, name)
	for i, n := range t.order {
		if n == name {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
	return c, true
}

func (t *ddlTable) has(name string) bool {
	_, ok := t.columns[name]
	return ok
}

func (t *ddlTable) names() string { return javaListString(t.order) }

var (
	ddlCreate       = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([\w."]+)\s*\((.*)\)\s*$`)
	ddlAlter        = regexp.MustCompile(`(?is)^ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?([\w."]+)\s+(.*)$`)
	ddlAddColumn    = regexp.MustCompile(`(?is)^ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(.*)$`)
	ddlDropColumn   = regexp.MustCompile(`(?is)^DROP\s+(?:COLUMN\s+)?(?:IF\s+EXISTS\s+)?([\w"]+).*$`)
	ddlRenameColumn = regexp.MustCompile(`(?is)^RENAME\s+(?:COLUMN\s+)?([\w"]+)\s+TO\s+([\w"]+)\s*$`)
	ddlBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	ddlLineComment  = regexp.MustCompile(`(?m)--.*$`)
	ddlReferences   = regexp.MustCompile(`(?is)\bREFERENCES\s+([\w."]+)`)
	ddlForeignKey   = regexp.MustCompile(`(?is)\bFOREIGN\s+KEY\s*\(([^)]*)\)\s*REFERENCES\s+([\w."]+)`)
)

var tableConstraintStarters = map[string]bool{"PRIMARY": true, "FOREIGN": true, "CONSTRAINT": true, "UNIQUE": true, "CHECK": true, "EXCLUDE": true, "LIKE": true}

// defaultSchemas are where a table created without a schema lands: PostgreSQL's public and SQL
// Server's dbo. A seed's public.closures is a migration's closures, and the other way around.
var defaultSchemas = []string{"public", "dbo"}

// key is the name a table has in the schema: name itself, or the same table named with or
// without its default schema.
func (s *ddlSchema) key(name string) (string, bool) {
	name = strings.ToLower(name)
	if _, ok := s.tables[name]; ok {
		return name, true
	}
	if schema, table, qualified := strings.Cut(name, "."); qualified {
		if _, ok := s.tables[table]; ok && slices.Contains(defaultSchemas, schema) {
			return table, true
		}
		return "", false
	}
	for _, schema := range defaultSchemas {
		if _, ok := s.tables[schema+"."+name]; ok {
			return schema + "." + name, true
		}
	}
	return "", false
}

func (s *ddlSchema) table(name string) *ddlTable {
	if k, ok := s.key(name); ok {
		return s.tables[k]
	}
	return nil
}

func (s *ddlSchema) tableNames() string { return javaListString(s.order) }

// insertOrder is the order a seed's tables insert in without breaking a foreign key: a table
// after the tables it references, and otherwise in the order the DDL creates them. Only keys on
// columns the seed fills count (filled holds them by table), since a NULL key references nothing.
// A table the DDL does not have, and tables whose keys reference each other, keep their order.
func (s *ddlSchema) insertOrder(tables []string, filled map[string]map[string]bool) []string {
	position := map[string]int{}
	for i, t := range s.order {
		position[t] = i
	}
	rank := func(table string) int {
		if k, ok := s.key(table); ok {
			return position[k]
		}
		return len(s.order)
	}
	pending := slices.Clone(tables)
	sort.SliceStable(pending, func(i, j int) bool { return rank(pending[i]) < rank(pending[j]) })
	// parents are the tables of the seed each table's foreign keys reference.
	parents := map[string][]string{}
	for _, table := range pending {
		t := s.table(table)
		if t == nil {
			continue
		}
		for _, ref := range t.refs {
			rk, ok := s.key(ref.table)
			if !ok || !slices.ContainsFunc(ref.columns, func(c string) bool { return filled[table][c] }) {
				continue
			}
			for _, other := range pending {
				if k, _ := s.key(other); other != table && k == rk {
					parents[table] = append(parents[table], other)
				}
			}
		}
	}
	placed := map[string]bool{}
	out := make([]string, 0, len(pending))
	for len(pending) > 0 {
		next := 0 // when every table waits for another (a cycle), the first goes
		for i, table := range pending {
			if !slices.ContainsFunc(parents[table], func(p string) bool { return !placed[p] }) {
				next = i
				break
			}
		}
		placed[pending[next]] = true
		out = append(out, pending[next])
		pending = slices.Delete(pending, next, next+1)
	}
	return out
}

// parseDDL parses a SQL file, or every *.sql file under a directory in
// sorted order.
func parseDDL(path string) (*ddlSchema, error) {
	files, err := sqlFiles(path)
	if err != nil {
		return nil, err
	}
	s := &ddlSchema{tables: map[string]*ddlTable{}}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, schemaError("cannot read DDL %s: %v", f, err)
		}
		for _, stmt := range splitStatements(stripSQLComments(string(data))) {
			s.apply(strings.TrimFunc(stmt, isJavaWhitespace), f)
		}
	}
	if len(s.tables) == 0 {
		return nil, schemaError("no CREATE TABLE statements found under %s", path)
	}
	return s, nil
}

// isJavaWhitespace is the String.strip() predicate for ASCII input.
func isJavaWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' || r >= 0x1C && r <= 0x1F
}

func sqlFiles(path string) ([]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, schemaError("DDL path does not exist: %s", path)
	}
	if fi.Mode().IsRegular() {
		return []string{path}, nil
	}
	if !fi.IsDir() {
		return nil, schemaError("DDL path does not exist: %s", path)
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".sql") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, schemaError("cannot scan %s: %v", path, err)
	}
	if len(files) == 0 {
		return nil, schemaError("no *.sql files under %s", path)
	}
	sort.Strings(files)
	return files, nil
}

func (s *ddlSchema) apply(stmt, file string) {
	if stmt == "" {
		return
	}
	if m := ddlCreate.FindStringSubmatch(stmt); m != nil {
		name := normalizeIdentifier(m[1])
		if k, ok := s.key(name); ok {
			name = k // created again, under its other name
		}
		t := newDDLTable()
		for _, def := range splitTopLevel(m[2]) {
			if c, ok := parseColumn(strings.TrimFunc(def, isJavaWhitespace)); ok {
				t.put(c)
			}
			t.reference(def)
		}
		if _, ok := s.tables[name]; !ok {
			s.order = append(s.order, name)
		}
		s.tables[name] = t
		return
	}
	if m := ddlAlter.FindStringSubmatch(stmt); m != nil {
		name := normalizeIdentifier(m[1])
		t := s.table(name)
		if t == nil {
			slog.Debug("fixtures DDL: ALTER for unknown table skipped", "table", name, "file", file)
			return
		}
		for _, action := range splitTopLevel(m[2]) {
			action = strings.TrimFunc(action, isJavaWhitespace)
			applyAlterAction(action, t)
			if m := ddlAddColumn.FindStringSubmatch(action); m != nil {
				t.reference(strings.TrimFunc(m[1], isJavaWhitespace)) // the column's definition
			}
		}
		return
	}
	slog.Debug("fixtures DDL: skipping unsupported statement", "file", filepath.Base(file), "statement", strings.SplitN(stmt, "\n", 2)[0])
}

func applyAlterAction(action string, t *ddlTable) {
	if m := ddlAddColumn.FindStringSubmatch(action); m != nil {
		if c, ok := parseColumn(strings.TrimFunc(m[1], isJavaWhitespace)); ok {
			t.put(c)
		}
		return
	}
	if m := ddlDropColumn.FindStringSubmatch(action); m != nil {
		t.remove(normalizeIdentifier(m[1]))
		return
	}
	if m := ddlRenameColumn.FindStringSubmatch(action); m != nil {
		if old, ok := t.remove(normalizeIdentifier(m[1])); ok {
			n := normalizeIdentifier(m[2])
			t.put(ddlColumn{name: n, required: old.required})
		}
	}
}

// parseColumn reads one column definition; table constraints are skipped.
func parseColumn(def string) (ddlColumn, bool) {
	if def == "" {
		return ddlColumn{}, false
	}
	tokens := regexp.MustCompile(`\s+`).Split(def, 2)
	if tableConstraintStarters[strings.ToUpper(tokens[0])] {
		return ddlColumn{}, false
	}
	rest := ""
	if len(tokens) > 1 {
		rest = strings.ToUpper(tokens[1])
	}
	notNull := strings.Contains(rest, "NOT NULL") || strings.Contains(rest, "PRIMARY KEY")
	hasDefault := strings.Contains(rest, "DEFAULT ") || strings.HasSuffix(rest, "DEFAULT")
	generated := strings.Contains(rest, "GENERATED ") || strings.Contains(rest, "SERIAL")
	return ddlColumn{name: normalizeIdentifier(tokens[0]), required: notNull && !hasDefault && !generated}, true
}

func normalizeIdentifier(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, `"`, ""))
}

// splitTopLevel splits on commas at parenthesis depth zero.
func splitTopLevel(text string) []string {
	var out []string
	depth := 0
	var cur strings.Builder
	for _, c := range text {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		}
		if c == ',' && depth == 0 {
			out = append(out, cur.String())
			cur.Reset()
		} else {
			cur.WriteRune(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func splitStatements(sql string) []string {
	var out []string
	depth := 0
	inString := false
	var cur strings.Builder
	for _, c := range sql {
		if c == '\'' {
			inString = !inString
		}
		if !inString {
			switch {
			case c == '(':
				depth++
			case c == ')':
				depth--
			case c == ';' && depth == 0:
				out = append(out, cur.String())
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(c)
	}
	if strings.TrimFunc(cur.String(), isJavaWhitespace) != "" {
		out = append(out, cur.String())
	}
	return out
}

func stripSQLComments(sql string) string {
	return ddlLineComment.ReplaceAllString(ddlBlockComment.ReplaceAllString(sql, " "), "")
}
