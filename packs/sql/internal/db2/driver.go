package db2

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	cancelTimeout = 5 * time.Second
	cancelAgain   = 500 * time.Millisecond
	// Db2's codes this client acts on.
	codeIsQuery  = -84  // a query, run as a statement that gives no rows
	codeNotQuery = -517 // a statement that gives no rows, opened as a query
)

// broken marks the connection as one database/sql must not use again.
func (c *conn) broken(err error) error {
	c.closed = true
	_ = c.nc.Close()
	return fmt.Errorf("db2: the connection to %s broke: %w", c.cfg.Addr, err)
}

// watch ends what the connection does when ctx ends: it asks the server to
// interrupt the request that runs, over a connection of its own as DRDA has
// it, and stops waiting for an answer soon after. A server interrupts the
// request it is running when it is asked, so one that was between two of a
// statement's requests is asked again. The function it returns ends the
// watch.
func (c *conn) watch(ctx context.Context) (stop func()) {
	if ctx.Done() == nil {
		return func() {}
	}
	finished := make(chan struct{})
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		select {
		case <-finished:
			return
		case <-ctx.Done():
		}
		c.interrupted.Store(true)
		giveUp := time.After(cancelTimeout)
		for c.cancel() == nil {
			select {
			case <-finished:
				return
			case <-giveUp:
				_ = c.nc.SetDeadline(time.Now())
				return
			case <-time.After(cancelAgain):
			}
		}
		_ = c.nc.SetDeadline(time.Now())
	}()
	return func() {
		close(finished)
		<-gone
	}
}

// cancel asks the server to interrupt this connection's request.
func (c *conn) cancel() error {
	if len(c.interrupt) == 0 {
		return errors.New("db2: the server gave no token to interrupt a request by")
	}
	ctx, done := context.WithTimeout(context.Background(), cancelTimeout)
	defer done()
	nc, err := dial(ctx, c.cfg)
	if err != nil {
		return err
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(cancelTimeout))
	other := newConn(c.cfg, nc)
	if err := other.attributes(); err != nil {
		return err
	}
	database, err := other.name(c.cfg.Database)
	if err != nil {
		return err
	}
	var p buf
	var rdbnam buf
	rdbnam.padded(database, 18, other.space())
	p.param(cpRDBNAM, rdbnam.b)
	p.param(cpRDBINTTKN, c.interrupt)
	if err := other.write(chain([]part{command(cpINTRDBRQS, &p)})); err != nil {
		return err
	}
	// The server says the command is complete when it interrupted the
	// request, or closes the connection without a word.
	items, err := other.read()
	if err != nil {
		return nil
	}
	for _, it := range items {
		if it.reply && it.cp == cpCMDCMPRM {
			continue
		}
		if err := other.refusal(it); err != nil {
			return err
		}
		return fmt.Errorf("%w: it answered a request to interrupt with %#04x", errProtocol, it.cp)
	}
	return nil
}

// result is what a chain of replies says of a statement.
type result struct {
	// ca is how the statement ended: its first error when it has one, and
	// otherwise the last word on it.
	ca   *sqlca
	cols []column
	// described says the server described the statement's columns.
	described bool
	layout    []byte
	data      []byte
	outside   [][]byte
	// opened says a query is open, and ended that it ended.
	opened, ended bool
	instance      []byte
}

// take reads a chain of replies into a result.
func (c *conn) take(res *result, items []item) error {
	note := func(ca *sqlca) {
		if ca == nil {
			return
		}
		if res.ca == nil || res.ca.code >= 0 {
			res.ca = ca
		}
	}
	for _, it := range items {
		switch it.cp {
		case cpSQLCARD:
			ca, err := c.sqlca(&reader{b: it.data, little: c.little})
			if err != nil {
				return err
			}
			note(ca)
		case cpSQLDARD:
			ca, cols, err := c.describe(it.data)
			if err != nil {
				return err
			}
			note(ca)
			if ca == nil || ca.code >= 0 {
				res.cols, res.described = cols, true
			}
		case cpOPNQRYRM:
			ps, err := params(it.data)
			if err != nil {
				return err
			}
			res.opened = true
			res.instance = ps[cpQRYINSID]
		case cpQRYDSC:
			res.layout = append(res.layout, it.data...)
		case cpQRYDTA:
			res.data = append(res.data, it.data...)
		case cpEXTDTA:
			res.outside = append(res.outside, it.data)
		case cpENDQRYRM:
			res.ended = true
		default:
			if err := c.refusal(it); err != nil {
				return err
			}
		}
	}
	return nil
}

