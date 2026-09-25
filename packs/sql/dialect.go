package sql

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	// Pure-Go drivers (CGO_ENABLED=0 builds).
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

// Dialect captures the database-specific parts of the SQL pack.
type Dialect struct {
	Name   string
	Driver string
	// ForUpdate is appended to SELECT to lock rows ("" when unsupported).
	ForUpdate string
	// ForUpdateHint is inserted after the table name (SQL Server style).
	ForUpdateHint string
	// Postgres enables JSONB steps and trigger simulation.
	Postgres bool
	// BackslashEscapes means backslashes in string literals are escapes (MySQL).
	BackslashEscapes bool
}

var (
	postgres  = Dialect{Name: "postgresql", Driver: "pgx", ForUpdate: " FOR UPDATE", Postgres: true}
	mysql     = Dialect{Name: "mysql", Driver: "mysql", ForUpdate: " FOR UPDATE", BackslashEscapes: true}
	sqlite    = Dialect{Name: "sqlite", Driver: "sqlite"}
	sqlserver = Dialect{Name: "sqlserver", Driver: "sqlserver", ForUpdateHint: " WITH (UPDLOCK, ROWLOCK)"}
)

// Target is a resolved connection target.
type Target struct {
	Dialect Dialect
	DSN     string
	// Host, Port and Database are known for PostgreSQL (dblink phantom inserts).
	Host, Port, Database string
}

// Resolve converts a JDBC URL (as used in feature files) or a native URL into
// a driver DSN, adding user and password.
func Resolve(rawURL, user, password string) (Target, error) {
	u := strings.TrimSpace(rawURL)
	lower := strings.ToLower(u)
	switch {
	case strings.HasPrefix(lower, "jdbc:postgresql:"), strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return resolvePostgres(u, user, password)
	case strings.HasPrefix(lower, "jdbc:mysql:"), strings.HasPrefix(lower, "jdbc:mariadb:"), strings.HasPrefix(lower, "mysql://"):
		return resolveMySQL(u, user, password)
	case strings.HasPrefix(lower, "jdbc:sqlserver:"), strings.HasPrefix(lower, "sqlserver://"):
		return resolveSQLServer(u, user, password)
	case strings.HasPrefix(lower, "jdbc:sqlite:"), strings.HasPrefix(lower, "sqlite:"), strings.HasPrefix(lower, "file:"):
		path := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(u, "jdbc:"), "sqlite:"), "//")
		return Target{Dialect: sqlite, DSN: path}, nil
	}
	return Target{}, fmt.Errorf("unsupported database URL %q (supported: jdbc:postgresql, jdbc:mysql, jdbc:mariadb, jdbc:sqlserver, jdbc:sqlite, or native postgres://, mysql://, sqlserver:// URLs)", rawURL)
}

func resolvePostgres(raw, user, password string) (Target, error) {
	s := raw
	if strings.HasPrefix(strings.ToLower(s), "jdbc:") {
		s = s[len("jdbc:"):]
	}
	s = regexp.MustCompile(`(?i)^postgresql:`).ReplaceAllString(s, "postgres:")
	pu, err := url.Parse(s)
	if err != nil {
		return Target{}, fmt.Errorf("invalid PostgreSQL URL %q: %w", raw, err)
	}
	q := pu.Query()
	// JDBC-only parameters the pgx driver does not understand.
	for _, k := range []string{"currentSchema", "ApplicationName", "loginTimeout", "socketTimeout", "prepareThreshold", "stringtype"} {
		if v := q.Get(k); v != "" && k == "currentSchema" {
			q.Set("search_path", v)
		}
		q.Del(k)
	}
	if user != "" {
		pu.User = url.UserPassword(user, password)
	}
	if q.Get("sslmode") == "" {
		q.Set("sslmode", "disable")
	}
	pu.RawQuery = q.Encode()
	host, port := pu.Hostname(), pu.Port()
	if port == "" {
		port = "5432"
	}
	return Target{Dialect: postgres, DSN: pu.String(), Host: host, Port: port, Database: strings.TrimPrefix(pu.Path, "/")}, nil
}

func resolveMySQL(raw, user, password string) (Target, error) {
	s := regexp.MustCompile(`(?i)^(jdbc:)?(mysql|mariadb):`).ReplaceAllString(raw, "mysql:")
	pu, err := url.Parse(s)
	if err != nil {
		return Target{}, fmt.Errorf("invalid MySQL URL %q: %w", raw, err)
	}
	port := pu.Port()
	if port == "" {
		port = "3306"
	}
	q := pu.Query()
	q.Set("parseTime", "true")
	q.Set("multiStatements", "true")
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)%s?%s", user, password, pu.Hostname(), port, pu.Path, q.Encode())
	return Target{Dialect: mysql, DSN: dsn}, nil
}

func resolveSQLServer(raw, user, password string) (Target, error) {
	s := strings.TrimPrefix(raw, "jdbc:")
	if strings.HasPrefix(strings.ToLower(s), "sqlserver://") && strings.Contains(s, ";") {
		// jdbc:sqlserver://host:port;databaseName=db;encrypt=false
		parts := strings.Split(strings.TrimPrefix(s, "sqlserver://"), ";")
		hostPort := parts[0]
		q := url.Values{}
		for _, kv := range parts[1:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(k) {
			case "databasename", "database":
				q.Set("database", v)
			case "encrypt", "trustservercertificate":
				q.Set(strings.ToLower(k), v)
			}
		}
		s = "sqlserver://" + hostPort + "?" + q.Encode()
	}
	pu, err := url.Parse(s)
	if err != nil {
		return Target{}, fmt.Errorf("invalid SQL Server URL %q: %w", raw, err)
	}
	if user != "" {
		pu.User = url.UserPassword(user, password)
	}
	return Target{Dialect: sqlserver, DSN: pu.String()}, nil
}

// Literal renders s as a SQL string literal, escaping quotes (and
// backslashes where they are escapes), so values containing quotes are safe.
func (d Dialect) Literal(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	if d.BackslashEscapes {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}
	return "'" + s + "'"
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$]*){0,2}$`)

// Ident validates a (possibly schema-qualified) identifier. Identifiers are
// emitted unquoted, as before, so the database's case folding still applies.
func Ident(s string) (string, error) {
	if !identRe.MatchString(s) {
		return "", fmt.Errorf("invalid table or column name %q (letters, digits, _ and $; qualify with schema.table)", s)
	}
	return s, nil
}
