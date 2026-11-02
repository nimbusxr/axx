package sql

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nimbusxr/axx/internal/columntype"
)

func TestOracleLongLiteral(t *testing.T) {
	if got := oracleLongLiteral("'short'"); got != "'short'" {
		t.Errorf("a literal that fits changed: %s", got)
	}
	if got := oracleLongLiteral("1234"); got != "1234" {
		t.Errorf("a number changed: %s", got)
	}

	// Each content puts a character of two bytes or a doubled quote on every byte a
	// cut could fall on.
	for _, want := range []string{
		strings.Repeat("ü", 5000),
		strings.Repeat("''", 5000),
		strings.Repeat("a''", 3000),
		strings.Repeat("aü''", 2000),
		strings.Repeat("a", 3900) + "''" + strings.Repeat("b", 3900),
		strings.Repeat("a", 3899) + "''" + strings.Repeat("b", 3900),
		strings.Repeat("a", 3899) + "ü" + strings.Repeat("b", 3900),
	} {
		got := oracleLongLiteral("'" + want + "'")
		var joined strings.Builder
		for piece := range strings.SplitSeq(got, " || ") {
			inner, ok := strings.CutPrefix(piece, "TO_CLOB('")
			if !ok {
				t.Fatalf("a piece of %s", piece[:min(len(piece), 40)])
			}
			inner, ok = strings.CutSuffix(inner, "')")
			if !ok {
				t.Fatalf("the end of a piece of %s", piece[max(len(piece)-40, 0):])
			}
			if len(inner) > oracleMaxLiteral {
				t.Errorf("a piece of %d bytes", len(inner))
			}
			if !utf8.ValidString(inner) {
				t.Errorf("a cut inside a character: %q", inner[max(len(inner)-8, 0):])
			}
			if quotes := len(inner) - len(strings.TrimRight(inner, "'")); quotes%2 == 1 {
				t.Errorf("a cut inside a doubled quote: %q", inner[max(len(inner)-8, 0):])
			}
			joined.WriteString(inner)
		}
		if joined.String() != want {
			t.Errorf("the pieces of %d bytes did not make the literal again", len(want))
		}
	}
}

func TestOracleNumberKind(t *testing.T) {
	for _, c := range []struct {
		typeName        string
		kind            columntype.Kind
		known, isNumber bool
	}{
		{"NUMBER", columntype.Other, false, true},
		{"NUMBER(10,0)", columntype.Integer, true, true},
		{"NUMBER(*,0)", columntype.Integer, true, true},
		{"NUMBER(5,-2)", columntype.Integer, true, true},
		{"NUMBER(10,2)", columntype.Decimal, true, true},
		{"NUMBER(*,2)", columntype.Decimal, true, true},
		{"VARCHAR2", columntype.Other, false, false},
	} {
		kind, known, isNumber := oracleNumberKind(c.typeName)
		if kind != c.kind || known != c.known || isNumber != c.isNumber {
			t.Errorf("%s: kind %v, known %v, a number %v", c.typeName, kind, known, isNumber)
		}
	}
}

func TestOracleReaders(t *testing.T) {
	for _, c := range []struct {
		name     string
		read     func(any) any
		in, want any
	}{
		{"a stored integer", oracleWhole, "-7", int64(-7)},
		{"a number no int64 holds", oracleWhole, "12345678901234567890123", exactNumber("12345678901234567890123")},
		{"a decimal", oracleWhole, "7.5", exactNumber("7.5")},
		{"all of a decimal's digits", oracleWhole, "0.1234567890123456789", exactNumber("0.1234567890123456789")},
		{"null, as a whole number", oracleWhole, nil, nil},
		{"text no number reads as", oracleWhole, "seven", "seven"},
		{"true", oracleBoolean, "1", true},
		{"false", oracleBoolean, "0", false},
		{"null, as a boolean", oracleBoolean, nil, nil},
		{"bytes", oracleHex, "\xde\xad\xbe\xef", "DEADBEEF"},
		{"null, as bytes", oracleHex, nil, nil},
		{"a vector of floats", oracleVector("VECTOR(2,FLOAT32,DENSE)"), []float32{1, 0.1}, "[1.0,0.1]"},
		{"a vector of doubles", oracleVector("VECTOR(2,FLOAT64,DENSE)"), []float64{1, 0.1}, "[1.0,0.1]"},
		{"a vector of integers", oracleVector("VECTOR(2,INT8,DENSE)"), "\x01\xfe", "[1,-2]"},
		{"a vector of bits", oracleVector("VECTOR(8,BINARY,DENSE)"), "[254]", "[254]"},
		{"null, as a vector", oracleVector("VECTOR(2,FLOAT32,DENSE)"), nil, nil},
		{"XML", oracleXML, "<parcel/>\n", "<parcel/>"},
		{"null, as XML", oracleXML, nil, nil},
		{"a decimal's places", func(v any) any { return oracleDecimal(v, 2) }, "0.5", "0.50"},
		{"a whole decimal", func(v any) any { return oracleDecimal(v, 2) }, "-2", "-2.00"},
		{"more places than its scale", func(v any) any { return oracleDecimal(v, 1) }, "0.125", "0.125"},
	} {
		if got := c.read(c.in); got != c.want {
			t.Errorf("%s: %v (%T), want %v", c.name, got, got, c.want)
		}
	}
	for number, want := range map[exactNumber]string{"12345678901234567890123": "12345678901234567890123", "0.1234567890123456789": "0.1234567890123456789"} {
		if got := storedJSON(number); got != want {
			t.Errorf("stored: %s, want %s", got, want)
		}
		if got := text(number); got != want {
			t.Errorf("read: %s, want %s", got, want)
		}
	}
}