// exchange writes a chain of requests and reads the replies into res.
func (c *conn) exchange(res *result, requests ...[]part) error {
	if err := c.write(chain(requests...)); err != nil {
		return c.broken(err)
	}
	items, err := c.read()
	if err != nil {
		return c.broken(err)
	}
	if err := c.take(res, items); err != nil {
		if errors.Is(err, errProtocol) {
			// What follows on this connection is not to be trusted.
			c.closed = true
			_ = c.nc.Close()
		}
		return err
	}
	return nil
}

var commit = []part{command(cpRDBCMM, nil)}

// update runs a statement that gives no rows, and says how many it changed.
// A query is run all the same, for no rows.
func (c *conn) update(sql string) (int64, error) {
	n, err := c.immediately(sql)
	var e *Error
	if errors.As(err, &e) && e.Code == codeIsQuery && !c.interrupted.Load() {
		if _, err := c.query(sql); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return n, err
}

func (c *conn) immediately(sql string) (int64, error) {
	var res result
	requests := [][]part{c.immediate(sql)}
	if !c.inTx {
		requests = append(requests, commit)
	}
	if err := c.exchange(&res, requests...); err != nil {
		return 0, err
	}
	if err := c.failure(res.ca, sql); err != nil {
		return 0, err
	}
	if res.ca == nil {
		return 0, nil
	}
	return res.ca.rows, nil
}

// query runs a statement and reads all of its rows. A statement that gives
// no rows is run all the same, for none.
func (c *conn) query(sql string) (*rows, error) {
	rs, err := c.open(sql)
	var e *Error
	if errors.As(err, &e) && e.Code == codeNotQuery && !c.interrupted.Load() {
		// Db2 prepared it and ran nothing.
		if _, err := c.immediately(sql); err != nil {
			return nil, err
		}
		return &rows{}, nil
	}
	return rs, err
}

func (c *conn) open(sql string) (*rows, error) {
	var res result
	if err := c.exchange(&res, c.prepare(sql, 1), c.openQuery(1)); err != nil {
		return nil, err
	}
	for res.opened && !res.ended && (res.ca == nil || res.ca.code >= 0) {
		if err := c.exchange(&res, c.continueQuery(1, res.instance)); err != nil {
			return nil, err
		}
	}
	// Outside a transaction a statement is a unit of work of its own. Inside
	// one, a unit Db2 rolled back (a deadlock's) is the caller's to end: what
	// it runs next is no part of what it rolled back.
	if !c.inTx {
		var end result
		if err := c.exchange(&end, commit); err != nil {
			return nil, err
		}
	}
	if err := c.failure(res.ca, sql); err != nil {
		return nil, err
	}
	return c.rows(&res)
}

// failure is the error of a statement that ended with one, with Db2's own
// message for it.
func (c *conn) failure(ca *sqlca, sql string) error {
	if ca == nil || ca.code >= 0 {
		return nil
	}
	err := &Error{Code: ca.code, SQLState: ca.state, Tokens: ca.tokens, SQL: sql, Message: ca.text()}
	if c.interrupted.Load() {
		// Its time is up: the caller is told so, and waits for no message.
		return err
	}
	if text := c.message(ca); text != "" {
		err.Message = text
	}
	return err
}

// message asks Db2 for the text of a message: a connection carries a
// message's number and what it names, and the server keeps the texts.
func (c *conn) message(ca *sqlca) string {
	if c.closed || c.asking {
		return ""
	}
	c.asking = true
	defer func() { c.asking = false }()
	delimiter := ""
	for _, d := range []string{";", "|", "~", "^", "\x1F"} {
		if !strings.Contains(strings.Join(ca.tokens, ""), d) {
			delimiter = d
			break
		}
	}
	if delimiter == "" {
		return ""
	}
	code := ca.code
	if code < 0 {
		code = -code
	}
	sql := fmt.Sprintf("VALUES SYSPROC.SQLERRM('SQL%04d', %s, '%s', 'en_US', 1)",
		code, quote(strings.Join(ca.tokens, delimiter)), delimiter)
	rs, err := c.query(sql)
	if err != nil || len(rs.data) != 1 || len(rs.data[0]) != 1 {
		return ""
	}
	text, _ := rs.data[0][0].(string)
	return strings.TrimSpace(text)
}

// quote is a string literal of s.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// run runs a statement until ctx ends.
func (c *conn) run(ctx context.Context, sql string, query bool) (*rows, int64, error) {
	if c.closed {
		return nil, 0, driver.ErrBadConn
	}
	c.interrupted.Store(false)
	stop := c.watch(ctx)
	var (
		rs  *rows
		n   int64
		err error
	)
	if query {
		rs, err = c.query(sql)
	} else {
		n, err = c.update(sql)
	}
	stop()
	if err != nil && ctx.Err() != nil {
		if !c.closed {
			// The deadline the watch set must not end the next statement.
			_ = c.nc.SetDeadline(time.Time{})
		}
		return nil, 0, ctx.Err()
	}
	return rs, n, err
}

// Prepare keeps a statement to run it later, as it is written.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{c: c, sql: query}, nil
}

