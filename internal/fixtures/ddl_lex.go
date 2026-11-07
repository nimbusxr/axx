package fixtures

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sqlToken is one token of a SQL statement. Comments and whitespace are not tokens.
type sqlToken struct {
	kind sqlKind
	text string // as written; a quoted identifier or a string without its quotes
}

type sqlKind int

const (
	sqlWord   sqlKind = iota // a keyword or an unquoted identifier: CREATE, parcels
	sqlQuoted                // a quoted identifier: "order", `order`, [order]
	sqlString                // a string: 'it''s', E'it\'s', N'x', $$x$$, q'[x]'
	sqlNumber
	sqlSymbol // one character of punctuation or an operator
)

// is reports whether the token is the keyword word.
func (t sqlToken) is(word string) bool { return t.kind == sqlWord && strings.EqualFold(t.text, word) }

func (t sqlToken) isSymbol(s string) bool { return t.kind == sqlSymbol && t.text == s }

// isName reports whether the token can name a table or a column.
func (t sqlToken) isName() bool { return t.kind == sqlWord || t.kind == sqlQuoted }

var (
	// sqlGoLine ends a SQL Server batch; sqlSlashLine ends an Oracle (SQL*Plus) block.
	sqlGoLine    = regexp.MustCompile(`(?i)^[ \t]*GO(?:[ \t]+\d+)?[ \t]*(?:--.*)?\r?$`)
	sqlSlashLine = regexp.MustCompile(`^[ \t]*/[ \t]*\r?$`)
	// sqlBlockLines finds either in a file: its routines run to one, not to a ";".
	sqlBlockLines = regexp.MustCompile(`(?im)^[ \t]*(?:GO(?:[ \t]+\d+)?|/)[ \t]*\r?$`)
	// sqlDelimiterLine is the mysql client's DELIMITER, which changes what ends a statement.
	sqlDelimiterLine = regexp.MustCompile(`(?i)^[ \t]*DELIMITER[ \t]+(\S+)`)
)

// sqlLexer splits SQL text into statements of tokens.
type sqlLexer struct {
	src       string
	i         int
	mysql     bool // backslash escapes, # comments, "strings", and /*! comments that are SQL
	delimiter string
	blocks    bool // the file has GO or "/" lines: a routine runs to one
	versioned int  // the MySQL /*! comments open here, whose text is SQL
	stmt      []sqlToken
	out       [][]sqlToken
}

// splitSQL splits a SQL file into its statements, each a list of tokens, leaving out comments,
// psql's meta-commands (\restrict) and the mysql client's DELIMITER lines. A statement ends at
// a ";" (or the delimiter DELIMITER set), a GO line (SQL Server) or a "/" line (SQL*Plus), and
// before a line that starts another (SQL Server's need no ";"). A routine or trigger runs on
// past the ";" in its body: to the END that closes its BEGIN, or to the file's next GO or "/"
// line when it has them.
func splitSQL(src string, mysql bool) [][]sqlToken {
	src = strings.TrimPrefix(src, "\uFEFF") // a byte order mark
	l := &sqlLexer{src: src, mysql: mysql, delimiter: ";", blocks: sqlBlockLines.MatchString(src)}
	lineStart, lineFirst := true, true
	for l.i < len(l.src) {
		if lineStart && l.line() {
			continue
		}
		lineStart = false
		c := l.src[l.i]
		rest := l.src[l.i:]
		if lineFirst && c > ' ' && !strings.HasPrefix(rest, "--") && !strings.HasPrefix(rest, "/*") {
			lineFirst = false
			if len(l.stmt) > 0 && !isRoutine(l.stmt) && startsStatement(rest) {
				l.end()
			}
		}
		switch {
		case c == '\n':
			lineStart, lineFirst = true, true
			l.i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.i++
		case l.atDelimiter():
			l.end()
			l.i += len(l.delimiter)
		case c == ';' && l.delimiter == ";":
			l.i++
			if l.inBody() {
				l.add(sqlSymbol, ";")
			} else {
				l.end()
			}
		case strings.HasPrefix(rest, "--") && (!l.mysql || len(rest) == 2 || rest[2] <= ' '):
			l.skipLine()
		case c == '#' && l.mysql:
			l.skipLine()
		case strings.HasPrefix(rest, "/*!") && l.mysql:
			l.i += 3
			for l.i < len(l.src) && l.src[l.i] >= '0' && l.src[l.i] <= '9' {
				l.i++
			}
			l.versioned++
		case strings.HasPrefix(rest, "/*"):
			if end := strings.Index(rest[2:], "*/"); end >= 0 {
				l.i += end + 4
			} else {
				l.i = len(l.src)
			}
		case strings.HasPrefix(rest, "*/") && l.versioned > 0:
			l.versioned--
			l.i += 2
		case c == '\'':
			l.add(sqlString, l.quoted('\'', l.mysql))
		case c == '"' && l.mysql:
			l.add(sqlString, l.quoted('"', true))
		case c == '"', c == '`':
			l.add(sqlQuoted, l.quoted(c, false))
		case c == '[' && l.bracketQuotes():
			l.add(sqlQuoted, l.quoted(']', false))
		case c == '$' && !l.mysql && l.dollarTag() != "":
			l.add(sqlString, l.dollarQuoted())
		case c >= '0' && c <= '9':
			start := l.i
			for l.i < len(l.src) && (isWordByte(l.src[l.i]) || l.src[l.i] == '.') && !l.atDelimiter() {
				l.i++
			}
			l.add(sqlNumber, l.src[start:l.i])
		default:
			r, n := utf8.DecodeRuneInString(rest)
			if l.wordStart(r) {
				l.word()
			} else {
				l.add(sqlSymbol, string(r))
				l.i += n
			}
		}
	}
	l.end()
	return l.out
}

