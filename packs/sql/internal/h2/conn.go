// Package h2 is a client of H2's server, for database/sql.
//
// H2 is a Java database, and its server speaks a protocol of its own, which
// only its Java driver spoke. This package speaks it too, in Go, from H2's
// source (org.h2.value.Transfer, org.h2.engine.SessionRemote,
// org.h2.server.TcpServerThread): the versions of it that H2 1.4.197 to 2.5
// speak (17 to 21). It runs statements as they are written, with no
// parameters, which is all the sql pack sends, and gives each value as what
// it is: see readValue.
package h2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
)

// The versions of the protocol this client speaks: H2 1.4.197's to H2 2.5's.
const (
	minVersion = 17
	maxVersion = 21
)

// What a client asks of a server.
const (
	opSessionClose   = 1
	opExecuteQuery   = 2
	opExecuteUpdate  = 3
	opCommandClose   = 4
	opResultFetch    = 5
	opResultClose    = 7
	opSessionSetID   = 12
	opCancel         = 13
	opSetAutoCommit  = 15
	opLobRead        = 17
	opPrepare        = 18
	statusError      = 0
	statusOK         = 1
	statusClosed     = 2
	statusOKChanged  = 3
	defaultPort      = "9092"
	lobChunk         = 64 << 10
	everyRow         = 1<<31 - 1
	dialTimeout      = 30 * time.Second
	lockDialTimeout  = 3 * time.Second
	cancelTimeout    = 5 * time.Second
	codeBrokenServer = 90067
)

// Config is where an H2 database is and how to open a session of it.
type Config struct {
	// Addr is the server's host and port.
	Addr string
	// TLS is for a server started with -tcpSSL (jdbc:h2:ssl:).
	TLS bool
	// Database is the database's name as the server knows it: a path under
	// its base directory, or mem:name.
	Database string
	// LockFile is, for a database a service opened with AUTO_SERVER=TRUE, the
	// file H2 writes beside it, which says where it listens and with what
	// key. Addr and Database come from it each time a connection opens.
	LockFile string
	// URL is the database's URL as the feature gives it, without what a
	// connection must not show.
	URL      string
	User     string
	Password string
	// Settings are the URL's (MODE=PostgreSQL, SCHEMA=PARCELS), by their
	// names in upper case.
	Settings map[string]string
	// TimeZone is the session's zone, for H2 2: the one its CURRENT_TIMESTAMP
	// is in.
	TimeZone string
}

// Connector opens connections to one database.
type Connector struct {
	cfg Config
	// published says the last connection of a database found by its lock file
	// was to this machine's port and not to the address the file names.
	published atomic.Bool
}

// NewConnector is a connector for database/sql's OpenDB.
func NewConnector(cfg Config) *Connector { return &Connector{cfg: cfg} }

// Driver is the connector's driver, which opens nothing by a name.
func (c *Connector) Driver() driver.Driver { return drv{} }

type drv struct{}

func (drv) Open(string) (driver.Conn, error) {
	return nil, errors.New("h2: open a database with NewConnector")
}

// Connect opens a session.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	cfg := c.cfg
	if cfg.LockFile != "" {
		at, err := readLock(cfg.LockFile)
		if err != nil {
			return nil, err
		}
		cfg.Addr, cfg.Database = at.server, at.key
		host, port, splitErr := net.SplitHostPort(at.server)
		if splitErr != nil || net.ParseIP(host).IsLoopback() {
			return open(ctx, cfg)
		}
		// A service in a container writes the container's address, which not
		// every machine can reach; its port is then the one the machine
		// publishes. The key says it is the same database.
		named, published := cfg, cfg
		published.Addr = net.JoinHostPort("127.0.0.1", port)
		if c.published.Load() {
			if cn, err := open(ctx, published); err == nil {
				return cn, nil
			}
		}
		near, cancel := context.WithTimeout(ctx, lockDialTimeout)
		cn, err := open(near, named)
		cancel()
		if err == nil || ctx.Err() != nil || !isDialError(err) && near.Err() == nil {
			c.published.Store(false)
			return cn, err
		}
		if local, localErr := open(ctx, published); localErr == nil {
			c.published.Store(true)
			return local, nil
		}
		return nil, err
	}
	return open(ctx, cfg)
}

type dialError struct{ err error }

func (e *dialError) Error() string { return e.err.Error() }
func (e *dialError) Unwrap() error { return e.err }

func isDialError(err error) bool {
	var d *dialError
	return errors.As(err, &d)
}

func dial(ctx context.Context, cfg Config) (net.Conn, error) {
	addr := cfg.Addr
	if _, _, err := net.SplitHostPort(addr); err != nil {
		// No port: H2's own. An IPv6 address is in brackets either way.
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), defaultPort)
	}
	d := net.Dialer{Timeout: dialTimeout}
	var nc net.Conn
	var err error
	if cfg.TLS {
		// H2's server makes its own certificate when it starts, as its own
		// client expects: there is no authority to hold it to.
		nc, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{InsecureSkipVerify: true}}).DialContext(ctx, "tcp", addr) //nolint:gosec // see above
	} else {
		nc, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, &dialError{fmt.Errorf("h2: cannot reach the server at %s: %w", addr, err)}
	}
	return nc, nil
}

