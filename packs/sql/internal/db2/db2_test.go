package db2

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/cipher"
	"crypto/des" //nolint:gosec // the standard's cipher, as the server's side of a test
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests hold the client to what a real Db2 said: testdata holds the
// bytes of sessions with Db2 12.1.5 (Community Edition, on Linux), each
// recorded by the test that replays it. Replayed, a stand-in server expects
// the client's bytes as they were and answers with Db2's.
//
// To record them again, against a Db2 with a database named parcels, another
// that is not Unicode's (db2 "create db latin1 using codeset ISO8859-1
// territory US"), and the user below:
//
//	AXX_DB2_RECORD=localhost:50000 go test ./packs/sql/internal/db2 -run 'TestSession|TestRefused|TestSingleByteDatabase'
//
// TestEncryptedLogin's are of a Db2 that takes no password as it is (db2
// update dbm cfg using AUTHENTICATION SERVER_ENCRYPT, and a restart), and are
// recorded against one, by -run TestEncryptedLogin.
const (
	testDatabase = "parcels"
	testUser     = "db2inst1"
	testPassword = "Parcels-Secret-1"
)

func init() {
	// The machine's name travels to the server, and a recording has one.
	hostname = func() (string, error) { return "axx-test", nil }
}

// chunk is what one side sent before the other answered.
type chunk struct {
	client bool
	data   []byte
}

// recorder keeps what travels over a connection.
type recorder struct {
	net.Conn
	mu     sync.Mutex
	chunks []chunk
}

func (r *recorder) add(client bool, b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.chunks); n > 0 && r.chunks[n-1].client == client {
		r.chunks[n-1].data = append(r.chunks[n-1].data, b...)
		return
	}
	r.chunks = append(r.chunks, chunk{client: client, data: append([]byte{}, b...)})
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.Conn.Write(b)
	r.add(true, b[:n])
	return n, err
}

func (r *recorder) Read(b []byte) (int, error) {
	n, err := r.Conn.Read(b)
	if n > 0 {
		r.add(false, b[:n])
	}
	return n, err
}

func recording(name string) string { return filepath.Join("testdata", name+".drda.gz") }

