package sql

import (
	"context"
	dbsql "database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// lockingDriver answers a query at once, and fails, or waits, only as its
// rows are fetched, as drivers that stream their rows do.
type lockingDriver struct {
	next func(ctx context.Context) error
}

func (d lockingDriver) Connect(context.Context) (driver.Conn, error) { return lockingConn(d), nil }
func (d lockingDriver) Driver() driver.Driver                        { return nil }

type lockingConn lockingDriver

func (lockingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no statements") }
func (lockingConn) Close() error                        { return nil }
func (lockingConn) Begin() (driver.Tx, error)           { return lockingTx{}, nil } //nolint:staticcheck // the driver's own interface

func (c lockingConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return lockingRows{ctx: ctx, next: c.next}, nil
}

type lockingTx struct{}

func (lockingTx) Commit() error   { return nil }
func (lockingTx) Rollback() error { return nil }

type lockingRows struct {
	ctx  context.Context
	next func(ctx context.Context) error
}

func (lockingRows) Columns() []string           { return []string{"reference"} }
func (lockingRows) Close() error                { return nil }
func (r lockingRows) Next([]driver.Value) error { return r.next(r.ctx) }

func lockService(t *testing.T, next func(ctx context.Context) error) *Service {
	t.Helper()
	db := dbsql.OpenDB(lockingDriver{next: next})
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{Target: Target{Dialect: postgres}, DB: db}
	t.Cleanup(func() { _ = svc.releaseLocks() })
	return svc
}

var lockedRow = &core.Table{Rows: [][]string{{"reference", "PX-LCK-1"}}}

// An error while the rows are fetched is an error of the lock: nothing was
// locked, and the step does not say it was.
func TestAnErrorFetchingTheLockedRowsFails(t *testing.T) {
	svc := lockService(t, func(context.Context) error { return errors.New("cannot convert the column") })
	err := svc.lockRows(context.Background(), "parcels", lockedRow)
	if err == nil || err.Error() != "Could not lock rows in table 'parcels': cannot convert the column" {
		t.Errorf("got %v", err)
	}
}

func TestATimeoutFetchingTheLockedRowsSaysSo(t *testing.T) {
	svc := lockService(t, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := svc.lockRows(ctx, "parcels", lockedRow)
	if err == nil || !strings.Contains(err.Error(), "Could not acquire row lock on table 'parcels'") {
		t.Errorf("got %v", err)
	}
}

func TestRowsThatFetchLock(t *testing.T) {
	svc := lockService(t, func(context.Context) error { return io.EOF })
	if err := svc.lockRows(context.Background(), "parcels", lockedRow); err != nil {
		t.Error(err)
	}
}
