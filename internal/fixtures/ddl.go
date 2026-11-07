package fixtures

import (
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ddlSchema is statically parsed DDL: table names to ordered columns, from committed SQL,
// never a live database. It reads a migration history (a folder's files applied in order) or
// the schema a database's own tool wrote (pg_dump, mysqldump, SqlPackage, Oracle's
// DBMS_METADATA, SQLite's sqlite_master), in the SQL of PostgreSQL, MySQL, SQL Server, Oracle
// and SQLite: CREATE TABLE, ALTER TABLE, DROP TABLE and table renames. Every other statement
// is skipped.
//
// A column is required when the database refuses an INSERT that leaves it out: it is NOT
// NULL (or in the primary key) with no DEFAULT, and the database does not fill it itself.
type ddlSchema struct {
	order  []string
	tables map[string]*ddlTable
	// mysql reads MODIFY as MySQL does: the column's whole definition again.
	mysql bool
	// sqlite reads a primary key as SQLite does: it allows NULL, and an INTEGER one is the rowid.
	sqlite bool
}

type ddlColumn struct {
	name      string
	typ       string // the declared type, lower case
	notNull   bool
	defaulted bool // a DEFAULT other than NULL
	generated bool // the database fills it: identity, serial, AUTO_INCREMENT, computed, rowversion, rowid
	key       bool // in the primary key
}

// required reports whether a seed row must give the column.
func (c ddlColumn) required() bool { return c.notNull && !c.defaulted && !c.generated }

type ddlTable struct {
	order   []string
	columns map[string]ddlColumn
	// refs are its foreign keys: the columns, and the table they reference as the DDL names it.
	refs []ddlRef
	// named are its NOT NULL and DEFAULT constraints by name, which DROP CONSTRAINT takes away.
	named map[string]ddlNamed
	// partitions are the tables PostgreSQL routes its rows to, which its ALTER TABLE changes too.
	partitions []string
}

type ddlRef struct {
	name    string // the constraint's name, when the DDL gives one
	columns []string
	table   string
}

type ddlNamed struct {
	column  string
	notNull bool // its NOT NULL; otherwise its DEFAULT
}

func newDDLTable() *ddlTable {
	return &ddlTable{columns: map[string]ddlColumn{}, named: map[string]ddlNamed{}}
}

// put adds a column at the end, or changes the one of that name where it is.
func (t *ddlTable) put(c ddlColumn) {
	if _, ok := t.columns[c.name]; !ok {
		t.order = append(t.order, c.name)
	}
	t.columns[c.name] = c
}

// place puts a column where MySQL's FIRST or AFTER other says, or else as put does.
func (t *ddlTable) place(c ddlColumn, first bool, after string) {
	if !first && !t.has(after) {
		t.put(c)
		return
	}
	t.order = slices.DeleteFunc(t.order, func(n string) bool { return n == c.name })
	position := 0
	if !first {
		position = slices.Index(t.order, after) + 1
	}
	t.order = slices.Insert(t.order, position, c.name)
	t.columns[c.name] = c
}

func (t *ddlTable) remove(name string) {
	if _, ok := t.columns[name]; !ok {
		return
	}
	delete(t.columns, name)
	t.order = slices.DeleteFunc(t.order, func(n string) bool { return n == name })
	t.refs = slices.DeleteFunc(t.refs, func(r ddlRef) bool { return slices.Contains(r.columns, name) })
	for n, c := range t.named {
		if c.column == name {
			delete(t.named, n)
		}
	}
}

// rename gives a column a new name where it is.
func (t *ddlTable) rename(from, to string) {
	c, ok := t.columns[from]
	if !ok || from == to {
		return
	}
	delete(t.columns, from)
	c.name = to
	t.columns[to] = c
	t.order[slices.Index(t.order, from)] = to
	for i := range t.refs {
		t.refs[i].columns = slices.Clone(t.refs[i].columns)
		for j, n := range t.refs[i].columns {
			if n == from {
				t.refs[i].columns[j] = to
			}
		}
	}
	for n, c := range t.named {
		if c.column == from {
			t.named[n] = ddlNamed{column: to, notNull: c.notNull}
		}
	}
}

// change applies f to a column, if the table has it.
func (t *ddlTable) change(name string, f func(*ddlColumn)) {
	if c, ok := t.columns[name]; ok {
		f(&c)
		t.columns[name] = c
	}
}

// dropConstraint takes away the named constraint: a column's NOT NULL or DEFAULT, or a foreign key.
func (t *ddlTable) dropConstraint(name string) {
	if n, ok := t.named[name]; ok {
		t.change(n.column, func(c *ddlColumn) {
			if n.notNull {
				c.notNull = false
			} else {
				c.defaulted = false
			}
		})
		delete(t.named, name)
	}
	t.refs = slices.DeleteFunc(t.refs, func(r ddlRef) bool { return r.name == name })
}

func (t *ddlTable) has(name string) bool {
	_, ok := t.columns[name]
	return ok
}

func (t *ddlTable) names() string { return javaListString(t.order) }

// defaultSchemas are where a table created without a schema lands: PostgreSQL's public, SQL
// Server's dbo and SQLite's main. A seed's public.closures is a migration's closures, and the
// other way around.
var defaultSchemas = []string{"public", "dbo", "main"}

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

// mysqlMarks are what only MySQL's SQL has. A schema that has one is read as MySQL's: backslash
// escapes in strings, # comments, and MODIFY as the column's whole definition.
var mysqlMarks = regexp.MustCompile(`(?im)\bAUTO_INCREMENT\b|\bENGINE\s*=|/\*!|^\s*DELIMITER\s|\bUNSIGNED\b|\b(?:MODIFY|CHANGE)\s+COLUMN\b`)

// parseDDL parses a SQL file, or every *.sql file under a directory in
// sorted order.
func parseDDL(path string) (*ddlSchema, error) {
	files, err := sqlFiles(path)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(files))
	s := &ddlSchema{tables: map[string]*ddlTable{}}
	for i, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, schemaError("cannot read DDL %s: %v", f, err)
		}
		texts[i] = string(data)
		s.mysql = s.mysql || mysqlMarks.Match(data)
	}
	statements := make([][][]sqlToken, len(files))
	for i, text := range texts {
		statements[i] = splitSQL(text, s.mysql)
		s.sqlite = s.sqlite || slices.ContainsFunc(statements[i], isSQLite)
	}
	for i, f := range files {
		for _, stmt := range statements[i] {
			s.apply(stmt, f)
		}
	}
	if len(s.tables) == 0 {
		return nil, schemaError("no CREATE TABLE statements found under %s", path)
	}
	return s, nil
}