func save(t *testing.T, name string, chunks []chunk) {
	t.Helper()
	var raw bytes.Buffer
	z := gzip.NewWriter(&raw)
	for _, c := range chunks {
		head := []byte{'S', 0, 0, 0, 0}
		if c.client {
			head[0] = 'C'
		}
		binary.BigEndian.PutUint32(head[1:], uint32(len(c.data)))
		_, _ = z.Write(head)
		_, _ = z.Write(c.data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recording(name), raw.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T, name string) []chunk {
	t.Helper()
	f, err := os.Open(recording(name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []chunk
	for len(raw) > 0 {
		n := int(binary.BigEndian.Uint32(raw[1:5]))
		chunks = append(chunks, chunk{client: raw[0] == 'C', data: raw[5 : 5+n]})
		raw = raw[5+n:]
	}
	return chunks
}

// replay serves a recorded session to one connection: it expects what the
// client sent then, byte for byte, and answers as Db2 did.
func replay(t *testing.T, nc net.Conn, chunks []chunk) {
	defer nc.Close()
	r := bufio.NewReader(nc)
	for i, c := range chunks {
		if !c.client {
			if _, err := nc.Write(c.data); err != nil {
				return
			}
			continue
		}
		got := make([]byte, len(c.data))
		if _, err := io.ReadFull(r, got); err != nil {
			return // the client went away: its own test says why
		}
		if !bytes.Equal(got, c.data) {
			at := 0
			for at < len(got) && got[at] == c.data[at] {
				at++
			}
			t.Errorf("the client's request %d differs from the recorded one at byte %d of %d:\n  sent     %x\n  recorded %x",
				i/2+1, at, len(got), got[at:min(at+48, len(got))], c.data[at:min(at+48, len(got))])
			return
		}
	}
}

// session opens the database a test talks to: the real Db2 AXX_DB2_RECORD
// names, recording, or the stand-in that replays the recording of that name.
// done ends it, and saves a recording.
func session(t *testing.T, name string, cfg Config) (db *sql.DB, done func()) {
	t.Helper()
	if cfg.Database == "" {
		cfg.Database = testDatabase
	}
	if cfg.User == "" {
		cfg.User, cfg.Password = testUser, testPassword
	}
	if addr := os.Getenv("AXX_DB2_RECORD"); addr != "" {
		cfg.Addr = addr
		var rec *recorder
		c := NewConnector(cfg)
		c.wrap = func(nc net.Conn) net.Conn {
			if rec != nil {
				t.Fatal("a recording is of one connection")
			}
			rec = &recorder{Conn: nc}
			return rec
		}
		db = sql.OpenDB(c)
		db.SetMaxOpenConns(1)
		return db, func() {
			_ = db.Close()
			if rec != nil {
				save(t, name, rec.chunks)
			}
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		replay(t, nc, load(t, name))
	}()
	cfg.Addr = ln.Addr().String()
	db = sql.OpenDB(NewConnector(cfg))
	db.SetMaxOpenConns(1)
	return db, func() {
		_ = db.Close()
		_ = ln.Close()
		<-served
	}
}

func mustExec(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, q string,
) int64 {
	t.Helper()
	res, err := db.Exec(q)
	if err != nil {
		t.Fatalf("%s: %v", abbreviate(q), err)
	}
	n, _ := res.RowsAffected()
	return n
}

func abbreviate(q string) string {
	if len(q) > 120 {
		return q[:120] + "..."
	}
	return q
}

// table reads a query's rows, each value as the driver gives it.
func table(t *testing.T, db *sql.DB, q string) (types []string, rows [][]any) {
	t.Helper()
	rs, err := db.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", abbreviate(q), err)
	}
	defer rs.Close()
	cts, err := rs.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range cts {
		types = append(types, ct.Name()+" "+ct.DatabaseTypeName())
	}
	for rs.Next() {
		vals := make([]any, len(cts))
		ptrs := make([]any, len(cts))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, vals)
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	return types, rows
}

func wantError(t *testing.T, err error, code int, state, text string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code || e.SQLState != state || !strings.Contains(e.Message, text) {
		t.Errorf("got %v (%+v), want SQLCODE %d, SQLSTATE %s and a message with %q", err, e, code, state, text)
	}
}

// A session with Db2: every type's values, large values, errors with Db2's
// own messages, statements of either kind run either way, transactions, a
// result of several blocks and a statement longer than one structure.
func TestSession(t *testing.T) {
	db, done := session(t, "session", Config{})
	defer done()

	for _, tbl := range []string{"axx_types", "axx_large", "axx_rows"} {
		_, _ = db.Exec("DROP TABLE " + tbl)
	}
	mustExec(t, db, `CREATE TABLE axx_types (id INTEGER NOT NULL PRIMARY KEY, s SMALLINT, b BIGINT, r REAL, d DOUBLE, dec DECIMAL(31,6),
		df16 DECFLOAT(16), df34 DECFLOAT(34), bo BOOLEAN, dt DATE, tm TIME, ts TIMESTAMP, ts12 TIMESTAMP(12), ts0 TIMESTAMP(0),
		c CHAR(5), vc VARCHAR(100), g GRAPHIC(4), vg VARGRAPHIC(20), cb CHAR(4) FOR BIT DATA, vcb VARCHAR(8) FOR BIT DATA,
		bi BINARY(4), vb VARBINARY(8))`)
	if n := mustExec(t, db, `INSERT INTO axx_types VALUES (1, -5, 9007199254740993, 1.5, -2.25E10, 1234567890123456789012345.123456, 1.20,
		DECFLOAT('-1.234567890123456789012345678901234E-20'), TRUE, '2026-11-19', '13:28:03', '2026-11-19-13.28.03.326001',
		'2026-11-19-13.28.03.123456789012', '2026-11-19-13.28.03', 'ab', 'Zoë ✓ 日本', 'ab', 'héllo 日本', BX'00FF10', BX'DEADBEEF', BX'0102', BX'CAFE'),
		(2, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
		(3, 0, 0, 0, 1E300, -0.000001, DECFLOAT('Infinity'), DECFLOAT('NaN'), FALSE, '0001-01-01', '00:00:00', '9999-12-31-23.59.59.999999',
		NULL, NULL, '', '', '', '', BX'', BX'', BX'', BX''),
		(4, 32767, -9223372036854775808, -0.1, 0, 0, 1E+20, 0.00, NULL, NULL, '24:00:00', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`); n != 4 {
		t.Errorf("the insert changed %d rows, want 4", n)
	}
	types, rows := table(t, db, "SELECT * FROM axx_types ORDER BY id")
	wantTypes := []string{
		"ID INTEGER", "S SMALLINT", "B BIGINT", "R REAL", "D DOUBLE", "DEC DECIMAL", "DF16 DECFLOAT", "DF34 DECFLOAT", "BO SMALLINT",
		"DT DATE", "TM TIME", "TS TIMESTAMP", "TS12 TIMESTAMP", "TS0 TIMESTAMP", "C CHAR", "VC VARCHAR", "G GRAPHIC", "VG VARGRAPHIC",
		"CB CHAR FOR BIT DATA", "VCB VARCHAR FOR BIT DATA", "BI BINARY", "VB VARBINARY",
	}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Errorf("columns\n got %q\nwant %q", types, wantTypes)
	}
	at := func(s string) time.Time {
		v, err := time.Parse("2006-01-02 15:04:05.999999999", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	wantRows := [][]any{
		{
			int64(1), int64(-5), int64(9007199254740993), float64(1.5), float64(-2.25e10), "1234567890123456789012345.123456", "1.20",
			"-1.234567890123456789012345678901234E-20", int64(1), at("2026-11-19 00:00:00"), "13:28:03", at("2026-11-19 13:28:03.326001"),
			at("2026-11-19 13:28:03.123456789"), at("2026-11-19 13:28:03"), "ab   ", "Zoë ✓ 日本", "ab  ", "héllo 日本",
			[]byte{0, 0xFF, 0x10, 0x20},
			[]byte{0xDE, 0xAD, 0xBE, 0xEF},
			[]byte{1, 2, 0, 0},
			[]byte{0xCA, 0xFE},
		},
		{int64(2), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
		{
			int64(3), int64(0), int64(0), float64(0), float64(1e300), "-0.000001", "Infinity", "NaN", int64(0), at("0001-01-01 00:00:00"), "00:00:00",
			at("9999-12-31 23:59:59.999999"), nil, nil, "     ", "", "    ", "",
			[]byte{0x20, 0x20, 0x20, 0x20},
			[]byte{},
			[]byte{0, 0, 0, 0},
			[]byte{},
		},
		{
			int64(4), int64(32767), int64(-9223372036854775808), float64(float32(-0.1)), float64(0), "0.000000", "1E+20", "0.00", nil, nil, "24:00:00",
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		},
	}
	if len(rows) != len(wantRows) {
		t.Fatalf("%d rows, want %d", len(rows), len(wantRows))
	}
	for i, row := range rows {
		for j, v := range row {
			if !reflect.DeepEqual(v, wantRows[i][j]) {
				t.Errorf("row %d, %s: %T(%v), want %T(%v)", i+1, types[j], v, v, wantRows[i][j], wantRows[i][j])
			}
		}
	}

	// Large values travel outside their rows: one of no length does not
	// travel at all, and an XML document's length is not known.
	mustExec(t, db, `CREATE TABLE axx_large (id INTEGER NOT NULL PRIMARY KEY, c CLOB(1M), b BLOB(1M), x XML, d DBCLOB(1K), c2 CLOB(100) NOT NULL DEFAULT '')`)
	mustExec(t, db, `INSERT INTO axx_large VALUES (1, 'héllo clob', BLOB(X'00FF7F'), '<a><b>1</b></a>', 'dbclob 日本', 'two'),
		(2, NULL, NULL, NULL, NULL, ''), (3, '', BLOB(X''), '<e/>', '', 'x')`)
	types, rows = table(t, db, "SELECT * FROM axx_large ORDER BY id")
	if got, want := fmt.Sprint(types), "[ID INTEGER C CLOB B BLOB X XML D DBCLOB C2 CLOB]"; got != want {
		t.Errorf("columns %s, want %s", got, want)
	}
	wantRows = [][]any{
		{int64(1), "héllo clob", []byte{0, 0xFF, 0x7F}, "<a><b>1</b></a>", "dbclob 日本", "two"},
		{int64(2), nil, nil, nil, nil, ""},
		{int64(3), "", []byte{}, "<e/>", "", "x"},
	}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("large values\n got %q\nwant %q", rows, wantRows)
	}
	big := strings.Repeat("x", 30000) + strings.Repeat("ÿ", 15000) + strings.Repeat("z", 30000)
	mustExec(t, db, "UPDATE axx_large SET c = CAST(REPEAT('x', 30000) AS CLOB(1M)) || REPEAT('ÿ', 15000) || REPEAT('z', 30000) WHERE id = 2")
	var got string
	if err := db.QueryRow("SELECT c FROM axx_large WHERE id = 2").Scan(&got); err != nil || got != big {
		t.Errorf("a large value of %d bytes arrived as %d: %v", len(big), len(got), err)
	}

	// Db2's errors, with the messages it keeps.
	_, err := db.Query("SELECT * FROM axx_nosuch")
	wantError(t, err, -204, "42704", `"DB2INST1.AXX_NOSUCH" is an undefined name`)
	_, err = db.Exec("INSERT INTO axx_types (id) VALUES (1)")
	wantError(t, err, -803, "23505", "DB2INST1.AXX_TYPES")
	_, err = db.Exec("UPDATE axx_types SET nosuch = 1")
	wantError(t, err, -206, "42703", `"NOSUCH" is not valid in the context where it is used`)
	_, err = db.Exec("SELEC 1")
	wantError(t, err, -104, "42601", "An unexpected token")
	if _, err := db.Exec("SELECT 1 FROM sysibm.sysdummy1", 1); !errors.Is(err, errParameters) {
		t.Errorf("a parameter: %v", err)
	}

	// How many rows a statement changed, and a statement of either kind run
	// either way.
	if n := mustExec(t, db, "UPDATE axx_types SET s = 7 WHERE id IN (1, 2)"); n != 2 {
		t.Errorf("the update changed %d rows, want 2", n)
	}
	if n := mustExec(t, db, "DELETE FROM axx_types WHERE id = 99"); n != 0 {
		t.Errorf("a delete of no row changed %d", n)
	}
	if n := mustExec(t, db, "SELECT id FROM axx_types"); n != 0 {
		t.Errorf("a query run for no rows changed %d", n)
	}
	if _, rows := table(t, db, "UPDATE axx_types SET s = 8 WHERE id = 1"); len(rows) != 0 {
		t.Errorf("an update run as a query gave %d rows", len(rows))
	}
	var s int
	if err := db.QueryRow("SELECT s FROM axx_types WHERE id = 1").Scan(&s); err != nil || s != 8 {
		t.Errorf("after an update run as a query: %d, %v", s, err)
	}
	if _, rows := table(t, db, "SELECT id FROM axx_types WHERE 1 = 0"); len(rows) != 0 {
		t.Errorf("a query of no rows gave %d", len(rows))
	}

	// A transaction is one unit of work, and each statement outside one its own.
	count := func(q interface {
		QueryRow(string, ...any) *sql.Row
	},
	) (n int) {
		t.Helper()
		if err := q.QueryRow("SELECT COUNT(*) FROM axx_types").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, tx, "INSERT INTO axx_types (id) VALUES (10)")
	if n := count(tx); n != 5 {
		t.Errorf("%d rows in the transaction, want 5", n)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := count(db); n != 4 {
		t.Errorf("%d rows after the rollback, want 4", n)
	}
	if tx, err = db.Begin(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, tx, "INSERT INTO axx_types (id) VALUES (10)")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(db); n != 5 {
		t.Errorf("%d rows after the commit, want 5", n)
	}
	if _, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable}); err == nil {
		t.Error("an isolation level was taken")
	}

	// A result of more rows than a block holds, and a statement longer than
	// one structure.
	mustExec(t, db, "CREATE TABLE axx_rows (id INTEGER NOT NULL PRIMARY KEY, txt VARCHAR(200))")
	if n := mustExec(t, db, `INSERT INTO axx_rows SELECT n, REPEAT('row ', 40) || n FROM
		(SELECT ROW_NUMBER() OVER () AS n FROM syscat.columns FETCH FIRST 900 ROWS ONLY)`); n != 900 {
		t.Fatalf("%d rows inserted, want 900", n)
	}
	_, rows = table(t, db, "SELECT id, txt FROM axx_rows ORDER BY id /* "+strings.Repeat("a long statement ", 4000)+" */")
	if len(rows) != 900 {
		t.Fatalf("%d rows, want 900", len(rows))
	}
	for i, row := range rows {
		if row[0] != int64(i+1) || row[1] != strings.Repeat("row ", 40)+fmt.Sprint(i+1) {
			t.Fatalf("row %d is %v", i+1, row)
		}
	}
	// A statement kept and run later, and a ping.
	kept, err := db.Prepare("SELECT COUNT(*) FROM axx_rows WHERE id <= 10")
	if err != nil {
		t.Fatal(err)
	}
	var ten int
	if err := kept.QueryRow().Scan(&ten); err != nil || ten != 10 {
		t.Errorf("a kept query: %d, %v", ten, err)
	}
	if _, err := kept.Exec(1); !errors.Is(err, errParameters) && (err == nil || !strings.Contains(err.Error(), "expected 0 arguments")) {
		t.Errorf("a kept statement's parameter: %v", err)
	}
	_ = kept.Close()
	deleting, err := db.Prepare("DELETE FROM axx_rows WHERE id > 890")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := deleting.Exec(); err != nil {
		t.Errorf("a kept delete: %v", err)
	} else if n, _ := res.RowsAffected(); n != 10 {
		t.Errorf("a kept delete changed %d rows, want 10", n)
	}
	_ = deleting.Close()
	if err := db.Ping(); err != nil {
		t.Errorf("a ping: %v", err)
	}
	for _, tbl := range []string{"axx_types", "axx_large", "axx_rows"} {
		mustExec(t, db, "DROP TABLE "+tbl)
	}
}

// A user Db2 refuses and a database it does not have, each said plainly.
func TestRefused(t *testing.T) {
	db, done := session(t, "wrong-password", Config{User: testUser, Password: "not-the-password"})
	err := db.Ping()
	done()
	wantError(t, err, -30082, "28000", "the server refused the user db2inst1: the password is not the user's")

	db, done = session(t, "no-user", Config{User: "nobody", Password: testPassword})
	err = db.Ping()
	done()
	wantError(t, err, -30082, "28000", "the server refused the user nobody")
}

// A database in a single-byte character set gives its text in that set, and
// takes statements in Unicode: what it has no character for is not kept.
func TestSingleByteDatabase(t *testing.T) {
	db, done := session(t, "latin1", Config{Database: "latin1"})
	defer done()
	_, _ = db.Exec("DROP TABLE axx_latin")
	mustExec(t, db, "CREATE TABLE axx_latin (id INT NOT NULL, c CHAR(6), v VARCHAR(40), l CLOB(1K), b VARCHAR(4) FOR BIT DATA)")
	mustExec(t, db, "INSERT INTO axx_latin VALUES (1, 'Zoë', 'Zoë ñ ß 日', 'clöb', BX'00FFE9')")
	types, rows := table(t, db, "SELECT c, v, l, b, HEX(v) FROM axx_latin WHERE v LIKE 'Zoë ñ%' AND c = 'Zoë'")
	if got, want := fmt.Sprint(types), "[C CHAR V VARCHAR L CLOB B VARCHAR FOR BIT DATA 5 VARCHAR]"; got != want {
		t.Errorf("columns %s, want %s", got, want)
	}
	want := [][]any{{"Zoë   ", "Zoë ñ ß \x1a", "clöb", []byte{0, 0xFF, 0xE9}, "5A6FEB20F120DF201A"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("text of ISO 8859-1\n got %q\nwant %q", rows, want)
	}
	_, err := db.Query("SELECT * FROM axx_nosüch")
	wantError(t, err, -204, "42704", `"DB2INST1.AXX_NOSÜCH" is an undefined name`)
	mustExec(t, db, "DROP TABLE axx_latin")
}

// standIn answers one connection with what a test writes, after reading the
// client's attributes.
func standIn(t *testing.T, serve func(nc net.Conn, r *bufio.Reader)) (addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer nc.Close()
				serve(nc, bufio.NewReader(nc))
			}()
		}
	}()
	return ln.Addr().String()
}

// answer is a reply message, or a reply's object, as a server frames it.
func answer(kind byte, id uint16, chained bool, cp int, data []byte) []byte {
	if chained {
		kind |= dssChained
	}
	return appendDSS(nil, kind, id, object(cp, data))
}

// attributesReply is a server's EXCSATRD: the managers at the levels it
// speaks, with names in UTF-8.
func attributesReply() []byte {
	var levels, p buf
	for _, m := range [][2]int{{mgrAGENT, 10}, {mgrSQLAM, 10}, {mgrCMNTCPIP, 5}, {mgrRDB, 8}, {mgrSECMGR, 9}, {mgrCCSIDMGR, ccsidUTF8}} {
		levels.u16(m[0])
		levels.u16(m[1])
	}
	p.param(cpMGRLVLLS, levels.b)
	return answer(dssReply, 1, false, cpEXCSATRD, p.b)
}

func readRequest(t *testing.T, r *bufio.Reader) []item {
	items, _, err := readDSS(r)
	if err != nil {
		return nil
	}
	return items
}

// logIn answers a client's ACCSEC, which the stand-in has read, and the
// SECCHK and ACCRDB that follow, as a server that takes the user: its numbers
// little-endian, its text in UTF-8, and a token to interrupt requests by.
func logIn(t *testing.T, nc net.Conn, r *bufio.Reader, accsec []item) (seen []item) {
	var sec buf
	sec.param16(cpSECMEC, secUserPassword)
	_, _ = nc.Write(answer(dssReply, 1, false, cpACCSECRD, sec.b))
	seen = append(seen, accsec...)
	seen = append(seen, readRequest(t, r)...)
	seen = append(seen, readRequest(t, r)...)
	var chk, acc, ovr buf
	chk.param16(cpSVRCOD, 0)
	chk.param8(cpSECCHKCD, secOK)
	acc.param(cpPRDID, []byte("SQL12015"))
	acc.param(cpTYPDEFNAM, []byte("QTDSQLX86"))
	ovr.param16(cpCCSIDSBC, ccsidUTF8)
	ovr.param16(cpCCSIDDBC, ccsidUTF16)
	ovr.param16(cpCCSIDMBC, ccsidUTF8)
	acc.param(cpTYPDEFOVR, ovr.b)
	acc.param(cpRDBINTTKN, []byte("token-1"))
	_, _ = nc.Write(append(answer(dssReply, 1, true, cpSECCHKRM, chk.b), answer(dssReply, 2, false, cpACCRDBRM, acc.b)...))
	return seen
}

// fixedRandom makes a connection's key the same each time, so that a session
// with an encrypted password replays.
func fixedRandom(t *testing.T) {
	t.Helper()
	was := random
	random = bytes.NewReader(bytes.Repeat([]byte{0x5A, 0x17, 0xC3, 0x09}, 64))
	t.Cleanup(func() { random = was })
}

// A Db2 that takes no password as it is (AUTHENTICATION SERVER_ENCRYPT) is
// logged in to with the user and the password encrypted, or the password
// alone when that is what the server takes. These sessions are of such a
// Db2: record them against one.
func TestEncryptedLogin(t *testing.T) {
	both := encrypted
	t.Cleanup(func() { encrypted = both })
	for name, order := range map[string][]int{"encrypted": both, "encrypted-password": {secEncryptedPassword}} {
		encrypted = order
		fixedRandom(t)
		db, done := session(t, name, Config{})
		var user string
		err := db.QueryRow("VALUES CURRENT USER").Scan(&user)
		done()
		if err != nil || strings.TrimSpace(user) != "DB2INST1" {
			t.Errorf("%s: logged in as %q, %v", name, user, err)
		}
	}
	encrypted = both
	fixedRandom(t)
	db, done := session(t, "encrypted-wrong-password", Config{User: testUser, Password: "not-the-password"})
	err := db.Ping()
	done()
	wantError(t, err, -30082, "28000", "the password is not the user's")
}

// decrypt is what a server does with what the client encrypted.
func decrypt(t *testing.T, key, token, secret []byte) string {
	t.Helper()
	block, err := des.NewCipher(key)
	if err != nil || len(secret) == 0 || len(secret)%des.BlockSize != 0 {
		t.Errorf("a secret of %d bytes: %v", len(secret), err)
		return ""
	}
	plain := make([]byte, len(secret))
	cipher.NewCBCDecrypter(block, token).CryptBlocks(plain, secret)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > des.BlockSize {
		t.Errorf("padding of %d", pad)
		return ""
	}
	return fromEBCDIC(plain[:len(plain)-pad])
}

// encryptingServer is a stand-in that takes a user by the mechanisms it is
// given, and says what it read of the user and the password.
func encryptingServer(t *testing.T, takes []int, got chan<- [3]string) string {
	return standIn(t, func(nc net.Conn, r *bufio.Reader) {
		readRequest(t, r) // EXCSAT
		_, _ = nc.Write(attributesReply())
		var offer buf
		for _, m := range takes {
			offer.u16(m)
		}
		var mine *agreement
		var clientKey []byte
		mechanism := 0
		for {
			items := readRequest(t, r)
			if len(items) == 0 {
				return // the client went away
			}
			if items[0].cp != cpACCSEC {
				// SECCHK, and the ACCRDB after it.
				ps, _ := params(items[0].data)
				readRequest(t, r)
				key, err := mine.key(clientKey)
				if err != nil {
					t.Error(err)
					return
				}
				var tokens [][]byte
				for data := items[0].data; len(data) > 0; {
					cp, v, rest, _ := next(data)
					if cp == cpSECTKN {
						tokens = append(tokens, v)
					}
					data = rest
				}
				user, userText := fromEBCDIC(nil), ""
				switch mechanism {
				case secEncryptedUserPassword:
					from := mine.public()[12:20]
					user = decrypt(t, key, from, tokens[0])
					userText = decrypt(t, key, from, tokens[1])
				case secEncryptedPassword:
					user = string(ps[cpUSRID])
					e, _ := toEBCDIC(user)
					userText = decrypt(t, key, token(secEncryptedPassword, e, nil), tokens[0])
				}
				got <- [3]string{fmt.Sprint(mechanism), user, userText}
				return
			}
			ps, _ := params(items[0].data)
			mechanism = int(binary.BigEndian.Uint16(ps[cpSECMEC]))
			var reply buf
			if !offers(offer.b, mechanism) {
				reply.param(cpSECMEC, offer.b)
			} else {
				mine, _ = newAgreement()
				clientKey = ps[cpSECTKN]
				reply.param16(cpSECMEC, mechanism)
				reply.param(cpSECTKN, mine.public())
			}
			_, _ = nc.Write(answer(dssReply, 1, false, cpACCSECRD, reply.b))
		}
	})
}

// The client's side of the encryption, held to a server's side of it: the
// key the two agree on, the cipher, and what each mechanism encrypts.
func TestEncryption(t *testing.T) {
	// The key agreement, from numbers worked out apart from this code.
	hex := func(s string) []byte {
		var b []byte
		_, _ = fmt.Sscanf(s, "%X", &b)
		return b
	}
	a := &agreement{private: new(big.Int).SetBytes(hex("0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20"))}
	if got := fmt.Sprintf("%X", a.public()); got != "1E193463D8FF28DE84D42805ED9F150120DE19CB2B7296AB013969C9205B12F3" {
		t.Errorf("this side's public key is %s", got)
	}
	key, err := a.key(hex("50D265D163338FD437F0DEDA5CE186AC29709F88575EE5390F234B24365F8FE9"))
	if err != nil || fmt.Sprintf("%X", key) != "70AFA9E26CEC9C1D" {
		t.Errorf("the shared key is %X, %v", key, err)
	}
	for _, bad := range [][]byte{nil, make([]byte, 32), make([]byte, 31), bytes.Repeat([]byte{0xFF}, 32)} {
		if _, err := a.key(bad); !errors.Is(err, errProtocol) {
			t.Errorf("a key of %x: %v", bad, err)
		}
	}
	// DES in CBC mode, as its standard's example has it (FIPS 81), and padded.
	secret, err := encrypt(hex("0123456789ABCDEF"), hex("1234567890ABCDEF"), []byte("Now is the time for all "))
	if err != nil || fmt.Sprintf("%X", secret[:24]) != "E5C7CDDE872BF27C43E934008C389C0F683788499A7C05F6" || len(secret) != 32 {
		t.Errorf("the cipher gives %X, %v", secret, err)
	}
	if got := token(secEncryptedPassword, []byte("ab"), nil); !bytes.Equal(got, []byte{'a', 'b', 0, 0, 0, 0, 0, 0}) {
		t.Errorf("a short user's token is %x", got)
	}
	if got := token(secEncryptedPassword, []byte("a-long-user"), nil); string(got) != "a-long-u" {
		t.Errorf("a long user's token is %q", got)
	}

	// Each mechanism, against a server that decrypts what it is sent.
	for _, c := range []struct {
		takes []int
		want  [3]string
	}{
		{[]int{7, 9, 10}, [3]string{"9", "clerk_1", "S3cret! pass"}},
		{[]int{9}, [3]string{"9", "clerk_1", "S3cret! pass"}},
		{[]int{7}, [3]string{"7", "clerk_1", "S3cret! pass"}},
	} {
		got := make(chan [3]string, 1)
		addr := encryptingServer(t, c.takes, got)
		_, err := NewConnector(Config{Addr: addr, Database: testDatabase, User: "clerk_1", Password: "S3cret! pass"}).Connect(context.Background())
		if err == nil {
			t.Errorf("%v: the stand-in opens no database, and the client connected", c.takes)
		}
		select {
		case read := <-got:
			if read != c.want {
				t.Errorf("a server that takes %v read %q, want %q", c.takes, read, c.want)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("a server that takes %v was sent no user", c.takes)
		}
	}

	// A URL that names a mechanism gets that one, or is told the server
	// does not take it.
	got := make(chan [3]string, 1)
	addr := encryptingServer(t, []int{3, 7, 9}, got)
	_, _ = NewConnector(Config{Addr: addr, Database: testDatabase, User: "clerk_1", Password: "S3cret! pass", Mechanism: 7}).Connect(context.Background())
	select {
	case read := <-got:
		if read != [3]string{"7", "clerk_1", "S3cret! pass"} {
			t.Errorf("a URL that asks for mechanism 7 was read as %q", read)
		}
	case <-time.After(5 * time.Second):
		t.Error("a URL that asks for mechanism 7 sent no user")
	}
	addr = encryptingServer(t, []int{7}, make(chan [3]string, 1))
	_, err = NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: testPassword, Mechanism: 9}).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not take security mechanism 9 (it offers 7)") {
		t.Errorf("a server that takes 7, asked for 9: %v", err)
	}
	addr = encryptingServer(t, []int{7, 9}, make(chan [3]string, 1))
	_, err = NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: testPassword, Mechanism: 3}).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "securityMechanism=3 asks for (it offers the security mechanisms 7, 9)") {
		t.Errorf("a server of encrypted passwords, asked for them as they are: %v", err)
	}

	// A server that takes the mechanism with another cipher gives no key for
	// this one's (a Db2 set to AES only).
	addr = standIn(t, func(nc net.Conn, r *bufio.Reader) {
		readRequest(t, r)
		_, _ = nc.Write(attributesReply())
		for {
			items := readRequest(t, r)
			if len(items) == 0 {
				return
			}
			ps, _ := params(items[0].data)
			var reply buf
			if int(binary.BigEndian.Uint16(ps[cpSECMEC])) == secUserPassword {
				reply.param(cpSECMEC, []byte{0, 7, 0, 9, 0, 10})
			} else {
				reply.param(cpSECMEC, ps[cpSECMEC])
			}
			_, _ = nc.Write(answer(dssReply, 1, false, cpACCSECRD, reply.b))
		}
	})
	_, err = NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: testPassword}).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AES_ONLY") || !strings.Contains(err.Error(), "TLS") {
		t.Errorf("a server of another cipher: %v", err)
	}

	// A server that takes a user in no way this client speaks, and a
	// password the cipher's character set has no place for.
	addr = encryptingServer(t, []int{11, 15}, make(chan [3]string, 1))
	_, err = NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: testPassword}).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "it offers the security mechanisms 11, 15") {
		t.Errorf("a server of Kerberos and plug-ins only: %v", err)
	}
	addr = encryptingServer(t, []int{9}, make(chan [3]string, 1))
	_, err = NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: "pässword"}).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "encrypted, in EBCDIC") || !strings.Contains(err.Error(), "TLS") {
		t.Errorf("a password the cipher has no place for: %v", err)
	}
}

