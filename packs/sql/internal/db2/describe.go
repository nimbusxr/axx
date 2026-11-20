package db2

import (
	"encoding/binary"
	"fmt"
)

// column is one column of a result, as the server describes it (an SQLDAGRP
// and its optional groups).
type column struct {
	name string
	// label is the column's label when it has one, and its name otherwise.
	label string
	// sqlType is Db2's SQLTYPE without its nullable bit: 496 for INTEGER.
	sqlType   int
	nullable  bool
	precision int
	scale     int
	// length is the column's length: bytes or characters, as its type counts.
	length int64
	// ccsid is the character set of the column's text: 0 for bytes.
	ccsid int
	// udt is the name of the column's own type, when it has one.
	udt string
	// table and schema are those of the column's own table, when the server
	// says.
	table, schema string
}

// Db2's SQLTYPEs, without their nullable bit.
const (
	sqlDate        = 384
	sqlTime        = 388
	sqlTimestamp   = 392
	sqlDatalink    = 396
	sqlBlob        = 404
	sqlClob        = 408
	sqlDBClob      = 412
	sqlVarchar     = 448
	sqlChar        = 452
	sqlLongVarchar = 456
	sqlVarGraphic  = 464
	sqlGraphic     = 468
	sqlLongGraphic = 472
	sqlFloat       = 480
	sqlDecimal     = 484
	sqlZoned       = 488
	sqlBigint      = 492
	sqlInteger     = 496
	sqlSmallint    = 500
	sqlNumeric     = 504
	sqlRowID       = 904
	sqlVarBinary   = 908
	sqlBinary      = 912
	sqlBlobLocator = 960
	sqlClobLocator = 964
	sqlDBClobLoc   = 968
	sqlXML         = 988
	sqlDecFloat    = 996
	sqlTimestampTZ = 2448
	sqlBoolean     = 2436
)

// vcs reads a string with a two-byte length (VCS, VCM).
func (r *reader) vcs(ccsid int) string {
	b := r.take(r.length())
	if len(b) == 0 {
		return ""
	}
	return decode(b, ccsid)
}

// mixedOrSingle reads the two forms a name travels in, of which one is
// empty: mixed-byte, then single-byte.
func (c *conn) mixedOrSingle(r *reader) string {
	m := r.vcs(c.mbcs)
	s := r.vcs(c.sbcs)
	if m != "" {
		return m
	}
	return s
}

// describe reads an SQLDARD at SQLAM 7 and later: how the statement ended,
// a header (SQLDHGRP), the number of columns, and each column (SQLDAGRP: its
// precision, scale, length, type and character set, an array's extent, then
// its optional groups: its names, its own type, and where it comes from).
func (c *conn) describe(data []byte) (*sqlca, []column, error) {
	r := &reader{b: data, little: c.little}
	ca, err := c.sqlca(r)
	if err != nil {
		return nil, nil, err
	}
	if ca != nil && ca.code < 0 {
		return ca, nil, nil
	}
	if r.u8() != null {
		// SQLDHGRP: whether the cursor holds, returns, scrolls and is
		// sensitive, the statement's kind, its key's kind, whether the
		// columns are doubled; then the database, the schema and the module.
		r.take(7 * 2)
		r.vcs(c.sbcs)
		c.mixedOrSingle(r)
		c.mixedOrSingle(r)
	}
	n := r.i16()
	if r.err != nil || n < 0 {
		return nil, nil, fmt.Errorf("%w: a description of %d columns", errProtocol, n)
	}
	cols := make([]column, n)
	for i := range cols {
		col := &cols[i]
		col.precision = r.i16()
		col.scale = r.i16()
		col.length = r.i64()
		t := r.i16()
		col.sqlType, col.nullable = t&^1, t&1 == 1
		col.ccsid = int(binary.BigEndian.Uint16(r.take(2)))
		r.take(8) // SQLARRAYEXTENT
		if r.u8() == null {
			continue
		}
		// SQLDOPTGRP: whether the column is unnamed, its name, its label and
		// its comment.
		r.i16()
		col.name = c.mixedOrSingle(r)
		col.label = c.mixedOrSingle(r)
		c.mixedOrSingle(r)
		if col.label == "" {
			col.label = col.name
		}
		if r.u8() != null {
			// SQLUDTGRP: the kind of the column's own type, the database,
			// the schema and the type's name.
			r.take(4)
			r.vcs(c.sbcs)
			c.mixedOrSingle(r)
			col.udt = c.mixedOrSingle(r)
			c.mixedOrSingle(r)
		}
		if r.u8() != null {
			// SQLDXGRP: whether the column is of a key, can be updated, is
			// generated, its parameter's mode, its part in optimistic
			// locking, whether it is hidden; then the database, the
			// correlation name, the table, the schema, the column's own name
			// and its module.
			r.take(6 * 2)
			r.vcs(c.sbcs)
			c.mixedOrSingle(r)
			col.table = c.mixedOrSingle(r)
			col.schema = c.mixedOrSingle(r)
			c.mixedOrSingle(r)
			c.mixedOrSingle(r)
		}
		if r.err != nil {
			return nil, nil, r.err
		}
	}
	if r.err != nil {
		return nil, nil, r.err
	}
	if len(r.b) != 0 {
		return nil, nil, fmt.Errorf("%w: %d bytes after a description of %d columns", errProtocol, len(r.b), n)
	}
	return ca, cols, nil
}
