package h2

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseURL(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs("data/parcels")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		url  string
		want Config
	}{
		{"jdbc:h2:tcp://localhost:9092/~/parcels", Config{Addr: "localhost:9092", Database: "~/parcels"}},
		{"h2:tcp://db/mem:parcels;MODE=PostgreSQL;schema=depot", Config{Addr: "db", Database: "mem:parcels", Settings: map[string]string{"MODE": "PostgreSQL", "SCHEMA": "depot"}}},
		{"JDBC:H2:SSL://db:9093/file:/data/parcels", Config{Addr: "db:9093", Database: "file:/data/parcels", TLS: true}},
		{"jdbc:h2:tcp://db/parcels;USER=clerk;PASSWORD=secret;TIME ZONE=Europe/Berlin", Config{Addr: "db", Database: "parcels", User: "clerk", Password: "secret", TimeZone: "Europe/Berlin"}},
		{"jdbc:h2:file:./data/parcels;AUTO_SERVER=TRUE;AUTO_SERVER_PORT=9090", Config{LockFile: abs + ".lock.db"}},
		{"jdbc:h2:./data/parcels;auto_server=true", Config{LockFile: abs + ".lock.db"}},
		{"jdbc:h2:~/parcels;AUTO_SERVER=TRUE", Config{LockFile: filepath.Join(home, "parcels") + ".lock.db"}},
	} {
		got, err := ParseURL(c.url, "", "")
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if c.want.TimeZone == "" {
			got.TimeZone = ""
		}
		got.URL = ""
		if len(got.Settings) == 0 {
			got.Settings = nil
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.url, got, c.want)
		}
	}

	// A user the step names wins over the URL's, and the URL a connection shows
	// has no password in it.
	cfg, err := ParseURL("jdbc:h2:tcp://db/parcels;USER=clerk;PASSWORD=secret", "sa", "")
	if err != nil || cfg.User != "sa" || strings.Contains(cfg.URL, "secret") {
		t.Errorf("%+v, %v", cfg, err)
	}

	for url, want := range map[string]string{
		"jdbc:h2:mem:parcels":                            "memory",
		"jdbc:h2:mem:":                                   "memory",
		"jdbc:h2:file:./data/parcels":                    "AUTO_SERVER=TRUE",
		"jdbc:h2:~/parcels":                              "AUTO_SERVER=TRUE",
		"jdbc:h2:tcp://localhost":                        "write the server and the database",
		"jdbc:h2:tcp://a,b/parcels":                      "cluster",
		"jdbc:h2:zip:~/parcels.zip!/parcels":             "zip: is neither",
		"jdbc:h2:tcp://db/parcels;CIPHER=AES":            "encrypted",
		"jdbc:h2:tcp://db/parcels;MODE":                  "has no value",
		"jdbc:postgresql://localhost/parcels":            "is not an H2 URL",
		"jdbc:h2:file:./data/parcels;AUTO_SERVER=FALSE;": "AUTO_SERVER=TRUE",
	} {
		_, err := ParseURL(url, "sa", "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %s", url, err, want)
		}
		private := strings.Contains(want, "memory") || strings.Contains(want, "AUTO_SERVER")
		if errors.Is(err, ErrPrivate) != private {
			t.Errorf("%s: private %v, want %v: %v", url, !private, private, err)
		}
	}
}

func TestReadLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "parcels.lock.db")
	if _, err := readLock(path); err == nil || !strings.Contains(err.Error(), "no service has the database open") {
		t.Errorf("no lock file: %v", err)
	}
	// As H2 2.5 wrote it.
	file := "#FileLock\n#Thu Nov 19 02:40:59 GMT 2026\nhostName=49f2c38860a2\nid=1a1f1adc29a31af72c349ab679d326c33603971d5e1\nmethod=file\nserver=172.17.0.3\\:19392\n"
	if err := os.WriteFile(path, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	at, err := readLock(path)
	if err != nil || at.server != "172.17.0.3:19392" || at.key != "1a1f1adc29a31af72c349ab679d326c33603971d5e1" {
		t.Errorf("%+v, %v", at, err)
	}
	// A database opened without AUTO_SERVER has a lock file too, with no server in it.
	if err := os.WriteFile(path, []byte("#FileLock\nid=1a1f\nmethod=file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLock(path); err == nil || !strings.Contains(err.Error(), "AUTO_SERVER=TRUE") {
		t.Errorf("a lock with no server: %v", err)
	}
}

func TestTexts(t *testing.T) {
	for got, want := range map[string]string{
		timeOfDay(int64(7*time.Hour + 40*time.Minute)):                             "07:40:00",
		timeOfDay(int64(23*time.Hour + 59*time.Minute + 59*time.Second + 5000000)): "23:59:59.005",
		offsetText(7200):   "+02",
		offsetText(-19800): "-05:30",
		offsetText(0):      "+00",
		uuidText([16]byte{0x0f, 0x8f, 0xad, 0x5b, 0xd9, 0xcb, 0x46, 0x9f, 0xa1, 0x65, 0x70, 0x86, 0x77, 0x28, 0x95, 0x0e}): "0f8fad5b-d9cb-469f-a165-70867728950e",
		intervalText(0, false, 10, 0):        "INTERVAL '10' YEAR",
		intervalText(2, true, 1, 0):          "INTERVAL '-1' DAY",
		intervalText(5, false, 4, 500000000): "INTERVAL '4.5' SECOND",
		intervalText(6, true, 1, 2):          "INTERVAL '-1-2' YEAR TO MONTH",
		intervalText(8, false, 1, 2*60+3):    "INTERVAL '1 02:03' DAY TO MINUTE",
		intervalText(9, false, 1, int64(2*time.Hour+3*time.Minute+4*time.Second+500*time.Millisecond)): "INTERVAL '1 02:03:04.5' DAY TO SECOND",
		intervalText(12, false, 3, int64(4*time.Second)):                                               "INTERVAL '3:04' MINUTE TO SECOND",
		jsonText(list{int64(1), number("2.50"), "a\"b", nil, true, list{1.5}}):                         `[1, 2.50, "a\"b", null, true, [1.5]]`,
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	// H2 packs a date into a number: its year, month and day.
	date := int64(2026)<<9 | 9<<5 | 24
	if got := dateOf(date, int64(7*time.Hour+40*time.Minute), time.UTC); !got.Equal(time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC)) {
		t.Errorf("date: %s", got)
	}
	if got := (columnType{code: colIntervalYear + 9}).name(); got != "INTERVAL DAY TO SECOND" {
		t.Errorf("an interval's name: %s", got)
	}
}

// server is an H2 server as far as these tests go: it answers a client as H2's
// TcpServerThread does, for one session and the queries the test gives it.
type server struct {
	t       *testing.T
	version int32
	// results are what a query's text gives: what follows the answer's status.
	results map[string]func(w *wire)
	// got is what the client sent to open its session.
	got struct {
		min, max      int32
		database, url string
		user          string
		hash          []byte
		settings      map[string]string
		zone          string
	}
	lobReads int
	// tls makes it a server started with -tcpSSL.
	tls *tls.Config
}

func (s *server) serve(nc net.Conn) {
	defer nc.Close()
	w := newWire(nc)
	s.got.min, s.got.max = w.readInt(), w.readInt()
	s.got.database, s.got.url, s.got.user = w.str(), w.str(), w.str()
	s.got.hash = w.readBytes()
	w.readBytes()
	s.got.settings = map[string]string{}
	for n := w.readInt(); n > 0; n-- {
		k := w.str()
		s.got.settings[k] = w.str()
	}
	w.writeInt(statusOK)
	w.writeInt(s.version)
	_ = w.flush()
	sql := map[int32]string{}
	for w.err == nil {
		switch op := w.readInt(); op {
		case opSessionSetID:
			w.str()
			if s.version >= 20 {
				s.got.zone = w.str()
			}
			w.writeInt(statusOK)
			w.writeBool(true)
		case opPrepare:
			id := w.readInt()
			sql[id] = w.str()
			if _, ok := s.results[sql[id]]; !ok {
				w.writeInt(statusError)
				w.writeString("42S02")
				w.writeString(`Table "NOWHERE" not found`)
				w.writeString(sql[id])
				w.writeInt(42102)
				w.writeString("org.h2.jdbc.JdbcSQLSyntaxErrorException")
				break
			}
			w.writeInt(statusOK)
			w.writeBool(true) // a query
			w.writeBool(true)
			w.writeInt(66)
			w.writeInt(0)
		case opExecuteQuery:
			id := w.readInt()
			w.readInt()
			if s.version >= 20 {
				w.readLong()
			} else {
				w.readInt()
			}
			w.readInt()
			w.readInt()
			w.writeInt(statusOK)
			s.results[sql[id]](w)
		case opLobRead:
			s.lobReads++
			w.readLong()
			w.readBytes()
			offset := w.readLong()
			w.readInt()
			w.writeInt(statusOK)
			text := "a large text ü"
			if offset >= int64(len(text)) {
				w.writeInt(0)
			} else {
				w.writeInt(int32(len(text)))
				w.write([]byte(text))
			}
		case opCommandClose, opResultClose:
			w.readInt()
			continue // nothing answers these
		case opSessionClose:
			w.writeInt(statusOK)
			_ = w.flush()
			return
		default:
			if w.err == nil {
				s.t.Errorf("the client asked for %d, which this server does not know", op)
			}
			return
		}
		_ = w.flush()
	}
}

// start listens and gives a database of the server.
func (s *server) start(cfg Config) *sql.DB {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.t.Fatal(err)
	}
	if s.tls != nil {
		l = tls.NewListener(l, s.tls)
	}
	s.t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(nc)
		}
	}()
	cfg.Addr = l.Addr().String()
	db := sql.OpenDB(NewConnector(cfg))
	db.SetMaxOpenConns(1)
	s.t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeColumn(w *wire, version int32, name string, typ func()) {
	w.writeString(name)
	w.writeString("PUBLIC")
	w.writeString("PICKUPS")
	w.writeString(name)
	typ()
	if version < 20 {
		w.writeInt(10)
	}
	w.writeBool(false)
	w.writeInt(1)
}