// A statement that runs past its time is interrupted over a connection of
// its own, with the token the server gave, and the connection it ran on
// lives on.
func TestInterrupt(t *testing.T) {
	interrupted := make(chan []byte, 1)
	release := make(chan struct{})
	addr := standIn(t, func(nc net.Conn, r *bufio.Reader) {
		readRequest(t, r) // EXCSAT
		_, _ = nc.Write(attributesReply())
		items := readRequest(t, r)
		if len(items) == 1 && items[0].cp == cpINTRDBRQS {
			ps, _ := params(items[0].data)
			interrupted <- ps[cpRDBINTTKN]
			close(release)
			// As Db2 answers: the command is complete.
			var done buf
			done.param16(cpSVRCOD, 0)
			_, _ = nc.Write(answer(dssReply, 1, false, cpCMDCMPRM, done.b))
			return
		}
		logIn(t, nc, r, items)
		// The statement, and its commit: answered when the interrupt comes.
		readRequest(t, r)
		readRequest(t, r)
		readRequest(t, r)
		<-release
		// SQLCODE -952, SQLSTATE 57014: the statement was interrupted.
		ca := append([]byte{0}, binary.LittleEndian.AppendUint32(nil, uint32(0xFFFFFFFF-951))...)
		ca = append(ca, "57014SQLRI01F"...)
		ca = append(ca, null, null)
		_, _ = nc.Write(append(answer(dssObject, 1, true, cpSQLCARD, ca), answer(dssObject, 2, false, cpSQLCARD, []byte{null})...))
		// The next statement: one row of one integer.
		readRequest(t, r)
		readRequest(t, r)
		readRequest(t, r)
		var opened buf
		opened.param16(cpSVRCOD, 0)
		opened.param(cpQRYINSID, make([]byte, 8))
		var out []byte
		out = append(out, answer(dssObject, 1, true, cpSQLCARD, []byte{null})...)
		out = append(out, answer(dssReply, 2, true, cpOPNQRYRM, opened.b)...)
		out = append(out, answer(dssObject, 2, true, cpQRYDSC, []byte{0x06, 0x76, 0xD0, 0x02, 0x00, 0x04})...)
		out = append(out, answer(dssObject, 2, true, cpQRYDTA, []byte{null, 0, 7, 0, 0, 0})...)
		out = append(out, answer(dssReply, 2, false, cpENDQRYRM, nil)...)
		_, _ = nc.Write(out)
		readRequest(t, r) // the commit
		_, _ = nc.Write(answer(dssObject, 1, false, cpSQLCARD, []byte{null}))
	})
	db := sql.OpenDB(NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: testPassword}))
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, "CALL a_long_one()")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a statement past its time: %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("it ended after %s", took)
	}
	select {
	case token := <-interrupted:
		if string(token) != "token-1" {
			t.Errorf("interrupted by the token %q", token)
		}
	default:
		t.Error("the server was not asked to interrupt")
	}
	var n int
	if err := db.QueryRow("VALUES 7").Scan(&n); err != nil || n != 7 {
		t.Errorf("the statement after it: %d, %v", n, err)
	}
	if open := db.Stats().OpenConnections; open != 1 {
		t.Errorf("%d connections open, want the one the statement ran on", open)
	}
}