// isSQLite reports whether a statement has what only SQLite's SQL has: AUTOINCREMENT, or a
// table that is WITHOUT ROWID or STRICT.
func isSQLite(stmt []sqlToken) bool {
	for i, t := range stmt {
		if t.is("AUTOINCREMENT") || t.is("ROWID") && i > 0 && stmt[i-1].is("WITHOUT") {
			return true
		}
		if t.is("STRICT") && stmt[0].is("CREATE") && slices.ContainsFunc(stmt[:i], func(t sqlToken) bool { return t.is("TABLE") }) &&
			(stmt[i-1].isSymbol(")") || stmt[i-1].isSymbol(",")) {
			return true
		}
	}
	return false
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

func (s *ddlSchema) apply(stmt []sqlToken, file string) {
	c := &sqlCursor{toks: stmt}
	switch {
	case c.at("CREATE"):
		if s.createTable(c, file) {
			return
		}
	case c.accept("ALTER", "TABLE"):
		s.alterTable(c, file)
		return
	case c.accept("DROP", "TABLE"):
		s.dropTables(c)
		return
	case c.at("DROP", "TEMPORARY", "TABLE"):
		return
	case c.accept("RENAME", "TABLE"), c.accept("RENAME"): // MySQL's, and Oracle's RENAME a TO b
		for _, pair := range splitCommas(c.rest()) {
			p := &sqlCursor{toks: pair}
			if from := p.name(); p.accept("TO") {
				if k, ok := s.key(from); ok {
					s.renameTable(k, p.name())
				}
			}
		}
		return
	case c.accept("EXEC"), c.accept("EXECUTE"):
		if name := c.name(); name == "sp_rename" || strings.HasSuffix(name, ".sp_rename") {
			s.spRename(c)
			return
		}
	}
	slog.Debug("fixtures DDL: skipping unsupported statement", "file", filepath.Base(file), "statement", tokenText(stmt[:min(len(stmt), 6)]))
}

// tokenText is tokens as text again, for messages.
func tokenText(toks []sqlToken) string {
	words := make([]string, len(toks))
	for i, t := range toks {
		words[i] = t.text
	}
	return strings.Join(words, " ")
}

// tableQualifiers are the words CREATE TABLE may have between its two words.
var tableQualifiers = map[string]bool{"OR": true, "REPLACE": true, "GLOBAL": true, "LOCAL": true, "UNLOGGED": true, "TEMPORARY": true, "TEMP": true, "PRIVATE": true}

// createTable reads CREATE TABLE, reporting false for a CREATE of something else. A
// temporary table is skipped: it is gone when the migration's session ends.
func (s *ddlSchema) createTable(c *sqlCursor, file string) bool {
	c.next()
	temporary, replace := false, false
	for !c.at("TABLE") {
		t := c.next()
		if t.kind != sqlWord || !tableQualifiers[strings.ToUpper(t.text)] {
			return false
		}
		temporary = temporary || t.is("TEMPORARY") || t.is("TEMP")
		replace = replace || t.is("REPLACE")
	}
	c.next()
	ifNotExists := c.accept("IF", "NOT", "EXISTS")
	name := c.name()
	if name == "" || temporary || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "temp.") {
		return true
	}
	if k, ok := s.key(name); ok {
		if ifNotExists && !replace {
			return true // there already
		}
		name = k // created again, under its other name
	}
	t := newDDLTable()
	if c.accept("PARTITION", "OF") {
		parent, ok := s.key(c.name())
		if !ok {
			slog.Debug("fixtures DDL: partition of an unknown table skipped", "table", name, "file", filepath.Base(file))
			return true
		}
		p := s.tables[parent]
		for _, n := range p.order {
			t.put(p.columns[n])
		}
		t.refs = slices.Clone(p.refs)
		maps.Copy(t.named, p.named)
		p.partitions = append(p.partitions, name)
	} else {
		definitions := c.group()
		if definitions == nil {
			slog.Debug("fixtures DDL: CREATE TABLE without columns skipped", "table", name, "file", filepath.Base(file))
			return true
		}
		options := c.rest()
		withoutRowid := slices.ContainsFunc(options, func(t sqlToken) bool { return t.is("ROWID") })
		strict := slices.ContainsFunc(options, func(t sqlToken) bool { return t.is("STRICT") })
		var key []string
		for _, def := range splitCommas(definitions) {
			if k, ok := s.constraint(t, def); ok {
				key = append(key, k...)
			} else if col, ok := s.column(t, def); ok {
				if col.key {
					key = append(key, col.name)
				}
			}
		}
		for _, n := range key {
			t.change(n, func(col *ddlColumn) {
				col.key = true
				switch {
				case !s.sqlite || withoutRowid:
					col.notNull = true
				case len(key) == 1 && col.typ == "integer":
					col.generated = true // the rowid
				case strict:
					col.notNull = true
				}
			})
		}
	}
	if _, ok := s.tables[name]; !ok {
		s.order = append(s.order, name)
	}
	s.tables[name] = t
	return true
}

