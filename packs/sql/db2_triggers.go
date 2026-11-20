package sql

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// db2TriggerRow is the name a Db2 trigger knows the row by: the row as it
// would be stored, or the row a delete would take away.
const db2TriggerRow = "AXXROW"

// db2DDLLockTimeout is how long, in seconds, a trigger's creation waits for a
// table a service holds: Db2 waits forever unless a session says otherwise.
const db2DDLLockTimeout = ddlLockTimeoutMS / 1000

var db2SQLState = regexp.MustCompile(`^[0-9A-Z]{5}$`)

// db2State is the SQLSTATE a Db2 trigger raises, from what a step gives. Db2
// raises any but those of success (00), a warning (01) and no data (02),
// which fail nothing.
func db2State(code string) (string, error) {
	state := strings.ToUpper(code)
	switch {
	case strings.HasPrefix(state, "ORA-"):
		return "", fmt.Errorf("%s is an Oracle error: Db2's are five-character SQLSTATEs, like 40001", code)
	case !db2SQLState.MatchString(state):
		return "", fmt.Errorf("%s is no SQLSTATE: Db2's are five digits or letters, like 40001", code)
	case strings.HasPrefix(state, "00"), strings.HasPrefix(state, "01"), strings.HasPrefix(state, "02"):
		return "", fmt.Errorf("%s fails nothing on Db2: an SQLSTATE of class %s says a statement succeeded, with a warning or with no rows", code, state[:2])
	}
	return state, nil
}

// db2Variable is the name of the variable a trigger that keeps the row has:
// set while the row is kept, it stops the trigger for that insert. It is the
// session's, so the procedure that sets it sets it back.
func db2Variable(tr *Trigger) string { return strings.Replace(tr.Function, "AXX_P_", "AXX_V_", 1) }

// db2RaisedQuery counts the raises of a trigger from its sequence, which
// starts at 1 and is not cached, so the catalog has the next one.
func db2RaisedQuery(tr *Trigger) string {
	return "SELECT INT(nextcachefirstvalue) - 1 FROM syscat.sequences WHERE seqschema = CURRENT SCHEMA AND seqname = '" + tr.RaisedSequence + "'"
}

