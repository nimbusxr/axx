// Package sql provides database steps: services, seeds, selections (with
// polling and JSONB containment), JSON column assertions, row locks and
// before-insert trigger fault injection (PostgreSQL).
package sql

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Pack returns the SQL pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

// whereTable is the table of a step that picks rows by their values: rows
// says who holds them, like "the selected rows hold".
func whereTable(rows string) *core.TableDoc {
	return &core.TableDoc{
		Columns: []string{"column", "value"},
		Note:    "Each row names a column and the value " + rows + " in it, or `null` for none (`IS NULL`); values are escaped.",
	}
}

// numbered is how selections are numbered, for the steps that retrieve one.
const numbered = "Selections are numbered in the order they are retrieved, whatever ordinal the step says; `the selection` means the first."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "sql",
		Namespace: "sql",
		Doc: "Seed, query and assert on relational databases. PostgreSQL is fully supported (including JSONB and " +
			"trigger fault injection); MySQL/MariaDB and SQL Server support seeds, selections, row counts and locks, " +
			"and SQLite all of them but locks. JDBC URLs (jdbc:postgresql://...) are accepted as-is.",
		Params: []core.ParamType{
			{
				Name: "dbService", Regexps: []string{`([^\s]+)`},
				Doc:      "the name of a database registered in the scenario",
				Examples: []string{"parcels-db"},
				Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
					return stateKey.Of(sc).services.Get(name)
				},
			},
			{
				Name: "sqlState", Regexps: []string{`[0-9A-Za-z]{5}`},
				Doc:      "a five-character SQLSTATE error code, like `40001` (serialization failure) or `23505` (unique violation)",
				Examples: []string{"40001", "23505"},
			},
		},
		Steps: steps(),
	}
}