func TestSinglePrecision(t *testing.T) {
	for _, c := range []struct {
		dialect  Dialect
		typeName string
		want     bool
	}{
		{postgres, "FLOAT4", true},
		{postgres, "FLOAT8", false},
		{mysql, "FLOAT", true},
		{mysql, "UNSIGNED FLOAT", true},
		{mysql, "DOUBLE", false},
		{sqlserver, "REAL", true},
		{sqlserver, "FLOAT", false},
		{sqlite, "REAL", false},
		{oracle, "BINARY_FLOAT", false},
	} {
		if got := c.dialect.singlePrecision(c.typeName); got != c.want {
			t.Errorf("%s %s: %v", c.dialect.Name, c.typeName, got)
		}
	}
	if got := singlePrecisionValue(float64(float32(0.1))); got != float32(0.1) {
		t.Errorf("0.1 of 32 bits reads as %v (%T)", got, got)
	}
	if got := storedJSON(float32(0.1)); got != "0.1" {
		t.Errorf("0.1 of 32 bits is stored as %s", got)
	}
	if got := storedJSON(float32(2)); got != "2.0" {
		t.Errorf("2 of 32 bits is stored as %s", got)
	}
}

func TestSQLServerUUID(t *testing.T) {
	// 6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11, as SQL Server orders its bytes.
	if got := sqlServerUUID("\x52\x3a\x1c\x6f\x0e\x0d\x57\x4a\x9a\x1f\x2d\x8c\x1b\x0f\x7e\x11"); got != "6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11" {
		t.Errorf("reads as %v", got)
	}
	for _, v := range []any{nil, "not 16 bytes"} {
		if got := sqlServerUUID(v); got != v {
			t.Errorf("%v reads as %v", v, got)
		}
	}
}

func TestResolveOracleCredentials(t *testing.T) {
	for _, c := range []struct{ url, user, password, want string }{
		{"oracle://host:1521/FREEPDB1", "parcels", "secret", "parcels"},
		{"oracle://parcels:secret@host:1521/FREEPDB1", "", "", "parcels"},
		{"oracle://other:secret@host:1521/FREEPDB1", "parcels", "secret", "parcels"},
		{"jdbc:oracle:thin:@//host:1521/FREEPDB1", "parcels", "secret", "parcels"},
		{"jdbc:oracle:thin:@host:1521:FREE", "parcels", "secret", "parcels"},
		{"oracle://host:1521/FREEPDB1?TIMEOUT=30", "parcels", "secret", "TIMEOUT=30"},
	} {
		tgt, err := Resolve(c.url, c.user, c.password)
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if !tgt.Dialect.Oracle || !strings.Contains(tgt.DSN, c.want) {
			t.Errorf("%s: dialect %s, a DSN without %s", c.url, tgt.Dialect.Name, c.want)
		}
	}
	if _, err := Resolve("oracle://host:port%zz/FREEPDB1", "parcels", "secret"); err == nil {
		t.Error("a URL that does not parse was accepted")
	}
}

