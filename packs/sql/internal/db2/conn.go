// Package db2 is a client of Db2, for database/sql.
//
// Db2's servers speak DRDA, the Distributed Relational Database Architecture,
// a standard The Open Group publishes (DRDA, Version 5: its three volumes,
// DRDA, FD:OCA and DDM). This package speaks it in Go, written from the
// standard and tried against Db2 for Linux, UNIX and Windows. It runs
// statements as they are written, with no parameters, which is all the sql
// pack sends, and gives each value as what it is: see readValue.
package db2

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultPort = "50000"
	dialTimeout = 30 * time.Second
	// The levels of DRDA's managers this client speaks.
	levelAgent  = 10
	levelSQLAM  = 11
	levelRDB    = 12
	levelSecMgr = 9
	levelTCPIP  = 5
	ccsidUTF8   = 1208
	ccsidUTF16  = 1200
	// The product this client says it is: a name of its own, three letters
	// and a version, as DRDA lays a product's identity out.
	productID = "AXX01000"
	// The package of dynamic statements every Db2 database has, at cursor
	// stability, and the token it was bound with.
	packageCollection = "NULLID"
	packageID         = "SYSSH200"
	packageToken      = "SYSLVL01"
	// The size of a block of rows this client asks for.
	blockSize = 65535
)

// Config is where a Db2 database is and how to open a connection to it.
type Config struct {
	// Addr is the server's host and port.
	Addr string
	// TLS is for a port that speaks TLS (sslConnection=true).
	TLS bool
	// RootCAs are the authorities a server's certificate is held to: the
	// machine's when nil.
	RootCAs *x509.CertPool
	// Database is the database's name.
	Database string
	User     string
	Password string
	// Schema is the schema names without one are looked up in
	// (currentSchema): the user's own when empty.
	Schema string
	// Mechanism is how the user is proved (securityMechanism): 3, a user and
	// a password as they are; 7, the password encrypted; 9, both encrypted.
	// When 0, it is as they are for a server that takes them so, and
	// encrypted for one that does not.
	Mechanism int
}

// Connector opens connections to one database.
type Connector struct {
	cfg Config
	// wrap, when set, stands between a connection and the network, for this
	// package's tests.
	wrap func(net.Conn) net.Conn
}

// NewConnector is a connector for database/sql's OpenDB.
func NewConnector(cfg Config) *Connector { return &Connector{cfg: cfg} }

// Driver is the connector's driver, which opens nothing by a name.
func (c *Connector) Driver() driver.Driver { return drv{} }

type drv struct{}

func (drv) Open(string) (driver.Conn, error) {
	return nil, errors.New("db2: open a database with NewConnector")
}

// Connect opens a connection.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	return open(ctx, c.cfg, c.wrap)
}

func dial(ctx context.Context, cfg Config) (net.Conn, error) {
	addr := cfg.Addr
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, defaultPort)
	}
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	d := &net.Dialer{}
	if !cfg.TLS {
		return d.DialContext(ctx, "tcp", addr)
	}
	host, _, _ := net.SplitHostPort(addr)
	td := &tls.Dialer{NetDialer: d, Config: &tls.Config{
		ServerName: host,
		RootCAs:    cfg.RootCAs,
		MinVersion: tls.VersionTLS12,
	}}
	nc, err := td.DialContext(ctx, "tcp", addr)
	var untrusted *tls.CertificateVerificationError
	if errors.As(err, &untrusted) {
		return nil, fmt.Errorf("db2: the certificate of the server at %s is not one this machine trusts: %w. "+
			"Give the URL the certificates to hold it to, a file of them in PEM: sslCertLocation=/path/to/ca.pem", addr, err)
	}
	return nc, err
}

func open(ctx context.Context, cfg Config, wrap func(net.Conn) net.Conn) (*conn, error) {
	nc, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if wrap != nil {
		nc = wrap(nc)
	}
	c := newConn(cfg, nc)
	if deadline, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(deadline)
	} else {
		_ = nc.SetDeadline(time.Now().Add(dialTimeout))
	}
	if err := c.handshake(); err != nil {
		_ = nc.Close()
		return nil, err
	}
	if cfg.Schema != "" {
		if _, err := c.update("SET CURRENT SCHEMA = " + quote(cfg.Schema)); err != nil {
			_ = nc.Close()
			return nil, err
		}
	}
	_ = nc.SetDeadline(time.Time{})
	return c, nil
}