func open(ctx context.Context, cfg Config) (*conn, error) {
	nc, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c := &conn{cfg: cfg, nc: nc, wire: newWire(nc)}
	stop := c.watch(ctx)
	err = c.handshake()
	stop()
	if err != nil {
		_ = nc.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return c, nil
}

// conn is a session of a database.
type conn struct {
	cfg     Config
	nc      net.Conn
	wire    *wire
	version int32
	session string
	nextID  int32
	inTx    bool
	closed  bool
	// zeroBasedEnums is the database's setting of that name, once asked.
	zeroBasedEnums *bool

	mu      sync.Mutex
	running int32 // the statement that runs, for a cancel; 0 for none
}

func (c *conn) handshake() error {
	t := c.wire
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	c.session = hex.EncodeToString(id[:])
	t.writeInt(minVersion)
	t.writeInt(maxVersion)
	t.writeString(c.cfg.Database)
	t.writeString(c.cfg.URL)
	user := strings.ToUpper(c.cfg.User)
	t.writeString(user)
	t.writeBytes(passwordHash(user, c.cfg.Password))
	t.writeBytes(nil) // no password of an encrypted file
	t.writeInt(int32(len(c.cfg.Settings)))
	for _, k := range sortedKeys(c.cfg.Settings) {
		t.writeString(k)
		t.writeString(c.cfg.Settings[k])
	}
	if err := c.done(); err != nil {
		return err
	}
	c.version = t.readInt()
	if t.err == nil && (c.version < minVersion || c.version > maxVersion) {
		return fmt.Errorf("h2: the server speaks version %d of H2's protocol, and this client versions %d to %d (H2 1.4.197 to 2.5)", c.version, minVersion, maxVersion)
	}
	t.writeInt(opSessionSetID)
	t.writeString(c.session)
	if c.version >= 20 {
		t.writeString(c.cfg.TimeZone)
	}
	if err := c.done(); err != nil {
		return err
	}
	t.readBool() // auto-commit, which a new session has
	return t.err
}

// passwordHash is what H2 keeps of a password: the SHA-256 of the user's name,
// an @ and the password, in UTF-16.
func passwordHash(user, password string) []byte {
	if user == "" && password == "" {
		return []byte{}
	}
	var buf []byte
	for _, u := range utf16.Encode([]rune(user + "@" + password)) {
		buf = binary.BigEndian.AppendUint16(buf, u)
	}
	sum := sha256.Sum256(buf)
	return sum[:]
}

// Error is an error an H2 server reports.
type Error struct {
	SQLState string
	Message  string
	SQL      string
	Code     int
}

func (e *Error) Error() string { return "h2: " + e.Message }

// done sends what was written and reads the server's answer to it: nil when
// it did what was asked.
func (c *conn) done() error {
	t := c.wire
	if err := t.flush(); err != nil {
		return c.broken(err)
	}
	switch status := t.readInt(); {
	case t.err != nil:
		return c.broken(t.err)
	case status == statusError:
		return c.readError()
	case status == statusOK, status == statusOKChanged:
		return nil
	case status == statusClosed:
		c.closed = true
		return nil
	default:
		return c.broken(fmt.Errorf("a status of %d: this is not an H2 server's answer", status))
	}
}

func (c *conn) readError() error {
	t := c.wire
	e := &Error{SQLState: t.str(), Message: t.str(), SQL: t.str(), Code: int(t.readInt())}
	t.str() // the server's stack trace
	if t.err != nil {
		return c.broken(t.err)
	}
	if e.Code == codeBrokenServer {
		c.closed = true
	}
	return e
}

// broken marks the connection as of no more use.
func (c *conn) broken(err error) error {
	c.closed = true
	c.wire.fail(err)
	return fmt.Errorf("h2: the connection to %s broke: %w", c.cfg.Addr, err)
}

// watch ends what the connection does when ctx ends: it asks the server to
// cancel the statement that runs, over a connection of its own as H2's
// clients do, and stops waiting for an answer soon after. The function it
// returns ends the watch.
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
		c.mu.Lock()
		running := c.running
		c.mu.Unlock()
		if running != 0 && c.cancel(running) == nil {
			select {
			case <-finished:
				return
			case <-time.After(cancelTimeout):
			}
		}
		_ = c.nc.SetDeadline(time.Now())
	}()
	return func() {
		close(finished)
		<-gone
	}
}

// cancel asks the server to stop a statement of this session.
func (c *conn) cancel(statement int32) error {
	ctx, done := context.WithTimeout(context.Background(), cancelTimeout)
	defer done()
	nc, err := dial(ctx, c.cfg)
	if err != nil {
		return err
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(cancelTimeout))
	t := newWire(nc)
	t.writeInt(minVersion)
	t.writeInt(c.version)
	t.writeNullString()
	t.writeNullString()
	t.writeString(c.session)
	t.writeInt(opCancel)
	t.writeInt(statement)
	return t.flush()
}

