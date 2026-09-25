package javare

import (
	"math"
	"strings"
	"unicode"
)

// This file is a port of the parser in java.util.regex.Pattern (JDK 21):
// the same cursor discipline over a zero-padded code point array, the same
// \Q...\E pre-pass and the same decisions and errors. Instead of Java's
// matcher nodes it builds the small AST that emit.go turns into a regexp2
// pattern.

type node interface{}

type qkind int

const (
	greedy qkind = iota
	lazy
	possessive
)

type gkind int

const (
	gCapture gkind = iota
	gNonCapture
	gAtomic
	gLookahead
	gNegLookahead
	gLookbehind
	gNegLookbehind
)

type akind int

const (
	aBegin        akind = iota // \A, ^
	aEnd                       // \z
	aCaret                     // ^ with MULTILINE
	aUnixCaret                 // ^ with MULTILINE|UNIX_LINES
	aDollar                    // $, \Z
	aDollarML                  // $ with MULTILINE
	aUnixDollar                // $, \Z with UNIX_LINES
	aUnixDollarML              // $ with MULTILINE|UNIX_LINES
	aLastMatch                 // \G
	aBound                     // \b
	aNotBound                  // \B
)

type (
	seqNode []node
	altNode []node
	setNode struct{ cs charset }
	repNode struct {
		sub      node
		min, max int // max < 0: unbounded
		kind     qkind
	}
	groupNode struct {
		kind  gkind
		index int
		sub   node
		// rmin and rmax are the length bounds Java computed for a
		// look-behind (TreeInfo, with its int overflow quirks).
		rmin, rmax int32
	}
	assertNode struct {
		kind  akind
		uword bool
	}
	lineEndNode  struct{}
	graphemeNode struct{}
	backrefNode  struct {
		index int
		ci    bool
	}
)

type syntaxPanic struct{ err *SyntaxError }

type jparser struct {
	pattern    string
	temp       []rune
	plen       int
	cursor     int
	flags      Flag
	groupCount int // Java's capturingGroupCount: starts at 1
	names      map[string]int
	root       node
	predicate  charset
}

func parse(pattern string, flags Flag) (tree node, groups int, names map[string]int, err error) {
	defer func() {
		if r := recover(); r != nil {
			sp, ok := r.(syntaxPanic)
			if !ok {
				panic(r)
			}
			err = sp.err
		}
	}()
	runes := []rune(pattern)
	p := &jparser{
		pattern:    pattern,
		temp:       append(append([]rune(nil), runes...), 0, 0),
		plen:       len(runes),
		flags:      flags,
		groupCount: 1,
		names:      map[string]int{},
	}
	if flags&UnicodeCharacterClass != 0 {
		p.flags |= UnicodeCase
	}
	if flags&Literal != 0 {
		var seq seqNode
		for _, r := range runes {
			seq = append(seq, &setNode{cs: p.sliceCharSet(r)})
		}
		return seq, 0, p.names, nil
	}
	p.removeQEQuoting()
	tree = p.expr()
	if p.plen != p.cursor {
		if p.peek() == ')' {
			p.fail("Unmatched closing ')'")
		}
		if p.cursor == p.plen+1 && p.temp[p.plen-1] == '\\' {
			p.fail("Unescaped trailing backslash")
		}
		p.fail("Unexpected internal error")
	}
	return tree, p.groupCount - 1, p.names, nil
}

func (p *jparser) fail(desc string) {
	panic(syntaxPanic{&SyntaxError{Description: desc, Index: p.cursor - 1, Pattern: p.pattern}})
}

func (p *jparser) has(f Flag) bool { return p.flags&f != 0 }

// at reads temp like Java's array access (panicking past the padding would
// be an ArrayIndexOutOfBoundsException in Java too; treat it as end).
func (p *jparser) at(i int) rune {
	if i < 0 || i >= len(p.temp) {
		return 0
	}
	return p.temp[i]
}