// column adds the column a definition declares to a table, with the foreign key and named
// constraints it has, reporting false when there is none.
func (s *ddlSchema) column(t *ddlTable, def []sqlToken) (ddlColumn, bool) {
	if len(def) == 0 || !def[0].isName() {
		return ddlColumn{}, false
	}
	name := strings.ToLower(def[0].text)
	d := readColumn(def[1:])
	col := s.newColumn(name, d)
	t.place(col, d.first, d.after)
	d.record(t, name)
	return col, true
}

// newColumn is the column a definition declares, before its table's primary key.
func (s *ddlSchema) newColumn(name string, d columnDefinition) ddlColumn {
	col := ddlColumn{name: name, typ: d.typ, notNull: d.notNull, defaulted: d.defaulted, generated: d.generated, key: d.primaryKey}
	switch first, _, _ := strings.Cut(d.typ, " "); {
	case serialTypes[first]:
		col.generated, col.notNull = true, true
	case first == "rowversion":
		col.generated = true
	case s.mysql && strings.HasPrefix(d.typ, "enum"):
		col.defaulted = true // a NOT NULL enum defaults to its first value
	}
	if d.primaryKey && !s.sqlite {
		col.notNull = true
	}
	return col
}

var serialTypes = map[string]bool{"serial": true, "serial2": true, "serial4": true, "serial8": true, "smallserial": true, "bigserial": true}

// columnDefinition is what a column's definition says past its name.
type columnDefinition struct {
	typ                  string
	notNull, null        bool // NOT NULL, NULL
	defaulted, noDefault bool // a DEFAULT, DEFAULT NULL
	generated            bool
	dropIdentity         bool // Oracle's MODIFY (c DROP IDENTITY)
	primaryKey           bool
	ref                  ddlRef // its REFERENCES, when ref.table is set
	named                map[string]bool
	first                bool // MySQL's FIRST
	after                string
}