func TestParseDatabaseNow(t *testing.T) {
	at, err := parseDatabaseNow("2026-11-02 20:14:24.613106", 330)
	if err != nil {
		t.Fatal(err)
	}
	if got := at.UTC().Format("2006-01-02 15:04:05"); got != "2026-11-02 14:44:24" {
		t.Errorf("the moment: %s", got)
	}
	if got := at.Format("2006-01-02 15:04:05"); got != "2026-11-02 20:14:24" {
		t.Errorf("the wall clock: %s", got)
	}
	if got := oracle.Time(at, "2006-01-02 15:04:05"); got != "'2026-11-02 20:14:24+05:30'" {
		t.Errorf("Oracle's literal: %s", got)
	}
	if got := mysql.Time(at, "2006-01-02 15:04:05"); got != "'2026-11-02 20:14:24'" {
		t.Errorf("MySQL's literal: %s", got)
	}
	if _, err := parseDatabaseNow("soon", 0); err == nil {
		t.Error("a time that does not read was accepted")
	}
}

func TestSQLiteHasTheMachinesClock(t *testing.T) {
	svc := &Service{Target: Target{Dialect: sqlite}}
	now, err := svc.databaseNow(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if off := time.Since(now); off < 0 || off > time.Minute {
		t.Errorf("%s off the machine's clock", off)
	}
}

func TestZoneless(t *testing.T) {
	for _, c := range []struct {
		dialect  Dialect
		typeName string
		want     bool
	}{
		{oracle, "DATE", true},
		{oracle, "TIMESTAMP(6)", true},
		{oracle, "TIMESTAMP(6) WITH TIME ZONE", false},
		{oracle, "TIMESTAMP(6) WITH LOCAL TIME ZONE", false},
		{oracle, "VARCHAR2", false},
		{postgres, "TIMESTAMP", true},
		{postgres, "DATE", true},
		{postgres, "TIMESTAMPTZ", false},
		{postgres, "TEXT", false},
		{mysql, "DATETIME", true},
		{mysql, "TIMESTAMP", true},
		{sqlserver, "DATETIME2", true},
		{sqlserver, "SMALLDATETIME", true},
		{sqlserver, "DATETIMEOFFSET", false},
		{sqlite, "DATETIME", false},
	} {
		if got := c.dialect.zoneless(c.typeName); got != c.want {
			t.Errorf("%s %s: %v", c.dialect.Name, c.typeName, got)
		}
	}
	for _, c := range []struct {
		dialect  Dialect
		typeName string
		want     bool
	}{
		{oracle, "TIMESTAMP(6) WITH TIME ZONE", true},
		{oracle, "TIMESTAMP(6) WITH LOCAL TIME ZONE", true},
		{oracle, "TIMESTAMP(6)", false},
		{oracle, "DATE", false},
		{postgres, "TIMESTAMPTZ", true},
		{postgres, "TIMESTAMP", false},
		{sqlserver, "DATETIMEOFFSET", true},
		{sqlserver, "DATETIME2", false},
		{mysql, "TIMESTAMP", false},
		{sqlite, "TIMESTAMP", false},
	} {
		if got := c.dialect.holdsMoment(c.typeName); got != c.want {
			t.Errorf("%s %s holds a moment: %v", c.dialect.Name, c.typeName, got)
		}
	}
	for _, c := range []struct {
		dialect         Dialect
		typeName, value string
		is, form        string
	}{
		{postgres, "TIMESTAMP", "2026-09-24T07:40:00.5+02:00", "has no time zone", "2026-09-24 07:40:00.5"},
		{postgres, "TIMESTAMP", "2026-09-24 07:40:00", "", ""},
		{postgres, "TIMESTAMPTZ", "2026-09-24 07:40:00", "holds a moment", "2026-09-24T07:40:00Z"},
		{postgres, "TIMESTAMPTZ", "2026-09-24", "holds a moment", "2026-09-24T00:00:00Z"},
		{postgres, "TIMESTAMPTZ", "2026-09-24T07:40:00Z", "", ""},
		{postgres, "TIMESTAMPTZ", "soon", "", ""},
		{postgres, "TEXT", "2026-09-24 07:40:00", "", ""},
		{sqlite, "TIMESTAMP", "2026-09-24T07:40:00Z", "", ""},
	} {
		is, _, form := c.dialect.wrongTime(c.typeName, c.value)
		if is != c.is || form != c.form {
			t.Errorf("%s %s %s: %q, %q", c.dialect.Name, c.typeName, c.value, is, form)
		}
	}
	for s, want := range map[string]bool{
		"2026-09-24T07:40:00+02:00": true, "2026-09-24 07:40:00-05:00": true, "2026-09-24T07:40:00.123Z": true,
		"2026-09-24": false, "2026-09-24 07:40:00": false, "2026-09-24T07:40:00": false, "soon": false,
	} {
		if _, got := offsetTime(s); got != want {
			t.Errorf("%s has an offset: %v", s, got)
		}
	}
}
