package db2

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Error is what Db2 says of a statement it refused.
type Error struct {
	// Code is Db2's SQLCODE: -204 for a name it does not know.
	Code     int
	SQLState string
	// Message is Db2's own text when it gave one, and otherwise the code and
	// what Db2 named (the message's tokens).
	Message string
	// Tokens are what the message names: the table, the column.
	Tokens []string
	SQL    string
}

func (e *Error) Error() string { return "db2: " + e.Message }

// sqlca is how a statement ended (the SQL communications area).
type sqlca struct {
	code   int
	state  string
	tokens []string
	// rows is how many rows the statement changed.
	rows int64
	// warnings are the SQLWARN flags.
	warnings string
}

// err is the statement's error, when it ended with one. A positive code is a
// warning and no error: 100 is "no rows".
func (ca *sqlca) err() error {
	if ca == nil || ca.code >= 0 {
		return nil
	}
	return &Error{Code: ca.code, SQLState: ca.state, Tokens: ca.tokens, Message: ca.text()}
}

// text is what this client says of an error whose message it could not ask
// Db2 for.
func (ca *sqlca) text() string {
	msg := fmt.Sprintf("SQL%04dN, SQLSTATE %s", -ca.code, ca.state)
	if ca.code >= 0 {
		msg = fmt.Sprintf("SQL%04dW, SQLSTATE %s", ca.code, ca.state)
	}
	if len(ca.tokens) > 0 {
		msg += ": " + strings.Join(ca.tokens, ", ")
	}
	return msg
}

// sqlca reads an SQLCAGRP, DRDA's early group of an SQL communications area
// at SQLAM 7 and later: null, or the code, the state, the procedure, then two
// nullable groups: its exceptions (six numbers, eleven warnings, the
// database's name and the message's tokens) and its diagnostics.
func (c *conn) sqlca(r *reader) (*sqlca, error) {
	if r.u8() == null {
		return nil, r.err
	}
	ca := &sqlca{code: int(r.i32())}
	ca.state = string(r.take(5))
	r.take(8) // SQLERRPROC: the server's module
	if r.u8() != null {
		var errd [6]int32
		for i := range errd {
			errd[i] = r.i32()
		}
		ca.rows = int64(errd[2])
		ca.warnings = string(r.take(11))
		r.take(r.length()) // SQLRDBNAME
		mixed := r.take(r.length())
		single := r.take(r.length())
		tokens := mixed
		set := c.mbcs
		if len(tokens) == 0 {
			tokens, set = single, c.sbcs
		}
		if len(tokens) > 0 {
			for _, token := range strings.Split(string(tokens), "\xFF") {
				ca.tokens = append(ca.tokens, decode([]byte(token), set))
			}
		}
	}
	if r.u8() != null {
		return nil, fmt.Errorf("%w: diagnostics this client did not ask for", errProtocol)
	}
	return ca, r.err
}

// refusal is the error of a reply message that says a request failed, and
// nil for a message that says no such thing.
func (c *conn) refusal(it item) error {
	if !it.reply {
		return nil
	}
	var what string
	switch it.cp {
	case cpRDBNFNRM:
		return &Error{Code: -1013, SQLState: "42705", Message: fmt.Sprintf("the server has no database %q", c.cfg.Database)}
	case cpRDBATHRM:
		return &Error{Code: -1060, SQLState: "08004", Message: fmt.Sprintf("the user %s may not connect to the database %q", c.cfg.User, c.cfg.Database)}
	case cpRDBAFLRM, cpSQLERRRM, cpOPNQFLRM, cpENDUOWRM, cpRDBUPDRM, cpENDQRYRM, cpOPNQRYRM, cpSECCHKRM, cpACCRDBRM, cpQRYPOPRM:
		// An SQLCARD follows and says why, or nothing failed.
		return nil
	case cpRDBNACRM:
		what = "the database is not open on this connection"
	case cpRDBACCRM:
		what = "the database is already open on this connection"
	case cpABNUOWRM:
		// The unit of work ended abnormally: the SQLCARD that follows says why.
		return nil
	case cpQRYNOPRM:
		what = "the query is not open"
	case cpCMDCHKRM:
		what = "the server could not run the command"
	case cpCMDNSPRM:
		what = "the server does not know the command"
	case cpCMDATHRM:
		what = "the user may not run the command"
	case cpCMDVLTRM:
		what = "the command breaks the conversation's rules"
	case cpAGNPRMRM:
		what = "the server's agent failed"
	case cpRSCLMTRM:
		what = "the server ran out of a resource"
	case cpSYNTAXRM:
		what = "the server could not read the request"
	case cpPRCCNVRM:
		what = "the request breaks the conversation's rules"
	case cpPRMNSPRM:
		what = "the server does not know a parameter"
	case cpVALNSPRM:
		what = "the server does not take a parameter's value"
	case cpOBJNSPRM:
		what = "the server does not know an object"
	case cpTRGNSPRM:
		what = "the server does not know what the command is for"
	case cpMGRLVLRM:
		what = "the server does not speak a manager's level"
	case cpDSCINVRM:
		what = "the server could not read a description of data"
	case cpDTAMCHRM:
		what = "the data does not match its description"
	case cpPKGBNARM:
		what = "the package is being bound"
	case cpCMMRQSRM:
		what = "the server asks for a commit or a rollback"
	default:
		return nil
	}
	ps, err := params(it.data)
	if err != nil {
		return err
	}
	var details []string
	if v := ps[cpSYNERRCD]; len(v) == 1 {
		details = append(details, fmt.Sprintf("syntax error %#02x", v[0]))
	}
	if v := ps[cpPRCCNVCD]; len(v) == 1 {
		details = append(details, fmt.Sprintf("conversation error %#02x", v[0]))
	}
	if v := ps[cpCODPNT]; len(v) == 2 {
		details = append(details, fmt.Sprintf("at code point %#04x", binary.BigEndian.Uint16(v)))
	}
	if v := ps[cpSRVDGN]; len(v) > 0 {
		details = append(details, strings.TrimSpace(c.text(v)))
	}
	if len(details) > 0 {
		what += " (" + strings.Join(details, ", ") + ")"
	}
	return fmt.Errorf("%w: %s", errProtocol, what)
}
