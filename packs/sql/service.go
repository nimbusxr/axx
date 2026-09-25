package sql

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Service is a database registered in a scenario.
type Service struct {
	Name     string
	URL      string
	User     string
	Password string
	Schema   string
	Target   Target
	DB       *dbsql.DB

	mu         sync.Mutex
	selections []*Selection
	triggers   []*Trigger
	lockConn   *dbsql.Conn
	lockTx     *dbsql.Tx
}

// Selection is a materialized query result (like DbUnit's ITable).
type Selection struct {
	Table   string
	Query   string
	Columns []string
	Rows    [][]any
}

// Value returns the value of column in row (0-based); column names match
// case-insensitively.
func (s *Selection) Value(row int, column string) (any, error) {
	if row < 0 || row >= len(s.Rows) {
		return nil, fmt.Errorf("row %d does not exist; the selection has %d row(s)", row+1, len(s.Rows))
	}
	for i, c := range s.Columns {
		if strings.EqualFold(c, column) {
			return s.Rows[row][i], nil
		}
	}
	return nil, fmt.Errorf("column %q is not in the selection (columns: %s)", column, strings.Join(s.Columns, ", "))
}

type ScenarioContext struct {
	services *core.Services[*Service]
}

var stateKey = core.NewStateKey("sql", func(sc *core.Scenario) *ScenarioContext {
	st := &ScenarioContext{services: core.NewServices[*Service]("Database service", "No database services set")}
	sc.Describe("sql", st.describe)
	return st
}, func(_ *core.Scenario, st *ScenarioContext) error {
	return st.cleanup()
})

// cleanup releases row locks and drops simulated triggers when the scenario
// ends. Trigger cleanup failures fail the scenario.
func (st *ScenarioContext) cleanup() error {
	var errs []string
	for _, svc := range st.services.All() {
		if err := svc.releaseLocks(); err != nil {
			errs = append(errs, err.Error())
		}
		if err := svc.dropTriggers(context.Background()); err != nil {
			errs = append(errs, fmt.Sprintf("could not clean up triggers for db service %s: %v", svc.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (st *ScenarioContext) describe() any {
	out := map[string]any{}
	for _, svc := range st.services.All() {
		svc.mu.Lock()
		if n := len(svc.selections); n > 0 {
			last := svc.selections[n-1]
			rows := last.Rows
			if len(rows) > 5 {
				rows = rows[:5]
			}
			out[svc.Name] = map[string]any{"selections": n, "lastQuery": last.Query, "lastRowCount": len(last.Rows), "lastColumns": last.Columns, "lastRows": rows}
		}
		svc.mu.Unlock()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// service returns the service from argument i, or the default one.
func service(sc *core.Scenario, a core.Args, i int) (*Service, error) {
	if i >= 0 && a.Present(i) {
		return a.Value(i).(*Service), nil
	}
	return stateKey.Of(sc).services.Default()
}

// openDB returns a pooled handle shared across the run.
func openDB(s *core.Suite, t Target) (*dbsql.DB, error) {
	return core.Cached(s, "sql.db:"+t.Dialect.Driver+":"+t.DSN, func() (*dbsql.DB, error) {
		db, err := dbsql.Open(t.Dialect.Driver, t.DSN)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(32)
		db.SetConnMaxIdleTime(time.Minute)
		s.OnClose(func(context.Context) error { return db.Close() })
		return db, nil
	})
}

func (svc *Service) addSelection(sel *Selection) {
	svc.mu.Lock()
	svc.selections = append(svc.selections, sel)
	svc.mu.Unlock()
}

// selection returns selection i (0-based). New selections are always
// appended; ordinals only address them.
func (svc *Service) selection(i int) (*Selection, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.selections) == 0 {
		return nil, fmt.Errorf("no selections set for db service %s", svc.Name)
	}
	if i < 0 || i >= len(svc.selections) {
		return nil, fmt.Errorf("selection %d does not exist for db service %s; %d selection(s) were retrieved in this scenario", i+1, svc.Name, len(svc.selections))
	}
	return svc.selections[i], nil
}

// query runs a SELECT and materializes all rows.
func (svc *Service) query(ctx context.Context, table, q string) (*Selection, error) {
	rows, err := svc.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	sel := &Selection{Table: table, Query: q, Columns: cols}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		sel.Rows = append(sel.Rows, vals)
	}
	return sel, rows.Err()
}

// text renders a scanned column value for variables and messages.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	default:
		return fmt.Sprint(x)
	}
}
