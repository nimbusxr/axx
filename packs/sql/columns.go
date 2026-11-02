package sql

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/columntype"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// A step that picks rows by their values holds each value to its column's
// type before it asks (ADR 0014): a database that compares a column with a
// value of another type converts one of them, or refuses, each its own way.
// A column's type comes from the database each time, not from a cache, since a
// scenario's service may change its schema as the scenario runs.

// columnTypes is the type of each of the table's columns, by its name in lower
// case, as the driver names it ("" for a column with no declared type: an
// SQLite one). A table the database cannot read has none: the query that
// follows says why, as it always has.
func (svc *Service) columnTypes(ctx context.Context, tbl string) map[string]string {
	if svc.Target.Dialect.Oracle {
		return svc.oracleColumnTypes(ctx, tbl)
	}
	rows, err := svc.DB.QueryContext(ctx, "SELECT * FROM "+tbl+" WHERE 1=0")
	if err != nil {
		return nil
	}
	defer rows.Close()
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil
	}
	types := make(map[string]string, len(cols))
	for _, c := range cols {
		types[strings.ToLower(c.Name())] = c.DatabaseTypeName()
	}
	if err := rows.Close(); err != nil {
		return nil
	}
	if svc.Target.Dialect.Postgres {
		for name, t := range types {
			types[name] = svc.postgresTypeName(ctx, t)
		}
	}
	return types
}

var oidName = regexp.MustCompile(`^[0-9]+$`)

// postgresTypeName is the name of a type pgx knows only by its number: an
// extension's (citext), an enum, money.
func (svc *Service) postgresTypeName(ctx context.Context, t string) string {
	if !oidName.MatchString(t) {
		return t
	}
	var name string
	if svc.DB.QueryRowContext(ctx, "SELECT upper(typname) FROM pg_type WHERE oid = "+t).Scan(&name) != nil {
		return t
	}
	return name
}

// untypedCondition is a condition on a column with no declared type, which is
// held to what each row stores (keepStored).
type untypedCondition struct {
	column string
	value  core.Value
}

// holdToColumns fails with what to write when a condition's value is not of its
// column's type, for all of them at once, and returns the conditions on columns
// with no declared type. A column the table does not have is the database's to
// report.
func (svc *Service) holdToColumns(ctx context.Context, tbl string, t *core.Table) ([]untypedCondition, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	types := svc.columnTypes(ctx, tbl)
	var untyped []untypedCondition
	var wrong []string
	for _, p := range pairs {
		v, err := core.ParseValue(p.Value, p.Null, false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Key, err)
		}
		if str, ok := v.Str(); ok && str == "" && svc.Target.Dialect.Oracle {
			wrong = append(wrong, fmt.Sprintf(`%s: Oracle stores an empty string as null, so "" selects nothing: write null`, p.Key))
			continue
		}
		name := p.Key[strings.LastIndexByte(p.Key, '.')+1:]
		typeName, ok := types[strings.ToLower(name)]
		if !ok {
			continue
		}
		kind, known := svc.Target.Dialect.columnKind(typeName)
		if !known {
			if columntype.Checked(v) {
				untyped = append(untyped, untypedCondition{column: name, value: v})
			}
			continue
		}
		c := columntype.Column{Name: p.Key, Table: tbl, Type: typeName, Kind: kind}
		if m := c.Wrong(v); m != "" {
			wrong = append(wrong, m)
		}
	}
	return untyped, columntype.Fail(wrong)
}

// keepStored drops the rows the database selected whose value in a column with
// no declared type is not the condition's value, type and all: SQLite compares
// the integer 850 and the real 850.0 as equal, and a boolean is the integer 1
// to it. It is the same comparison as a check's, of the value the row stores. A
// row that differs only so leaves a hint for the checks of the selection that
// come after it.
func keepStored(sel *Selection, conditions []untypedCondition) {
	if len(conditions) == 0 {
		return
	}
	kept := sel.Rows[:0:0]
	for _, row := range sel.Rows {
		keep := true
		for _, c := range conditions {
			i := slices.IndexFunc(sel.Columns, func(name string) bool { return strings.EqualFold(name, c.column) })
			if i < 0 {
				continue
			}
			if same, hint := jsonassert.Strict(c.column, storedJSON(row[i]), c.value); !same {
				keep = false
				sel.hint(hint)
			}
		}
		if keep {
			kept = append(kept, row)
		}
	}
	sel.Rows = kept
}