// columnStops are the words that end a column's type, and a DEFAULT's expression.
var columnStops = map[string]bool{
	"CONSTRAINT": true, "NOT": true, "NULL": true, "DEFAULT": true, "PRIMARY": true, "KEY": true, "UNIQUE": true,
	"CHECK": true, "REFERENCES": true, "FOREIGN": true, "GENERATED": true, "AS": true, "IDENTITY": true,
	"AUTO_INCREMENT": true, "AUTOINCREMENT": true, "COLLATE": true, "COMMENT": true, "ON": true, "FIRST": true,
	"AFTER": true, "ENABLE": true, "DISABLE": true, "VISIBLE": true, "INVISIBLE": true, "STORED": true,
	"VIRTUAL": true, "PERSISTED": true, "SPARSE": true, "ROWGUIDCOL": true, "MASKED": true, "HIDDEN": true, "DROP": true,
}

func isColumnStop(t sqlToken) bool { return t.kind == sqlWord && columnStops[strings.ToUpper(t.text)] }

// readColumn reads a column's definition past its name.
func readColumn(toks []sqlToken) columnDefinition {
	d := columnDefinition{named: map[string]bool{}}
	c := &sqlCursor{toks: toks}
	var typ []string
	for !c.done() && !isColumnStop(c.peek(0)) {
		typ = append(typ, strings.ToLower(c.next().text))
	}
	d.typ = strings.Join(typ, " ")
	constraint := "" // the name CONSTRAINT gives what follows it
	for !c.done() {
		switch {
		case c.accept("CONSTRAINT"):
			constraint = c.name()
			continue
		case c.accept("NOT", "NULL"):
			if !c.at("DISABLE") {
				d.notNull = true
				if constraint != "" {
					d.named[constraint] = true
				}
			}
		case c.accept("NULL"):
			d.null = true
		case c.accept("DEFAULT"):
			switch {
			case c.accept("ON", "NULL"):
				d.defaulted = true // Oracle's DEFAULT ON NULL
			case isNullDefault(c):
				d.noDefault = true
				c.skip()
			default:
				d.defaulted = true
				c.skip()
			}
			if constraint != "" {
				d.named[constraint] = false
			}
			for !c.done() && !isColumnStop(c.peek(0)) {
				c.skip()
			}
		case c.accept("FOREIGN", "KEY"): // SQL Server's FOREIGN KEY REFERENCES t
		case c.accept("PRIMARY", "KEY"), c.accept("KEY"):
			d.primaryKey = true
		case c.accept("UNIQUE"):
			_ = c.accept("KEY") || c.accept("INDEX")
		case c.accept("REFERENCES"):
			d.ref = ddlRef{name: constraint, table: c.name()}
		case c.accept("IDENTITY"):
			d.generated, d.notNull = true, true // an identity is NOT NULL
		case c.accept("GENERATED"):
			d.generated = true
			_ = c.accept("ALWAYS") || c.accept("BY", "DEFAULT", "ON", "NULL") || c.accept("BY", "DEFAULT")
		case c.accept("AUTO_INCREMENT"), c.accept("AUTOINCREMENT"):
			d.generated = true
		case c.at("AS") && c.peek(1).isSymbol("("):
			d.generated = true // computed
			c.next()
		case c.accept("DROP", "IDENTITY"):
			d.dropIdentity = true
		case c.accept("FIRST"):
			d.first = true
		case c.accept("AFTER"):
			d.after = c.name()
		default:
			c.skip()
		}
		constraint = ""
	}
	return d
}

// isNullDefault reports whether a DEFAULT's expression is NULL: no default at all.
func isNullDefault(c *sqlCursor) bool {
	if c.peek(0).isSymbol("(") && c.peek(1).is("NULL") && c.peek(2).isSymbol(")") {
		return true
	}
	return c.peek(0).is("NULL") && (c.peek(1).kind != sqlSymbol || c.peek(1).isSymbol(":") || c.peek(1).isSymbol(","))
}

// record keeps a column's foreign key and named constraints in its table.
func (d columnDefinition) record(t *ddlTable, column string) {
	if d.ref.table != "" {
		t.refs = append(t.refs, ddlRef{name: d.ref.name, columns: []string{column}, table: d.ref.table})
	}
	for name, notNull := range d.named {
		t.named[name] = ddlNamed{column: column, notNull: notNull}
	}
}