// A port that speaks TLS is one this client reaches when the URL says so and
// the server's certificate is one it is told to trust, or the machine does.
func TestTLS(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "parcels-db"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"parcels-db"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go replay(t, nc, load(t, "wrong-password"))
		}
	}()
	cfg := Config{Addr: ln.Addr().String(), Database: testDatabase, User: testUser, Password: "not-the-password", TLS: true, RootCAs: roots}
	// Over TLS the session is the recorded one: the server's refusal of the password.
	wantError(t, sql.OpenDB(NewConnector(cfg)).Ping(), -30082, "28000", "the password is not the user's")
	cfg.RootCAs = nil
	var unknown x509.UnknownAuthorityError
	if err := sql.OpenDB(NewConnector(cfg)).Ping(); !errors.As(err, &unknown) || !strings.Contains(err.Error(), "sslCertLocation=") {
		t.Errorf("a certificate no one vouches for: %v", err)
	}
}

func TestParseURL(t *testing.T) {
	pem := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(pem, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		url, user, password string
		want                Config
		err                 string
	}{
		{
			url: "jdbc:db2://db.example:50000/PARCELS", user: "app", password: "secret",
			want: Config{Addr: "db.example:50000", Database: "PARCELS", User: "app", Password: "secret"},
		},
		{url: "jdbc:db2://db.example/parcels", want: Config{Addr: "db.example:50000", Database: "parcels"}},
		{
			url: "db2://[::1]:50001/parcels:currentSchema=DEPOT;sslConnection=true;", user: "app",
			want: Config{Addr: "[::1]:50001", Database: "parcels", User: "app", Schema: "DEPOT", TLS: true},
		},
		{
			url: "JDBC:DB2://localhost:50000/parcels:user=app;password=secret;retrieveMessagesFromServerOnGetMessage=true;",
			err: "is not a URL of a Db2 server",
		},
		{
			url:  "jdbc:db2://localhost:50000/parcels:user=app;password=secret;retrieveMessagesFromServerOnGetMessage=true;",
			want: Config{Addr: "localhost:50000", Database: "parcels", User: "app", Password: "secret"},
		},
		{
			url: "jdbc:db2://localhost:50000/parcels:user=app;password=secret;", user: "other", password: "another",
			want: Config{Addr: "localhost:50000", Database: "parcels", User: "other", Password: "another"},
		},
		{url: "jdbc:db2://localhost:50000/parcels:securityMechanism=3;", want: Config{Addr: "localhost:50000", Database: "parcels", Mechanism: 3}},
		{url: "jdbc:db2://localhost:50000/parcels:securityMechanism=9;", want: Config{Addr: "localhost:50000", Database: "parcels", Mechanism: 9}},
		{url: "jdbc:db2://localhost:50000/parcels:securityMechanism=7;", want: Config{Addr: "localhost:50000", Database: "parcels", Mechanism: 7}},
		{url: "jdbc:db2://localhost:50000/parcels:securityMechanism=11;", err: "securityMechanism=11 is not one this client speaks"},
		{url: "jdbc:db2://localhost:50000/parcels:sslConnection=true;sslCertLocation=" + pem + ";", err: "holds no certificate in PEM"},
		{url: "jdbc:db2://localhost:50000/parcels:sslCertLocation=/no/such/file;", err: "the certificates of sslCertLocation"},
		{url: "jdbc:db2://localhost:50000", err: "names no database"},
		{url: "jdbc:db2://localhost:50000/", err: "names no database"},
		{url: "jdbc:db2:parcels", err: "is not a URL of a Db2 server"},
	} {
		got, err := ParseURL(c.url, c.user, c.password)
		switch {
		case c.err != "":
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: %v, want %s", c.url, err, c.err)
			}
		case err != nil:
			t.Errorf("%s: %v", c.url, err)
		case !reflect.DeepEqual(got, c.want):
			t.Errorf("%s:\n got %+v\nwant %+v", c.url, got, c.want)
		}
	}
}

