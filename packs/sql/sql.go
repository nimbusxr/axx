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

const tableDoc = "Values in the table become `column = 'value'` conditions joined with AND; `null` becomes `IS NULL`. Values are escaped."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "sql",
		Namespace: "sql",
		Doc: "Seed, query and assert on relational databases. PostgreSQL is fully supported (including JSONB and " +
			"trigger fault injection); MySQL/MariaDB, SQL Server and SQLite support seeds, selections, row counts and locks. " +
			"JDBC URLs (jdbc:postgresql://...) are accepted as-is.",
		Params: []core.ParamType{
			{
				Name: "dbService", Regexps: []string{`([^\s]+)`},
				Doc: "The name of a database registered in the scenario.",
				Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
					return stateKey.Of(sc).services.Get(name)
				},
			},
			{Name: "sqlState", Regexps: []string{`[0-9A-Za-z]{5}`}, Doc: "A five-character SQLSTATE code, e.g. `23505` (unique violation)."},
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
			Doc: "Register a database. The first one registered in a scenario is the default.\n\n" +
				"Properties: `url` (JDBC or native URL), `user`, `password` (all required; `${env:..}`/`${sys:..}` expanded), `schema` (optional).",
			Examples: []string{"Given a parcels-db database with the following properties:"},
			Run:      addService,
		},
		core.StepDef{
			ID: "sql.seed", Keyword: "Given",
			Expr: "a {filepath} db seed[[ on {dbService}]]",
			Doc: "Insert the rows of a dataset file (resolved against `resources`). Formats by extension: `.yaml`/`.yml` " +
				"(`schema.table:` → list of rows), flat XML (`<dataset><schema.table col=\"v\"/></dataset>`), `.json`, " +
				"`.csv` (a directory of `<table>.csv` files with `table-ordering.txt`) and `.xlsx` (one sheet per table). " +
				"Replacers: `[null]`, `[DAY,NOW]`, `[DAY,PLUS,1]`, `[UNIX_TIMESTAMP]`. Rows are inserted in one transaction and never deleted.",
			Examples: []string{"Given a seeds/manifest-kestrel.yaml db seed", "Given a seeds/dispatching.yaml db seed on parcels-db"},
			Run:      seedStep,
		},
		core.StepDef{
			ID: "sql.lock", Keyword: "Given", Arg: core.ArgTable,
			Expr:     "the rows in the {word} table[[ on {dbService}]] are locked where:",
			Doc:      "Lock matching rows with SELECT ... FOR UPDATE on a separate connection, held until the locks are released or the scenario ends. " + tableDoc,
			Examples: []string{"Given the rows in the parcels.parcels table are locked where:"},
			Run:      lockStep,
		},
		core.StepDef{
			ID: "sql.unlock", Keyword: "Then",
			Expr:     "the row locks[[ on {dbService}]] are released",
			Doc:      "Release row locks taken with the lock step.",
			Examples: []string{"Then the row locks are released"},
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
			Expr: "a[[ {ordinal}]] selection of rows is retrieved from the {word} table[[ on {dbService}]] where:",
			Doc: "Query rows (SELECT * ... WHERE) and keep the result as the next selection for later assertions. " +
				"Selections are numbered in the order they are retrieved; `the selection` means the first. " + tableDoc,
			Examples: []string{"Then a selection of rows is retrieved from the parcels.parcels table where:"},
			Run:      selectStep,
		},
		core.StepDef{
			ID: "sql.select.poll", Keyword: "Then", Arg: core.ArgTable,
			Expr: "within {duration} a[[ {ordinal}]] selection of at least {int} row(s) is retrieved from the {word} table[[ on {dbService}]] where:",
			Doc: "Poll every 500ms until the query returns at least the given number of rows or the time is up. " +
				"On timeout the last result (possibly empty) is kept, so assert on it with a row-count step.",
			Examples: []string{"Then within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:"},
			Run:      pollStep,
		},
		core.StepDef{
			ID: "sql.select.jsonb", Keyword: "Then", Arg: core.ArgTable,
			Expr: "a[[ {ordinal}]] selection of rows is retrieved from the {word} table[[ on {dbService}]] where the {word} jsonb column contains:",
			Doc: "PostgreSQL: select rows whose JSONB column contains the given properties (`@>`). Dotted keys (`a.b`) build nested objects; " +
				"every value is compared as a JSON string (`null` means JSON null).",
			Examples: []string{"Then a selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:"},
			Run:      jsonbStep,
		},
		core.StepDef{
			ID: "sql.json.are", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the {ordinal} row {word} property for the[[ {ordinal}]] selection[[ on {dbService}]] json properties are:",
			Doc: "Assert JSON properties (JSONPath) of a JSON column in the given row of a selection. Every scalar is compared as text; " +
				"`null` means JSON null and `undefined` means the property is absent.",
			Examples: []string{"Then the 1st row details property for the 2nd selection json properties are:"},
			Run:      func(sc *core.Scenario, a core.Args) error { return jsonProperties(sc, a, false) },
		},
		core.StepDef{
			ID: "sql.json.match", Keyword: "Then", Arg: core.ArgTable,
			Expr:     "the {ordinal} row {word} property for the[[ {ordinal}]] selection[[ on {dbService}]] json properties match:",
			Doc:      "Like the `are` form, but each value is a regular expression that must match the whole property value (as text).",
			Examples: []string{"Then the 1st row recipient property for the 3rd selection json properties match:"},
			Run:      func(sc *core.Scenario, a core.Args) error { return jsonProperties(sc, a, true) },
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
		Doc:      fmt.Sprintf("Assert that a selection has %s the given number of rows. `the selection` means the first selection of the scenario.", words),
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