func newConn(cfg Config, nc net.Conn) *conn {
	return &conn{cfg: cfg, nc: nc, r: bufio.NewReaderSize(nc, 64<<10)}
}

type conn struct {
	cfg Config
	nc  net.Conn
	r   *bufio.Reader
	// unicode says the two sides agreed that names travel in UTF-8, not in
	// EBCDIC.
	unicode bool
	// little says the server's numbers are little-endian (a Db2 on x86).
	little bool
	// The server's character sets: of single-byte, mixed and double-byte
	// text.
	sbcs, mbcs, dbcs int
	// sqlam is the level of the SQL application manager the server speaks.
	sqlam int
	// product is the server's (SQL12015: Db2 12.1.5).
	product string
	// interrupt is the token a request of this connection is interrupted by.
	interrupt []byte
	closed    bool
	inTx      bool
	// asking says the connection is asking Db2 for a message's text.
	asking bool
	// interrupted says the statement that runs was asked to stop.
	interrupted atomic.Bool
}

func (c *conn) write(b []byte) error {
	_, err := c.nc.Write(b)
	return err
}

// read reads a whole chain of replies.
func (c *conn) read() ([]item, error) {
	var all []item
	for {
		items, chained, err := readDSS(c.r)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if !chained {
			return all, nil
		}
	}
}

// name is a name as the two sides agreed names travel.
func (c *conn) name(s string) ([]byte, error) {
	if c.unicode {
		return []byte(s), nil
	}
	b, ok := toEBCDIC(s)
	if !ok {
		return nil, fmt.Errorf("db2: %q has characters this server takes only in Unicode, which it does not speak", s)
	}
	return b, nil
}

func (c *conn) space() byte {
	if c.unicode {
		return ' '
	}
	return 0x40
}

// hostname is the machine's name, which a server shows beside a connection.
var hostname = os.Hostname

func (c *conn) handshake() error {
	if err := c.attributes(); err != nil {
		return err
	}
	return c.logIn()
}

// attributes exchanges what the two sides are and the managers they speak.
func (c *conn) attributes() error {
	host, _ := hostname()
	if host == "" {
		host = "localhost"
	}
	var p buf
	for _, attr := range []struct {
		cp    int
		value string
	}{{cpEXTNAM, "axx"}, {cpSRVNAM, host}, {cpSRVRLSLV, productID}, {cpSRVCLSNM, "axx"}} {
		b, _ := toEBCDIC(attr.value)
		p.param(attr.cp, b)
	}
	var levels buf
	for _, m := range [][2]int{
		{mgrAGENT, levelAgent},
		{mgrSQLAM, levelSQLAM},
		{mgrCMNTCPIP, levelTCPIP},
		{mgrRDB, levelRDB},
		{mgrSECMGR, levelSecMgr},
		{mgrCCSIDMGR, ccsidUTF8},
	} {
		levels.u16(m[0])
		levels.u16(m[1])
	}
	p.param(cpMGRLVLLS, levels.b)
	if err := c.write(chain([]part{command(cpEXCSAT, &p)})); err != nil {
		return err
	}
	items, err := c.read()
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.cp != cpEXCSATRD {
			if err := c.refusal(it); err != nil {
				return err
			}
			continue
		}
		ps, err := params(it.data)
		if err != nil {
			return err
		}
		lv := ps[cpMGRLVLLS]
		for ; len(lv) >= 4; lv = lv[4:] {
			mgr, level := int(binary.BigEndian.Uint16(lv)), int(binary.BigEndian.Uint16(lv[2:]))
			switch mgr {
			case mgrUNICODEMGR, mgrCCSIDMGR:
				c.unicode = level == ccsidUTF8
			case mgrSQLAM:
				c.sqlam = level
			}
		}
	}
	if c.sqlam < 7 {
		return fmt.Errorf("db2: the server speaks level %d of DRDA's SQL application manager, and this client needs 7 or later", c.sqlam)
	}
	return nil
}