// What travels: structures of any length in their segments, objects of any
// length, and names in EBCDIC until the two sides agree on Unicode.
func TestWire(t *testing.T) {
	for _, size := range []int{0, 1, dssMax - 10, dssMax - 9, dssMax, 2*dssMax - 8, 200000} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i * 7)
		}
		sent := chain([]part{command(cpEXCSQLIMM, &buf{b: []byte{0, 5, 0x21, 0x05, 0xF1}}), objects(cpSQLSTT, data)}, []part{command(cpRDBCMM, nil)})
		r := bufio.NewReader(bytes.NewReader(sent))
		var got []item
		var flags []bool
		for {
			items, chained, err := readDSS(r)
			if err != nil {
				t.Fatalf("%d bytes: %v", size, err)
			}
			got = append(got, items...)
			flags = append(flags, chained)
			if !chained {
				break
			}
		}
		if len(got) != 3 || got[0].cp != cpEXCSQLIMM || got[1].cp != cpSQLSTT || got[2].cp != cpRDBCMM || !bytes.Equal(got[1].data, data) {
			t.Errorf("%d bytes: read back %d objects", size, len(got))
		}
		if fmt.Sprint(flags) != "[true true false]" {
			t.Errorf("%d bytes: chained %v", size, flags)
		}
		if _, err := r.ReadByte(); err != io.EOF {
			t.Errorf("%d bytes: bytes left over", size)
		}
	}

	// An object with no length runs to the end of its structure.
	open := appendDSS(nil, dssObject, 1, append([]byte{0x80, 0x04, 0x24, 0x1B}, "rows"...))
	items, _, err := readDSS(bufio.NewReader(bytes.NewReader(open)))
	if err != nil || len(items) != 1 || items[0].cp != cpQRYDTA || string(items[0].data) != "rows" {
		t.Errorf("an object with no length: %v, %v", items, err)
	}
	for _, bad := range [][]byte{
		{0, 10, 0xD1, 2, 0, 1, 0, 4, 0, 0},
		{0, 3, 0xD0, 2, 0, 1},
		{0, 9, 0xD0, 2, 0, 1, 0, 9, 0},
		{0, 10, 0xD0, 2, 0, 1, 0, 9, 0x24, 0x08},
	} {
		if _, _, err := readDSS(bufio.NewReader(bytes.NewReader(bad))); !errors.Is(err, errProtocol) {
			t.Errorf("%x: %v", bad, err)
		}
	}

	all := " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
	e, ok := toEBCDIC(all)
	if !ok || fromEBCDIC(e) != all {
		t.Errorf("EBCDIC and back: %q", fromEBCDIC(e))
	}
	// Code page 500's, where it differs from the United States' 037.
	if got := fmt.Sprintf("%X", e[strings.IndexByte(all, '!')]) + fmt.Sprintf("%X", e[strings.IndexByte(all, '[')]) + fmt.Sprintf("%X", e[strings.IndexByte(all, ']')]); got != "4F4A5A" {
		t.Errorf("! [ ] are %s in code page 500, want 4F 4A 5A", got)
	}
	if b, _ := toEBCDIC("Az09 "); !bytes.Equal(b, []byte{0xC1, 0xA9, 0xF0, 0xF9, 0x40}) {
		t.Errorf("Az09 and a space: %X", b)
	}
	if _, ok := toEBCDIC("Zoë"); ok {
		t.Error("ë has no place this client knows in code page 500")
	}
}

