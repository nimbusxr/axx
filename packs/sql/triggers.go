package sql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// Trigger DDL takes exclusive table locks and phantom inserts run in a second
// backend the outer transaction waits on. Under parallel scenarios this can
// form a cross-backend cycle PostgreSQL's deadlock detector cannot see (the
// outer session waits on dblink, not on a lock), so every edge axx controls
// times out instead of waiting forever.
const (
	ddlLockTimeoutMS     = 30000
	phantomLockTimeoutMS = 10000
)

// Trigger is a simulated before-insert trigger.
type Trigger struct {
	Name, Function, Sequence, RaisedSequence, Table string
	MaxInvocations                                  int
}

func triggerSteps() []core.StepDef {
	type variant struct {
		id, verb, limit string
		insert, limited bool
	}
	variants := []variant{
		{"sql.trigger.raise", "will raise", "", false, false},
		{"sql.trigger.raise.times", "will raise", " {int} time(s)", false, true},
		{"sql.trigger.insertRaise", "will insert and raise", "", true, false},
		{"sql.trigger.insertRaise.times", "will insert and raise", " {int} time(s)", true, true},
	}
	var out []core.StepDef
	for _, v := range variants {
		v := v
		doc := "PostgreSQL: create a BEFORE INSERT trigger that raises the SQLSTATE for inserted rows matching the table " +
			"(`column | value`, `null` for IS NULL), so you can test how your app handles database errors. The trigger is dropped when the scenario ends."
		if v.insert {
			doc += " The row is still committed through a second connection (dblink) before the error, simulating a " +
				"write that succeeded but reported failure."
		}
		if v.limited {
			doc += " Only the first N matching inserts raise."
		}
		out = append(out, core.StepDef{
			ID: v.id, Keyword: "Given", Arg: core.ArgTable,
			Expr: "a(n)[[ {ordinal} ordered]] before insert trigger on the {word} table[[ on {dbService}]] " + v.verb + " a(n) {sqlState} exception" + v.limit + " where:",
			Doc:  doc,
			Examples: []string{
				"Given a before insert trigger on the parcels.parcels table " + v.verb + " a 40001 exception" + strings.ReplaceAll(v.limit, "{int} time(s)", "1 time") + " where:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := service(sc, a, 2)
				if err != nil {
					return err
				}
				max := -1
				if v.limited {
					max = a.Int(4)
				}
				return svc.createTrigger(sc.Context(), a.String(1), a.String(3), max, v.insert, a.Table)
			},
		})
	}
	out = append(out, core.StepDef{
		ID: "sql.trigger.raised", Keyword: "Then",
		Expr:     "the[[ {ordinal} ordered]] before insert trigger on the {word} table[[ on {dbService}]] was raised {int} time(s)",
		Doc:      "Assert how many times a simulated trigger raised its exception. Triggers are numbered in creation order; the table must match the trigger's table.",
		Examples: []string{"Then the before insert trigger on the parcels.parcels table was raised 1 time"},
		Run: func(sc *core.Scenario, a core.Args) error {
			svc, err := service(sc, a, 2)
			if err != nil {
				return err
			}
			return svc.verifyTrigger(sc, a.IntOr(0, 1)-1, a.String(1), a.Int(3))
		},
	})
	return out
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (svc *Service) createTrigger(ctx context.Context, table, sqlState string, maxInvocations int, insertRow bool, t *core.Table) error {
	d := svc.Target.Dialect
	if !d.Postgres {
		return fmt.Errorf("Unsupported database type for triggers. Only PostgreSQL is currently supported. URL: %s", svc.URL) //nolint:staticcheck // user-facing message
	}
	tbl, err := Ident(table)
	if err != nil {
		return err
	}
	where, err := whereClause(d, t, "NEW.")
	if err != nil {
		return err
	}
	svc.mu.Lock()
	index := len(svc.triggers)
	svc.mu.Unlock()
	base := strings.ReplaceAll(tbl, ".", "_") + fmt.Sprintf("_%d_", index) + randomSuffix()
	tr := &Trigger{
		Name: "axx_trigger_" + base, Function: "axx_trigger_fn_" + base,
		Sequence: "axx_trigger_seq_" + base, RaisedSequence: "axx_trigger_raised_" + base,
		Table: tbl, MaxInvocations: maxInvocations,
	}
	insertBlock, recursionGuard := "", ""
	if insertRow {
		conn, err := svc.libpq()
		if err != nil {
			return err
		}
		guardSet := ""
		if maxInvocations < 0 {
			// Unlimited: a session variable stops the dblink insert from re-raising.
			recursionGuard = "IF current_setting('axx.skip_" + tr.Function + "', true) = 'true' THEN RETURN NEW; END IF; "
			guardSet = "SET axx.skip_" + tr.Function + " TO true; "
		}
		insertBlock = "PERFORM public.dblink_exec(" + conn + ", format(" +
			"'SET lock_timeout TO " + fmt.Sprint(phantomLockTimeoutMS) + "; " + guardSet +
			"INSERT INTO %I.%I SELECT * FROM json_populate_record(null::%I.%I, %L::json) ON CONFLICT DO NOTHING', " +
			"TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_TABLE_SCHEMA, TG_TABLE_NAME, row_to_json(NEW)::text)); "
	}
	raise := insertBlock + "PERFORM nextval('public." + tr.RaisedSequence + "'); " +
		"RAISE EXCEPTION 'axx simulated exception (sqlState: " + sqlState + ")' USING ERRCODE = '" + sqlState + "';"
	if maxInvocations > 0 {
		raise = fmt.Sprintf("IF current_count < %d THEN %s END IF;", maxInvocations, raise)
	}
	stmts := []string{"SET lock_timeout TO " + fmt.Sprint(ddlLockTimeoutMS)}
	if insertRow {
		stmts = append(stmts, "CREATE EXTENSION IF NOT EXISTS dblink WITH SCHEMA public")
	}
	stmts = append(stmts,
		"CREATE SEQUENCE public."+tr.Sequence+" START 0 MINVALUE 0",
		"CREATE SEQUENCE public."+tr.RaisedSequence+" START 1 MINVALUE 0",
		"CREATE OR REPLACE FUNCTION "+tr.Function+"() RETURNS TRIGGER AS $$ DECLARE current_count INTEGER; BEGIN "+
			recursionGuard+"IF "+where+" THEN current_count := nextval('public."+tr.Sequence+"'); "+raise+
			" END IF; RETURN NEW; END; $$ LANGUAGE plpgsql",
		"CREATE TRIGGER "+tr.Name+" BEFORE INSERT ON "+tbl+" FOR EACH ROW EXECUTE FUNCTION "+tr.Function+"()",
	)
	conn, err := svc.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for i, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			if strings.HasPrefix(s, "CREATE EXTENSION") {
				continue // may race with a concurrent scenario creating it
			}
			_ = i
			return fmt.Errorf("Could not create before insert trigger on table '%s': %w", table, err) //nolint:staticcheck // user-facing message
		}
	}
	_, _ = conn.ExecContext(context.Background(), "RESET lock_timeout")
	svc.mu.Lock()
	svc.triggers = append(svc.triggers, tr)
	svc.mu.Unlock()
	return nil
}