func (p *jparser) removeQEQuoting() {
	pLen := p.plen
	i := 0
	for i < pLen-1 {
		if p.temp[i] != '\\' {
			i++
		} else {
			if p.temp[i+1] == 'Q' {
				break
			}
			i += 2
		}
	}
	if i >= pLen-1 {
		return
	}
	j := i
	i += 2
	newtemp := make([]rune, j+2+3*(pLen-i)+2)
	copy(newtemp, p.temp[:i])
	inQuote, beginQuote := true, true
	for i < pLen {
		c := p.temp[i]
		i++
		if c < 128 && !isASCIILetter(c) {
			switch {
			case c >= '0' && c <= '9':
				if beginQuote {
					newtemp[j], newtemp[j+1], newtemp[j+2] = '\\', 'x', '3'
					j += 3
				}
				newtemp[j] = c
				j++
			case c != '\\':
				if inQuote {
					newtemp[j] = '\\'
					j++
				}
				newtemp[j] = c
				j++
			case inQuote:
				if p.temp[i] == 'E' {
					i++
					inQuote = false
				} else {
					newtemp[j], newtemp[j+1] = '\\', '\\'
					j += 2
				}
			default:
				if p.temp[i] == 'Q' {
					i++
					inQuote, beginQuote = true, true
					continue
				}
				newtemp[j] = c
				j++
				if i != pLen {
					newtemp[j] = p.temp[i]
					j++
					i++
				}
			}
		} else {
			newtemp[j] = c
			j++
		}
		beginQuote = false
	}
	p.plen = j
	newtemp = append(newtemp[:j:j], 0, 0)
	p.temp = newtemp
}

func isASCIISpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == 0x0B || c == '\f' || c == '\r'
}

func (p *jparser) isLineSeparator(c rune) bool {
	if p.has(UnixLines) {
		return c == '\n'
	}
	return c == '\n' || c == '\r' || c|1 == 0x2029 || c == 0x85
}

func (p *jparser) peek() rune {
	ch := p.at(p.cursor)
	if p.has(Comments) {
		ch = p.peekPastWhitespace(ch)
	}
	return ch
}

func (p *jparser) read() rune {
	ch := p.at(p.cursor)
	p.cursor++
	if p.has(Comments) {
		ch = p.parsePastWhitespace(ch)
	}
	return ch
}

func (p *jparser) next() rune {
	p.cursor++
	ch := p.at(p.cursor)
	if p.has(Comments) {
		ch = p.peekPastWhitespace(ch)
	}
	return ch
}

func (p *jparser) nextEscaped() rune {
	p.cursor++
	return p.at(p.cursor)
}

func (p *jparser) peekPastWhitespace(ch rune) rune {
	for isASCIISpace(ch) || ch == '#' {
		for isASCIISpace(ch) {
			p.cursor++
			ch = p.at(p.cursor)
		}
		if ch == '#' {
			ch = p.peekPastLine()
		}
	}
	return ch
}

func (p *jparser) parsePastWhitespace(ch rune) rune {
	for isASCIISpace(ch) || ch == '#' {
		for isASCIISpace(ch) {
			ch = p.at(p.cursor)
			p.cursor++
		}
		if ch == '#' {
			ch = p.parsePastLine()
		}
	}
	return ch
}

func (p *jparser) parsePastLine() rune {
	ch := p.at(p.cursor)
	p.cursor++
	for ch != 0 && !p.isLineSeparator(ch) {
		ch = p.at(p.cursor)
		p.cursor++
	}
	if ch == 0 && p.cursor > p.plen {
		p.cursor = p.plen
		ch = p.at(p.cursor)
		p.cursor++
	}
	return ch
}

func (p *jparser) peekPastLine() rune {
	p.cursor++
	ch := p.at(p.cursor)
	for ch != 0 && !p.isLineSeparator(ch) {
		p.cursor++
		ch = p.at(p.cursor)
	}
	if ch == 0 && p.cursor > p.plen {
		p.cursor = p.plen
		ch = p.at(p.cursor)
	}
	return ch
}

func (p *jparser) skip() rune {
	i := p.cursor
	ch := p.at(i + 1)
	p.cursor = i + 2
	return ch
}

func (p *jparser) unread() { p.cursor-- }

func (p *jparser) accept(ch rune, msg string) {
	test := p.at(p.cursor)
	p.cursor++
	if p.has(Comments) {
		test = p.parsePastWhitespace(test)
	}
	if ch != test {
		p.fail(msg)
	}
}

func (p *jparser) expr() node {
	var alts altNode
	for {
		alts = append(alts, p.sequence())
		if p.peek() != '|' {
			break
		}
		p.next()
	}
	if len(alts) == 1 {
		return alts[0]
	}
	return alts
}

