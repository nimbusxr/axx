package h2

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The types of H2's values as a server sends them before each value.
const (
	valNull             = 0
	valBoolean          = 1
	valTinyint          = 2
	valSmallint         = 3
	valInteger          = 4
	valBigint           = 5
	valNumeric          = 6
	valDouble           = 7
	valReal             = 8
	valTime             = 9
	valDate             = 10
	valTimestamp        = 11
	valVarbinary        = 12
	valVarchar          = 13
	valVarcharIgnorecas = 14
	valBlob             = 15
	valClob             = 16
	valArray            = 17
	valJavaObject       = 19
	valUUID             = 20
	valChar             = 21
	valGeometry         = 22
	valTimestampTZ      = 24
	valEnum             = 25
	valInterval         = 26
	valRow              = 27
	valJSON             = 28
	valTimeTZ           = 29
	valBinary           = 30
	valDecfloat         = 31
)

// The types of H2's columns as a server sends them in a result's description.
// Up to the enum they are the values' own; after it they are not.
const (
	colUnknown      = -1
	colIntervalYear = 26 // the thirteen kinds of interval follow each other
	colIntervalLast = 38
	colRow          = 39
	colJSON         = 40
	colTimeTZ       = 41
	colBinary       = 42
	colDecfloat     = 43
)

// intervalKinds are an interval's kinds in H2's order.
var intervalKinds = []string{
	"YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "SECOND", "YEAR TO MONTH", "DAY TO HOUR", "DAY TO MINUTE",
	"DAY TO SECOND", "HOUR TO MINUTE", "HOUR TO SECOND", "MINUTE TO SECOND",
}

// columnType is the type of a result's column.
type columnType struct {
	code      int32
	precision int64 // -1: none declared
	scale     int32 // -1: none declared
	// labels are an enum's, in their order.
	labels []string
	// element is the type of an array's elements, and fields those of a row's.
	element *columnType
	fields  []columnType
}

// name is the type's name as H2 writes it, without its size.
func (c columnType) name() string {
	switch c.code {
	case valNull, colUnknown:
		return "NULL"
	case valBoolean:
		return "BOOLEAN"
	case valTinyint:
		return "TINYINT"
	case valSmallint:
		return "SMALLINT"
	case valInteger:
		return "INTEGER"
	case valBigint:
		return "BIGINT"
	case valNumeric:
		return "NUMERIC"
	case valDouble:
		return "DOUBLE PRECISION"
	case valReal:
		return "REAL"
	case valTime:
		return "TIME"
	case valDate:
		return "DATE"
	case valTimestamp:
		return "TIMESTAMP"
	case valVarbinary:
		return "BINARY VARYING"
	case valVarchar:
		return "CHARACTER VARYING"
	case valVarcharIgnorecas:
		return "VARCHAR_IGNORECASE"
	case valBlob:
		return "BINARY LARGE OBJECT"
	case valClob:
		return "CHARACTER LARGE OBJECT"
	case valArray:
		return "ARRAY"
	case valJavaObject:
		return "JAVA_OBJECT"
	case valUUID:
		return "UUID"
	case valChar:
		return "CHARACTER"
	case valGeometry:
		return "GEOMETRY"
	case valTimestampTZ:
		return "TIMESTAMP WITH TIME ZONE"
	case valEnum:
		return "ENUM"
	case colRow:
		return "ROW"
	case colJSON:
		return "JSON"
	case colTimeTZ:
		return "TIME WITH TIME ZONE"
	case colBinary:
		return "BINARY"
	case colDecfloat:
		return "DECFLOAT"
	}
	if c.code >= colIntervalYear && c.code <= colIntervalLast {
		return "INTERVAL " + intervalKinds[c.code-colIntervalYear]
	}
	return "TYPE " + strconv.Itoa(int(c.code))
}