// createDb2Trigger makes writes of matching rows fail with an SQLSTATE
// (SIGNAL, which Db2 reports as SQL0438N with that state). Sequences count
// what it matched and raised: a sequence keeps its count when the write's
// statement rolls back. A trigger that keeps the row calls a procedure that
// inserts it in a transaction of its own (AUTONOMOUS), before it raises.
func (svc *Service) createDb2Trigger(ctx context.Context, event, table, code string, maxInvocations int, insertRow bool, t *core.Table) error {
	tbl, err := Ident(table)
	if err != nil {
		return err
	}
	state, err := db2State(code)
	if err != nil {
		return err
	}
	types, err := svc.columnTypes(ctx, tbl)
	if err != nil {
		return err
	}
	comparisons := svc.comparisons(types)
	where, err := whereClauseOf(t, db2TriggerRow+".", comparisons)
	if err != nil {
		return err
	}
	if _, err := svc.holdToColumns(types, tbl, t); err != nil {
		return err
	}
	fail := func(err error) error {
		return fmt.Errorf("Could not create before %s trigger on table '%s': %w", event, table, err) //nolint:staticcheck // user-facing message
	}
	check, _ := whereClauseOf(t, "", comparisons)
	if _, err := svc.DB.ExecContext(ctx, "SELECT 1 FROM "+tbl+" WHERE 1=0 AND "+check); err != nil {
		return fail(typeMismatch(err))
	}
	id := strings.ToUpper(randomSuffix())
	tr := &Trigger{
		Name: "AXX_T_" + id, Function: "AXX_P_" + id, Sequence: "AXX_S_" + id, RaisedSequence: "AXX_R_" + id,
		Table: tbl, MaxInvocations: maxInvocations, Event: event,
	}
	stmts := []string{
		"CREATE SEQUENCE " + tr.Sequence + " AS BIGINT START WITH 1 NO CACHE",
		"CREATE SEQUENCE " + tr.RaisedSequence + " AS BIGINT START WITH 1 NO CACHE",
	}
	keep := ""
	if insertRow {
		cols := svc.names(ctx, svc.db2StorableColumnsQuery(tbl))
		if len(cols) == 0 {
			return fail(errors.New("the database lists no columns of it"))
		}
		params := make([]string, len(cols))
		values := make([]string, len(cols))
		news := make([]string, len(cols))
		for i, c := range cols {
			values[i] = fmt.Sprintf("v%d", i)
			params[i] = fmt.Sprintf("IN v%d ANCHOR %s.%s", i, tbl, c)
			news[i] = db2TriggerRow + "." + c
		}
		// Db2 looks a variable and a procedure up by the session's path, which
		// has the user's schema and not the current one: they are named whole.
		schemas := svc.names(ctx, "VALUES CURRENT SCHEMA")
		if len(schemas) != 1 {
			return fail(errors.New("the database does not say its current schema"))
		}
		schema := `"` + strings.ReplaceAll(strings.TrimSpace(schemas[0]), `"`, `""`) + `".`
		variable, procedure := schema+db2Variable(tr), schema+tr.Function
		stmts = append(stmts,
			"CREATE VARIABLE "+variable+" SMALLINT DEFAULT 0",
			// A key the table has already is no other failure: the row is there.
			"CREATE PROCEDURE "+procedure+" ("+strings.Join(params, ", ")+") LANGUAGE SQL AUTONOMOUS BEGIN "+
				"DECLARE CONTINUE HANDLER FOR SQLSTATE '23505' BEGIN END; "+
				"SET "+variable+" = 1; "+
				"INSERT INTO "+tbl+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(values, ", ")+"); "+
				"SET "+variable+" = 0; END",
		)
		where = "(" + where + ") AND " + variable + " = 0"
		keep = "CALL " + procedure + "(" + strings.Join(news, ", ") + "); "
	}
	raise := keep + "SET n = NEXT VALUE FOR " + tr.RaisedSequence + "; " +
		"SIGNAL SQLSTATE '" + state + "' SET MESSAGE_TEXT = 'axx simulated exception (sqlState: " + state + ")';"
	if maxInvocations > 0 {
		raise = fmt.Sprintf("IF n <= %d THEN %s END IF;", maxInvocations, raise)
	}
	row := "NEW"
	if event == onDelete {
		row = "OLD"
	}
	stmts = append(stmts, "CREATE TRIGGER "+tr.Name+" NO CASCADE BEFORE "+strings.ToUpper(event)+" ON "+tbl+
		" REFERENCING "+row+" AS "+db2TriggerRow+" FOR EACH ROW WHEN ("+where+") "+
		"BEGIN DECLARE n BIGINT; SET n = NEXT VALUE FOR "+tr.Sequence+"; "+raise+" END")
	conn, err := svc.DB.Conn(ctx)
	if err != nil {
		return fail(err)
	}
	stmts = append([]string{fmt.Sprintf("SET CURRENT LOCK TIMEOUT %d", db2DDLLockTimeout)}, stmts...)
	for _, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SET CURRENT LOCK TIMEOUT NULL")
			_ = conn.Close() // before asking the pool for another
			_ = svc.dropDb2Triggers(context.WithoutCancel(ctx), []*Trigger{tr})
			return fail(err)
		}
	}
	_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SET CURRENT LOCK TIMEOUT NULL")
	if err := conn.Close(); err != nil {
		return fail(err)
	}
	svc.mu.Lock()
	svc.triggers = append(svc.triggers, tr)
	svc.mu.Unlock()
	return nil
}

// db2StorableColumnsQuery lists the columns a row's insert can set: not its
// generated ones, which Db2 fills in again, and not its hidden ones.
func (svc *Service) db2StorableColumnsQuery(tbl string) string {
	schema, name := splitTable(tbl)
	return "SELECT colname FROM syscat.columns WHERE " + svc.db2Table(schema, name) + " AND generated = '' AND hidden = '' ORDER BY colno"
}

// dropDb2Triggers drops what the triggers use; what a failed creation did
// not get to make, and what a trigger never had, is no failure.
func (svc *Service) dropDb2Triggers(ctx context.Context, trs []*Trigger) error {
	conn, err := svc.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET CURRENT LOCK TIMEOUT %d", db2DDLLockTimeout)); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), "SET CURRENT LOCK TIMEOUT NULL") }()
	var errs []error
	for _, tr := range trs {
		for _, s := range []string{
			"DROP TRIGGER " + tr.Name,
			"DROP PROCEDURE " + tr.Function,
			"DROP VARIABLE " + db2Variable(tr),
			"DROP SEQUENCE " + tr.Sequence,
			"DROP SEQUENCE " + tr.RaisedSequence,
		} {
			if _, err := conn.ExecContext(ctx, s); err != nil && db2Code(err) != db2Undefined {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// db2Undefined is Db2's code for a name it does not have.
const db2Undefined = -204