func TestNumbers(t *testing.T) {
	for _, c := range []struct {
		hex   string
		scale int
		want  string
	}{
		{"000001250C", 2, "12.50"},
		{"000000325D", 2, "-3.25"},
		{"0C", 0, "0"},
		{"0D", 0, "0"},
		{"1F", 0, "1"},
		{"00001D", 6, "-0.000001"},
		{"12345C", 0, "12345"},
		{"12345C", 5, "0.12345"},
		{"00000C", 3, "0.000"},
	} {
		var b []byte
		_, _ = fmt.Sscanf(c.hex, "%X", &b)
		if got := packed(b, c.scale); string(got) != c.want {
			t.Errorf("packed %s at scale %d: %s, want %s", c.hex, c.scale, got, c.want)
		}
	}
	// IEEE 754's decimal64 and decimal128, as its standard's examples and
	// Db2's own values have them.
	for _, c := range []struct{ hex, want string }{
		{"2238000000000001", "1"},
		{"2238000000000080", "100"},
		{"22300000000000A0", "1.20"},
		{"A2300000000003D0", "-7.50"},
		{"7800000000000000", "Infinity"},
		{"F800000000000000", "-Infinity"},
		{"7C00000000000000", "NaN"},
		{"7E00000000000000", "sNaN"},
		{"2288000000000001", "1E+20"},
		{"2230000000000000", "0.00"},
		{"77FCFF3FCFF3FCFF", "9.999999999999999E+384"},
		{"22080000000000000000000000000001", "1"},
		{"A20780000000000000000000000003D0", "-7.50"},
		{"77FFCFF3FCFF3FCFF3FCFF3FCFF3FCFF", "9.999999999999999999999999999999999E+6144"},
	} {
		var b []byte
		_, _ = fmt.Sscanf(c.hex, "%X", &b)
		got, err := decFloat(b)
		if err != nil || string(got) != c.want {
			t.Errorf("DECFLOAT %s: %s, %v, want %s", c.hex, got, err, c.want)
		}
	}
	if _, err := decFloat(make([]byte, 4)); !errors.Is(err, errProtocol) {
		t.Errorf("a DECFLOAT of four bytes: %v", err)
	}
}