type stmt struct {
	c   *conn
	sql string
}

func (s *stmt) Close() error  { return nil }
func (s *stmt) NumInput() int { return 0 }

func (s *stmt) Exec([]driver.Value) (driver.Result, error) {
	return s.c.ExecContext(context.Background(), s.sql, nil)
}

func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return s.c.QueryContext(context.Background(), s.sql, nil)
}

var errParameters = errors.New("db2: this client runs statements as they are written, with no parameters")

// ExecContext runs a statement that gives no rows.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) > 0 {
		return nil, errParameters
	}
	_, n, err := c.run(ctx, query, false)
	if err != nil {
		return nil, err
	}
	return driver.RowsAffected(n), nil
}

// QueryContext runs a statement and gives its rows.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) > 0 {
		return nil, errParameters
	}
	rs, _, err := c.run(ctx, query, true)
	if err != nil {
		return nil, err
	}
	return rs, nil
}

// Ping asks the server for a row.
func (c *conn) Ping(ctx context.Context) error {
	_, _, err := c.run(ctx, "VALUES 1", true)
	return err
}

// IsValid says whether the connection can run another statement.
func (c *conn) IsValid() bool { return !c.closed }

// ResetSession is called before a connection is used again.
func (c *conn) ResetSession(context.Context) error {
	if c.closed {
		return driver.ErrBadConn
	}
	return nil
}

// Begin starts a transaction.
func (c *conn) Begin() (driver.Tx, error) { return c.BeginTx(context.Background(), driver.TxOptions{}) }

// BeginTx starts a transaction: the statements until its end are one unit
// of work.
func (c *conn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.closed {
		return nil, driver.ErrBadConn
	}
	if opts.Isolation != 0 || opts.ReadOnly {
		return nil, errors.New("db2: this client starts transactions at the database's own isolation, cursor stability, and not read-only")
	}
	c.inTx = true
	return tx{c}, nil
}

type tx struct{ c *conn }

func (t tx) Commit() error   { return t.end(cpRDBCMM) }
func (t tx) Rollback() error { return t.end(cpRDBRLLBCK) }

func (t tx) end(how int) error {
	c := t.c
	if c.closed {
		return driver.ErrBadConn
	}
	c.inTx = false
	var res result
	if err := c.exchange(&res, []part{command(how, nil)}); err != nil {
		return err
	}
	return c.failure(res.ca, "")
}