func (p *jparser) sequence() node {
	var seq seqNode
	for {
		ch := p.peek()
		var n node
		switch ch {
		case 0:
			if p.cursor >= p.plen {
				return seq
			}
			n = p.atom()
		case '$':
			p.next()
			switch {
			case p.has(UnixLines) && p.has(Multiline):
				n = &assertNode{kind: aUnixDollarML}
			case p.has(UnixLines):
				n = &assertNode{kind: aUnixDollar}
			case p.has(Multiline):
				n = &assertNode{kind: aDollarML}
			default:
				n = &assertNode{kind: aDollar}
			}
		case '(':
			g := p.group0()
			if g != nil {
				seq = append(seq, g)
			}
			continue
		case ')', '|':
			return seq
		case '*', '+', '?':
			p.next()
			p.fail("Dangling meta character '" + string(ch) + "'")
		case '.':
			p.next()
			switch {
			case p.has(DotAll):
				n = &setNode{cs: csAll}
			case p.has(UnixLines):
				n = &setNode{cs: csRune('\n').negate()}
			default:
				n = &setNode{cs: lineTerms.negate()}
			}
		case '[':
			n = &setNode{cs: p.clazz(true)}
		case '\\':
			ch = p.nextEscaped()
			if ch != 'p' && ch != 'P' {
				p.unread()
				n = p.atom()
			} else {
				oneLetter := true
				comp := ch == 'P'
				ch = p.next()
				if ch != '{' {
					p.unread()
				} else {
					oneLetter = false
				}
				n = &setNode{cs: p.family(oneLetter, comp)}
			}
		case '^':
			p.next()
			switch {
			case p.has(Multiline) && p.has(UnixLines):
				n = &assertNode{kind: aUnixCaret}
			case p.has(Multiline):
				n = &assertNode{kind: aCaret}
			default:
				n = &assertNode{kind: aBegin}
			}
		default:
			n = p.atom()
		}
		seq = append(seq, p.closure(n))
	}
}

// atom collects a run of literal characters (a Java Slice).
func (p *jparser) atom() node {
	var buf []rune
	prev := -1
	ch := p.peek()
	for {
		switch ch {
		case 0:
			if p.cursor >= p.plen {
				return p.sliceNode(buf)
			}
			prev = p.cursor
			buf = append(buf, ch)
			ch = p.next()
		case '$', '(', ')', '.', '[', '^', '|':
			return p.sliceNode(buf)
		case '*', '+', '?', '{':
			if len(buf) > 1 {
				p.cursor = prev
				buf = buf[:len(buf)-1]
			}
			return p.sliceNode(buf)
		case '\\':
			ch = p.nextEscaped()
			if ch != 'p' && ch != 'P' {
				p.unread()
				prev = p.cursor
				ch = p.escape(false, len(buf) == 0, false)
				if ch >= 0 {
					buf = append(buf, ch)
					ch = p.peek()
					continue
				}
				if len(buf) == 0 {
					return p.root
				}
				p.cursor = prev
				return p.sliceNode(buf)
			}
			if len(buf) == 0 {
				comp := ch == 'P'
				oneLetter := true
				ch = p.next()
				if ch != '{' {
					p.unread()
				} else {
					oneLetter = false
				}
				return &setNode{cs: p.family(oneLetter, comp)}
			}
			p.unread()
			return p.sliceNode(buf)
		default:
			prev = p.cursor
			buf = append(buf, ch)
			ch = p.next()
		}
	}
}

// sliceNode builds the node for literal characters: a single character uses
// Java's single() case rules, a longer slice its SliceI/SliceU rules.
func (p *jparser) sliceNode(buf []rune) node {
	if len(buf) == 1 {
		return &setNode{cs: p.single(buf[0])}
	}
	seq := make(seqNode, len(buf))
	for i, r := range buf {
		seq[i] = &setNode{cs: p.sliceCharSet(r)}
	}
	return seq
}

func (p *jparser) sliceCharSet(r rune) charset {
	if !p.has(CaseInsensitive) {
		return csRune(r)
	}
	if p.has(UnicodeCase) {
		return unicodeSingleCaseSet(r)
	}
	if r < 128 && isASCIILetter(r) {
		return csRunes(asciiLower(r), asciiUpper(r))
	}
	return csRune(r)
}

// single is Pattern.single: the set a lone literal character matches.
func (p *jparser) single(ch rune) charset {
	if p.has(CaseInsensitive) {
		if p.has(UnicodeCase) {
			upper := unicode.ToUpper(ch)
			lower := unicode.ToLower(upper)
			if upper != lower {
				return unicodeSingleCaseSet(ch)
			}
		} else if ch < 128 {
			lower, upper := asciiLower(ch), asciiUpper(ch)
			if lower != upper {
				return csRunes(lower, upper)
			}
		}
	}
	return csRune(ch)
}