// readColumnType reads a column's type as the connection's version writes it.
func (c *conn) readColumnType() columnType {
	t := c.wire
	if c.version < 20 {
		return columnType{code: t.readInt(), precision: t.readLong(), scale: t.readInt()}
	}
	ct := columnType{code: t.readInt(), precision: -1, scale: -1}
	switch ct.code {
	case colUnknown, valNull, valBoolean, valTinyint, valSmallint, valInteger, valBigint, valDate, valUUID:
	case valChar, valVarchar, valVarcharIgnorecas, colBinary, valVarbinary, colDecfloat, valJavaObject, colJSON:
		ct.precision = int64(t.readInt())
	case valClob, valBlob:
		ct.precision = t.readLong()
	case valNumeric:
		ct.precision = int64(t.readInt())
		ct.scale = t.readInt()
		t.readBool() // declared as DECIMAL, which holds what NUMERIC does
	case valReal, valDouble:
		ct.precision = int64(int8(t.readByte()))
	case valTime, colTimeTZ, valTimestamp, valTimestampTZ:
		ct.scale = int32(int8(t.readByte()))
	case valEnum:
		for n := t.readInt(); n > 0 && t.err == nil; n-- {
			ct.labels = append(ct.labels, t.str())
		}
	case valGeometry:
		switch kind := t.readByte(); kind {
		case 0:
		case 1:
			t.readShort()
		case 2:
			t.readInt()
		case 3:
			t.readShort()
			t.readInt()
		default:
			t.fail(fmt.Errorf("a geometry's type written as %d, which this client does not know", kind))
		}
	case valArray:
		ct.precision = int64(t.readInt())
		element := c.readColumnType()
		ct.element = &element
	case colRow:
		for n := t.readInt(); n > 0 && t.err == nil; n-- {
			t.str() // the field's name
			ct.fields = append(ct.fields, c.readColumnType())
		}
	default:
		switch k := ct.code - colIntervalYear; {
		case k < 0 || k > colIntervalLast-colIntervalYear:
			t.fail(fmt.Errorf("a column of H2's type %d, which this client does not know", ct.code))
		case k == 5 || k == 9 || k == 11 || k == 12: // those that end in seconds
			ct.precision = int64(int8(t.readByte()))
			ct.scale = int32(int8(t.readByte()))
		default:
			ct.precision = int64(int8(t.readByte()))
		}
	}
	return ct
}

// number is a NUMERIC or a DECFLOAT as H2 writes it: every digit it holds.
type number string

// enumOrdinal is an enum's value before its label is known.
type enumOrdinal struct {
	ordinal int32
	labels  []string
}

// largeObject is a large value the server keeps until it is asked for.
type largeObject struct {
	id     int64
	mac    []byte
	text   bool
	octets int64 // -1: not known
}

// readValue reads a value as what a row gives for it: nil, bool, int64,
// float64, string, []byte or time.Time, or one of the types above, which
// finish (rows.go) turns into those.
func (c *conn) readValue(ct *columnType) any {
	t := c.wire
	switch kind := t.readInt(); kind {
	case valNull:
		return nil
	case valBoolean:
		return t.readBool()
	case valTinyint:
		return int64(int8(t.readByte()))
	case valSmallint:
		if c.version >= 20 {
			return int64(t.readShort())
		}
		return int64(int16(t.readInt()))
	case valInteger:
		return int64(t.readInt())
	case valBigint:
		return t.readLong()
	case valNumeric, valDecfloat:
		return number(t.str())
	case valDouble:
		return t.readDouble()
	case valReal:
		return float64(t.readFloat())
	case valTime:
		return timeOfDay(t.readLong())
	case valTimeTZ:
		return timeOfDay(t.readLong()) + offsetText(int(t.readInt()))
	case valDate:
		return dateOf(t.readLong(), 0, time.UTC)
	case valTimestamp:
		return dateOf(t.readLong(), t.readLong(), time.UTC)
	case valTimestampTZ:
		date, nanos, offset := t.readLong(), t.readLong(), int(t.readInt())
		if c.version < 19 {
			offset *= 60 // minutes there
		}
		return dateOf(date, nanos, time.FixedZone("", offset))
	case valVarbinary, valBinary, valJavaObject, valGeometry:
		if b := t.readBytes(); b != nil {
			return b
		}
		return []byte{}
	case valJSON:
		return string(t.readBytes())
	case valUUID:
		var id [16]byte
		copy(id[:], t.read(16))
		return uuidText(id)
	case valVarchar, valVarcharIgnorecas, valChar:
		return t.str()
	case valEnum:
		ordinal := t.readInt()
		if c.version < 20 {
			return t.str()
		}
		var labels []string
		if ct != nil {
			labels = ct.labels
		}
		return enumOrdinal{ordinal: ordinal, labels: labels}
	case valBlob:
		n := t.readLong()
		if n == -1 {
			t.readInt() // the table that holds it
			lob := largeObject{id: t.readLong(), mac: t.readBytes(), octets: -1}
			lob.octets = t.readLong()
			return lob
		}
		if n < 0 || n > maxLength {
			t.fail(fmt.Errorf("a binary value of %d bytes: this is not an H2 server's answer", n))
			return nil
		}
		b := t.read(int(n))
		c.lobEnd()
		return b
	case valClob:
		n := t.readLong()
		if n == -1 {
			t.readInt()
			lob := largeObject{id: t.readLong(), mac: t.readBytes(), text: true, octets: -1}
			if c.version >= 20 {
				lob.octets = t.readLong()
			}
			t.readLong() // its length in characters
			return lob
		}
		s := t.readChars(n)
		c.lobEnd()
		return s
	case valArray:
		n := t.readInt()
		if n < 0 { // H2 1.4's, with its elements' Java class
			n = ^n
			t.str()
		}
		var element *columnType
		if ct != nil {
			element = ct.element
		}
		list := make(list, 0, max(0, min(int(n), 1024)))
		for ; n > 0 && t.err == nil; n-- {
			list = append(list, c.readValue(element))
		}
		return list
	case valRow:
		n := int(t.readInt())
		list := make(list, 0, max(0, min(n, 1024)))
		for i := 0; i < n && t.err == nil; i++ {
			var field *columnType
			if ct != nil && i < len(ct.fields) {
				field = &ct.fields[i]
			}
			list = append(list, c.readValue(field))
		}
		return list
	case valInterval:
		k := int(int8(t.readByte()))
		negative := k < 0
		if negative {
			k = ^k
		}
		leading, remaining := t.readLong(), int64(0)
		if k >= 5 {
			remaining = t.readLong()
		}
		if k >= len(intervalKinds) {
			t.fail(fmt.Errorf("an interval of H2's kind %d, which this client does not know", k))
			return nil
		}
		return intervalText(k, negative, leading, remaining)
	default:
		t.fail(fmt.Errorf("a value of H2's type %d, which this client does not know", kind))
		return nil
	}
}

