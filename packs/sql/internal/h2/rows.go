package h2

import (
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"
)

// rows are a query's rows, all read: a statement of the sql pack's selects
// what a scenario looks at.
type rows struct {
	cols []column
	data [][]driver.Value
	next int
}

type column struct {
	name     string
	typ      columnType
	nullable int32
}

// readResult reads a result's description and every one of its rows.
func (c *conn) readResult(id int32) (*rows, error) {
	t := c.wire
	columns := int(t.readInt())
	count := c.readRowCount()
	rs := &rows{}
	for i := 0; i < columns && t.err == nil; i++ {
		col := column{name: t.str()}
		t.str() // its schema
		t.str() // its table
		t.str() // its name in the table
		col.typ = c.readColumnType()
		if c.version < 20 {
			t.readInt() // how wide to show it
		}
		t.readBool() // an identity
		col.nullable = t.readInt()
		rs.cols = append(rs.cols, col)
	}
	if count < 0 { // not counted yet: the rows end with a mark
		count = everyRow
	}
	unfinished := false
	for ; count > 0 && t.err == nil; count-- {
		mark := int8(t.readByte())
		if t.err != nil || mark == 0 {
			break
		}
		if mark != 1 {
			err := c.readError()
			return nil, err
		}
		row := make([]driver.Value, columns)
		for i := range row {
			v := c.readValue(&rs.cols[i].typ)
			switch v.(type) {
			case largeObject, enumOrdinal, list, number:
				unfinished = true
			}
			row[i] = v
		}
		rs.data = append(rs.data, row)
	}
	if t.err != nil {
		return nil, c.broken(t.err)
	}
	if unfinished {
		for _, row := range rs.data {
			for i, v := range row {
				done, err := c.finish(v)
				if err != nil {
					return nil, err
				}
				row[i] = done
			}
		}
	}
	// Nothing answers this: it goes with what is sent next.
	t.writeInt(opResultClose)
	t.writeInt(id)
	return rs, nil
}

// finish turns what readValue could not give at once into what a row gives:
// a large value the server kept, an enum's label, an array as JSON.
func (c *conn) finish(v any) (any, error) {
	v, err := c.resolve(v)
	switch x := v.(type) {
	case number:
		return string(x), err
	case list:
		return jsonText(x), err
	}
	return v, err
}

// resolve is finish for a value that may be in an array, where a number and
// an array stay what they are for the JSON.
func (c *conn) resolve(v any) (any, error) {
	switch x := v.(type) {
	case largeObject:
		b, err := c.readLob(x)
		if err != nil || !x.text {
			return b, err
		}
		return string(b), nil
	case enumOrdinal:
		zero, err := c.enumsFromZero()
		if err != nil {
			return nil, err
		}
		i := int(x.ordinal)
		if !zero {
			i--
		}
		if i < 0 || i >= len(x.labels) {
			return int64(x.ordinal), nil
		}
		return x.labels[i], nil
	case list:
		for i, e := range x {
			done, err := c.resolve(e)
			if err != nil {
				return nil, err
			}
			x[i] = done
		}
	}
	return v, nil
}

// readLob asks the server for a large value it kept.
func (c *conn) readLob(lob largeObject) ([]byte, error) {
	t := c.wire
	var out []byte
	for {
		t.writeInt(opLobRead)
		t.writeLong(lob.id)
		t.writeBytes(lob.mac)
		t.writeLong(int64(len(out)))
		t.writeInt(lobChunk)
		if err := c.done(); err != nil {
			return nil, err
		}
		n := t.readInt()
		if t.err != nil {
			return nil, c.broken(t.err)
		}
		if n <= 0 {
			return orEmpty(out), nil
		}
		out = append(out, t.read(int(n))...)
		if t.err != nil {
			return nil, c.broken(t.err)
		}
		if lob.octets >= 0 && int64(len(out)) >= lob.octets {
			return orEmpty(out), nil
		}
	}
}

func orEmpty(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// enumsFromZero says the database counts an enum's values from zero
// (ZERO_BASED_ENUMS), not from one as H2 2 does unless it is set to.
func (c *conn) enumsFromZero() (bool, error) {
	if c.zeroBasedEnums != nil {
		return *c.zeroBasedEnums, nil
	}
	rs, _, err := c.exec("SELECT SETTING_VALUE FROM INFORMATION_SCHEMA.SETTINGS WHERE SETTING_NAME = 'ZERO_BASED_ENUMS'", true)
	if err != nil {
		return false, err
	}
	zero := len(rs.data) > 0 && strings.EqualFold(fmt.Sprint(rs.data[0][0]), "true")
	c.zeroBasedEnums = &zero
	return zero, nil
}

func (r *rows) Columns() []string {
	names := make([]string, len(r.cols))
	for i, c := range r.cols {
		names[i] = c.name
	}
	return names
}

func (r *rows) Close() error { return nil }

func (r *rows) Next(dest []driver.Value) error {
	if r.next >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.next])
	r.next++
	return nil
}

// ColumnTypeDatabaseTypeName is the column's type as H2 names it, without
// its size: CHARACTER VARYING, NUMERIC, TIMESTAMP WITH TIME ZONE.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string { return r.cols[i].typ.name() }

// ColumnTypePrecisionScale is a NUMERIC's precision and scale.
func (r *rows) ColumnTypePrecisionScale(i int) (precision, scale int64, ok bool) {
	t := r.cols[i].typ
	if t.code != valNumeric || t.precision < 0 {
		return 0, 0, false
	}
	return t.precision, int64(max(t.scale, 0)), true
}

// ColumnTypeLength is how long a text or a binary column's values can be.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	t := r.cols[i].typ
	if !slices.Contains([]int32{valChar, valVarchar, valVarcharIgnorecas, valVarbinary, colBinary}, t.code) || t.precision < 0 {
		return 0, false
	}
	return t.precision, true
}

func (r *rows) ColumnTypeNullable(i int) (nullable, ok bool) {
	switch r.cols[i].nullable {
	case 0:
		return false, true
	case 1:
		return true, true
	}
	return false, false
}

var (
	typeString = reflect.TypeFor[string]()
	typeBytes  = reflect.TypeFor[[]byte]()
	typeInt    = reflect.TypeFor[int64]()
	typeFloat  = reflect.TypeFor[float64]()
	typeBool   = reflect.TypeFor[bool]()
	typeTime   = reflect.TypeFor[time.Time]()
)

func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	switch r.cols[i].typ.code {
	case valBoolean:
		return typeBool
	case valTinyint, valSmallint, valInteger, valBigint:
		return typeInt
	case valDouble, valReal:
		return typeFloat
	case valDate, valTimestamp, valTimestampTZ:
		return typeTime
	case valVarbinary, colBinary, valBlob, valJavaObject, valGeometry:
		return typeBytes
	}
	return typeString
}
