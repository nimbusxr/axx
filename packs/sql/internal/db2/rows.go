package db2

import (
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

// rows are a query's rows, all read.
type rows struct {
	cols []column
	data [][]driver.Value
	at   int
}

// rows reads the rows of a result: each an SQL communications area, null
// when the row has nothing to say, a byte that says whether a row follows,
// and the row's fields; then the large values, which follow the rows in
// their order.
func (c *conn) rows(res *result) (*rows, error) {
	rs := &rows{cols: res.cols}
	if !res.opened {
		return rs, nil
	}
	fields, err := layout(res.layout)
	if err != nil {
		return nil, err
	}
	if res.described && len(fields) != len(res.cols) {
		return nil, fmt.Errorf("%w: rows of %d fields for %d columns", errProtocol, len(fields), len(res.cols))
	}
	if !res.described {
		rs.cols = make([]column, len(fields))
	}
	for i, f := range fields {
		if f.ccsid == 0 && !readable(c.fieldSet(f, &rs.cols[i])) {
			return nil, unreadable(c.fieldSet(f, &rs.cols[i]))
		}
	}
	r := &reader{b: res.data, little: c.little}
	outside := res.outside
	for len(r.b) > 0 {
		ca, err := c.sqlca(r)
		if err != nil {
			return nil, err
		}
		if err := ca.err(); err != nil {
			return nil, err
		}
		if r.u8() == null {
			continue
		}
		row := make([]driver.Value, len(fields))
		for i, f := range fields {
			v, err := c.value(r, f, &rs.cols[i])
			if err != nil {
				return nil, err
			}
			row[i] = v
		}
		if r.err != nil {
			return nil, r.err
		}
		for i, v := range row {
			ref, ok := v.(lobRef)
			if !ok {
				continue
			}
			row[i], outside, err = c.large(ref, &rs.cols[i], outside)
			if err != nil {
				return nil, err
			}
		}
		rs.data = append(rs.data, row)
	}
	return rs, nil
}

// fieldSet is the character set a field's text is in.
func (c *conn) fieldSet(f field, col *column) int {
	if col.ccsid != 0 && col.ccsid != 65535 {
		return col.ccsid
	}
	switch f.typ {
	case tChar, tVarchar, tLongVarchar, tClobSBCS:
		return c.sbcs
	case tMixed, tVarMixed, tLongMixed, tClobMixed:
		return c.mbcs
	case tGraphic, tVarGraphic, tLongGraphic, tDBClob:
		return c.dbcs
	}
	return 0
}

// large is a large value, taken off the values that follow the rows.
func (c *conn) large(ref lobRef, col *column, outside [][]byte) (driver.Value, [][]byte, error) {
	if ref.length == 0 {
		if ref.field.typ == tBlob {
			return []byte{}, outside, nil
		}
		return "", outside, nil
	}
	if len(outside) == 0 {
		return nil, nil, fmt.Errorf("%w: a large value that does not follow its row", errProtocol)
	}
	b := outside[0]
	outside = outside[1:]
	if ref.field.nullable {
		// A nullable value's first byte says whether it is null.
		if len(b) == 0 {
			return nil, nil, fmt.Errorf("%w: an empty large value", errProtocol)
		}
		if b[0] == null {
			return nil, outside, nil
		}
		b = b[1:]
	}
	switch ref.field.typ {
	case tBlob:
		return append([]byte{}, b...), outside, nil
	case tDBClob:
		return decode(b, c.fieldSet(ref.field, col)), outside, nil
	case tXML, tXMLInternal, tXMLExternal:
		return xmlText(b), outside, nil
	default:
		return decode(b, c.fieldSet(ref.field, col)), outside, nil
	}
}

// xmlText is an XML value's text, without the declaration Db2 leads it with.
func xmlText(b []byte) string {
	s := string(b)
	if strings.HasPrefix(s, "<?xml") {
		if end := strings.Index(s, "?>"); end >= 0 {
			s = s[end+2:]
		}
	}
	return s
}

func (r *rows) Columns() []string {
	names := make([]string, len(r.cols))
	for i, c := range r.cols {
		names[i] = c.label
	}
	return names
}

func (r *rows) Close() error { return nil }

func (r *rows) Next(dest []driver.Value) error {
	if r.at >= len(r.data) {
		return io.EOF
	}
	for i, v := range r.data[r.at] {
		if d, ok := v.(decimal); ok {
			v = string(d)
		}
		dest[i] = v
	}
	r.at++
	return nil
}

// ColumnTypeDatabaseTypeName is the column's type as Db2 names it.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string { return r.cols[i].typeName() }

// ColumnTypePrecisionScale is a number's precision and scale.
func (r *rows) ColumnTypePrecisionScale(i int) (precision, scale int64, ok bool) {
	c := r.cols[i]
	switch c.sqlType {
	case sqlDecimal, sqlNumeric, sqlZoned:
		return int64(c.precision), int64(c.scale), true
	}
	return 0, 0, false
}

// ColumnTypeLength is the length of a column of text or bytes.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	c := r.cols[i]
	switch c.sqlType {
	case sqlChar, sqlVarchar, sqlLongVarchar, sqlGraphic, sqlVarGraphic, sqlLongGraphic,
		sqlBinary, sqlVarBinary, sqlBlob, sqlClob, sqlDBClob:
		return c.length, true
	}
	return 0, false
}