func TestTemporal(t *testing.T) {
	for _, c := range []struct {
		typ      int
		in, want string
	}{
		{tDate, "2026-11-19", "2026-11-19T00:00:00Z"},
		{tTimestamp, "2026-11-19-13.28.03.326001", "2026-11-19T13:28:03.326001Z"},
		{tTimestamp, "2026-11-19-13.28.03", "2026-11-19T13:28:03Z"},
		{tTimestamp, "2026-11-19-13.28.03.123456789012", "2026-11-19T13:28:03.123456789Z"},
		{tTimestamp, "2026-11-19-13.28.03.5", "2026-11-19T13:28:03.5Z"},
	} {
		got, err := temporal(c.typ, c.in)
		at, ok := got.(time.Time)
		if err != nil || !ok || at.Format(time.RFC3339Nano) != c.want {
			t.Errorf("%s: %v, %v, want %s", c.in, got, err, c.want)
		}
	}
	if got, _ := temporal(tTime, "13.28.03"); got != "13:28:03" {
		t.Errorf("a time: %v", got)
	}
	for _, bad := range []string{"", "2026-11-19", "2026-11-19-13.28.03.x", "not a timestamp at all"} {
		if _, err := temporal(tTimestamp, bad); err == nil {
			t.Errorf("%q read as a timestamp", bad)
		}
	}
}

// A server that does not speak Unicode's names gets them in EBCDIC, and a
// name that has no place there is said to be one.
func TestWithoutUnicode(t *testing.T) {
	seen := make(chan []item, 1)
	addr := standIn(t, func(nc net.Conn, r *bufio.Reader) {
		readRequest(t, r) // EXCSAT
		var levels, p buf
		for _, m := range [][2]int{{mgrAGENT, 7}, {mgrSQLAM, 7}, {mgrRDB, 7}, {mgrSECMGR, 7}, {mgrCCSIDMGR, 0}} {
			levels.u16(m[0])
			levels.u16(m[1])
		}
		p.param(cpMGRLVLLS, levels.b)
		_, _ = nc.Write(answer(dssReply, 1, false, cpEXCSATRD, p.b))
		seen <- logIn(t, nc, r, readRequest(t, r))
	})
	cfg := Config{Addr: addr, Database: "Parcels_1", User: "app$1", Password: "s3cret!"}
	c, err := NewConnector(cfg).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	got := map[int]string{}
	for _, it := range <-seen {
		ps, err := params(it.data)
		if err != nil {
			t.Fatal(err)
		}
		for _, cp := range []int{cpRDBNAM, cpUSRID, cpPASSWORD, cpPRDID, cpTYPDEFNAM} {
			if v, ok := ps[cp]; ok {
				got[cp] = fmt.Sprintf("%X", v)
			}
		}
	}
	want := map[int]string{
		cpRDBNAM:    "D78199838593A26DF1404040404040404040", // Parcels_1, padded with EBCDIC's blanks
		cpUSRID:     "8197975BF1",
		cpPASSWORD:  "A2F3839985A34F",
		cpPRDID:     "C1E7E7F0F1F0F0F0",
		cpTYPDEFNAM: "D8E3C4E2D8D3C1E2C3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names in EBCDIC\n got %v\nwant %v", got, want)
	}

	cfg.Password = "pässword"
	_, err = NewConnector(cfg).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "takes only in Unicode") {
		t.Errorf("a password with a character EBCDIC has no place for: %v", err)
	}

	// A server of a level before the one this client reads.
	addr = standIn(t, func(nc net.Conn, r *bufio.Reader) {
		readRequest(t, r)
		var levels, p buf
		levels.u16(mgrSQLAM)
		levels.u16(6)
		p.param(cpMGRLVLLS, levels.b)
		_, _ = nc.Write(answer(dssReply, 1, false, cpEXCSATRD, p.b))
	})
	_, err = NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser}).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "level 6") {
		t.Errorf("a server at level 6: %v", err)
	}
}