// run prepares and runs a statement, as a query (its rows) or not (how many
// rows it changed), until ctx ends.
func (c *conn) run(ctx context.Context, sql string, query bool) (*rows, int64, error) {
	if c.closed {
		return nil, 0, driver.ErrBadConn
	}
	stop := c.watch(ctx)
	rs, n, err := c.exec(sql, query)
	stop()
	if err != nil && ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	return rs, n, err
}

// exec is run with nothing to end it.
func (c *conn) exec(sql string, query bool) (*rows, int64, error) {
	t := c.wire
	c.nextID++
	id := c.nextID
	c.setRunning(id)
	defer c.setRunning(0)

	t.writeInt(opPrepare)
	t.writeInt(id)
	t.writeString(sql)
	if err := c.done(); err != nil {
		return nil, 0, err
	}
	isQuery := t.readBool()
	t.readBool() // read-only
	t.readInt()  // the kind of statement
	for n := t.readInt(); n > 0 && t.err == nil; n-- {
		c.readColumnType() // a parameter's type
		t.readInt()
	}
	if t.err != nil {
		return nil, 0, c.broken(t.err)
	}
	defer func() {
		// Nothing answers this: it goes with what is sent next.
		t.writeInt(opCommandClose)
		t.writeInt(id)
	}()

	if !isQuery {
		n, err := c.update(id)
		if err != nil {
			return nil, 0, err
		}
		if query {
			// A statement that gives no rows, asked for its rows: it has run,
			// and has none.
			return &rows{}, n, nil
		}
		return nil, n, nil
	}
	c.nextID++
	result := c.nextID
	t.writeInt(opExecuteQuery)
	t.writeInt(id)
	t.writeInt(result)
	c.writeRowCount(0) // no limit
	t.writeInt(everyRow)
	t.writeInt(0) // no parameters
	if err := c.done(); err != nil {
		return nil, 0, err
	}
	rs, err := c.readResult(result)
	return rs, 0, err
}

func (c *conn) setRunning(id int32) {
	c.mu.Lock()
	c.running = id
	c.mu.Unlock()
}

func (c *conn) update(id int32) (int64, error) {
	t := c.wire
	t.writeInt(opExecuteUpdate)
	t.writeInt(id)
	t.writeInt(0) // no parameters
	t.writeInt(0) // no generated keys
	if err := c.done(); err != nil {
		return 0, err
	}
	n := c.readRowCount()
	t.readBool() // auto-commit
	if t.err != nil {
		return 0, c.broken(t.err)
	}
	return n, nil
}

func (c *conn) readRowCount() int64 {
	if c.version >= 20 {
		return c.wire.readLong()
	}
	return int64(c.wire.readInt())
}

func (c *conn) writeRowCount(n int64) {
	if c.version >= 20 {
		c.wire.writeLong(n)
	} else {
		c.wire.writeInt(int32(n))
	}
}

// Prepare is not what this client is for: it runs statements as written.
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

var errParameters = errors.New("h2: this client runs statements as they are written, with no parameters")

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

// QueryContext runs a query and reads all of its rows.
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

// Ping asks the server for nothing, to know it answers.
func (c *conn) Ping(ctx context.Context) error {
	_, _, err := c.run(ctx, "SELECT 1", true)
	return err
}

// IsValid says the connection can go back into the pool.
func (c *conn) IsValid() bool { return !c.closed }

// ResetSession is asked before a connection is used again.
func (c *conn) ResetSession(context.Context) error {
	if c.closed {
		return driver.ErrBadConn
	}
	return nil
}

func (c *conn) Begin() (driver.Tx, error) { return c.BeginTx(context.Background(), driver.TxOptions{}) }

// BeginTx starts a transaction: the session stops committing each statement.
func (c *conn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.closed {
		return nil, driver.ErrBadConn
	}
	if opts.ReadOnly || opts.Isolation != 0 {
		return nil, errors.New("h2: this client starts a transaction as the database has it set, and no other way")
	}
	if err := c.autoCommit(false); err != nil {
		return nil, err
	}
	c.inTx = true
	return tx{c}, nil
}

func (c *conn) autoCommit(on bool) error {
	c.wire.writeInt(opSetAutoCommit)
	c.wire.writeBool(on)
	return c.done()
}

type tx struct{ c *conn }

func (t tx) Commit() error   { return t.end("COMMIT") }
func (t tx) Rollback() error { return t.end("ROLLBACK") }

func (t tx) end(how string) error {
	c := t.c
	if !c.inTx {
		return nil
	}
	c.inTx = false
	_, _, err := c.run(context.Background(), how, false)
	if c.closed {
		return err
	}
	if acErr := c.autoCommit(true); err == nil {
		err = acErr
	}
	return err
}

// Close ends the session.
func (c *conn) Close() error {
	if !c.closed {
		c.closed = true
		_ = c.nc.SetDeadline(time.Now().Add(cancelTimeout))
		c.wire.writeInt(opSessionClose)
		if c.wire.flush() == nil {
			c.wire.readInt()
		}
	}
	return c.nc.Close()
}