func scan(t *testing.T, db *sql.DB, q string) (types []string, row []any) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		types = append(types, c.DatabaseTypeName())
	}
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	row = make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range row {
		ptrs[i] = &row[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	return types, row
}

// What a client sends to open a session is what H2's own sends: the user's name
// in upper case, the password as H2 keeps it, the URL's settings, and, to H2 2,
// the session's time zone.
func TestHandshake(t *testing.T) {
	s := &server{t: t, version: 21, results: map[string]func(*wire){"SELECT 1": func(w *wire) {
		w.writeInt(1)
		w.writeLong(1)
		writeColumn(w, 21, "1", func() { w.writeInt(valInteger) })
		w.writeByte(1)
		w.writeInt(valInteger)
		w.writeInt(1)
	}}}
	db := s.start(Config{
		Database: "mem:parcels", URL: "jdbc:h2:tcp://db/mem:parcels", User: "clerk", Password: "secret",
		Settings: map[string]string{"MODE": "PostgreSQL", "DB_CLOSE_DELAY": "-1"}, TimeZone: "Asia/Kolkata",
	})
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	// SHA-256 of "CLERK@secret" in UTF-16, as H2's SHA256.getKeyPasswordHash gives it.
	const hash = "be121c1a18809dd187a00e0219b0def5a789f169bfa88f2c17334803dab1a5ae"
	got := s.got
	if got.min != 17 || got.max != 21 || got.database != "mem:parcels" || got.url != "jdbc:h2:tcp://db/mem:parcels" || got.user != "CLERK" ||
		hex.EncodeToString(got.hash) != hash || got.zone != "Asia/Kolkata" || !reflect.DeepEqual(got.settings, map[string]string{"MODE": "PostgreSQL", "DB_CLOSE_DELAY": "-1"}) {
		t.Errorf("the client sent %+v", got)
	}
	if len(passwordHash("", "")) != 0 || passwordHash("", "") == nil {
		t.Error("no user and no password is an empty hash, not a null one")
	}

	// An error of the server's is the statement's, and the session goes on.
	_, err := db.QueryContext(context.Background(), "SELECT * FROM nowhere")
	var h2err *Error
	if !errors.As(err, &h2err) || h2err.Code != 42102 || h2err.SQLState != "42S02" || !strings.Contains(err.Error(), `Table "NOWHERE" not found`) {
		t.Errorf("an error: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Errorf("after an error: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), "SELECT ?", 1); err == nil || !strings.Contains(err.Error(), "no parameters") {
		t.Errorf("a parameter: %v", err)
	}
}

// Every type of value reads as what it holds, as H2 2 writes it.
func TestValuesOfH2Two(t *testing.T) {
	const version = 21
	date := int64(2026)<<9 | 9<<5 | 24
	type column struct {
		typ   func(w *wire)
		value func(w *wire)
		name  string
		want  any
	}
	short := func(w *wire, n int16) { w.write(binary.BigEndian.AppendUint16(nil, uint16(n))) }
	columns := []column{
		{func(w *wire) { w.writeInt(valBoolean) }, func(w *wire) { w.writeInt(valBoolean); w.writeBool(true) }, "BOOLEAN", true},
		{func(w *wire) { w.writeInt(valTinyint) }, func(w *wire) { w.writeInt(valTinyint); w.writeByte(0xfc) }, "TINYINT", int64(-4)},
		{func(w *wire) { w.writeInt(valSmallint) }, func(w *wire) { w.writeInt(valSmallint); short(w, -3) }, "SMALLINT", int64(-3)},
		{func(w *wire) { w.writeInt(valInteger) }, func(w *wire) { w.writeInt(valInteger); w.writeInt(850) }, "INTEGER", int64(850)},
		{func(w *wire) { w.writeInt(valBigint) }, func(w *wire) { w.writeInt(valBigint); w.writeLong(3000000000) }, "BIGINT", int64(3000000000)},
		{
			func(w *wire) { w.writeInt(valNumeric); w.writeInt(10); w.writeInt(2); w.writeBool(true) },
			func(w *wire) { w.writeInt(valNumeric); w.writeString("12.50") }, "NUMERIC", "12.50",
		},
		{func(w *wire) { w.writeInt(colDecfloat); w.writeInt(34) }, func(w *wire) { w.writeInt(valDecfloat); w.writeString("1E+3") }, "DECFLOAT", "1E+3"},
		{
			func(w *wire) { w.writeInt(valDouble); w.writeByte(0xff) },
			func(w *wire) { w.writeInt(valDouble); w.writeLong(int64(math.Float64bits(1.5))) }, "DOUBLE PRECISION", 1.5,
		},
		{
			func(w *wire) { w.writeInt(valReal); w.writeByte(0xff) },
			func(w *wire) { w.writeInt(valReal); w.writeInt(int32(math.Float32bits(0.1))) }, "REAL", float64(float32(0.1)),
		},
		{
			func(w *wire) { w.writeInt(valTime); w.writeByte(0) },
			func(w *wire) { w.writeInt(valTime); w.writeLong(int64(7*time.Hour + 40*time.Minute)) }, "TIME", "07:40:00",
		},
		{
			func(w *wire) { w.writeInt(colTimeTZ); w.writeByte(0) },
			func(w *wire) { w.writeInt(valTimeTZ); w.writeLong(int64(7 * time.Hour)); w.writeInt(7200) }, "TIME WITH TIME ZONE", "07:00:00+02",
		},
		{func(w *wire) { w.writeInt(valDate) }, func(w *wire) { w.writeInt(valDate); w.writeLong(date) }, "DATE", time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)},
		{
			func(w *wire) { w.writeInt(valTimestamp); w.writeByte(6) },
			func(w *wire) { w.writeInt(valTimestamp); w.writeLong(date); w.writeLong(int64(7*time.Hour) + 123000) }, "TIMESTAMP",
			time.Date(2026, 9, 24, 7, 0, 0, 123000, time.UTC),
		},
		{
			func(w *wire) { w.writeInt(valTimestampTZ); w.writeByte(6) },
			func(w *wire) {
				w.writeInt(valTimestampTZ)
				w.writeLong(date)
				w.writeLong(int64(7 * time.Hour))
				w.writeInt(7200)
			},
			"TIMESTAMP WITH TIME ZONE", time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC),
		},
		{func(w *wire) { w.writeInt(valVarchar); w.writeInt(40) }, func(w *wire) { w.writeInt(valVarchar); w.writeString("héllo 😀") }, "CHARACTER VARYING", "héllo 😀"},
		{func(w *wire) { w.writeInt(valChar); w.writeInt(5) }, func(w *wire) { w.writeInt(valChar); w.writeString("ab   ") }, "CHARACTER", "ab   "},
		{func(w *wire) { w.writeInt(valVarbinary); w.writeInt(10) }, func(w *wire) { w.writeInt(valVarbinary); w.writeBytes([]byte{0xca, 0xfe}) }, "BINARY VARYING", []byte{0xca, 0xfe}},
		{func(w *wire) { w.writeInt(colBinary); w.writeInt(2) }, func(w *wire) { w.writeInt(valBinary); w.writeBytes([]byte{1, 2}) }, "BINARY", []byte{1, 2}},
		{func(w *wire) { w.writeInt(valUUID) }, func(w *wire) {
			w.writeInt(valUUID)
			w.write([]byte{0x0f, 0x8f, 0xad, 0x5b, 0xd9, 0xcb, 0x46, 0x9f, 0xa1, 0x65, 0x70, 0x86, 0x77, 0x28, 0x95, 0x0e})
		}, "UUID", "0f8fad5b-d9cb-469f-a165-70867728950e"},
		{func(w *wire) { w.writeInt(colJSON); w.writeInt(100) }, func(w *wire) { w.writeInt(valJSON); w.writeBytes([]byte(`{"zone":"DE-1"}`)) }, "JSON", `{"zone":"DE-1"}`},
		{
			func(w *wire) { w.writeInt(valEnum); w.writeInt(2); w.writeString("PLANNED"); w.writeString("DONE") },
			func(w *wire) { w.writeInt(valEnum); w.writeInt(2) }, "ENUM", "DONE",
		},
		{
			func(w *wire) {
				w.writeInt(valArray)
				w.writeInt(10)
				w.writeInt(valNumeric)
				w.writeInt(10)
				w.writeInt(2)
				w.writeBool(false)
			},
			func(w *wire) {
				w.writeInt(valArray)
				w.writeInt(2)
				w.writeInt(valNumeric)
				w.writeString("1.50")
				w.writeInt(valNull)
			}, "ARRAY", "[1.50, null]",
		},
		{func(w *wire) { w.writeInt(colIntervalYear + 9); w.writeByte(2); w.writeByte(6) }, func(w *wire) {
			w.writeInt(valInterval)
			w.writeByte(9)
			w.writeLong(1)
			w.writeLong(int64(2 * time.Hour))
		}, "INTERVAL DAY TO SECOND", "INTERVAL '1 02:00:00' DAY TO SECOND"},
		// A small text comes with its row, each UTF-16 unit in one to three bytes.
		{func(w *wire) { w.writeInt(valClob); w.writeLong(1000) }, func(w *wire) {
			w.writeInt(valClob)
			w.writeLong(4)
			w.write([]byte{'a', 0xc3, 0xbc, 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80}) // a, ü, and 😀's two units
			w.writeInt(0x1234)
		}, "CHARACTER LARGE OBJECT", "aü😀"},
		// A large one stays with the server, which is asked for it.
		{func(w *wire) { w.writeInt(valClob); w.writeLong(1000) }, func(w *wire) {
			w.writeInt(valClob)
			w.writeLong(-1)
			w.writeInt(3)
			w.writeLong(77)
			w.writeBytes([]byte{1, 2, 3})
			w.writeLong(-1)
			w.writeLong(14)
		}, "CHARACTER LARGE OBJECT", "a large text ü"},
		{func(w *wire) { w.writeInt(valBlob); w.writeLong(1000) }, func(w *wire) {
			w.writeInt(valBlob)
			w.writeLong(2)
			w.write([]byte{7, 8})
			w.writeInt(0x1234)
		}, "BINARY LARGE OBJECT", []byte{7, 8}},
		{func(w *wire) { w.writeInt(valInteger) }, func(w *wire) { w.writeInt(valNull) }, "INTEGER", nil},
	}
	enums := func(w *wire) {
		w.writeInt(1)
		w.writeLong(0)
		writeColumn(w, version, "SETTING_VALUE", func() { w.writeInt(valVarchar); w.writeInt(-1) })
	}
	s := &server{t: t, version: version, results: map[string]func(*wire){
		"SELECT * FROM pickups": func(w *wire) {
			w.writeInt(int32(len(columns)))
			w.writeLong(1)
			for _, c := range columns {
				writeColumn(w, version, "C", func() { c.typ(w) })
			}
			w.writeByte(1)
			for _, c := range columns {
				c.value(w)
			}
		},
		"SELECT SETTING_VALUE FROM INFORMATION_SCHEMA.SETTINGS WHERE SETTING_NAME = 'ZERO_BASED_ENUMS'": enums,
	}}
	db := s.start(Config{Database: "mem:parcels", User: "sa"})
	types, row := scan(t, db, "SELECT * FROM pickups")
	for i, c := range columns {
		if types[i] != c.name {
			t.Errorf("column %d is %s, want %s", i+1, types[i], c.name)
		}
		got := row[i]
		if at, ok := got.(time.Time); ok {
			if want := c.want.(time.Time); !at.Equal(want) || c.name == "TIMESTAMP WITH TIME ZONE" && at.Format("-07:00") != "+02:00" {
				t.Errorf("%s reads as %s, want %s", c.name, at, want)
			}
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s reads as %#v, want %#v", c.name, got, c.want)
		}
	}
	if s.lobReads != 2 {
		t.Errorf("the server was asked for the large text %d times, want until it has no more: 2", s.lobReads)
	}
}