func (p *jparser) ref(refNum int) node {
	for {
		ch := p.peek()
		if ch < '0' || ch > '9' {
			break
		}
		n := refNum*10 + int(ch-'0')
		if p.groupCount-1 < n {
			break
		}
		refNum = n
		p.read()
	}
	return &backrefNode{index: refNum, ci: p.has(CaseInsensitive)}
}

// escape is Pattern.escape: it returns the escaped character, or -1 after
// setting p.root (outside a class) / p.predicate for escapes that denote a
// node or a class.
func (p *jparser) escape(inclass, create, isrange bool) rune {
	ch := p.skip()
	uword := p.has(UnicodeCharacterClass)
	setPred := func(cs charset) rune {
		if create {
			p.predicate = cs
			if !inclass {
				p.root = &setNode{cs: cs}
			}
		}
		return -1
	}
	switch ch {
	case '0':
		return p.octal()
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if !inclass {
			if create {
				p.root = p.ref(int(ch - '0'))
			}
			return -1
		}
	case 'A':
		if !inclass {
			if create {
				p.root = &assertNode{kind: aBegin}
			}
			return -1
		}
	case 'B':
		if !inclass {
			if create {
				p.root = &assertNode{kind: aNotBound, uword: uword}
			}
			return -1
		}
	case 'D':
		return setPred(digitSet(p.flags).negate())
	case 'G':
		if !inclass {
			if create {
				p.root = &assertNode{kind: aLastMatch}
			}
			return -1
		}
	case 'H':
		return setPred(hSpace.negate())
	case 'N':
		return p.namedChar()
	case 'R':
		if !inclass {
			if create {
				p.root = &lineEndNode{}
			}
			return -1
		}
	case 'S':
		return setPred(spaceSet(p.flags).negate())
	case 'V':
		return setPred(vSpace.negate())
	case 'W':
		return setPred(wordSet(p.flags).negate())
	case 'X':
		if !inclass {
			if create {
				p.root = &graphemeNode{}
			}
			return -1
		}
	case 'Z':
		if !inclass {
			if create {
				if p.has(UnixLines) {
					p.root = &assertNode{kind: aUnixDollar}
				} else {
					p.root = &assertNode{kind: aDollar}
				}
			}
			return -1
		}
	case 'a':
		return 7
	case 'b':
		if !inclass {
			if !create {
				return -1
			}
			if p.peek() == '{' {
				if p.skip() == 'g' {
					if p.read() == '}' {
						p.fail("Grapheme boundaries (\\b{g}) are not supported")
					}
					p.fail("Illegal/unsupported escape sequence")
				}
				p.unread()
				p.unread()
			}
			p.root = &assertNode{kind: aBound, uword: uword}
			return -1
		}
	case 'c':
		if p.cursor < p.plen {
			return p.read() ^ 64
		}
		p.fail("Illegal control escape sequence")
	case 'd':
		return setPred(digitSet(p.flags))
	case 'e':
		return 27
	case 'f':
		return 12
	case 'h':
		return setPred(hSpace)
	case 'k':
		if !inclass {
			if p.read() != '<' {
				p.fail("\\k is not followed by '<' for named capturing group")
			}
			name := p.groupname(p.read())
			number, ok := p.names[name]
			if !ok {
				p.fail("named capturing group <" + name + "> does not exist")
			}
			if create {
				p.root = &backrefNode{index: number, ci: p.has(CaseInsensitive)}
			}
			return -1
		}
	case 'n':
		return 10
	case 'r':
		return 13
	case 's':
		return setPred(spaceSet(p.flags))
	case 't':
		return 9
	case 'u':
		return p.unicodeEscape()
	case 'v':
		if isrange {
			return 11
		}
		return setPred(vSpace)
	case 'w':
		return setPred(wordSet(p.flags))
	case 'x':
		return p.hexEscape()
	case 'z':
		if !inclass {
			if create {
				p.root = &assertNode{kind: aEnd}
			}
			return -1
		}
	case 'C', 'E', 'F', 'I', 'J', 'K', 'L', 'M', 'O', 'P', 'Q', 'T', 'U', 'Y',
		'g', 'i', 'j', 'l', 'm', 'o', 'p', 'q', 'y':
	default:
		return ch
	}
	p.fail("Illegal/unsupported escape sequence")
	return -1
}

