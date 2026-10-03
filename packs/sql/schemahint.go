package sql

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// The database errors of a seed that names a column or a table the database
// does not have, as each one words them.
var (
	unknownColumn = regexp.MustCompile(`(?i)column .* does not exist|unknown column|invalid column name|has no column named`)
	unknownTable  = regexp.MustCompile(`(?i)relation .* does not exist|table .* doesn't exist|invalid object name|no such table`)
	duplicateKey  = regexp.MustCompile(`(?i)duplicate key|duplicate entry|unique constraint failed|violation of (?:primary key|unique key) constraint`)
)

// DuplicateKeyHint completes the error of a seed whose row has a key the
// database already holds: data stays after a scenario, so it is most often
// a row of an earlier run, or of another scenario's seed.
const DuplicateKeyHint = "; a row with that key is already there, from an earlier run or another scenario's seed, and a seed never overwrites: " +
	"give each scenario's rows keys of their own, and to insert the same keys again start from a fresh environment " +
	"(https://axx.nimbusxr.us/explanations/scenario-isolation/)"

// schemaHint completes the error of a seed row the database refused for an
// unknown column or table: it names the columns the table has, or the tables
// of its schema, so a seed written by guessing shows what to write. A key
// the table already holds gets DuplicateKeyHint. It is "" for any other
// error, or when the database cannot be asked.
func (svc *Service) schemaHint(ctx context.Context, table string, err error) string {
	msg := err.Error()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	schema, name := splitTable(table)
	switch {
	case duplicateKey.MatchString(msg):
		return DuplicateKeyHint
	case unknownColumn.MatchString(msg):
		if cols := svc.names(ctx, svc.columnsQuery(schema, name)); len(cols) > 0 {
			return "; " + table + " has the columns " + strings.Join(cols, ", ")
		}
	case unknownTable.MatchString(msg):
		if tables := svc.names(ctx, svc.tablesQuery(schema)); len(tables) > 0 {
			where := "the database"
			if schema != "" {
				where = "the schema " + schema
			}
			return "; " + where + " has the tables " + strings.Join(tables, ", ")
		}
	}
	return ""
}

func splitTable(table string) (schema, name string) {
	if i := strings.LastIndex(table, "."); i >= 0 {
		return table[:i], table[i+1:]
	}
	return "", table
}

// columnsQuery lists a table's columns in order; the table's schema is the
// connection's current one when the seed does not name it.
func (svc *Service) columnsQuery(schema, name string) string {
	d := svc.Target.Dialect
	if d.Name == sqlite.Name {
		return "SELECT name FROM pragma_table_info(" + d.Literal(name) + ") ORDER BY cid"
	}
	return "SELECT column_name FROM information_schema.columns WHERE table_schema = " + svc.schemaExpr(schema) +
		" AND table_name = " + d.Literal(name) + " ORDER BY ordinal_position"
}

// tablesQuery lists the tables of a schema.
func (svc *Service) tablesQuery(schema string) string {
	d := svc.Target.Dialect
	if d.Name == sqlite.Name {
		return "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
	}
	return "SELECT table_name FROM information_schema.tables WHERE table_schema = " + svc.schemaExpr(schema) + " ORDER BY table_name"
}

// schemaExpr is the schema as SQL: the one named, or the current one.
func (svc *Service) schemaExpr(schema string) string {
	d := svc.Target.Dialect
	switch {
	case schema != "":
		return d.Literal(schema)
	case d.Name == mysql.Name:
		return "DATABASE()"
	case d.Name == sqlserver.Name:
		return "SCHEMA_NAME()"
	default:
		return "current_schema()"
	}
}

// names runs a query of one text column and returns its values, or none if
// the database cannot answer it.
func (svc *Service) names(ctx context.Context, q string) []string {
	rows, err := svc.DB.QueryContext(ctx, q)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out
}
