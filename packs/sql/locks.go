package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nimbusxr/axx/core"
)

// lockTimeout bounds waiting for rows locked by someone else.
const lockTimeout = 10 * time.Second

func lockStep(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 1)
	if err != nil {
		return err
	}
	return svc.lockRows(sc.Context(), a.String(0), a.Table)
}

// lockRows runs SELECT ... FOR UPDATE on a dedicated connection whose
// transaction stays open until the locks are released.
func (svc *Service) lockRows(ctx context.Context, table string, t *core.Table) error {
	d := svc.Target.Dialect
	if d.ForUpdate == "" && d.ForUpdateHint == "" {
		return fmt.Errorf("row locks are not supported by %s", d.Name)
	}
	tbl, where, _, err := tableAndWhere(ctx, svc, table, t)
	if err != nil {
		return err
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.lockTx == nil {
		conn, err := svc.DB.Conn(context.Background())
		if err != nil {
			return fmt.Errorf("Could not lock rows in table '%s': %w", table, err) //nolint:staticcheck // user-facing message
		}
		tx, err := conn.BeginTx(context.Background(), nil)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("Could not lock rows in table '%s': %w", table, err) //nolint:staticcheck // user-facing message
		}
		svc.lockConn, svc.lockTx = conn, tx
	}
	q := "SELECT * FROM " + tbl + d.ForUpdateHint + " WHERE " + where + d.ForUpdate
	qctx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	failed := func(err error) error {
		if errors.Is(qctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("Could not acquire row lock on table '%s'; rows may be locked by another scenario. Timed out after 10 seconds.", table) //nolint:staticcheck // user-facing message
		}
		return fmt.Errorf("Could not lock rows in table '%s': %w", table, typeMismatch(err)) //nolint:staticcheck // user-facing message
	}
	rows, err := svc.lockTx.QueryContext(qctx, q)
	if err != nil {
		return failed(err)
	}
	for rows.Next() { //nolint:revive // drain
	}
	// A driver may meet an error, or the wait for a lock, only as it fetches
	// the rows, and then Next just stops: nothing is locked.
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return failed(err)
	}
	return rows.Close()
}

// releaseLocks rolls back the lock transaction and returns its connection.
func (svc *Service) releaseLocks() error {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.lockTx == nil {
		return nil
	}
	err := svc.lockTx.Rollback()
	cerr := svc.lockConn.Close()
	svc.lockTx, svc.lockConn = nil, nil
	if err != nil {
		return fmt.Errorf("Could not release row locks: %w", err) //nolint:staticcheck // user-facing message
	}
	return cerr
}