// H2 1.4 writes a column's type, a small integer, an enum and an array its own
// ways (version 19 of the protocol).
func TestValuesOfH2One(t *testing.T) {
	const version = 19
	old := func(w *wire, code int32, precision int64, scale int32) func() {
		return func() { w.writeInt(code); w.writeLong(precision); w.writeInt(scale) }
	}
	s := &server{t: t, version: version}
	s.results = map[string]func(*wire){"SELECT * FROM pickups": func(w *wire) {
		w.writeInt(5)
		w.writeInt(1) // its rows are counted in an int
		writeColumn(w, version, "TRIES", old(w, valSmallint, 5, 0))
		writeColumn(w, version, "STATUS", old(w, valEnum, 7, 0))
		writeColumn(w, version, "STOPS", old(w, valArray, 10, 0))
		writeColumn(w, version, "KILOS", old(w, valNumeric, 10, 2))
		writeColumn(w, version, "ZONED_AT", old(w, valTimestampTZ, 35, 9))
		w.writeByte(1)
		w.writeInt(valSmallint)
		w.writeInt(-3)
		w.writeInt(valEnum)
		w.writeInt(1)
		w.writeString("DONE")
		w.writeInt(valArray)
		w.writeInt(^int32(2)) // two elements, and their Java class
		w.writeString("java.lang.Integer")
		w.writeInt(valInteger)
		w.writeInt(1)
		w.writeInt(valInteger)
		w.writeInt(2)
		w.writeInt(valNumeric)
		w.writeString("12.50")
		w.writeInt(valTimestampTZ)
		w.writeLong(int64(2026)<<9 | 9<<5 | 24)
		w.writeLong(int64(7 * time.Hour))
		w.writeInt(7200)
	}}
	db := s.start(Config{Database: "mem:parcels", User: "sa"})
	types, row := scan(t, db, "SELECT * FROM pickups")
	if want := []string{"SMALLINT", "ENUM", "ARRAY", "NUMERIC", "TIMESTAMP WITH TIME ZONE"}; !reflect.DeepEqual(types, want) {
		t.Errorf("types %v, want %v", types, want)
	}
	at, _ := row[4].(time.Time)
	if !reflect.DeepEqual(row[:4], []any{int64(-3), "DONE", "[1, 2]", "12.50"}) || !at.Equal(time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("the row reads as %#v", row)
	}
	if s.got.zone != "" {
		t.Errorf("H2 1.4 was sent a time zone, which it does not read: %q", s.got.zone)
	}
}

