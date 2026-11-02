package sql

import (
	"context"
	dbsql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	goora "github.com/sijms/go-ora/v2"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/columntype"
)

// resolveOracle reads Oracle's URLs: jdbc:oracle:thin:@//host:1521/service,
// jdbc:oracle:thin:@host:1521/service, jdbc:oracle:thin:@host:1521:SID, a TNS
// descriptor after jdbc:oracle:thin:@, and oracle://host:1521/service.
func resolveOracle(raw, user, password string) (Target, error) {
	invalid := func() (Target, error) {
		return Target{}, fmt.Errorf("invalid Oracle URL %q: write jdbc:oracle:thin:@//host:1521/service or oracle://host:1521/service", raw)
	}
	s := strings.TrimSpace(raw)
	var hostPort, service, sid string
	options := map[string]string{}
	if strings.HasPrefix(strings.ToLower(s), "jdbc:") {
		rest, ok := cutPrefixFold(s, "jdbc:oracle:thin:@")
		if !ok {
			return invalid()
		}
		rest = strings.TrimPrefix(rest, "//")
		if strings.HasPrefix(rest, "(") {
			return Target{Dialect: oracle, DSN: goora.BuildJDBC(user, password, rest, nil)}, nil
		}
		var named bool
		if hostPort, service, named = strings.Cut(rest, "/"); !named {
			// host:port:SID
			i := strings.LastIndexByte(rest, ':')
			if i < 0 {
				return invalid()
			}
			hostPort, sid = rest[:i], rest[i+1:]
		}
	} else {
		pu, err := url.Parse(s)
		if err != nil {
			return invalid()
		}
		hostPort, service = pu.Host, strings.TrimPrefix(pu.Path, "/")
		for k, v := range pu.Query() {
			options[k] = strings.Join(v, ",")
		}
		if user == "" && pu.User != nil {
			user = pu.User.Username()
			password, _ = pu.User.Password()
		}
	}
	host, portText, err := net.SplitHostPort(hostPort)
	if err != nil {
		host, portText = hostPort, "1521"
	}
	port, err := strconv.Atoi(portText)
	if err != nil || host == "" || service == "" && sid == "" {
		return invalid()
	}
	if sid != "" {
		options["SID"] = sid
	}
	return Target{Dialect: oracle, DSN: goora.BuildUrl(host, port, service, user, password, options)}, nil
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}

// oracleSession gives each new connection the same reading of the dates, times and
// numbers a seed or a table writes as strings, whatever the database's own settings
// are: Oracle converts them by the session's formats.
type oracleSession struct {
	driver.Connector
}

const oracleSessionSettings = "ALTER SESSION SET NLS_DATE_FORMAT = 'YYYY-MM-DD HH24:MI:SS'" +
	" NLS_TIMESTAMP_FORMAT = 'YYYY-MM-DD HH24:MI:SS.FF'" +
	" NLS_TIMESTAMP_TZ_FORMAT = 'YYYY-MM-DD HH24:MI:SS.FF TZR'" +
	" NLS_NUMERIC_CHARACTERS = '.,'" +
	" DDL_LOCK_TIMEOUT = 30"

func (s oracleSession) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := s.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	exec, ok := conn.(driver.ExecerContext)
	if !ok {
		return conn, nil
	}
	if _, err := exec.ExecContext(ctx, oracleSessionSettings, nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("could not set the session's date and number formats: %w", err)
	}
	return conn, nil
}

var oracleNumber = regexp.MustCompile(`^NUMBER\((\*|\d+),(-?\d+)\)$`)

// oracleNumberKind classifies a NUMBER by the scale oracleColumnTypes puts in its
// name, which says what it holds. A NUMBER with none declared is a column with no
// declared type, held to what each row stores.
func oracleNumberKind(typeName string) (kind columntype.Kind, known, isNumber bool) {
	if typeName == "NUMBER" {
		return columntype.Other, false, true
	}
	m := oracleNumber.FindStringSubmatch(typeName)
	if m == nil {
		return columntype.Other, false, false
	}
	if scale, _ := strconv.Atoi(m[2]); scale > 0 {
		return columntype.Decimal, true, true
	}
	return columntype.Integer, true, true
}

