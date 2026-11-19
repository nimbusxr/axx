package sql

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/packs/sql/internal/h2"
)

// H2 is a Java database that most services open inside their own process. axx
// reaches one that listens: an H2 server, or a file database its service opened
// with AUTO_SERVER=TRUE (internal/h2, a client of H2's own protocol).

// h2Guide is where the docs say how a service's H2 database is made reachable.
const h2Guide = "/guides/seed-and-query-sql/#h2"

// resolveH2 reads an H2 URL.
func resolveH2(raw, user, password string) (Target, error) {
	cfg, err := h2.ParseURL(raw, user, password)
	if errors.Is(err, h2.ErrPrivate) {
		return Target{}, fmt.Errorf("%w. For its test runs, give the service a URL that others can reach too, and give axx the same one: "+
			"a file database with AUTO_SERVER=TRUE (jdbc:h2:file:./build/parcels;AUTO_SERVER=TRUE), "+
			"or a database of an H2 server (jdbc:h2:tcp://localhost:9092/mem:parcels) (%s)", err, axxerr.Docs(h2Guide))
	}
	if err != nil {
		return Target{}, err
	}
	// The DSN is the pool's name in the run: a database as each user of it.
	return Target{Dialect: h2db, DSN: raw + "\x00" + cfg.User + "\x00" + cfg.Password, H2: &cfg}, nil
}

// h2Now is the moment by the session's clock, in its zone. H2 2 gives a
// session the zone of its client, as it does its Java client's: axx's is the
// machine's, or the URL's `TIME ZONE` setting. H2 1.4 has the server's.
func (svc *Service) h2Now(ctx context.Context) (time.Time, error) {
	var now any
	if err := svc.DB.QueryRowContext(ctx, "SELECT CAST(CURRENT_TIMESTAMP AS TIMESTAMP WITH TIME ZONE)").Scan(&now); err != nil {
		return time.Time{}, err
	}
	at, ok := now.(time.Time)
	if !ok {
		return time.Time{}, fmt.Errorf("its time reads as %v", now)
	}
	return at, nil
}

// h2JSON says a column holds JSON. H2 reads a string it is given for one as a
// JSON string, quotes and all, unless the string says it is JSON: FORMAT JSON.
func h2JSON(typeName string) bool { return normalizeType(typeName) == "JSON" }

// h2Binary says a column holds bytes. H2 1.4 reads a string it is given for
// one as hex, and H2 2 as the string's own bytes, and refuses to compare the
// two: axx writes bytes in hex, as it does for Oracle, and says so to each.
func h2Binary(typeName string) bool {
	switch normalizeType(typeName) {
	case "BINARY", "BINARY VARYING", "BINARY LARGE OBJECT":
		return true
	}
	return false
}

var hexDigits = regexp.MustCompile(`^'(?:[0-9A-Fa-f]{2})*'$`)

// h2Value is a string literal as H2 is to read it for a column of that type:
// a JSON column's as JSON, a column of bytes' as hex. Any other is as it is.
func h2Value(typeName, literal string) string {
	switch {
	case !strings.HasPrefix(literal, "'"):
	case h2JSON(typeName):
		return literal + " FORMAT JSON"
	case h2Binary(typeName) && hexDigits.MatchString(literal):
		return "X" + literal
	}
	return literal
}

// h2SeedValue is a seed's value for a column.
func h2SeedValue(types map[string]string, column, literal string) string {
	return h2Value(types[strings.ToLower(column)], literal)
}

// h2IdentityQuery lists the table's columns that H2 2 generates always, which
// take a row's value only when the INSERT says so. H2 1.4 has no such column,
// and no such catalog: its identity columns take what a row names.
func (svc *Service) h2IdentityQuery(table string) string {
	schema, name := splitTable(table)
	return "SELECT column_name FROM information_schema.columns WHERE " + svc.h2Table(schema, name) + " AND identity_generation = 'ALWAYS'"
}

// h2Table is the condition that picks a table's rows in H2's catalog. H2
// folds a name that is not quoted to upper case, unless the database is set
// not to, so the catalog's name is compared whatever its case.
func (svc *Service) h2Table(schema, name string) string {
	d := svc.Target.Dialect
	return "UPPER(table_schema) = UPPER(" + svc.h2Schema(schema) + ") AND UPPER(table_name) = UPPER(" + d.Literal(name) + ")"
}

func (svc *Service) h2Schema(schema string) string {
	if schema == "" {
		return "SCHEMA()"
	}
	return svc.Target.Dialect.Literal(schema)
}

// h2LockTimeout is H2's error for a row someone else holds, past the session's
// LOCK_TIMEOUT.
const h2LockTimeout = 50200

// h2DataConversion is H2's error for a value it cannot read as its column's type.
const h2DataConversion = 22018

func h2Code(err error) int {
	var e *h2.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}
