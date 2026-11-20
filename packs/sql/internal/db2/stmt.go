package db2

// section names the section of the package a statement runs in
// (PKGNAMCSN): the database, the package's collection and name, each in
// eighteen bytes or with its length when one is longer, the token the package
// was bound with, and the section's number.
func (c *conn) section(n int) []byte {
	parts := [][]byte{[]byte(c.cfg.Database), []byte(packageCollection), []byte(packageID)}
	long := false
	for i, s := range parts {
		if !c.unicode {
			parts[i], _ = toEBCDIC(string(s))
		}
		long = long || len(parts[i]) > 18
	}
	var w buf
	for _, s := range parts {
		if long {
			w.u16(max(len(s), 18))
		}
		w.padded(s, 18, c.space())
	}
	token := []byte(packageToken)
	if !c.unicode {
		token, _ = toEBCDIC(packageToken)
	}
	w.bytes(token)
	w.u16(n)
	return w.b
}

// statement is a statement's text as it travels (SQLSTT, at SQLAM 7 and
// later): nullable mixed-byte text with a four-byte length, then the null of
// the single-byte text it is not.
func statement(sql string) part {
	var w buf
	w.u8(0)
	w.u32(len(sql))
	w.bytes([]byte(sql))
	w.u8(null)
	return objects(cpSQLSTT, w.b)
}

// immediate asks for a statement that gives no rows to be run.
func (c *conn) immediate(sql string) []part {
	var p buf
	p.param(cpPKGNAMCSN, c.section(1))
	p.param8(cpRDBCMTOK, yes)
	return []part{command(cpEXCSQLIMM, &p), statement(sql)}
}

// prepare asks for a statement to be prepared in a section, and for its
// columns.
func (c *conn) prepare(sql string, section int) []part {
	var p buf
	p.param(cpPKGNAMCSN, c.section(section))
	p.param8(cpRTNSQLDA, yes)
	p.param8(cpTYPSQLDA, extendedOutput)
	return []part{command(cpPRPSQLSTT, &p), statement(sql)}
}

// openQuery asks for the rows of the statement prepared in a section.
func (c *conn) openQuery(section int) []part {
	var p buf
	p.param(cpPKGNAMCSN, c.section(section))
	p.param32(cpQRYBLKSZ, blockSize)
	p.param16(cpMAXBLKEXT, -1)
	p.param8(cpQRYCLSIMP, 1)
	return []part{command(cpOPNQRY, &p)}
}

// continueQuery asks for more rows of an open query.
func (c *conn) continueQuery(section int, instance []byte) []part {
	var p buf
	p.param(cpPKGNAMCSN, c.section(section))
	p.param32(cpQRYBLKSZ, blockSize)
	p.param16(cpMAXBLKEXT, -1)
	if len(instance) > 0 {
		p.param(cpQRYINSID, instance)
	}
	p.param8(cpRTNEXTDTA, everyValue)
	return []part{command(cpCNTQRY, &p)}
}

const (
	yes = 0xF1
	// The description of columns with their names and tables.
	extendedOutput = 4
	// Values outside rows arrive with their rows, not when they are asked
	// for.
	everyValue = 2
)