func (c *conn) logIn() error {
	database, err := c.name(c.cfg.Database)
	if err != nil {
		return err
	}
	user, err := c.name(c.cfg.User)
	if err != nil {
		return err
	}
	password, err := c.name(c.cfg.Password)
	if err != nil {
		return err
	}
	var rdbnam buf
	rdbnam.padded(database, 18, c.space())

	// The server says which ways it takes a user by: as they are when it
	// takes them so, and otherwise encrypted, the user too when it can be. A
	// URL that names a way gets that one.
	mechanism := c.cfg.Mechanism
	if mechanism == 0 || mechanism == secUserPassword {
		offered, _, err := c.accessSecurity(secUserPassword, rdbnam.b, nil)
		if err != nil {
			return err
		}
		switch {
		case offers(offered, secUserPassword):
			mechanism = secUserPassword
		case mechanism == secUserPassword:
			return fmt.Errorf("db2: the server does not take a user and a password as they are, which the URL's securityMechanism=3 asks for (it offers the security mechanisms %s)", mechanisms(offered))
		default:
			for _, m := range encrypted {
				if offers(offered, m) {
					mechanism = m
					break
				}
			}
			if mechanism == 0 {
				return fmt.Errorf("db2: the server takes a user in none of the ways this client speaks, a user and a password as they are or encrypted (it offers the security mechanisms %s)", mechanisms(offered))
			}
		}
	}
	var chk buf
	if mechanism == secUserPassword {
		chk.param16(cpSECMEC, mechanism)
		chk.param(cpRDBNAM, rdbnam.b)
		chk.param(cpUSRID, user)
		chk.param(cpPASSWORD, password)
	} else {
		ours, err := newAgreement()
		if err != nil {
			return err
		}
		accepted, theirs, err := c.accessSecurity(mechanism, rdbnam.b, ours.public())
		if err != nil {
			return err
		}
		if !offers(accepted, mechanism) {
			return fmt.Errorf("db2: the server does not take security mechanism %d (it offers %s)", mechanism, mechanisms(accepted))
		}
		if len(theirs) == 0 {
			// It takes the mechanism and gives no key for the standard's cipher.
			return errors.New("db2: the server takes a password encrypted with another cipher than the one this client speaks, the standard's DES (a Db2 whose ALTERNATE_AUTH_ENC is AES_ONLY): connect over TLS to a server that takes a user and a password as they are")
		}
		key, err := ours.key(theirs)
		if err != nil {
			return err
		}
		// Under the cipher, a user and a password are in EBCDIC, whatever
		// names travel in.
		who, ok := toEBCDIC(c.cfg.User)
		word, ok2 := toEBCDIC(c.cfg.Password)
		if !ok || !ok2 {
			return errors.New("db2: the server takes a user and a password encrypted, in EBCDIC, where this one has a character with no place: connect over TLS to a server that takes them as they are")
		}
		from := token(mechanism, who, theirs)
		secret, err := encrypt(key, from, word)
		if err != nil {
			return err
		}
		chk.param16(cpSECMEC, mechanism)
		chk.param(cpRDBNAM, rdbnam.b)
		if mechanism == secEncryptedUserPassword {
			hidden, err := encrypt(key, from, who)
			if err != nil {
				return err
			}
			chk.param(cpSECTKN, hidden)
		} else {
			chk.param(cpUSRID, user)
		}
		chk.param(cpSECTKN, secret)
	}

	var acc buf
	acc.param(cpRDBNAM, rdbnam.b)
	acc.param16(cpRDBACCCL, mgrSQLAM)
	product, _ := c.name(productID)
	acc.param(cpPRDID, product)
	typdef, _ := c.name("QTDSQLASC")
	acc.param(cpTYPDEFNAM, typdef)
	var ovr buf
	ovr.param16(cpCCSIDSBC, ccsidUTF8)
	ovr.param16(cpCCSIDDBC, ccsidUTF16)
	ovr.param16(cpCCSIDMBC, ccsidUTF8)
	acc.param(cpTYPDEFOVR, ovr.b)

	if err := c.write(chain([]part{command(cpSECCHK, &chk)}, []part{command(cpACCRDB, &acc)})); err != nil {
		return err
	}
	items, err := c.read()
	if err != nil {
		return err
	}
	accessed := false
	for _, it := range items {
		switch it.cp {
		case cpSECCHKRM:
			ps, err := params(it.data)
			if err != nil {
				return err
			}
			if code := ps[cpSECCHKCD]; len(code) > 0 && code[0] != secOK {
				return &Error{SQLState: "28000", Code: -30082, Message: "the server refused the user " + c.cfg.User + ": " + refused(code[0])}
			}
		case cpACCRDBRM:
			ps, err := params(it.data)
			if err != nil {
				return err
			}
			accessed = true
			c.product = c.text(ps[cpPRDID])
			c.little = strings.TrimSpace(c.text(ps[cpTYPDEFNAM])) == "QTDSQLX86"
			c.interrupt = ps[cpRDBINTTKN]
			ccsids, err := params(ps[cpTYPDEFOVR])
			if err != nil {
				return err
			}
			c.sbcs, c.mbcs, c.dbcs = ccsid(ccsids[cpCCSIDSBC]), ccsid(ccsids[cpCCSIDMBC]), ccsid(ccsids[cpCCSIDDBC])
		case cpSQLCARD:
			ca, err := c.sqlca(&reader{b: it.data, little: c.little})
			if err != nil {
				return err
			}
			if err := ca.err(); err != nil {
				return err
			}
		default:
			if err := c.refusal(it); err != nil {
				return err
			}
		}
	}
	if !accessed {
		return fmt.Errorf("%w: it did not say the database is open", errProtocol)
	}
	return nil
}