// oracleColumnTypes is the type of each of the table's columns, by its name in lower
// case, from Oracle's catalog: the driver names a VARCHAR2 an NCHAR, and gives no
// scale a NUMBER with none can be told by.
func (svc *Service) oracleColumnTypes(ctx context.Context, tbl string) map[string]string {
	d := svc.Target.Dialect
	schema, name := splitTable(tbl)
	rows, err := svc.DB.QueryContext(ctx, "SELECT column_name, data_type, data_precision, data_scale FROM all_tab_columns WHERE owner = "+
		svc.schemaExpr(schema)+" AND table_name = UPPER("+d.Literal(name)+")")
	if err != nil {
		return nil
	}
	defer rows.Close()
	types := map[string]string{}
	for rows.Next() {
		var column, dataType string
		var precision, scale dbsql.NullInt64
		if rows.Scan(&column, &dataType, &precision, &scale) != nil {
			return nil
		}
		if dataType == "NUMBER" && scale.Valid {
			size := "*"
			if precision.Valid {
				size = strconv.FormatInt(precision.Int64, 10)
			}
			dataType = fmt.Sprintf("NUMBER(%s,%d)", size, scale.Int64)
		}
		types[strings.ToLower(column)] = dataType
	}
	return types
}

// oracleReaders are what turns the values of a result's columns, by their places in
// it, into what they hold, as a source keeps its types (ADR 0014): the driver gives
// every NUMBER as text, a BOOLEAN's too, so 2.00 in a NUMBER(10,2) would read as the
// text 2.
func (svc *Service) oracleReaders(ctx context.Context, table string, cols []string) map[int]func(any) any {
	types := svc.oracleColumnTypes(ctx, table)
	out := map[int]func(any) any{}
	for i, c := range cols {
		typeName, ok := types[strings.ToLower(c)]
		if !ok {
			continue
		}
		kind, known := svc.Target.Dialect.columnKind(typeName)
		switch {
		case typeName == "BOOLEAN":
			out[i] = oracleBoolean
		case !known:
			out[i] = oracleWhole
		case kind == columntype.Integer:
			out[i] = oracleWhole
		case kind == columntype.Decimal:
			scale := 1
			if m := oracleNumber.FindStringSubmatch(typeName); m != nil {
				scale, _ = strconv.Atoi(m[2])
			}
			out[i] = func(v any) any { return oracleDecimal(v, scale) }
		}
	}
	return out
}

func oracleBoolean(v any) any {
	switch v {
	case "1":
		return true
	case "0":
		return false
	}
	return v
}

// oracleWhole is a NUMBER that is an integer as one, and any other as a decimal: for
// a column with no scale, what each row stores says which.
func oracleWhole(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return f
	}
	return v
}

// oracleDecimal is a value of a column that holds decimals, with its scale's places
// at least: 2 in a NUMBER(10,2) is 2.00.
func oracleDecimal(v any, scale int) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	whole, fraction, _ := strings.Cut(s, ".")
	if len(fraction) < scale {
		fraction += strings.Repeat("0", scale-len(fraction))
	}
	return whole + "." + fraction
}

// The forms a date or a time takes as a string, in a seed or a table, with its offset
// or without.
var (
	oracleTimes = []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	oracleZoned = []string{"2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05 -07:00", "2006-01-02T15:04:05Z07:00"}
)

// oracleTemporal is a string as a literal of a column of that type when the column
// holds dates or times and the string is one. Oracle's own conversion goes by a
// session's single format, which refuses a T, an offset, or a time zone column's
// value with no fraction.
func oracleTemporal(typeName, s string) (string, bool) {
	if typeName != "DATE" && !strings.HasPrefix(typeName, "TIMESTAMP") {
		return "", false
	}
	var at time.Time
	zoned := false
	for _, layout := range oracleTimes {
		if t, err := time.Parse(layout, s); err == nil {
			at = t
		}
	}
	for _, layout := range oracleZoned {
		if t, err := time.Parse(layout, s); err == nil {
			at, zoned = t, true
		}
	}
	if at.IsZero() {
		return "", false
	}
	const plain = "2006-01-02 15:04:05.999999999"
	switch {
	case typeName == "DATE":
		return "TO_DATE('" + at.Format("2006-01-02 15:04:05") + "', 'YYYY-MM-DD HH24:MI:SS')", true
	case zoned && strings.Contains(typeName, "TIME ZONE"):
		return "TIMESTAMP '" + at.Format(plain+" -07:00") + "'", true
	}
	return "TIMESTAMP '" + at.Format(plain) + "'", true
}