// constraint applies a table constraint to a table, returning the primary key's columns, and
// reports false for a column definition. It keeps PRIMARY KEY (...), FOREIGN KEY (...)
// REFERENCES t, SQL Server's DEFAULT x FOR c and PostgreSQL's NOT NULL c; UNIQUE, CHECK, an
// index and the like change no column.
func (s *ddlSchema) constraint(t *ddlTable, def []sqlToken) ([]string, bool) {
	c := &sqlCursor{toks: def}
	name := ""
	if c.accept("CONSTRAINT") {
		name = c.name()
	}
	switch {
	case c.accept("PRIMARY", "KEY"):
		for !c.done() && !c.peek(0).isSymbol("(") {
			c.next() // CLUSTERED, NONCLUSTERED
		}
		return columnNames(c.group()), true
	case c.accept("FOREIGN", "KEY"):
		columns := columnNames(c.group())
		if c.accept("REFERENCES") {
			t.refs = append(t.refs, ddlRef{name: name, columns: columns, table: c.name()})
		}
	case c.accept("DEFAULT"):
		noDefault := isNullDefault(c)
		for !c.done() && !c.at("FOR") {
			c.skip()
		}
		if c.accept("FOR") {
			column := c.name()
			t.change(column, func(col *ddlColumn) { col.defaulted = !noDefault })
			if name != "" {
				t.named[name] = ddlNamed{column: column}
			}
		}
	case c.accept("NOT", "NULL"):
		column := c.name()
		t.change(column, func(col *ddlColumn) { col.notNull = true })
		if name != "" {
			t.named[name] = ddlNamed{column: column, notNull: true}
		}
	case name != "", c.at("UNIQUE"), c.at("CHECK"), c.at("FOREIGN"), c.at("LIKE"), c.at("FULLTEXT"), c.at("SPATIAL"),
		c.at("EXCLUDE") && (c.peek(1).is("USING") || c.peek(1).isSymbol("(")),
		c.at("PERIOD") && c.peek(1).is("FOR"),
		(c.at("KEY") || c.at("INDEX")) && isIndex(def):
	default:
		return nil, false
	}
	return nil, true
}

// isIndex reports whether a definition that starts with KEY or INDEX is an index, as MySQL's
// KEY name (a) and SQL Server's INDEX name NONCLUSTERED (a) are, rather than a column named key.
func isIndex(def []sqlToken) bool {
	at := func(i int) sqlToken {
		if i < len(def) {
			return def[i]
		}
		return sqlToken{kind: -1}
	}
	return at(1).isSymbol("(") || at(2).isSymbol("(") && at(3).isName() ||
		at(2).is("USING") || at(2).is("CLUSTERED") || at(2).is("NONCLUSTERED")
}

// alterVerbs are the words an ALTER TABLE action starts with. SQL Server's ADD a int, b int
// and DROP COLUMN a, b go on without one.
var alterVerbs = map[string]bool{
	"ADD": true, "ALTER": true, "ALGORITHM": true, "ANALYZE": true, "ATTACH": true, "AUTO_INCREMENT": true,
	"CHANGE": true, "CHARACTER": true, "CHARSET": true, "CHECK": true, "CLUSTER": true, "COALESCE": true,
	"COLLATE": true, "COMMENT": true, "COMPRESS": true, "CONVERT": true, "DEFAULT": true, "DETACH": true,
	"DISABLE": true, "DISCARD": true, "DROP": true, "ENABLE": true, "ENGINE": true, "EXCHANGE": true,
	"FORCE": true, "IMPORT": true, "INHERIT": true, "LOCK": true, "LOGGING": true, "MODIFY": true, "MOVE": true,
	"NO": true, "NOCHECK": true, "NOCOMPRESS": true, "NOLOGGING": true, "NOPARALLEL": true, "NOT": true,
	"OF": true, "OPTIMIZE": true, "ORDER": true, "OWNER": true, "PARALLEL": true, "PARTITION": true,
	"REBUILD": true, "REMOVE": true, "RENAME": true, "REORGANIZE": true, "REPAIR": true, "REPLICA": true,
	"RESET": true, "ROW": true, "SET": true, "SHRINK": true, "SPLIT": true, "SWITCH": true, "TRUNCATE": true,
	"UPGRADE": true, "VALIDATE": true, "WITH": true, "WITHOUT": true,
}

func isAlterVerb(t sqlToken) bool { return t.kind == sqlWord && alterVerbs[strings.ToUpper(t.text)] }

