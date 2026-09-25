package jsonx

// charIndex ports com.jayway.jsonpath.internal.CharacterIndex: a cursor over
// a path or filter string with an adjustable end position. charAt reads the
// underlying string (not bounded by the end position) and throws Java's
// StringIndexOutOfBoundsException past its length, which Jayway surfaces in
// some error messages.
type charIndex struct {
	s        jstr
	position int
	end      int
}

func newCharIndex(s string) *charIndex {
	j := toJ(s)
	return &charIndex{s: j, end: len(j) - 1}
}

func (ci *charIndex) length() int                 { return ci.end + 1 }
func (ci *charIndex) charAt(i int) uint16         { return ci.s.at(i) }
func (ci *charIndex) currentChar() uint16         { return ci.s.at(ci.position) }
func (ci *charIndex) currentCharIs(c uint16) bool { return ci.s.at(ci.position) == c }
func (ci *charIndex) lastCharIs(c uint16) bool    { return ci.s.at(ci.end) == c }
func (ci *charIndex) nextCharIs(c uint16) bool {
	return ci.inBounds(ci.position+1) && ci.s.at(ci.position+1) == c
}
func (ci *charIndex) incrementPosition(n int)         { ci.position += n }
func (ci *charIndex) decrementEndPosition(n int)      { ci.end -= n }
func (ci *charIndex) setPosition(p int)               { ci.position = p }
func (ci *charIndex) inBounds(i int) bool             { return i >= 0 && i <= ci.end }
func (ci *charIndex) inBoundsCur() bool               { return ci.inBounds(ci.position) }
func (ci *charIndex) isOutOfBounds(i int) bool        { return !ci.inBounds(i) }
func (ci *charIndex) currentIsTail() bool             { return ci.position >= ci.end }
func (ci *charIndex) hasMoreCharacters() bool         { return ci.inBounds(ci.position + 1) }
func (ci *charIndex) subSequence(from, to int) string { return ci.s.sub(from, to) }
func (ci *charIndex) String() string                  { return ci.s.String() }

func (ci *charIndex) indexOfMatchingCloseChar(start int, open, closeCh uint16, skipStrings, skipRegex bool) int {
	if ci.charAt(start) != open {
		throw(InvalidPath, "Expected %s but found %s", charObj(open), charObj(ci.charAt(start)))
	}
	opened := 1
	for read := start + 1; ci.inBounds(read); read++ {
		if skipStrings {
			q := ci.charAt(read)
			if q == '\'' || q == '"' {
				read = ci.nextIndexOfUnescaped(read, q)
				if read == -1 {
					throw(InvalidPath, "Could not find matching close quote for %s when parsing : %s", charObj(q), ci.String())
				}
				read++
			}
		}
		if skipRegex && ci.charAt(read) == '/' {
			read = ci.nextIndexOfUnescaped(read, '/')
			if read == -1 {
				throw(InvalidPath, "Could not find matching close for / when parsing regex in : %s", ci.String())
			}
			read++
		}
		if ci.charAt(read) == open {
			opened++
		}
		if ci.charAt(read) == closeCh {
			opened--
			if opened == 0 {
				return read
			}
		}
	}
	return -1
}

func (ci *charIndex) indexOfClosingBracket(start int, skipStrings, skipRegex bool) int {
	return ci.indexOfMatchingCloseChar(start, '(', ')', skipStrings, skipRegex)
}

func (ci *charIndex) indexOfNextSignificantCharFrom(start int, c uint16) int {
	read := start + 1
	for !ci.isOutOfBounds(read) && ci.charAt(read) == ' ' {
		read++
	}
	if ci.charAt(read) == c {
		return read
	}
	return -1
}

func (ci *charIndex) indexOfNextSignificantChar(c uint16) int {
	return ci.indexOfNextSignificantCharFrom(ci.position, c)
}

func (ci *charIndex) nextIndexOf(start int, c uint16) int {
	for read := start; !ci.isOutOfBounds(read); read++ {
		if ci.charAt(read) == c {
			return read
		}
	}
	return -1
}

func (ci *charIndex) nextIndexOfUnescaped(start int, c uint16) int {
	inEscape := false
	for read := start + 1; !ci.isOutOfBounds(read); read++ {
		switch {
		case inEscape:
			inEscape = false
		case ci.charAt(read) == '\\':
			inEscape = true
		case ci.charAt(read) == c:
			return read
		}
	}
	return -1
}

func (ci *charIndex) nextSignificantCharIsFrom(start int, c uint16) bool {
	read := start + 1
	for !ci.isOutOfBounds(read) && ci.charAt(read) == ' ' {
		read++
	}
	return !ci.isOutOfBounds(read) && ci.charAt(read) == c
}

func (ci *charIndex) nextSignificantCharIs(c uint16) bool {
	return ci.nextSignificantCharIsFrom(ci.position, c)
}

func (ci *charIndex) nextSignificantCharFrom(start int) uint16 {
	read := start + 1
	for !ci.isOutOfBounds(read) && ci.charAt(read) == ' ' {
		read++
	}
	if !ci.isOutOfBounds(read) {
		return ci.charAt(read)
	}
	return ' '
}

func (ci *charIndex) nextSignificantChar() uint16 { return ci.nextSignificantCharFrom(ci.position) }

func (ci *charIndex) readSignificantChar(c uint16) {
	if ci.skipBlanks().currentChar() != c {
		throw(InvalidPath, "Expected character: %s", charObj(c))
	}
	ci.incrementPosition(1)
}

func (ci *charIndex) hasSignificantSubSequence(s string) bool {
	ci.skipBlanks()
	js := toJ(s)
	if !ci.inBounds(ci.position + len(js) - 1) {
		return false
	}
	if ci.subSequence(ci.position, ci.position+len(js)) != s {
		return false
	}
	ci.incrementPosition(len(js))
	return true
}

func (ci *charIndex) indexOfPreviousSignificantCharFrom(start int) int {
	read := start - 1
	for !ci.isOutOfBounds(read) && ci.charAt(read) == ' ' {
		read--
	}
	if !ci.isOutOfBounds(read) {
		return read
	}
	return -1
}

func (ci *charIndex) indexOfPreviousSignificantChar() int {
	return ci.indexOfPreviousSignificantCharFrom(ci.position)
}

func (ci *charIndex) previousSignificantChar() uint16 {
	i := ci.indexOfPreviousSignificantCharFrom(ci.position)
	if i == -1 {
		return ' '
	}
	return ci.charAt(i)
}

func (ci *charIndex) isNumberCharacter(read int) bool {
	c := ci.charAt(read)
	return isJavaDigit(c) || c == '-' || c == '.' || c == 'E' || c == 'e'
}

func (ci *charIndex) skipBlanks() *charIndex {
	for ci.inBoundsCur() && ci.position < ci.end && ci.currentChar() == ' ' {
		ci.incrementPosition(1)
	}
	return ci
}

func (ci *charIndex) skipBlanksAtEnd() {
	for ci.inBoundsCur() && ci.position < ci.end && ci.lastCharIs(' ') {
		ci.decrementEndPosition(1)
	}
}

func (ci *charIndex) trim() {
	ci.skipBlanks()
	ci.skipBlanksAtEnd()
}