// charTypes pad what they hold with blanks up to their size.
var charTypes = []string{"CHAR", "BPCHAR", "NCHAR", "CHARACTER", "NATIONAL CHARACTER", "NATIONAL CHAR"}

// The forms a date or a time takes in a check, with an offset or without.
var (
	wallTimes  = []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	zonedTimes = []string{"2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05 -07:00", "2006-01-02T15:04:05Z07:00"}
)

const wallClock = "2006-01-02 15:04:05.999999999"

// sameMoment says a string is the moment a date or time column holds: the same instant
// when it has an offset, and the same wall clock when it has none.
func sameMoment(s string, at time.Time) bool {
	for _, layout := range zonedTimes {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Equal(at)
		}
	}
	for _, layout := range wallTimes {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(wallClock) == at.Format(wallClock)
		}
	}
	return false
}

// cellIs says a value a selection holds is want, and gives what it holds as a failure
// shows it. A driver's Go type does not say the column's (numeric comes back as text),
// so the column's type does: a decimal is a decimal, whole or not (ADR 0014).
func (svc *Service) cellIs(ctx context.Context, table, column string, got any, want core.Value) (same bool, hint, actual string) {
	if at, ok := got.(time.Time); ok {
		if s, isString := want.Str(); isString && sameMoment(s, at) {
			return true, "", text(got)
		}
	}
	actual = svc.cellJSON(ctx, table, column, got, want)
	same, hint = jsonassert.Strict(column, actual, want)
	return same, hint, actual
}

// cellJSON is a value a selection holds, as JSON of its column's type.
func (svc *Service) cellJSON(ctx context.Context, table, column string, got any, want core.Value) string {
	if got == nil {
		return "null"
	}
	typeName, found := svc.columnTypes(ctx, table)[strings.ToLower(column)]
	kind, known := svc.Target.Dialect.columnKind(typeName)
	if !found || !known {
		if b, ok := got.(bool); ok {
			return strconv.FormatBool(b)
		}
		return storedJSON(got)
	}
	value := text(got)
	switch kind {
	case columntype.Integer:
		return value
	case columntype.Decimal:
		if !strings.ContainsAny(value, ".eE") {
			value += ".0"
		}
		return value
	case columntype.Boolean:
		return strconv.FormatBool(truthy(value))
	case columntype.IntegerOrBoolean:
		if _, isBool := boolValue(want); isBool {
			return strconv.FormatBool(truthy(value))
		}
		if len(value) == 1 && value[0] < '0' { // MySQL gives a BIT(1) as its byte
			return strconv.Itoa(int(value[0]))
		}
		return value
	case columntype.Text:
		if slices.Contains(charTypes, normalizeType(typeName)) {
			value = strings.TrimRight(value, " ")
		}
	default:
		// A JSON column holds what a value of `{...}` or `[...]` compares with.
		if j := want.JSON(); strings.HasPrefix(j, "{") || strings.HasPrefix(j, "[") {
			return value
		}
	}
	return jsonx.MarshalValue(value)
}