// alterTable applies ALTER TABLE's actions to a table, and to its partitions unless ONLY.
func (s *ddlSchema) alterTable(c *sqlCursor, file string) {
	c.accept("IF", "EXISTS")
	only := c.accept("ONLY")
	name := c.name()
	c.acceptSymbol("*")
	k, ok := s.key(name)
	if !ok {
		slog.Debug("fixtures DDL: ALTER for unknown table skipped", "table", name, "file", filepath.Base(file))
		return
	}
	// verb is the last ADD or DROP and what it took (COLUMN, CONSTRAINT), which SQL Server's
	// actions after it go on with.
	var verb []sqlToken
	for _, action := range splitCommas(c.rest()) {
		a := &sqlCursor{toks: action}
		_ = a.accept("WITH", "CHECK") || a.accept("WITH", "NOCHECK")
		first := a.peek(0)
		switch {
		case first.is("ADD"), first.is("DROP"):
			verb = action[a.i : a.i+1]
			if a.peek(1).is("COLUMN") || a.peek(1).is("CONSTRAINT") {
				verb = action[a.i : a.i+2]
			}
		case verb != nil && first.isName() && !isAlterVerb(first) && !a.peek(1).isSymbol("="):
			if first.is("COLUMN") || first.is("CONSTRAINT") {
				verb = verb[:1]
			}
			a = &sqlCursor{toks: append(slices.Clone(verb), action[a.i:]...)}
		default:
			verb = nil
		}
		k = s.alterAction(k, a, only)
	}
}

// alterAction applies one action of ALTER TABLE, returning the table's name after it.
func (s *ddlSchema) alterAction(k string, a *sqlCursor, only bool) string {
	switch {
	case a.accept("RENAME", "TO"), a.accept("RENAME", "AS"):
		return s.renameTable(k, a.name())
	case a.accept("RENAME", "COLUMN"):
		from := a.name()
		a.accept("TO")
		to := a.name()
		s.each(k, only, func(t *ddlTable) { t.rename(from, to) })
	case a.accept("RENAME", "CONSTRAINT"):
		from := a.name()
		a.accept("TO")
		to := a.name()
		s.each(k, only, func(t *ddlTable) { renameConstraint(t, from, to) })
	case a.at("RENAME", "INDEX"), a.at("RENAME", "KEY"):
	case a.accept("RENAME"):
		from := a.name()
		if !a.accept("TO") {
			return s.renameTable(k, from) // MySQL's RENAME new_name
		}
		to := a.name()
		s.each(k, only, func(t *ddlTable) { t.rename(from, to) })
	case a.accept("SET", "SCHEMA"):
		_, table, _ := strings.Cut(k, ".")
		if table == "" {
			table = k
		}
		return s.renameTable(k, a.name()+"."+table)
	case a.accept("ATTACH", "PARTITION"):
		if child, ok := s.key(a.name()); ok {
			s.tables[k].partitions = append(s.tables[k].partitions, child)
		}
	case a.accept("DETACH", "PARTITION"):
		if child, ok := s.key(a.name()); ok {
			s.tables[k].partitions = slices.DeleteFunc(s.tables[k].partitions, func(p string) bool { return p == child })
		}
	default:
		start := a.i
		s.each(k, only, func(t *ddlTable) {
			a.i = start
			s.alterColumns(t, a)
		})
	}
	return k
}

// each applies f to a table and, unless only, to its partitions.
func (s *ddlSchema) each(k string, only bool, f func(*ddlTable)) {
	t := s.tables[k]
	f(t)
	if !only {
		for _, p := range t.partitions {
			if _, ok := s.tables[p]; ok {
				s.each(p, false, f)
			}
		}
	}
}

// alterColumns applies an ALTER TABLE action that changes a table's columns or constraints.
func (s *ddlSchema) alterColumns(t *ddlTable, a *sqlCursor) {
	switch {
	case a.accept("ADD"):
		_ = a.accept("COLUMN")
		ifNotExists := a.accept("IF", "NOT", "EXISTS")
		definitions := [][]sqlToken{a.rest()}
		if len(definitions[0]) > 0 && definitions[0][0].isSymbol("(") { // ADD (a ..., b ...)
			definitions = splitCommas((&sqlCursor{toks: definitions[0]}).group())
		}
		for _, def := range definitions {
			if key, ok := s.constraint(t, def); ok {
				s.primaryKey(t, key)
			} else if !ifNotExists || len(def) == 0 || !t.has(strings.ToLower(def[0].text)) {
				s.column(t, def)
			}
		}
	case a.accept("DROP"):
		s.drop(t, a)
	case a.accept("ALTER"):
		_ = a.accept("COLUMN")
		s.alterColumn(t, a.name(), a)
	case a.accept("MODIFY"):
		if group := a.group(); group != nil { // Oracle's MODIFY (a ..., b ...)
			for _, def := range splitCommas(group) {
				s.merge(t, def)
			}
			return
		}
		if a.at("CONSTRAINT") || a.at("PRIMARY") || a.at("UNIQUE") || a.at("PARTITION") || a.at("DEFAULT", "ATTRIBUTES") {
			return
		}
		_ = a.accept("COLUMN")
		if s.mysql {
			s.replace(t, "", a.rest())
		} else {
			s.merge(t, a.rest())
		}
	case a.accept("CHANGE"):
		_ = a.accept("COLUMN")
		old := a.name()
		s.replace(t, old, a.rest())
	case a.accept("SET", "UNUSED"): // Oracle's: the column is gone
		_ = a.accept("COLUMN")
		names := []string{a.name()}
		if a.peek(0).isSymbol("(") {
			names = columnNames(a.group())
		}
		for _, n := range names {
			t.remove(n)
		}
	}
}