func steps() []core.StepDef {
	var out []core.StepDef
	out = append(out,
		core.StepDef{
			ID: "sql.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "a(n) {word} database with the following properties:",
			Doc: "Register a database.\n\n" +
				"- The first database registered in a scenario is the default.\n" +
				"- The scheme of the `url` says which database it is: PostgreSQL (`postgres://`, `postgresql://`, `jdbc:postgresql:`), " +
				"MySQL or MariaDB (`mysql://`, `jdbc:mysql:`, `jdbc:mariadb:`), SQL Server (`sqlserver://`, `jdbc:sqlserver:`) " +
				"or SQLite (`sqlite:`, `file:`, `jdbc:sqlite:`).\n" +
				"- A PostgreSQL URL connects without TLS unless it sets `sslmode`; its `currentSchema` sets the default schema.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "where the database is: a native or JDBC URL, like `postgres://localhost:5432/parcels`", Required: true},
					{Name: "user", Takes: "the user axx connects as; SQLite ignores it, but the table needs it all the same", Required: true},
					{Name: "password", Takes: "the user's password; SQLite ignores it, but the table needs it all the same", Required: true},
					{Name: "schema", Takes: "a schema, kept for code that builds on the pack; the steps don't use it, so name tables `schema.table` " +
						"(or set a PostgreSQL URL's `currentSchema`)"},
				},
				Note: "`url`, `user` and `password` can take `${env:…}` and `${sys:…}` references.",
			},
			Examples: []string{"Given a parcels-db database with the following properties:\n" +
				"  | url      | postgres://localhost:5432/parcels |\n" +
				"  | user     | parcels                           |\n" +
				"  | password | parcels                           |"},
			Run: addService,
		},
		core.StepDef{
			ID: "sql.seed", Keyword: "Given",
			Expr: "a {filepath} db seed[[ on {dbService}]]",
			Doc: "Insert the rows of a dataset file, in the file's order and in one transaction.\n\n" +
				"- A YAML file (`.yaml`, `.yml`) maps each `schema.table` to a list of rows, each a mapping of column to value.\n" +
				"- A flat XML file (`.xml`) has a `<schema.table column=\"value\"/>` element per row inside `<dataset>`.\n" +
				"- A JSON file (`.json`) maps each `schema.table` to an array of rows.\n" +
				"- A CSV file (`.csv`) stands for its directory, which has a `<schema.table>.csv` file per table, " +
				"in the order its `table-ordering.txt` lists or else by name; a first line names the columns, and `null` is NULL. " +
				"The step takes the directory itself too.\n" +
				"- An Excel file (`.xlsx`) has a sheet per table, named after it, with a first row that names the columns; an empty cell is NULL.\n" +
				"- A value can be `[null]` for NULL, `[UNIX_TIMESTAMP]` for the seconds since 1970, or a time counted from now, like " +
				"`[DAY,NOW]`, `[DAY,PLUS,1]` or `[HOUR,MINUS,2]` (the unit is `DAY`, `HOUR`, `MIN` or `SEC`).\n" +
				"- In YAML and JSON, a value that is a list or an object is stored as JSON text.\n" +
				"- A row the database refuses, like one whose key exists already, fails the step, and nothing of the file is written.\n" +
				"- Seeded rows are never deleted.",
			Examples: []string{
				"Given a seeds/manifest-kestrel.yaml db seed",
				"Given a seeds/dispatching.yaml db seed on parcels-db",
				"Given a seeds/manifests/csv/bulk-manifest/parcels.manifest_lines.csv db seed",
			},
			Run: seedStep,
		},
		core.StepDef{
			ID: "sql.lock", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the rows in the {word} table[[ on {dbService}]] are locked where:",
			Doc: "Lock the rows that match, with `SELECT ... FOR UPDATE` on a separate connection.\n\n" +
				"- The locks hold until the row locks are released or the scenario ends.\n" +
				"- Rows locked elsewhere, like by another scenario, are waited for 10 seconds at most; then the step fails.\n" +
				"- SQLite has no row locks: the step fails on it.",
			Table: whereTable("the locked rows hold"),
			Examples: []string{"Given the rows in the parcels.parcels table are locked where:\n" +
				"  | reference | PX-DSP-2001 |"},
			Run: lockStep,
		},
		core.StepDef{
			ID: "sql.unlock", Keyword: "Then",
			Expr:     "the row locks[[ on {dbService}]] are released",
			Doc:      "Release all the row locks the lock step took on the database.",
			Examples: []string{"Then the row locks are released", "Then the row locks on parcels-db are released"},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := service(sc, a, 0)
				if err != nil {
					return err
				}
				return svc.releaseLocks()
			},
		},
		core.StepDef{
			ID: "sql.select", Keyword: "Then", Arg: core.ArgTable,
			Expr:  "a[[ {ordinal}]] selection of rows is retrieved from the {word} table[[ on {dbService}]] where:",
			Doc:   "Select the rows that match (`SELECT * ... WHERE`) and keep them as the next selection, for the steps that check it. " + numbered,
			Table: whereTable("the selected rows hold"),
			Examples: []string{
				"Then a selection of rows is retrieved from the parcels.pickups table where:\n" +
					"  | reference | PX-WEB-5302 |\n" +
					"  | day       | Friday      |",
				"Then a 2nd selection of rows is retrieved from the parcels.manifest_lines table on parcels-db where:\n" +
					"  | manifest_id | M-KESTREL-0412 |\n" +
					"  | error       | null           |",
			},
			Run: selectStep,
		},
		core.StepDef{
			ID: "sql.select.poll", Keyword: "Then", Arg: core.ArgTable,
			Expr: "within {duration} a[[ {ordinal}]] selection of at least {int} row(s) is retrieved from the {word} table[[ on {dbService}]] where:",
			Doc: "Select the rows that match, again every 500ms, until at least that many come back or the time is up.\n\n" +
				"- The last result is kept as the next selection, even with fewer rows: check it with a row-count step.\n" +
				"- The step fails only when no query succeeds in that time.\n" +
				"- " + numbered,
			Table: whereTable("the selected rows hold"),
			Examples: []string{"Then within 10s a selection of at least 2 rows is retrieved from the parcels.manifest_lines table where:\n" +
				"  | manifest_id | M-KESTREL-0412 |\n" +
				"  | status      | IMPORTED       |"},
			Run: pollStep,
		},
		core.StepDef{
			ID: "sql.select.jsonb", Keyword: "Then", Arg: core.ArgTable,
			Expr: "a[[ {ordinal}]] selection of rows is retrieved from the {word} table[[ on {dbService}]] where the {word} jsonb column contains:",
			Doc: "Select the rows whose JSONB column contains the properties (`@>`), and keep them as the next selection.\n\n" +
				"- The database must be PostgreSQL.\n" +
				"- " + numbered,
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Note: "Each row names a property, dotted (`recipient.city`) for one inside an object, and the value it holds, " +
					"compared as a JSON string (`null` is JSON null), so it never matches a number or a boolean.",
			},
			Examples: []string{"Then a selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:\n" +
				"  | manifestId | M-KESTREL-0412 |\n" +
				"  | source     | manifest       |"},
			Run: jsonbStep,
		},
		core.StepDef{
			ID: "sql.json.are", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the {ordinal} row {word} property for the[[ {ordinal}]] selection[[ on {dbService}]] json properties are:",
			Doc: "Check the JSON a column holds in one row of a selection. " +
				"`the 1st row details property` is the `details` column of the selection's first row.",
			Table: &core.TableDoc{
				Columns: []string{"JSONPath", "value"},
				Note: "Each row names a JSONPath, like `source` or `$.recipient.city`, and the value there as text, like `true` or `850`: " +
					"`null` for JSON null, `undefined` for a property the JSON lacks.",
			},
			Examples: []string{"Then the 1st row details property for the selection json properties are:\n" +
				"  | source         | portal    |\n" +
				"  | signature      | true      |\n" +
				"  | customsInvoice | undefined |"},
			Run: func(sc *core.Scenario, a core.Args) error { return jsonProperties(sc, a, false) },
		},
		core.StepDef{
			ID: "sql.json.match", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the {ordinal} row {word} property for the[[ {ordinal}]] selection[[ on {dbService}]] json properties match:",
			Doc: "Check the JSON a column holds in one row of a selection, like the `json properties are` step, " +
				"with regular expressions (Java syntax) that must match the whole value, as text.",
			Table: &core.TableDoc{
				Columns: []string{"JSONPath", "pattern"},
				Note:    "Each row names a JSONPath, like `zone` or `$.recipient.postcode`, and a regular expression its value must match.",
			},
			Examples: []string{"Then the 1st row recipient property for the 2nd selection json properties match:\n" +
				"  | postcode | \\d{5}    |\n" +
				"  | country  | [A-Z]{2} |"},
			Run: func(sc *core.Scenario, a core.Args) error { return jsonProperties(sc, a, true) },
		},
		rowCount("sql.rows.eq", "has {int} row(s)", "exactly", func(got, want int) bool { return got == want }),
		rowCount("sql.rows.gt", "has more than {int} row(s)", "more than", func(got, want int) bool { return got > want }),
		rowCount("sql.rows.lt", "has fewer than {int} row(s)", "fewer than", func(got, want int) bool { return got < want }),
	)
	out = append(out, triggerSteps()...)
	return out
}