// A server that speaks a protocol this client does not is refused with what it speaks.
func TestAnotherVersion(t *testing.T) {
	s := &server{t: t, version: 22}
	db := s.start(Config{Database: "mem:parcels", User: "sa"})
	if err := db.PingContext(context.Background()); err == nil || !strings.Contains(err.Error(), "version 22") {
		t.Errorf("version 22: %v", err)
	}
}

// A server's certificate is held to what the machine trusts and to the server's
// name, as H2's own client holds it: H2's built-in certificate, which every H2
// server shows unless it is given another, is no one's to trust.
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
	one := map[string]func(*wire){"SELECT 1": func(w *wire) {
		w.writeInt(1)
		w.writeLong(1)
		writeColumn(w, 21, "1", func() { w.writeInt(valInteger) })
		w.writeByte(1)
		w.writeInt(valInteger)
		w.writeInt(1)
	}}
	served := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}

	s := &server{t: t, version: 21, results: one, tls: served}
	if err := s.start(Config{Database: "mem:parcels", User: "sa", TLS: true, RootCAs: roots}).PingContext(context.Background()); err != nil {
		t.Errorf("a certificate the client trusts: %v", err)
	}
	s = &server{t: t, version: 21, results: one, tls: served}
	err = s.start(Config{Database: "mem:parcels", User: "sa", TLS: true}).PingContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "is not one this machine trusts") || !strings.Contains(err.Error(), "javax.net.ssl.keyStore") {
		t.Errorf("a certificate no one vouches for: %v", err)
	}
}