// lobEnd reads the mark that ends a large value sent with its row.
func (c *conn) lobEnd() {
	if magic := c.wire.readInt(); magic != 0x1234 {
		c.wire.fail(fmt.Errorf("a large value does not end where it says: this is not an H2 server's answer"))
	}
}

// list is an array's or a row's values.
type list []any

// dateOf is the date H2 packs into a number (its year, month and day in the
// number's bits) at nanos past its midnight.
func dateOf(date, nanos int64, zone *time.Location) time.Time {
	year, month, day := int(date>>9), time.Month(date>>5&15), int(date&31)
	return time.Date(year, month, day, 0, 0, 0, 0, zone).Add(time.Duration(nanos))
}

// timeOfDay is a time of day as H2 writes it: 14:05:09, with the fraction of
// a second it has.
func timeOfDay(nanos int64) string {
	d := time.Duration(nanos)
	s := fmt.Sprintf("%02d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	return s + fraction(nanos%int64(time.Second))
}

// fraction is a second's fraction, as ".25", or nothing for none.
func fraction(nanos int64) string {
	if nanos == 0 {
		return ""
	}
	return "." + strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
}

// offsetText is a zone's offset in seconds as +01:00.
func offsetText(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	s := fmt.Sprintf("%s%02d", sign, seconds/3600)
	if m := seconds % 3600 / 60; m != 0 || seconds%60 != 0 {
		s += fmt.Sprintf(":%02d", m)
	}
	if sec := seconds % 60; sec != 0 {
		s += fmt.Sprintf(":%02d", sec)
	}
	return s
}

func uuidText(id [16]byte) string {
	h := hex.EncodeToString(id[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// intervalText is an interval as H2 writes it: INTERVAL '1-2' YEAR TO MONTH.
// What follows its first field is in remaining: months, hours, minutes, or
// nanoseconds for a kind that ends in seconds.
func intervalText(kind int, negative bool, leading, remaining int64) string {
	const second, minute, hour = int64(time.Second), int64(time.Minute), int64(time.Hour)
	seconds := func(nanos int64) string { return fmt.Sprintf("%02d", nanos/second) + fraction(nanos%second) }
	var body string
	switch kind {
	case 0, 1, 2, 3, 4: // one field
		body = strconv.FormatInt(leading, 10)
	case 5: // SECOND
		body = strconv.FormatInt(leading, 10) + fraction(remaining)
	case 6: // YEAR TO MONTH
		body = fmt.Sprintf("%d-%d", leading, remaining)
	case 7: // DAY TO HOUR
		body = fmt.Sprintf("%d %02d", leading, remaining)
	case 8: // DAY TO MINUTE
		body = fmt.Sprintf("%d %02d:%02d", leading, remaining/60, remaining%60)
	case 9: // DAY TO SECOND
		body = fmt.Sprintf("%d %02d:%02d:%s", leading, remaining/hour, remaining%hour/minute, seconds(remaining%minute))
	case 10: // HOUR TO MINUTE
		body = fmt.Sprintf("%d:%02d", leading, remaining)
	case 11: // HOUR TO SECOND
		body = fmt.Sprintf("%d:%02d:%s", leading, remaining/minute, seconds(remaining%minute))
	case 12: // MINUTE TO SECOND
		body = fmt.Sprintf("%d:%s", leading, seconds(remaining))
	}
	if negative {
		body = "-" + body
	}
	return "INTERVAL '" + body + "' " + intervalKinds[kind]
}

// jsonText is an array's or a row's values as JSON: its numbers as numbers,
// its texts and times as strings.
func jsonText(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return quoted(x)
	case []byte:
		return quoted(hex.EncodeToString(x))
	case time.Time:
		return quoted(x.Format("2006-01-02T15:04:05.999999999Z07:00"))
	case list:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = jsonText(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return quoted(fmt.Sprint(v))
}

// quoted is a string as JSON writes it.
func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