func rowCount(id, tail, words string, ok func(got, want int) bool) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then",
		Expr:     "the[[ {ordinal}]] selection[[ on {dbService}]] " + tail,
		Doc:      fmt.Sprintf("Check that a selection has %s the given number of rows. `the selection` means the first selection of the scenario.", words),
		Examples: []string{"Then the selection " + strings.ReplaceAll(tail, "{int} row(s)", "2 rows"), "Then the 2nd selection on parcels-db " + strings.ReplaceAll(tail, "{int} row(s)", "1 row")},
		Run: func(sc *core.Scenario, a core.Args) error {
			svc, err := service(sc, a, 1)
			if err != nil {
				return err
			}
			sel, err := svc.selection(a.IntOr(0, 1) - 1)
			if err != nil {
				return err
			}
			want := a.Int(2)
			if !ok(len(sel.Rows), want) {
				return core.Fail(fmt.Sprintf("expected the selection from %s to have %s %d row(s)", sel.Table, words, want), want, len(sel.Rows))
			}
			return nil
		},
	}
}

func addService(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	props := map[string]string{}
	for _, p := range pairs {
		if !p.Null {
			props[p.Key] = p.Value
		}
	}
	get := func(k string) (string, error) {
		v, ok := props[k]
		if !ok {
			return "", fmt.Errorf("Property %q is required", k) //nolint:staticcheck // user-facing message
		}
		return sc.Suite().Interpolate(v), nil
	}
	url, err := get("url")
	if err != nil {
		return err
	}
	user, err := get("user")
	if err != nil {
		return err
	}
	password, err := get("password")
	if err != nil {
		return err
	}
	svc, err := Connect(sc, a.String(0), url, user, password, props["schema"])
	if err != nil {
		return fmt.Errorf("Could not connect to database: %w", err) //nolint:staticcheck // user-facing message
	}
	return Context(sc).AddService(svc)
}

func seedStep(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 1)
	if err != nil {
		return err
	}
	path, err := sc.Suite().ResolvePath(a.String(0))
	if err != nil {
		return fmt.Errorf("Could not perform seed: %w", err) //nolint:staticcheck // user-facing message
	}
	ds, err := LoadDataset(path)
	if err != nil {
		return fmt.Errorf("Could not perform seed: %w", err) //nolint:staticcheck // user-facing message
	}
	if err := svc.insert(sc.Context(), ds, time.Now()); err != nil {
		return fmt.Errorf("Could not perform seed: %w", err) //nolint:staticcheck // user-facing message
	}
	return nil
}