// A connection that breaks, or a server that answers what this client does
// not read, ends the statement with what happened, and the next statement
// gets a connection of its own.
func TestBroken(t *testing.T) {
	var mu sync.Mutex
	connections := 0
	addr := standIn(t, func(nc net.Conn, r *bufio.Reader) {
		mu.Lock()
		connections++
		n := connections
		mu.Unlock()
		readRequest(t, r) // EXCSAT
		_, _ = nc.Write(attributesReply())
		logIn(t, nc, r, readRequest(t, r))
		readRequest(t, r) // the statement
		readRequest(t, r)
		readRequest(t, r)
		switch n {
		case 1:
			// The server goes away.
		case 2:
			// It could not read the request.
			var p buf
			p.param16(cpSVRCOD, 8)
			p.param8(cpSYNERRCD, 0x0E)
			p.param16(cpCODPNT, cpQRYINSID)
			p.param(cpSRVDGN, []byte("a diagnosis"))
			_, _ = nc.Write(answer(dssReply, 1, false, cpSYNTAXRM, p.b))
		case 3:
			// What is no structure of DRDA's.
			_, _ = nc.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		default:
			_, _ = nc.Write(append(answer(dssObject, 1, true, cpSQLCARD, []byte{null}), answer(dssObject, 2, false, cpSQLCARD, []byte{null})...))
		}
	})
	db := sql.OpenDB(NewConnector(Config{Addr: addr, Database: testDatabase, User: testUser, Password: testPassword}))
	defer db.Close()
	db.SetMaxOpenConns(1)
	for i, want := range []string{
		"the connection to " + addr + " broke",
		"the server could not read the request (syntax error 0x0e, at code point 0x215b, a diagnosis)",
		"a structure that starts",
		"",
	} {
		_, err := db.Exec("DELETE FROM pickups")
		switch {
		case want == "" && err != nil:
			t.Errorf("statement %d: %v", i+1, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("statement %d: %v, want %s", i+1, err, want)
		}
	}
	if connections != 4 {
		t.Errorf("%d connections, want one for each statement after a broken one", connections)
	}
}

// What a server says of a request it refuses, in words.
func TestRefusals(t *testing.T) {
	c := &conn{cfg: Config{Database: "parcels", User: "app"}, unicode: true}
	for cp, want := range map[int]string{
		cpRDBNFNRM: `the server has no database "parcels"`,
		cpRDBATHRM: `the user app may not connect to the database "parcels"`,
		cpRDBNACRM: "the database is not open on this connection",
		cpRDBACCRM: "the database is already open on this connection",
		cpQRYNOPRM: "the query is not open",
		cpCMDCHKRM: "the server could not run the command",
		cpCMDNSPRM: "the server does not know the command",
		cpCMDATHRM: "the user may not run the command",
		cpCMDVLTRM: "the command breaks the conversation's rules",
		cpAGNPRMRM: "the server's agent failed",
		cpRSCLMTRM: "the server ran out of a resource",
		cpPRCCNVRM: "the request breaks the conversation's rules (conversation error 0x11)",
		cpPRMNSPRM: "the server does not know a parameter",
		cpVALNSPRM: "the server does not take a parameter's value",
		cpOBJNSPRM: "the server does not know an object",
		cpTRGNSPRM: "the server does not know what the command is for",
		cpMGRLVLRM: "the server does not speak a manager's level",
		cpDSCINVRM: "the server could not read a description of data",
		cpDTAMCHRM: "the data does not match its description",
		cpPKGBNARM: "the package is being bound",
		cpCMMRQSRM: "the server asks for a commit or a rollback",
	} {
		var p buf
		if cp == cpPRCCNVRM {
			p.param8(cpPRCCNVCD, 0x11)
		}
		err := c.refusal(item{cp: cp, data: p.b, reply: true})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%#04x: %v, want %s", cp, err, want)
		}
	}
	// What says nothing failed, an object, and a message with parameters
	// that are none.
	for _, it := range []item{
		{cp: cpENDUOWRM, reply: true},
		{cp: cpRDBUPDRM, reply: true},
		{cp: cpSQLERRRM, reply: true},
		{cp: cpABNUOWRM, reply: true},
		{cp: cpSYNTAXRM},
		{cp: 0x1C03, reply: true},
	} {
		if err := c.refusal(it); err != nil {
			t.Errorf("%#04x: %v", it.cp, err)
		}
	}
	if err := c.refusal(item{cp: cpSYNTAXRM, data: []byte{0, 9, 0x11}, reply: true}); !errors.Is(err, errProtocol) {
		t.Errorf("a message that is cut short: %v", err)
	}
	for code, want := range map[byte]string{
		secMechUnsupported: "it does not take a user and a password as they are", secPasswordExpired: "the password has expired",
		secPasswordInvalid: "the password is not the user's", secPasswordMissing: "there is no password", secUserMissing: "there is no user",
		secUserInvalid: "it knows no such user", secUserRevoked: "the user is revoked", secNewPassword: "the new password is not one it takes",
		secLocalFailed: "its own security service failed", 0x7F: "security check code 0x7F",
	} {
		if got := refused(code); got != want {
			t.Errorf("security check code %#02x: %s, want %s", code, got, want)
		}
	}
}

func TestCharsets(t *testing.T) {
	for _, c := range []struct {
		ccsid int
		hex   string
		want  string
	}{
		{1208, "5A6FC3AB20E697A5", "Zoë 日"},
		{0, "5A6FC3AB", "Zoë"},
		{1200, "005A006F00EB65E5D83DDCE6", "Zoë日📦"},
		{13488, "005A00EB", "Zë"},
		{1202, "5A00EB00", "Zë"},
		{819, "5A6FEB", "Zoë"},
		{1252, "5A6FEB80", "Zoë€"},
		{923, "A4", "€"},
		{850, "5A6F89", "Zoë"},
		{500, "E99681", "Zoa"},
		{37, "E9965A", "Zo!"},
		{1140, "9F", "€"},
		{367, "5A6F", "Zo"},
		{9999, "5A6F", "Zo"},
	} {
		var b []byte
		_, _ = fmt.Sscanf(c.hex, "%X", &b)
		if got := decode(b, c.ccsid); got != c.want {
			t.Errorf("character set %d, %s: %q, want %q", c.ccsid, c.hex, got, c.want)
		}
	}
	// Bytes that are no text of a set this client does not know are read as
	// far as they can be.
	if got := decode([]byte{'Z', 0xFF, 'o'}, 9999); got != "Z\uFFFDo" {
		t.Errorf("bytes of an unknown set: %q", got)
	}
	for ccsid, want := range map[int]bool{1208: true, 1200: true, 819: true, 1252: true, 500: true, 0: true, 943: false, 1386: false} {
		if readable(ccsid) != want {
			t.Errorf("character set %d is readable: %v", ccsid, !want)
		}
	}
	if err := unreadable(943); !strings.Contains(err.Error(), "character set 943") {
		t.Error(err)
	}
	if got := zoned([]byte{0xF1, 0xF2, 0xD3}, 1); got != "-12.3" {
		t.Errorf("a zoned decimal: %s", got)
	}
	if got := zoned([]byte{0x31, 0x32, 0x33}, 0); got != "123" {
		t.Errorf("a zoned decimal in ASCII: %s", got)
	}
}
