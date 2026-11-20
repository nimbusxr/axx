package sql

import (
	"context"
	"errors"
	"strings"

	"github.com/nimbusxr/axx/packs/sql/internal/db2"
)

// Db2 is reached by axx's own client of DRDA, the protocol Db2's servers
// speak (internal/db2): Db2 for Linux, UNIX and Windows, with a user and a
// password.

// resolveDb2 reads a Db2 URL.
func resolveDb2(raw, user, password string) (Target, error) {
	cfg, err := db2.ParseURL(raw, user, password)
	if err != nil {
		return Target{}, err
	}
	// The DSN is the pool's name in the run: a database as each user of it.
	return Target{Dialect: db2db, DSN: raw + "\x00" + cfg.User + "\x00" + cfg.Password, Db2: &cfg}, nil
}

// db2Now asks Db2 for its clock and its zone: CURRENT TIMEZONE is a duration
// in hours, minutes and seconds, as a number (-53000 for five and a half
// hours west).
const db2Now = "SELECT VARCHAR_FORMAT(CURRENT TIMESTAMP, 'YYYY-MM-DD HH24:MI:SS.FF6'), " +
	"INT(CURRENT TIMEZONE / 10000) * 60 + INT(MOD(CURRENT TIMEZONE, 10000) / 100) FROM sysibm.sysdummy1"

// db2Booleans gives a table's BOOLEAN columns their type. A client of
// DRDA's own is told a BOOLEAN is a SMALLINT, as the standard has no boolean
// at the level Db2 speaks it: the catalog says which columns are.
func (svc *Service) db2Booleans(ctx context.Context, tbl string, types map[string]string) {
	schema, name := splitTable(tbl)
	for _, column := range svc.names(ctx, "SELECT colname FROM syscat.columns WHERE "+svc.db2Table(schema, name)+" AND typename = 'BOOLEAN'") {
		if _, ok := types[strings.ToLower(column)]; ok {
			types[strings.ToLower(column)] = "BOOLEAN"
		}
	}
}

// db2Table is the condition that picks a table's rows in Db2's catalog,
// which keeps a name that is not quoted in upper case.
func (svc *Service) db2Table(schema, name string) string {
	return "tabschema = " + svc.schemaExpr(schema) + " AND tabname = UPPER(" + svc.Target.Dialect.Literal(name) + ")"
}

// db2Binary says a column holds bytes. Db2 reads a string it is given for
// one as the string's own bytes, or refuses it: axx writes bytes in hex, as
// it does for Oracle and H2, and says so (BX'78AB').
func db2Binary(typeName string) bool {
	switch normalizeType(typeName) {
	case "BINARY", "VARBINARY", "BLOB", "CHAR FOR BIT DATA", "VARCHAR FOR BIT DATA":
		return true
	}
	return false
}

// db2Value is a string literal as Db2 is to read it for a column of that
// type: a column of bytes' as hex. Any other is as it is.
func db2Value(typeName, literal string) string {
	if db2Binary(typeName) && hexDigits.MatchString(literal) {
		return "BX" + literal
	}
	return literal
}

// db2SeedValue is a seed's value for a column.
func db2SeedValue(types map[string]string, column, literal string) string {
	return db2Value(types[strings.ToLower(column)], literal)
}

// Db2's codes for a value it cannot read as its column's type: a string that
// is no number, operands of two types, and a string that is no date or time.
var db2Conversions = map[int]bool{-420: true, -401: true, -180: true, -181: true}

func db2Code(err error) int {
	var e *db2.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}