func boolValue(v core.Value) (value, isBool bool) {
	switch v.JSON() {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// truthy reads a boolean column's value: true, a bit, or an integer.
func truthy(value string) bool {
	switch value {
	case "true", "t", "1", "\x01":
		return true
	}
	return false
}

// storedJSON is a value as a row stores it, of its class: 850 an integer, 850.0
// a real, "850" text.
func storedJSON(v any) string {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		s := strconv.FormatFloat(x, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return jsonx.MarshalValue(text(v))
}

// keepContaining drops the rows the database selected whose JSONB column has a
// number of the other class on a condition's path: containment compares
// numbers by their value, so `3.0` contains `3`. Numbers are all it needs to
// look at: JSONB's strings, booleans and nulls are of one type each. A name a
// JSONPath cannot say is left to the database.
func keepContaining(sel *Selection, column string, pairs []core.Pair) {
	name := column[strings.LastIndexByte(column, '.')+1:]
	idx := slices.IndexFunc(sel.Columns, func(c string) bool { return strings.EqualFold(c, name) })
	if idx < 0 {
		return
	}
	kept := sel.Rows[:0:0]
	for _, row := range sel.Rows {
		doc := text(row[idx])
		keep := true
		for _, p := range pairs {
			v, err := core.ParseValue(p.Value, p.Null, false)
			if err != nil || !jsonassert.IsNumber(v) {
				continue
			}
			if same, hint, err := jsonassert.StrictAt(doc, p.Key, v); err == nil && !same {
				keep = false
				sel.hint(hint)
			}
		}
		if keep {
			kept = append(kept, row)
		}
	}
	sel.Rows = kept
}

// columnKind classifies a column by the name its driver gives its type; known
// is false for a column with no declared type. The drivers name them so:
//
//   - pgx: its type's name in upper case (INT4, BPCHAR, TIMESTAMPTZ, _TEXT), or,
//     for a type it has no codec for, a number columnTypes looks up (CITEXT, an
//     enum, MONEY).
//   - go-sql-driver, for MySQL and MariaDB: the wire protocol's (INT, UNSIGNED
//     BIGINT, VARCHAR, LONGTEXT, TINYINT, BIT, ENUM); BOOLEAN is TINYINT there.
//   - go-mssqldb: T-SQL's (INT, DECIMAL, NVARCHAR, BIT, MONEY, UNIQUEIDENTIFIER).
//   - modernc's SQLite: what the table's CREATE statement declares, in upper
//     case and with its size (VARCHAR(20), UNSIGNED BIG INT), or nothing.
//   - Oracle's catalog, as go-ora's names are no help (oracleColumnTypes): VARCHAR2,
//     CHAR, CLOB, DATE, TIMESTAMP(6), and NUMBER with its precision and scale,
//     NUMBER(10,2), or none.
func (d Dialect) columnKind(typeName string) (columntype.Kind, bool) {
	if d.Oracle {
		if kind, known, isNumber := oracleNumberKind(typeName); isNumber {
			return kind, known
		}
	}
	name := normalizeType(typeName)
	if name == "" || d.Name == sqlite.Name && name == "ANY" {
		return columntype.Other, false
	}
	switch {
	case d.Name == mysql.Name && (name == "TINYINT" || name == "BIT"):
		return columntype.IntegerOrBoolean, true
	case d.Name == mysql.Name && name == "YEAR":
		return columntype.Integer, true
	case d.BitBooleans && name == "BIT":
		return columntype.Boolean, true
	case d.Postgres && name == "MONEY":
		// PostgreSQL has no comparison of money and a number: it reads a
		// string as money, like the dates and times it reads from strings.
		return columntype.Other, true
	}
	if kind, ok := columnKinds[name]; ok {
		return kind, true
	}
	return columntype.Other, true
}

var typeSize = regexp.MustCompile(`\([^)]*\)`)

// normalizeType is a type's name in upper case, without its size and the words
// that do not change what it holds.
func normalizeType(typeName string) string {
	fields := strings.Fields(strings.ToUpper(typeSize.ReplaceAllString(typeName, " ")))
	kept := fields[:0]
	for _, f := range fields {
		switch f {
		case "UNSIGNED", "SIGNED", "ZEROFILL":
		default:
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " ")
}

var columnKinds = func() map[string]columntype.Kind {
	m := map[string]columntype.Kind{}
	add := func(k columntype.Kind, names ...string) {
		for _, n := range names {
			m[n] = k
		}
	}
	add(columntype.Integer, "INT", "INTEGER", "INT2", "INT4", "INT8", "BIG INT", "BIGINT", "SMALLINT", "TINYINT", "MEDIUMINT",
		"SERIAL", "SERIAL2", "SERIAL4", "SERIAL8", "SMALLSERIAL", "BIGSERIAL", "OID")
	add(columntype.Decimal, "NUMERIC", "DECIMAL", "DEC", "FLOAT", "FLOAT4", "FLOAT8", "DOUBLE", "DOUBLE PRECISION", "REAL",
		"MONEY", "SMALLMONEY", "BINARY_FLOAT", "BINARY_DOUBLE", "IBFLOAT", "IBDOUBLE")
	add(columntype.Boolean, "BOOL", "BOOLEAN")
	add(columntype.Text, "CHAR", "VARCHAR", "VARCHAR2", "NCHAR", "NVARCHAR", "NVARCHAR2", "BPCHAR", "CHARACTER", "CHARACTER VARYING",
		"VARYING CHARACTER", "NATIONAL CHARACTER", "NATIONAL CHAR", "NATIVE CHARACTER", "TEXT", "NTEXT", "TINYTEXT", "MEDIUMTEXT",
		"LONGTEXT", "CITEXT", "NAME", "CLOB", "NCLOB", "LONG")
	return m
}()