// primaryKey makes a primary key's columns NOT NULL, as every database but SQLite does.
func (s *ddlSchema) primaryKey(t *ddlTable, columns []string) {
	for _, n := range columns {
		t.change(n, func(c *ddlColumn) {
			c.key = true
			c.notNull = c.notNull || !s.sqlite
		})
	}
}

// drop applies ALTER TABLE's DROP: a column, Oracle's (a, b), or a constraint, which takes
// away the NOT NULL, DEFAULT or foreign key it names. A primary key, index or check changes
// no column.
func (s *ddlSchema) drop(t *ddlTable, a *sqlCursor) {
	named := a.peek(1).isName() && !a.peek(1).is("CASCADE") && !a.peek(1).is("RESTRICT")
	switch {
	case a.accept("CONSTRAINT"):
		_ = a.accept("IF", "EXISTS")
		t.dropConstraint(a.name())
	case a.accept("FOREIGN", "KEY"):
		t.dropConstraint(a.name())
	case a.at("PRIMARY", "KEY"), a.at("UNUSED"), a.at("SYSTEM", "VERSIONING"),
		named && (a.at("INDEX") || a.at("KEY") || a.at("CHECK") || a.at("PARTITION") || a.at("PERIOD")):
	default:
		_ = a.accept("COLUMN")
		_ = a.accept("IF", "EXISTS")
		names := []string{a.name()}
		if a.peek(0).isSymbol("(") {
			names = columnNames(a.group())
		}
		for _, n := range names {
			t.remove(n)
		}
	}
}

// alterColumn applies ALTER [COLUMN] c: PostgreSQL's and MySQL's SET and DROP forms, or SQL
// Server's new type and nullability.
func (s *ddlSchema) alterColumn(t *ddlTable, name string, a *sqlCursor) {
	t.change(name, func(col *ddlColumn) {
		switch {
		case a.accept("SET", "NOT", "NULL"):
			col.notNull = true
		case a.accept("DROP", "NOT", "NULL"):
			col.notNull = false
		case a.accept("SET", "DEFAULT"):
			col.defaulted = !isNullDefault(a)
		case a.accept("DROP", "DEFAULT"):
			col.defaulted = false
		case a.accept("ADD", "GENERATED"):
			col.generated = true
		case a.accept("DROP", "IDENTITY"), a.accept("DROP", "EXPRESSION"):
			col.generated = false
		case a.accept("TYPE"), a.accept("SET", "DATA", "TYPE"):
			col.typ = readColumn(a.rest()).typ
		case a.at("SET"), a.at("RESET"), a.at("DROP"), a.at("ADD"), a.at("RESTART"), a.at("OPTIONS"):
		default: // SQL Server: ALTER COLUMN c type [NULL | NOT NULL], nullable unless NOT NULL
			d := readColumn(a.rest())
			col.typ, col.notNull = d.typ, d.notNull || col.key
		}
	})
}

// merge applies Oracle's MODIFY c ...: only what it says changes.
func (s *ddlSchema) merge(t *ddlTable, def []sqlToken) {
	if len(def) == 0 || !def[0].isName() {
		return
	}
	name := strings.ToLower(def[0].text)
	d := readColumn(def[1:])
	t.change(name, func(col *ddlColumn) {
		if d.typ != "" {
			col.typ = d.typ
		}
		switch {
		case d.notNull:
			col.notNull = true
		case d.null:
			col.notNull = false
		}
		switch {
		case d.defaulted:
			col.defaulted = true
		case d.noDefault:
			col.defaulted = false
		}
		if d.dropIdentity { // the identity's NOT NULL goes with it
			col.generated, col.notNull = false, false
		}
		col.generated = col.generated || d.generated
	})
	d.record(t, name)
}