// oracleSeedValue is a seed's value for a column, as oracleTemporal gives it when the
// column holds dates or times.
func oracleSeedValue(types map[string]string, column, literal string) string {
	if len(literal) < 2 || literal[0] != '\'' || literal[len(literal)-1] != '\'' {
		return literal
	}
	if out, ok := oracleTemporal(types[strings.ToLower(column)], literal[1:len(literal)-1]); ok {
		return out
	}
	return literal
}

// oracleLiterals is how whereClause writes a value of the table's columns on Oracle.
func (svc *Service) oracleLiterals(ctx context.Context, tbl string) func(string, core.Value) string {
	d := svc.Target.Dialect
	types := svc.oracleColumnTypes(ctx, tbl)
	return func(column string, v core.Value) string {
		name := column[strings.LastIndexByte(column, '.')+1:]
		if s, ok := v.Str(); ok {
			if out, ok := oracleTemporal(types[strings.ToLower(name)], s); ok {
				return out
			}
		}
		return d.Value(v)
	}
}

// oracleCode is the number of the ORA error a step gives: ORA-20001, or its digits.
func oracleCode(code string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(code, "ORA-"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s is no Oracle error: write its number, like ORA-20001", code)
	}
	return n, nil
}

// oracleRaisedQuery counts the raises of a trigger from its sequence, which starts
// at 1, and is NOCACHE, so its LAST_NUMBER is the next one.
func oracleRaisedQuery(tr *Trigger) string {
	return "SELECT last_number - 1 FROM user_sequences WHERE sequence_name = '" + tr.RaisedSequence + "'"
}

// createOracleTrigger makes inserts of matching rows fail with an ORA error. NOCACHE
// sequences count what it matched and raised: LAST_NUMBER is exact, and a sequence
// keeps its count when the insert's statement rolls back.
func (svc *Service) createOracleTrigger(ctx context.Context, table, code string, maxInvocations int, insertRow bool, t *core.Table) error {
	tbl, err := Ident(table)
	if err != nil {
		return err
	}
	number, err := oracleCode(code)
	if err != nil {
		return err
	}
	literals := svc.oracleLiterals(ctx, tbl)
	where, err := whereClauseOf(t, ":NEW.", literals)
	if err != nil {
		return err
	}
	if _, err := svc.holdToColumns(ctx, tbl, t); err != nil {
		return err
	}
	check, _ := whereClauseOf(t, "", literals)
	if _, err := svc.DB.ExecContext(ctx, "SELECT 1 FROM "+tbl+" WHERE 1=0 AND "+check); err != nil {
		return fmt.Errorf("Could not create before insert trigger on table '%s': %w", table, typeMismatch(err)) //nolint:staticcheck // user-facing message
	}
	id := strings.ToUpper(randomSuffix())
	tr := &Trigger{
		Name: "AXX_T_" + id, Function: "AXX_P_" + id, Sequence: "AXX_S_" + id, RaisedSequence: "AXX_R_" + id,
		Table: tbl, MaxInvocations: maxInvocations,
	}
	raise := oracleRaise(tr, number)
	if maxInvocations > 0 {
		raise = fmt.Sprintf("IF %s.NEXTVAL <= %d THEN %s END IF;", tr.Sequence, maxInvocations, raise)
	}
	declare, guard := "", ""
	if number < 20000 || number > 20999 {
		declare = fmt.Sprintf("simulated EXCEPTION; PRAGMA EXCEPTION_INIT(simulated, -%d); ", number)
	}
	stmts := []string{
		"CREATE SEQUENCE " + tr.Sequence + " START WITH 1 NOCACHE",
		"CREATE SEQUENCE " + tr.RaisedSequence + " START WITH 1 NOCACHE",
	}
	if insertRow {
		cols := svc.names(ctx, svc.storableColumnsQuery(tbl))
		if len(cols) == 0 {
			return fmt.Errorf("Could not create before insert trigger on table '%s': the database lists no columns of it", table) //nolint:staticcheck // user-facing message
		}
		values := make([]string, len(cols))
		params := make([]string, len(cols))
		for i, c := range cols {
			values[i] = fmt.Sprintf("v%d", i)
			params[i] = fmt.Sprintf("v%d IN %s.%s%%TYPE", i, tbl, c)
		}
		news := make([]string, len(cols))
		for i, c := range cols {
			news[i] = ":NEW." + c
		}
		stmts = append(stmts, "CREATE PACKAGE "+tr.Function+" AS skip BOOLEAN := FALSE; END;")
		declare += "PROCEDURE keep(" + strings.Join(params, ", ") + ") IS PRAGMA AUTONOMOUS_TRANSACTION; BEGIN " +
			tr.Function + ".skip := TRUE; " +
			"INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(values, ", ") + "); COMMIT; " +
			tr.Function + ".skip := FALSE; " +
			"EXCEPTION WHEN DUP_VAL_ON_INDEX THEN ROLLBACK; " + tr.Function + ".skip := FALSE; " +
			"WHEN OTHERS THEN ROLLBACK; " + tr.Function + ".skip := FALSE; RAISE; END; "
		guard = "IF " + tr.Function + ".skip THEN RETURN; END IF; "
		raise = "keep(" + strings.Join(news, ", ") + "); " + raise
	}
	stmts = append(stmts, "CREATE TRIGGER "+tr.Name+" BEFORE INSERT ON "+tbl+" FOR EACH ROW DECLARE "+declare+"BEGIN "+guard+
		"IF "+where+" THEN "+raise+" END IF; END;")
	fail := func(err error) error {
		return fmt.Errorf("Could not create before insert trigger on table '%s': %w", table, err) //nolint:staticcheck // user-facing message
	}
	conn, err := svc.DB.Conn(ctx)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()
	for _, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			_ = svc.dropOracleTriggers(context.WithoutCancel(ctx), []*Trigger{tr})
			return fail(err)
		}
	}
	// Oracle creates a trigger that does not compile, and it fails every insert.
	if errs := svc.names(ctx, "SELECT text FROM user_errors WHERE name = '"+tr.Name+"' ORDER BY sequence"); len(errs) > 0 {
		_ = svc.dropOracleTriggers(context.WithoutCancel(ctx), []*Trigger{tr})
		return fail(errors.New(strings.Join(errs, "; ")))
	}
	svc.mu.Lock()
	svc.triggers = append(svc.triggers, tr)
	svc.mu.Unlock()
	return nil
}