// line takes a line that is not SQL, reporting whether it did: a psql meta-command, a
// DELIMITER, or a GO or "/" that ends the statement.
func (l *sqlLexer) line() bool {
	n := strings.IndexByte(l.src[l.i:], '\n')
	if n < 0 {
		n = len(l.src) - l.i
	}
	line := l.src[l.i : l.i+n]
	if m := sqlDelimiterLine.FindStringSubmatch(line); m != nil {
		l.end()
		l.delimiter = m[1]
	} else if sqlGoLine.MatchString(line) || sqlSlashLine.MatchString(line) {
		l.end()
	} else if !strings.HasPrefix(strings.TrimLeft(line, " \t"), `\`) {
		return false
	}
	l.i += n
	return true
}

// atDelimiter reports whether the delimiter a DELIMITER line set is at the cursor: it ends a
// statement even right after a word (END$$).
func (l *sqlLexer) atDelimiter() bool {
	return l.delimiter != ";" && strings.HasPrefix(l.src[l.i:], l.delimiter)
}

func (l *sqlLexer) add(kind sqlKind, text string) { l.stmt = append(l.stmt, sqlToken{kind, text}) }

func (l *sqlLexer) end() {
	if len(l.stmt) > 0 {
		l.out = append(l.out, l.stmt)
	}
	l.stmt = nil
}

func (l *sqlLexer) skipLine() {
	if n := strings.IndexByte(l.src[l.i:], '\n'); n >= 0 {
		l.i += n
	} else {
		l.i = len(l.src)
	}
}

// inBody reports whether a ";" here is inside the body of a routine the statement creates.
func (l *sqlLexer) inBody() bool {
	return isRoutine(l.stmt) && (l.blocks || openBlocks(l.stmt) > 0)
}

// quoted reads the quoted text at the cursor up to the closing quote, which doubled stands
// for itself; with backslash, a backslash escapes the character after it.
func (l *sqlLexer) quoted(closing byte, backslash bool) string {
	var b strings.Builder
	i := l.i + 1
	for i < len(l.src) {
		c := l.src[i]
		switch {
		case backslash && c == '\\' && i+1 < len(l.src):
			b.WriteByte(l.src[i+1])
			i += 2
			continue
		case c == closing && i+1 < len(l.src) && l.src[i+1] == closing:
			b.WriteByte(c)
			i += 2
			continue
		case c == closing:
			l.i = i + 1
			return b.String()
		}
		b.WriteByte(c)
		i++
	}
	l.i = len(l.src)
	return b.String()
}

// bracketQuotes reports whether the "[" at the cursor opens a quoted name, as in SQL Server
// and SQLite, rather than PostgreSQL's array subscript or type (text[], ARRAY[1]).
func (l *sqlLexer) bracketQuotes() bool {
	if l.i == 0 {
		return true
	}
	prev := l.src[l.i-1]
	return !isWordByte(prev) && !strings.ContainsRune(`)]'"$`, rune(prev))
}