func selectStep(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 2)
	if err != nil {
		return err
	}
	table, where, err := tableAndWhere(svc, a.String(1), a.Table, "")
	if err != nil {
		return err
	}
	sel, err := svc.query(sc.Context(), table, "SELECT * FROM "+table+" WHERE "+where)
	if err != nil {
		return fmt.Errorf("Could not perform selection: %w", err) //nolint:staticcheck // user-facing message
	}
	svc.addSelection(sel)
	return nil
}

func pollStep(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 4)
	if err != nil {
		return err
	}
	d := a.Value(0).(time.Duration)
	minRows := a.Int(2)
	table, where, err := tableAndWhere(svc, a.String(3), a.Table, "")
	if err != nil {
		return err
	}
	q := "SELECT * FROM " + table + " WHERE " + where
	deadline := time.Now().Add(d)
	var last *Selection
	var lastErr error
	for {
		sel, err := svc.query(sc.Context(), table, q)
		if err == nil {
			last = sel
			if len(sel.Rows) >= minRows {
				break
			}
		} else {
			lastErr = err
		}
		if time.Now().Add(500 * time.Millisecond).After(deadline) {
			break
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if last == nil {
		return fmt.Errorf("Could not perform polling selection; no result was obtained within %s: %w", d, lastErr) //nolint:staticcheck // user-facing message
	}
	svc.addSelection(last)
	return nil
}

func jsonbStep(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 2)
	if err != nil {
		return err
	}
	if !svc.Target.Dialect.Postgres {
		return fmt.Errorf("JSONB containment requires PostgreSQL (db service %s is %s)", svc.Name, svc.Target.Dialect.Name)
	}
	table, err := Ident(a.String(1))
	if err != nil {
		return err
	}
	column, err := Ident(a.String(3))
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	doc, err := containmentJSON(pairs)
	if err != nil {
		return err
	}
	q := "SELECT * FROM " + table + " WHERE " + column + " @> " + svc.Target.Dialect.Literal(doc) + "::jsonb"
	sel, err := svc.query(sc.Context(), table, q)
	if err != nil {
		return fmt.Errorf("Could not perform JSONB containment selection: %w", err) //nolint:staticcheck // user-facing message
	}
	svc.addSelection(sel)
	return nil
}

// containmentJSON builds {"a":{"b":"v"}} from dotted keys; leaves are strings
// (or null).
func containmentJSON(pairs []core.Pair) (string, error) {
	root := orderedObject{}
	for _, p := range pairs {
		keys := strings.Split(p.Key, ".")
		cur := &root
		for _, k := range keys[:len(keys)-1] {
			next, ok := cur.get(k).(*orderedObject)
			if !ok {
				next = &orderedObject{}
				cur.set(k, next)
			}
			cur = next
		}
		leaf := keys[len(keys)-1]
		if strings.EqualFold(p.Value, "null") {
			cur.set(leaf, nil)
		} else {
			cur.set(leaf, p.Value)
		}
	}
	b, err := json.Marshal(&root)
	return string(b), err
}

type orderedObject struct {
	keys []string
	vals map[string]any
}

func (o *orderedObject) get(k string) any { return o.vals[k] }

func (o *orderedObject) set(k string, v any) {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *orderedObject) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// tableAndWhere validates the table name and builds the WHERE clause.
func tableAndWhere(svc *Service, table string, t *core.Table, prefix string) (string, string, error) {
	tbl, err := Ident(table)
	if err != nil {
		return "", "", err
	}
	where, err := whereClause(svc.Target.Dialect, t, prefix)
	return tbl, where, err
}

// whereClause turns key/value rows into `col = 'value'` joined with AND;
// `null` (any case) becomes `col IS NULL`.
func whereClause(d Dialect, t *core.Table, prefix string) (string, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return "", err
	}
	if len(pairs) == 0 {
		return "", fmt.Errorf("No conditions provided") //nolint:staticcheck // user-facing message
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		col, err := Ident(p.Key)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(p.Value, "null") {
			parts = append(parts, prefix+col+" IS NULL")
		} else {
			parts = append(parts, prefix+col+" = "+d.Literal(p.Value))
		}
	}
	return strings.Join(parts, " AND "), nil
}