// verifyTrigger compares the number of times the trigger raised, counted
// exactly (not from the match counter's last_value, which is one short for a
// single raise).
func (svc *Service) verifyTrigger(sc *core.Scenario, index int, table string, want int) error {
	svc.mu.Lock()
	if index < 0 || index >= len(svc.triggers) {
		n := len(svc.triggers)
		svc.mu.Unlock()
		return fmt.Errorf("No trigger found at index %d for db service %s (%d trigger(s) created)", index, svc.Name, n) //nolint:staticcheck // user-facing message
	}
	tr := svc.triggers[index]
	svc.mu.Unlock()
	if !strings.EqualFold(tr.Table, table) {
		sc.Log("warning: trigger %d was created on %s, not %s", index+1, tr.Table, table)
	}
	var got int
	q := "SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM public." + tr.RaisedSequence
	if err := svc.DB.QueryRowContext(sc.Context(), q).Scan(&got); err != nil {
		return fmt.Errorf("Could not verify trigger invocation count: %w", err) //nolint:staticcheck // user-facing message
	}
	if got != want {
		return core.Fail(fmt.Sprintf("Expected before insert trigger '%s' to have been raised %d time(s), but was raised %d time(s)", tr.Name, want, got), want, got)
	}
	return nil
}

func (svc *Service) dropTriggers(ctx context.Context) error {
	svc.mu.Lock()
	trs := svc.triggers
	svc.triggers = nil
	svc.mu.Unlock()
	if len(trs) == 0 {
		return nil
	}
	conn, err := svc.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET lock_timeout TO "+fmt.Sprint(ddlLockTimeoutMS)); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "RESET lock_timeout") }()
	for _, tr := range trs {
		for _, s := range []string{
			"DROP TRIGGER IF EXISTS " + tr.Name + " ON " + tr.Table,
			"DROP FUNCTION IF EXISTS " + tr.Function,
			"DROP SEQUENCE IF EXISTS public." + tr.Sequence,
			"DROP SEQUENCE IF EXISTS public." + tr.RaisedSequence,
		} {
			if _, err := conn.ExecContext(ctx, s); err != nil {
				return err
			}
		}
	}
	return nil
}

// libpq returns a SQL expression that builds the dblink connection string
// inside the database server: it connects to itself on its own port and
// database (current_setting('port'), current_database()), so it works no
// matter how the port is mapped to the host. (The port and database of the
// client URL would break with remapped ports and URL parameters.)
func (svc *Service) libpq() (string, error) {
	if !svc.Target.Dialect.Postgres {
		return "", fmt.Errorf("Insert-and-raise triggers require a PostgreSQL database. URL: %s", svc.URL) //nolint:staticcheck // user-facing message
	}
	q := func(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }
	creds := " user=" + q(svc.User) + " password=" + q(svc.Password)
	return "format('host=localhost port=%s dbname=%s" + strings.ReplaceAll(creds, "'", "''") + "', current_setting('port'), quote_ident(current_database()))", nil
}