// dollarTag is the PostgreSQL dollar quote ($$ or $tag$) at the cursor, if there is one.
func (l *sqlLexer) dollarTag() string {
	i := l.i + 1
	for i < len(l.src) && isWordByte(l.src[i]) && l.src[i] != '$' {
		if i == l.i+1 && l.src[i] >= '0' && l.src[i] <= '9' {
			return "" // $1 is a parameter
		}
		i++
	}
	if i < len(l.src) && l.src[i] == '$' {
		return l.src[l.i : i+1]
	}
	return ""
}

func (l *sqlLexer) dollarQuoted() string {
	tag := l.dollarTag()
	body := l.src[l.i+len(tag):]
	end := strings.Index(body, tag)
	if end < 0 {
		l.i = len(l.src)
		return body
	}
	l.i += len(tag) + end + len(tag)
	return body[:end]
}

func (l *sqlLexer) wordStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '@' || r == '#' && !l.mysql
}

// word reads a keyword or a name, or a string with a prefix: E” (backslash escapes), N”,
// X”, B”, MySQL's _utf8mb4”, and Oracle's q'[...]'.
func (l *sqlLexer) word() {
	start := l.i
	for l.i < len(l.src) {
		r, n := utf8.DecodeRuneInString(l.src[l.i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_$#@", r) || r == '#' && l.mysql || l.atDelimiter() {
			break
		}
		l.i += n
	}
	w := l.src[start:l.i]
	if l.i >= len(l.src) || l.src[l.i] != '\'' {
		l.add(sqlWord, w)
		return
	}
	switch upper := strings.ToUpper(w); {
	case (upper == "Q" || upper == "NQ") && !l.mysql && l.i+1 < len(l.src):
		l.add(sqlString, l.qQuoted())
	case upper == "E":
		l.add(sqlString, l.quoted('\'', true))
	case upper == "N" || upper == "X" || upper == "B" || strings.HasPrefix(w, "_"):
		l.add(sqlString, l.quoted('\'', l.mysql))
	default:
		l.add(sqlWord, w)
	}
}

// qQuoted reads Oracle's q'Xtext X', whose X is any character, or a bracket its pair closes.
func (l *sqlLexer) qQuoted() string {
	open := l.src[l.i+1]
	closing := open
	if i := strings.IndexByte("[{(<", open); i >= 0 {
		closing = "]})>"[i]
	}
	body := l.src[l.i+2:]
	end := strings.Index(body, string(closing)+"'")
	if end < 0 {
		l.i = len(l.src)
		return body
	}
	l.i += 2 + end + 2
	return body[:end]
}

// sqlStatementStart is a line that starts a statement of its own: CREATE, ALTER TABLE, DROP
// TABLE (or a view, routine and the like), EXEC.
var sqlStatementStart = regexp.MustCompile(`(?i)^(?:CREATE|EXEC|EXECUTE|ALTER\s+TABLE|DROP\s+(?:TABLE|VIEW|PROCEDURE|PROC|FUNCTION|TRIGGER|SEQUENCE|SCHEMA|TYPE|SYNONYM))\b`)

// startsStatement reports whether text, the start of a line's SQL, starts a statement.
func startsStatement(text string) bool { return sqlStatementStart.MatchString(text) }

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// isRoutine reports whether a statement creates a function, procedure, trigger, event or
// package body, whose body has statements of its own.
func isRoutine(stmt []sqlToken) bool {
	if len(stmt) == 0 || !stmt[0].is("CREATE") {
		return false
	}
	for i, t := range stmt[1:min(len(stmt), 12)] {
		switch {
		case t.is("TABLE"), t.is("VIEW"), t.is("INDEX"):
			return false
		case t.is("FUNCTION"), t.is("PROCEDURE"), t.is("PROC"), t.is("TRIGGER"), t.is("EVENT"), t.is("PACKAGE"):
			return true
		case t.is("TYPE"):
			return i+2 < len(stmt) && stmt[i+2].is("BODY")
		}
	}
	return false
}

// openBlocks counts the BEGIN and CASE blocks a statement has opened and not yet closed with
// END. END IF, END LOOP and the like close blocks it does not count.
func openBlocks(stmt []sqlToken) int {
	open := 0
	for i, t := range stmt {
		var prev, next sqlToken
		if i > 0 {
			prev = stmt[i-1]
		}
		if i+1 < len(stmt) {
			next = stmt[i+1]
		}
		switch {
		case t.is("BEGIN") && !next.is("TRANSACTION") && !next.is("TRAN") && !next.is("WORK") && !next.is("DISTRIBUTED"):
			open++
		case t.is("CASE") && !prev.is("END"):
			open++
		case t.is("END") && !next.is("IF") && !next.is("LOOP") && !next.is("WHILE") && !next.is("REPEAT") && !next.is("FOR"):
			open--
		}
	}
	return open
}

// sqlCursor reads a statement's tokens in order.
type sqlCursor struct {
	toks []sqlToken
	i    int
}

func (c *sqlCursor) done() bool { return c.i >= len(c.toks) }

// peek is the token n ahead of the cursor, or a zero token past the end.
func (c *sqlCursor) peek(n int) sqlToken {
	if c.i+n < len(c.toks) {
		return c.toks[c.i+n]
	}
	return sqlToken{kind: -1}
}

func (c *sqlCursor) next() sqlToken {
	t := c.peek(0)
	if !c.done() {
		c.i++
	}
	return t
}

// at reports whether the next tokens are these keywords.
func (c *sqlCursor) at(words ...string) bool {
	for n, w := range words {
		if !c.peek(n).is(w) {
			return false
		}
	}
	return true
}

// accept takes the next tokens when they are these keywords.
func (c *sqlCursor) accept(words ...string) bool {
	if !c.at(words...) {
		return false
	}
	c.i += len(words)
	return true
}

func (c *sqlCursor) acceptSymbol(s string) bool {
	if !c.peek(0).isSymbol(s) {
		return false
	}
	c.i++
	return true
}

// name takes a name, qualified or not (parcels, public.parcels, "PARCELS", [dbo].[parcels]),
// in lower case without its quotes; "" when there is none.
func (c *sqlCursor) name() string {
	var parts []string
	for c.peek(0).isName() {
		parts = append(parts, strings.ToLower(c.next().text))
		if !c.peek(0).isSymbol(".") || !c.peek(1).isName() {
			break
		}
		c.i++
	}
	return strings.Join(parts, ".")
}

// group takes the parenthesized tokens at the cursor and returns those inside; nil when the
// cursor is not at a "(".
func (c *sqlCursor) group() []sqlToken {
	if !c.peek(0).isSymbol("(") {
		return nil
	}
	start, depth := c.i+1, 0
	for !c.done() {
		t := c.next()
		switch {
		case t.isSymbol("("):
			depth++
		case t.isSymbol(")"):
			depth--
			if depth == 0 {
				return c.toks[start : c.i-1]
			}
		}
	}
	return c.toks[start:]
}

// skip takes the next token, or the whole parenthesized group at the cursor.
func (c *sqlCursor) skip() {
	if c.group() == nil {
		c.next()
	}
}

// rest takes the tokens left.
func (c *sqlCursor) rest() []sqlToken {
	r := c.toks[min(c.i, len(c.toks)):]
	c.i = len(c.toks)
	return r
}

// splitCommas splits tokens at their commas outside parentheses.
func splitCommas(toks []sqlToken) [][]sqlToken {
	var out [][]sqlToken
	depth, start := 0, 0
	for i, t := range toks {
		switch {
		case t.isSymbol("("):
			depth++
		case t.isSymbol(")"):
			depth--
		case t.isSymbol(",") && depth == 0:
			out = append(out, toks[start:i])
			start = i + 1
		}
	}
	if start < len(toks) {
		out = append(out, toks[start:])
	}
	return out
}

// columnNames are the columns a parenthesized list names: (a, b DESC, c(10)).
func columnNames(toks []sqlToken) []string {
	var names []string
	for _, part := range splitCommas(toks) {
		if len(part) > 0 && part[0].isName() {
			names = append(names, strings.ToLower(part[0].text))
		}
	}
	return names
}