// replace applies MySQL's MODIFY c ... and CHANGE old c ...: the column's whole definition
// again, where it was unless FIRST or AFTER moves it.
func (s *ddlSchema) replace(t *ddlTable, old string, def []sqlToken) {
	if len(def) == 0 || !def[0].isName() {
		return
	}
	name := strings.ToLower(def[0].text)
	if old == "" {
		old = name
	}
	prev, ok := t.columns[old]
	if !ok {
		return
	}
	d := readColumn(def[1:])
	col := s.newColumn(name, d)
	col.key = col.key || prev.key
	col.notNull = col.notNull || col.key
	t.rename(old, name)
	t.place(col, d.first, d.after)
	d.record(t, name)
}

func renameConstraint(t *ddlTable, from, to string) {
	if n, ok := t.named[from]; ok {
		delete(t.named, from)
		t.named[to] = n
	}
	for i := range t.refs {
		if t.refs[i].name == from {
			t.refs[i].name = to
		}
	}
}

// dropTables applies DROP TABLE a, b. With CASCADE the foreign keys that reference them go
// too; PostgreSQL's partitions always do.
func (s *ddlSchema) dropTables(c *sqlCursor) {
	c.accept("IF", "EXISTS")
	var names []string
	for n := c.name(); n != ""; n = c.name() {
		names = append(names, n)
		if !c.acceptSymbol(",") {
			break
		}
	}
	cascade := c.at("CASCADE")
	for _, n := range names {
		if k, ok := s.key(n); ok {
			s.dropTable(k, cascade)
		}
	}
}

func (s *ddlSchema) dropTable(k string, cascade bool) {
	t := s.tables[k]
	for _, u := range s.tables {
		u.partitions = slices.DeleteFunc(u.partitions, func(p string) bool { return p == k })
		if cascade {
			u.refs = slices.DeleteFunc(u.refs, func(r ddlRef) bool { rk, _ := s.key(r.table); return rk == k })
		}
	}
	delete(s.tables, k)
	s.order = slices.DeleteFunc(s.order, func(n string) bool { return n == k })
	for _, p := range t.partitions {
		if _, ok := s.tables[p]; ok {
			s.dropTable(p, cascade)
		}
	}
}

// renameTable gives a table a new name, returning it: the table keeps its columns and its
// place, and the foreign keys and partitions that name it follow it. A name without a schema
// keeps the table's.
func (s *ddlSchema) renameTable(k, to string) string {
	if schema, _, ok := strings.Cut(k, "."); ok && !strings.Contains(to, ".") {
		to = schema + "." + to
	}
	if to == "" || to == k {
		return k
	}
	for _, t := range s.tables {
		for i, r := range t.refs {
			if rk, _ := s.key(r.table); rk == k {
				t.refs[i].table = to
			}
		}
		for i, p := range t.partitions {
			if p == k {
				t.partitions[i] = to
			}
		}
	}
	if _, ok := s.tables[to]; ok {
		s.order = slices.DeleteFunc(s.order, func(n string) bool { return n == to })
	}
	s.tables[to] = s.tables[k]
	delete(s.tables, k)
	s.order[slices.Index(s.order, k)] = to
	return to
}

// spRename applies SQL Server's sp_rename: a table ('parcels', 'shipments'), a column
// ('parcels.sender_name', 'sender', 'COLUMN') or a constraint.
func (s *ddlSchema) spRename(c *sqlCursor) {
	var args []string
	for _, arg := range splitCommas(c.rest()) {
		if len(arg) > 0 && arg[len(arg)-1].kind == sqlString {
			args = append(args, arg[len(arg)-1].text)
		}
	}
	if len(args) < 2 {
		return
	}
	object := (&sqlCursor{toks: firstStatement(args[0])}).name()
	to := strings.ToLower(strings.Trim(args[1], "[]\""))
	kind := ""
	if len(args) > 2 {
		kind = strings.ToUpper(args[2])
	}
	switch kind {
	case "COLUMN":
		if dot := strings.LastIndexByte(object, '.'); dot > 0 {
			if k, ok := s.key(object[:dot]); ok {
				s.each(k, true, func(t *ddlTable) { t.rename(object[dot+1:], to) })
			}
		}
	case "", "OBJECT":
		if k, ok := s.key(object); ok {
			s.renameTable(k, to)
			return
		}
		_, constraint, ok := strings.Cut(object, ".")
		if !ok {
			constraint = object
		}
		for _, t := range s.tables {
			renameConstraint(t, constraint, to)
		}
	}
}

// firstStatement is the tokens of the first statement in SQL text.
func firstStatement(text string) []sqlToken {
	if statements := splitSQL(text, false); len(statements) > 0 {
		return statements[0]
	}
	return nil
}