// encrypted are the security mechanisms this client speaks for a server
// that takes no password as it is, the one it would rather have first: the
// user encrypted too.
var encrypted = []int{secEncryptedUserPassword, secEncryptedPassword}

// accessSecurity asks the server to take a user by a security mechanism,
// with this side's key for one that encrypts, and gives the mechanisms the
// server answers with (the one asked for when it takes it, and otherwise
// those it takes) and its own key.
func (c *conn) accessSecurity(mechanism int, rdbnam, key []byte) (mechs, serverKey []byte, err error) {
	var sec buf
	sec.param16(cpSECMEC, mechanism)
	sec.param(cpRDBNAM, rdbnam)
	if key != nil {
		sec.param(cpSECTKN, key)
	}
	if err := c.write(chain([]part{command(cpACCSEC, &sec)})); err != nil {
		return nil, nil, err
	}
	items, err := c.read()
	if err != nil {
		return nil, nil, err
	}
	for _, it := range items {
		if it.cp != cpACCSECRD {
			if err := c.refusal(it); err != nil {
				return nil, nil, err
			}
			continue
		}
		ps, err := params(it.data)
		if err != nil {
			return nil, nil, err
		}
		mechs, serverKey = ps[cpSECMEC], ps[cpSECTKN]
	}
	return mechs, serverKey, nil
}

func ccsid(b []byte) int {
	if len(b) != 2 {
		return 0
	}
	return int(binary.BigEndian.Uint16(b))
}

// text reads a name of a reply.
func (c *conn) text(b []byte) string {
	if c.unicode {
		return string(b)
	}
	return fromEBCDIC(b)
}

func offers(mechs []byte, want int) bool {
	for ; len(mechs) >= 2; mechs = mechs[2:] {
		if int(binary.BigEndian.Uint16(mechs)) == want {
			return true
		}
	}
	return false
}

func mechanisms(mechs []byte) string {
	var out []string
	for ; len(mechs) >= 2; mechs = mechs[2:] {
		out = append(out, fmt.Sprint(binary.BigEndian.Uint16(mechs)))
	}
	return strings.Join(out, ", ")
}

func refused(code byte) string {
	switch code {
	case secMechUnsupported:
		return "it does not take a user and a password as they are"
	case secPasswordExpired:
		return "the password has expired"
	case secPasswordInvalid:
		return "the password is not the user's"
	case secPasswordMissing:
		return "there is no password"
	case secUserMissing:
		return "there is no user"
	case secUserInvalid:
		return "it knows no such user"
	case secUserRevoked:
		return "the user is revoked"
	case secNewPassword:
		return "the new password is not one it takes"
	case secLocalFailed:
		return "its own security service failed"
	default:
		return fmt.Sprintf("security check code 0x%02X", code)
	}
}

// Close ends the connection.
func (c *conn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	return c.nc.Close()
}