// oracleRaise counts the raise and raises: RAISE_APPLICATION_ERROR for its range of
// numbers, and an exception bound to the number for any other.
func oracleRaise(tr *Trigger, number int) string {
	count := "DECLARE n INTEGER; BEGIN n := " + tr.RaisedSequence + ".NEXTVAL; END; "
	if number >= 20000 && number <= 20999 {
		return count + fmt.Sprintf("RAISE_APPLICATION_ERROR(-%d, 'axx simulated exception (ORA-%05d)');", number, number)
	}
	return count + "RAISE simulated;"
}

// storableColumnsQuery lists the columns a row's insert can set: not its generated
// ones, which the database fills in again.
func (svc *Service) storableColumnsQuery(tbl string) string {
	d := svc.Target.Dialect
	schema, name := splitTable(tbl)
	return "SELECT column_name FROM all_tab_cols WHERE owner = " + svc.schemaExpr(schema) +
		" AND table_name = UPPER(" + d.Literal(name) + ") AND virtual_column = 'NO' AND hidden_column = 'NO' AND identity_column = 'NO' ORDER BY column_id"
}

// dropOracleTriggers drops what the triggers use; what a failed creation did not get
// to make is no failure.
func (svc *Service) dropOracleTriggers(ctx context.Context, trs []*Trigger) error {
	var errs []error
	for _, tr := range trs {
		for _, s := range []string{
			"DROP TRIGGER " + tr.Name,
			"DROP PACKAGE " + tr.Function,
			"DROP SEQUENCE " + tr.Sequence,
			"DROP SEQUENCE " + tr.RaisedSequence,
		} {
			if _, err := svc.DB.ExecContext(ctx, s); err != nil && !oracleNotThere(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// oracleNotThere says a drop found nothing to drop: ORA-04080 a trigger, ORA-04043 a
// package and ORA-02289 a sequence.
func oracleNotThere(err error) bool {
	for _, code := range []string{"ORA-04080", "ORA-04043", "ORA-02289"} {
		if strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}