func (p *jparser) octal() rune {
	n := p.read()
	if n >= '0' && n <= '7' {
		m := p.read()
		if m >= '0' && m <= '7' {
			o := p.read()
			if o >= '0' && o <= '7' && n >= '0' && n <= '3' {
				return (n-'0')*64 + (m-'0')*8 + (o - '0')
			}
			p.unread()
			return (n-'0')*8 + (m - '0')
		}
		p.unread()
		return n - '0'
	}
	p.fail("Illegal octal escape sequence")
	return -1
}

func isHexDigit(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c rune) rune {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

func (p *jparser) hexEscape() rune {
	n := p.read()
	if isHexDigit(n) {
		m := p.read()
		if isHexDigit(m) {
			return hexVal(n)*16 + hexVal(m)
		}
	} else if n == '{' && isHexDigit(p.peek()) {
		ch := rune(0)
		for {
			n = p.read()
			if !isHexDigit(n) {
				break
			}
			ch = ch<<4 + hexVal(n)
			if ch > unicode.MaxRune {
				p.fail("Hexadecimal codepoint is too big")
			}
		}
		if n != '}' {
			p.fail("Unclosed hexadecimal escape sequence")
		}
		return ch
	}
	p.fail("Illegal hexadecimal escape sequence")
	return -1
}

func (p *jparser) uxxxx() rune {
	n := rune(0)
	for i := 0; i < 4; i++ {
		ch := p.read()
		if !isHexDigit(ch) {
			p.fail("Illegal Unicode escape sequence")
		}
		n = n*16 + hexVal(ch)
	}
	return n
}

func (p *jparser) unicodeEscape() rune {
	n := p.uxxxx()
	if n >= 0xD800 && n <= 0xDBFF {
		cur := p.cursor
		// Java reads a backslash and then a 'u', both unconditionally.
		first := p.read()
		second := p.read()
		if first == '\\' && second == 'u' {
			n2 := p.uxxxx()
			if n2 >= 0xDC00 && n2 <= 0xDFFF {
				return (n-0xD800)<<10 + (n2 - 0xDC00) + 0x10000
			}
		}
		p.cursor = cur
	}
	return n
}

func (p *jparser) namedChar() rune {
	if p.read() != '{' {
		p.fail("Illegal character name escape sequence")
	}
	i := p.cursor
	for p.read() != '}' {
		if p.cursor >= p.plen {
			p.fail("Unclosed character name escape sequence")
		}
	}
	name := string(p.temp[i : p.cursor-1])
	if r, ok := runeByName(name); ok {
		return r
	}
	p.fail("Unknown character name [" + name + "]")
	return -1
}

// clazz is Pattern.clazz: a character class, from '[' to ']'.
func (p *jparser) clazz(consume bool) charset {
	var prev, curr, bits charset
	havePrev, haveCurr, hasBits := false, false, false
	isNeg := false
	ch := p.next()
	if ch == '^' && p.at(p.cursor-1) == '[' {
		ch = p.next()
		isNeg = true
	}
	for {
		switch ch {
		case 0:
			if p.cursor >= p.plen {
				p.fail("Unclosed character class")
			}
		case '&':
			ch = p.next()
			if ch == '&' {
				ch = p.next()
				var right charset
				haveRight := false
				for ch != ']' && ch != '&' {
					var r charset
					if ch == '[' {
						r = p.clazz(true)
					} else {
						p.unread()
						r = p.clazz(false)
					}
					if haveRight {
						right = right.union(r)
					} else {
						right, haveRight = r, true
					}
					ch = p.peek()
				}
				if hasBits {
					if !havePrev {
						prev, curr, havePrev, haveCurr = bits, bits, true, true
					} else {
						prev = prev.union(bits)
					}
					hasBits = false
				}
				if haveRight {
					curr, haveCurr = right, true
				}
				if !havePrev {
					if !haveRight {
						p.fail("Bad class syntax")
					}
					prev, havePrev = right, true
				} else {
					if !haveCurr {
						p.fail("Bad intersection syntax")
					}
					prev = prev.intersect(curr)
				}
				continue
			}
			p.unread()
		case '[':
			curr, haveCurr = p.clazz(true), true
			if !havePrev {
				prev, havePrev = curr, true
			} else {
				prev = prev.union(curr)
			}
			ch = p.peek()
			continue
		case ']':
			if havePrev || hasBits {
				if consume {
					p.next()
				}
				if !havePrev {
					prev = bits
				} else if hasBits {
					prev = prev.union(bits)
				}
				if isNeg {
					return prev.negate()
				}
				return prev
			}
		}
		cs, isBits := p.rangeItem(&bits)
		if isBits {
			hasBits = true
			haveCurr = false
		} else {
			curr, haveCurr = cs, true
			if !havePrev {
				prev, havePrev = cs, true
			} else {
				prev = prev.union(cs)
			}
		}
		ch = p.peek()
	}
}

// bitsOrSingle is Pattern.bitsOrSingle: characters below 256 go into the
// class's bit set (with BitClass's case rules); others become single().
func (p *jparser) bitsOrSingle(bits *charset, ch rune) (charset, bool) {
	if ch >= 256 || p.has(CaseInsensitive) && p.has(UnicodeCase) &&
		strings.ContainsRune("ÿµIiSsKkÅå", ch) {
		return p.single(ch), false
	}
	add := csRune(ch)
	if p.has(CaseInsensitive) {
		if ch < 128 {
			add = add.union(csRunes(asciiUpper(ch), asciiLower(ch)))
		} else if p.has(UnicodeCase) {
			add = add.union(csRunes(unicode.ToLower(ch), unicode.ToUpper(ch)))
		}
	}
	*bits = bits.union(add)
	return nil, true
}

// rangeItem is Pattern.range: one class item (character, range, escape or
// property).
func (p *jparser) rangeItem(bits *charset) (charset, bool) {
	ch := p.peek()
	if ch == '\\' {
		ch = p.nextEscaped()
		if ch == 'p' || ch == 'P' {
			comp := ch == 'P'
			oneLetter := true
			ch = p.next()
			if ch != '{' {
				p.unread()
			} else {
				oneLetter = false
			}
			return p.family(oneLetter, comp), false
		}
		isrange := p.at(p.cursor+1) == '-'
		p.unread()
		ch = p.escape(true, true, isrange)
		if ch == -1 {
			return p.predicate, false
		}
	} else {
		p.next()
	}
	if ch < 0 {
		p.fail("Unexpected character '" + string(ch) + "'")
	}
	if p.peek() == '-' {
		endRange := p.at(p.cursor + 1)
		if endRange == '[' {
			return p.bitsOrSingle(bits, ch)
		}
		if endRange != ']' {
			p.next()
			m := p.peek()
			if m == '\\' {
				m = p.escape(true, false, true)
			} else {
				p.next()
			}
			if m < ch {
				p.fail("Illegal character range")
			}
			r := csRange(ch, m)
			if p.has(CaseInsensitive) {
				if p.has(UnicodeCase) {
					return r.unicodeRangeCaseClose(), false
				}
				return r.asciiCaseClose(), false
			}
			return r, false
		}
	}
	return p.bitsOrSingle(bits, ch)
}

// family is Pattern.family: \p{name}, \P{name}, \pX.
func (p *jparser) family(singleLetter, isComplement bool) charset {
	p.next()
	var name string
	if singleLetter {
		name = string(p.at(p.cursor))
		p.read()
	} else {
		i := p.cursor
		for {
			c := p.read()
			if c == '}' || p.cursor > p.plen {
				break
			}
		}
		j := p.cursor
		if j > p.plen {
			p.fail("Unclosed character family")
		}
		if i+1 >= j {
			p.fail("Empty character family")
		}
		name = string(p.temp[i : j-1])
	}
	cs, ok := p.lookupProperty(name)
	if !ok {
		if k, v, eq := strings.Cut(name, "="); eq {
			_ = k
			p.fail("Unknown Unicode property {name=<" + name + ">, value=<" + v + ">}")
		}
		p.fail("Unknown character property name {" + name + "}")
	}
	if isComplement {
		return cs.negate()
	}
	return cs
}

func (p *jparser) groupname(ch rune) string {
	if !isASCIILetter(ch) {
		p.fail("capturing group name does not start with a Latin letter")
	}
	var sb strings.Builder
	for {
		sb.WriteRune(ch)
		ch = p.read()
		if !isASCIILetter(ch) && (ch < '0' || ch > '9') {
			break
		}
	}
	if ch != '>' {
		p.fail("named capturing group is missing trailing '>'")
	}
	return sb.String()
}

// group0 is Pattern.group0: everything that starts with '('. It returns nil
// for a pure flag group such as (?i).
func (p *jparser) group0() node {
	save := p.flags
	var head node
	ch := p.next()
	if ch == '?' {
		ch = p.skip()
		switch ch {
		case '=', '!':
			kind := gLookahead
			if ch == '!' {
				kind = gNegLookahead
			}
			head = &groupNode{kind: kind, sub: p.expr()}
		case '$', '@':
			p.fail("Unknown group type")
		case ':':
			head = &groupNode{kind: gNonCapture, sub: p.expr()}
		case '<':
			ch = p.read()
			if ch != '=' && ch != '!' {
				name := p.groupname(ch)
				if _, dup := p.names[name]; dup {
					p.fail("Named capturing group <" + name + "> is already defined")
				}
				idx := p.groupCount
				p.groupCount++
				p.names[name] = idx
				head = &groupNode{kind: gCapture, index: idx, sub: p.expr()}
			} else {
				kind := gLookbehind
				if ch == '!' {
					kind = gNegLookbehind
				}
				sub := p.expr()
				info := newTreeInfo()
				studyNode(sub, info)
				if !info.maxValid {
					p.fail("Look-behind group does not have an obvious maximum length")
				}
				head = &groupNode{kind: kind, sub: sub, rmin: info.minLength, rmax: info.maxLength}
			}
		case '>':
			head = &groupNode{kind: gAtomic, sub: p.expr()}
		default:
			p.unread()
			p.addFlag()
			ch = p.read()
			if ch == ')' {
				return nil
			}
			if ch != ':' {
				p.fail("Unknown inline modifier")
			}
			head = &groupNode{kind: gNonCapture, sub: p.expr()}
		}
	} else {
		idx := p.groupCount
		p.groupCount++
		head = &groupNode{kind: gCapture, index: idx, sub: p.expr()}
	}
	p.accept(')', "Unclosed group")
	p.flags = save
	return p.closure(head)
}

func (p *jparser) addFlag() {
	ch := p.peek()
	for {
		switch ch {
		case '-':
			p.next()
			p.subFlag()
			return
		case 'U':
			p.flags |= UnicodeCharacterClass | UnicodeCase
		case 'c':
			p.flags |= CanonEq
		case 'd':
			p.flags |= UnixLines
		case 'i':
			p.flags |= CaseInsensitive
		case 'm':
			p.flags |= Multiline
		case 's':
			p.flags |= DotAll
		case 'u':
			p.flags |= UnicodeCase
		case 'x':
			p.flags |= Comments
		default:
			return
		}
		ch = p.next()
	}
}

func (p *jparser) subFlag() {
	ch := p.peek()
	for {
		switch ch {
		case 'U':
			p.flags &^= UnicodeCharacterClass | UnicodeCase
		case 'c':
			p.flags &^= CanonEq
		case 'd':
			p.flags &^= UnixLines
		case 'i':
			p.flags &^= CaseInsensitive
		case 'm':
			p.flags &^= Multiline
		case 's':
			p.flags &^= DotAll
		case 'u':
			p.flags &^= UnicodeCase
		case 'x':
			p.flags &^= Comments
		default:
			return
		}
		ch = p.next()
	}
}

func (p *jparser) qtype() qkind {
	ch := p.next()
	switch ch {
	case '?':
		p.next()
		return lazy
	case '+':
		p.next()
		return possessive
	}
	return greedy
}

func (p *jparser) closure(prev node) node {
	switch p.peek() {
	case '*':
		return &repNode{sub: prev, min: 0, max: -1, kind: p.qtype()}
	case '+':
		return &repNode{sub: prev, min: 1, max: -1, kind: p.qtype()}
	case '?':
		return &repNode{sub: prev, min: 0, max: 1, kind: p.qtype()}
	case '{':
		ch := p.skip()
		if ch < '0' || ch > '9' {
			p.fail("Illegal repetition")
		}
		cmin := 0
		cmax := 0
		overflow := false
		for {
			cmin = cmin*10 + int(ch-'0')
			if cmin > math.MaxInt32 {
				overflow = true
			}
			ch = p.read()
			if ch < '0' || ch > '9' {
				break
			}
		}
		if ch == ',' {
			ch = p.read()
			if ch == '}' {
				p.unread()
				if overflow {
					p.fail("Illegal repetition range")
				}
				return &repNode{sub: prev, min: cmin, max: -1, kind: p.qtype()}
			}
			for ch >= '0' && ch <= '9' {
				cmax = cmax*10 + int(ch-'0')
				if cmax > math.MaxInt32 {
					overflow = true
				}
				ch = p.read()
			}
		} else {
			cmax = cmin
		}
		if overflow {
			p.fail("Illegal repetition range")
		}
		if ch != '}' {
			p.fail("Unclosed counted closure")
		}
		if cmax < cmin {
			p.fail("Illegal repetition range")
		}
		p.unread()
		return &repNode{sub: prev, min: cmin, max: cmax, kind: p.qtype()}
	}
	return prev
}

// treeInfo mirrors java.util.regex.Pattern.TreeInfo, which Java uses to
// bound look-behinds. Lengths use int32 arithmetic, overflow included,
// because Java's decisions depend on it: (?<=a+) compiles (its maximum
// becomes Integer.MAX_VALUE) while (?<=(ab)+) does not.
type treeInfo struct {
	minLength, maxLength    int32
	maxValid, deterministic bool
}

func newTreeInfo() *treeInfo { return &treeInfo{maxValid: true, deterministic: true} }

func (t *treeInfo) reset() { *t = treeInfo{maxValid: true, deterministic: true} }

const maxReps = math.MaxInt32

// studyNode accumulates n's lengths into info the way Java's node study()
// methods do for the node Java builds for n.
func studyNode(n node, info *treeInfo) {
	switch x := n.(type) {
	case nil:
	case seqNode:
		for _, c := range x {
			studyNode(c, info)
		}
	case altNode:
		minL, maxL, maxV := info.minLength, info.maxLength, info.maxValid
		minL2, maxL2 := int32(math.MaxInt32), int32(-1)
		for _, a := range x {
			info.reset()
			studyNode(a, info)
			minL2 = min(minL2, info.minLength)
			maxL2 = max(maxL2, info.maxLength)
			maxV = maxV && info.maxValid
		}
		info.reset()
		info.minLength = minL + minL2
		info.maxLength = maxL + maxL2
		info.maxValid = maxV
		info.deterministic = false
	case *setNode:
		info.minLength++
		info.maxLength++
	case *repNode:
		studyRepeat(x, info)
	case *groupNode:
		switch x.kind {
		case gCapture, gNonCapture, gAtomic:
			studyNode(x.sub, info)
		}
	case *assertNode:
	case *lineEndNode:
		info.minLength++
		info.maxLength += 2
	case *graphemeNode:
		info.minLength++
		info.deterministic = false
	case *backrefNode:
		info.maxValid = false
	}
}

func studyRepeat(x *repNode, info *treeInfo) {
	_, isSet := x.sub.(*setNode)
	group, isGroup := x.sub.(*groupNode)
	isGroup = isGroup && (group.kind == gCapture || group.kind == gNonCapture)
	switch {
	case x.min == 0 && x.max == 1:
		// Ques (or, for a group, an equivalent Branch).
		minL := info.minLength
		studyNode(x.sub, info)
		info.minLength = minL
		info.deterministic = false
		return
	case isSet && x.max < 0 && x.kind == greedy:
		// CharPropertyGreedy: the maximum grows by Integer.MAX_VALUE.
		info.minLength += int32(x.min)
		if info.maxValid {
			info.maxLength += maxReps
		}
		info.deterministic = false
		return
	case isGroup && x.kind != possessive:
		sub := newTreeInfo()
		studyNode(group.sub, sub)
		if !sub.deterministic {
			// Loop / LazyLoop.
			info.maxValid = false
			info.deterministic = false
			return
		}
	}
	// Curly / GroupCurly.
	cmax := int32(maxReps)
	if x.max >= 0 {
		cmax = int32(x.max)
	}
	minL, maxL, maxV, detm := info.minLength, info.maxLength, info.maxValid, info.deterministic
	info.reset()
	if isGroup {
		studyNode(group.sub, info)
	} else {
		studyNode(x.sub, info)
	}
	temp := info.minLength*int32(x.min) + minL
	if temp < minL {
		temp = 268435455
	}
	info.minLength = temp
	if maxV && info.maxValid {
		temp = info.maxLength*cmax + maxL
		info.maxLength = temp
		if temp < maxL {
			info.maxValid = false
		}
	} else {
		info.maxValid = false
	}
	if info.deterministic && int32(x.min) == cmax {
		info.deterministic = detm
	} else {
		info.deterministic = false
	}
}