// ColumnTypeNullable says whether the column can be null.
func (r *rows) ColumnTypeNullable(i int) (nullable, ok bool) { return r.cols[i].nullable, true }

var (
	typeInt64   = reflect.TypeOf(int64(0))
	typeFloat64 = reflect.TypeOf(float64(0))
	typeBool    = reflect.TypeOf(false)
	typeString  = reflect.TypeOf("")
	typeTime    = reflect.TypeOf(time.Time{})
	typeBytes   = reflect.TypeOf([]byte(nil))
)

// ColumnTypeScanType is the Go type the column's values arrive as.
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	c := r.cols[i]
	switch c.sqlType {
	case sqlSmallint, sqlInteger, sqlBigint:
		return typeInt64
	case sqlFloat:
		return typeFloat64
	case sqlBoolean:
		return typeBool
	case sqlDate, sqlTimestamp:
		return typeTime
	case sqlBinary, sqlVarBinary, sqlBlob, sqlRowID:
		return typeBytes
	case sqlChar, sqlVarchar, sqlLongVarchar:
		if c.bits() {
			return typeBytes
		}
	}
	return typeString
}

// typeName is the column's type as Db2 names it.
func (c column) typeName() string {
	bits := c.bits()
	switch c.sqlType {
	case sqlSmallint:
		return "SMALLINT"
	case sqlInteger:
		return "INTEGER"
	case sqlBigint:
		return "BIGINT"
	case sqlFloat:
		if c.length == 4 {
			return "REAL"
		}
		return "DOUBLE"
	case sqlDecimal, sqlNumeric, sqlZoned:
		return "DECIMAL"
	case sqlDecFloat:
		return "DECFLOAT"
	case sqlBoolean:
		return "BOOLEAN"
	case sqlDate:
		return "DATE"
	case sqlTime:
		return "TIME"
	case sqlTimestamp:
		return "TIMESTAMP"
	case sqlTimestampTZ:
		return "TIMESTAMP WITH TIME ZONE"
	case sqlChar:
		if bits {
			return "CHAR FOR BIT DATA"
		}
		return "CHAR"
	case sqlVarchar, sqlLongVarchar:
		if bits {
			return "VARCHAR FOR BIT DATA"
		}
		return "VARCHAR"
	case sqlGraphic:
		return "GRAPHIC"
	case sqlVarGraphic, sqlLongGraphic:
		return "VARGRAPHIC"
	case sqlBinary:
		return "BINARY"
	case sqlVarBinary:
		return "VARBINARY"
	case sqlBlob, sqlBlobLocator:
		return "BLOB"
	case sqlClob, sqlClobLocator:
		return "CLOB"
	case sqlDBClob, sqlDBClobLoc:
		return "DBCLOB"
	case sqlXML:
		return "XML"
	case sqlRowID:
		return "ROWID"
	case sqlDatalink:
		return "DATALINK"
	}
	return fmt.Sprintf("SQLTYPE %d", c.sqlType)
}
