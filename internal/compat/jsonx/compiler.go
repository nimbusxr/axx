package jsonx

import (
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// This file ports com.jayway.jsonpath.internal.path.PathCompiler.

// compilePath is PathCompiler.compile(String): paths not starting with '$' or
// '@' get "$." prepended; every failure surfaces as InvalidPath.
func compilePath(path string) (cp *compiledPath) {
	err := catch(func() {
		ci := newCharIndex(path)
		ci.trim()
		if ci.charAt(0) != '$' && ci.charAt(0) != '@' {
			ci = newCharIndex("$." + path)
			ci.trim()
		}
		if ci.lastCharIs('.') {
			throw(InvalidPath, "Path must not end with a '.' or '..'")
		}
		cp = (&pathCompiler{path: ci}).compile()
	})
	if err == nil {
		return cp
	}
	if err.Kind.Is(InvalidPath) {
		panic(err)
	}
	panic(wrapCause(InvalidPath, err))
}

type pathCompiler struct {
	path *charIndex
}

func (pc *pathCompiler) compile() *compiledPath {
	root := pc.readContextToken()
	return newCompiledPath(root, root.pathFragment() == "$")
}

func isPathWhitespace(c uint16) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (pc *pathCompiler) readWhitespace() {
	for pc.path.inBoundsCur() && isPathWhitespace(pc.path.currentChar()) {
		pc.path.incrementPosition(1)
	}
}

func (pc *pathCompiler) readContextToken() *rootToken {
	pc.readWhitespace()
	c := pc.path.currentChar()
	if c != '$' && c != '@' {
		throw(InvalidPath, "Path must start with '$' or '@'")
	}
	root := newRootToken(c)
	if pc.path.currentIsTail() {
		return root
	}
	pc.path.incrementPosition(1)
	if pc.path.currentChar() != '.' && pc.path.currentChar() != '[' {
		throw(InvalidPath, "Illegal character at position %d expected '.' or '['", pc.path.position)
	}
	pc.readNextToken(root)
	return root
}

func (pc *pathCompiler) readNextToken(root *rootToken) bool {
	p := pc.path
	switch p.currentChar() {
	case '*':
		if !pc.readWildCardToken(root) {
			throw(InvalidPath, "Could not parse token starting at position %d", p.position)
		}
	case '.':
		if !pc.readDotToken(root) {
			throw(InvalidPath, "Could not parse token starting at position %d", p.position)
		}
	case '[':
		if !pc.readBracketPropertyToken(root) && !pc.readArrayToken(root) && !pc.readWildCardToken(root) &&
			!pc.readFilterToken(root) {
			pc.rejectPlaceholderToken()
			throw(InvalidPath, "Could not parse token starting at position %d. Expected ?, ', 0-9, * ", p.position)
		}
	default:
		if !pc.readPropertyOrFunctionToken(root) {
			throw(InvalidPath, "Could not parse token starting at position %d", p.position)
		}
	}
	return true
}

func (pc *pathCompiler) readDotToken(root *rootToken) bool {
	p := pc.path
	if p.currentCharIs('.') && p.nextCharIs('.') {
		root.appendToken(&scanToken{tokenBase: newBase()})
		p.incrementPosition(2)
	} else {
		if !p.hasMoreCharacters() {
			throw(InvalidPath, "Path must not end with a '.")
		}
		p.incrementPosition(1)
	}
	if p.currentCharIs('.') {
		throw(InvalidPath, "Character '.' on position %d is not valid.", p.position)
	}
	return pc.readNextToken(root)
}

func (pc *pathCompiler) readPropertyOrFunctionToken(root *rootToken) bool {
	p := pc.path
	if p.currentCharIs('[') || p.currentCharIs('*') || p.currentCharIs('.') || p.currentCharIs(' ') {
		return false
	}
	start := p.position
	read := start
	end := 0
	isFunction := false
	for ; p.inBounds(read); read++ {
		c := p.charAt(read)
		if c == ' ' {
			throw(InvalidPath, "Use bracket notion ['my prop'] if your property contains blank characters. position: %d", p.position)
		}
		if c == '.' || c == '[' {
			end = read
			break
		}
		if c == '(' {
			isFunction = true
			end = read
			break
		}
	}
	if end == 0 {
		end = p.length()
	}
	var params []*parameter
	if isFunction {
		parens := 1
		for i := read + 1; i < p.length(); i++ {
			switch p.charAt(i) {
			case ')':
				parens--
			case '(':
				parens++
			}
			if parens == 0 {
				break
			}
		}
		if parens != 0 {
			throw(InvalidPath, "Arguments to function: '%s' are not closed properly.", p.subSequence(start, end))
		}
		if p.inBounds(read + 1) {
			if p.charAt(read+1) != ')' {
				p.setPosition(end + 1)
				params = pc.parseFunctionParameters(p.subSequence(start, end))
			} else {
				p.setPosition(read + 1)
			}
		} else {
			p.setPosition(read)
		}
	} else {
		p.setPosition(end)
	}
	property := p.subSequence(start, end)
	if isFunction {
		root.appendToken(newFunctionToken(property, params))
	} else {
		root.appendToken(newPropertyToken([]string{property}, '\''))
	}
	return p.currentIsTail() || pc.readNextToken(root)
}

func (pc *pathCompiler) parseFunctionParameters(funcName string) []*parameter {
	p := pc.path
	var kind paramKind
	groupParen, groupBracket, groupBrace, groupQuote := 1, 0, 0, 0
	endOfStream := false
	var prior uint16
	var params []*parameter
	var param []uint16
	for p.inBoundsCur() && !endOfStream {
		c := p.currentChar()
		p.incrementPosition(1)
		if kind == 0 {
			if isPathWhitespace(c) {
				continue
			}
			switch {
			case c == '{' || isJavaDigit(c) || c == '"' || c == '-':
				kind = paramJSON
			case c == '$' || c == '@':
				kind = paramPath
			}
		}
		switch c {
		case '"':
			if prior != '\\' && groupQuote > 0 {
				groupQuote--
			} else {
				groupQuote++
			}
		case '(':
			groupParen++
		case ')', ',':
			if c == ')' {
				groupParen--
				if groupParen < 0 || prior == '(' {
					param = append(param, c)
				}
			}
			if groupQuote == 0 && groupBrace == 0 && groupBracket == 0 && (groupParen == 0 && c == ')' || groupParen == 1) {
				endOfStream = groupParen == 0
				if kind != 0 {
					text := jstr(param).String()
					switch kind {
					case paramJSON:
						params = append(params, &parameter{kind: paramJSON, json: text})
					case paramPath:
						params = append(params, &parameter{kind: paramPath, path: (&pathCompiler{path: newCharIndex(text)}).compile()})
					}
					param = param[:0]
					kind = 0
				}
			}
		case '[':
			groupBracket++
		case ']':
			if groupBracket == 0 {
				throw(InvalidPath, "Unexpected close bracket ']' at character position: %d", p.position)
			}
			groupBracket--
		case '{':
			groupBrace++
		case '}':
			if groupBrace == 0 {
				throw(InvalidPath, "Unexpected close brace '}' at character position: %d", p.position)
			}
			groupBrace--
		}
		if kind != 0 && (c != ',' || groupBrace != 0 || groupBracket != 0 || groupParen != 1) {
			param = append(param, c)
		}
		prior = c
	}
	if groupBrace != 0 || groupParen != 0 || groupBracket != 0 {
		throw(InvalidPath, "Arguments to function: '%s' are not closed properly.", funcName)
	}
	return params
}

// rejectPlaceholderToken is readPlaceholderToken: a "[?]" placeholder needs
// a Predicate passed alongside the path, which never happens here, so a
// recognized placeholder always fails.
func (pc *pathCompiler) rejectPlaceholderToken() {
	p := pc.path
	if !p.currentCharIs('[') {
		return
	}
	q := p.indexOfNextSignificantChar('?')
	if q == -1 {
		return
	}
	next := p.nextSignificantCharFrom(q)
	if next != ']' && next != ',' {
		return
	}
	begin := p.position + 1
	end := p.nextIndexOf(begin, ']')
	if end == -1 {
		return
	}
	expression := p.subSequence(begin, end)
	if len(javaSplit(expression, ",")) > 0 {
		throw(InvalidPath, "Not enough predicates supplied for filter [%s] at position %d", expression, p.position)
	}
}

func (pc *pathCompiler) readFilterToken(root *rootToken) bool {
	p := pc.path
	if !p.currentCharIs('[') && !p.nextSignificantCharIs('?') {
		return false
	}
	openStatement := p.position
	q := p.indexOfNextSignificantChar('?')
	if q == -1 {
		return false
	}
	openBracket := p.indexOfNextSignificantCharFrom(q, '(')
	if openBracket == -1 {
		return false
	}
	closeBracket := p.indexOfClosingBracket(openBracket, true, true)
	if closeBracket == -1 {
		return false
	}
	if !p.nextSignificantCharIsFrom(closeBracket, ']') {
		return false
	}
	closeStatement := p.indexOfNextSignificantCharFrom(closeBracket, ']')
	criteria := p.subSequence(openStatement, closeStatement+1)
	root.appendToken(&predicateToken{tokenBase: newBase(), predicates: []predicate{compileFilter(criteria)}})
	p.setPosition(closeStatement + 1)
	return p.currentIsTail() || pc.readNextToken(root)
}

func (pc *pathCompiler) readWildCardToken(root *rootToken) bool {
	p := pc.path
	inBracket := p.currentCharIs('[')
	if inBracket && !p.nextSignificantCharIs('*') {
		return false
	}
	if !p.currentCharIs('*') && p.isOutOfBounds(p.position+1) {
		return false
	}
	if inBracket {
		wildcard := p.indexOfNextSignificantChar('*')
		if !p.nextSignificantCharIsFrom(wildcard, ']') {
			throw(InvalidPath, "Expected wildcard token to end with ']' on position %d", wildcard+1)
		}
		closeIdx := p.indexOfNextSignificantCharFrom(wildcard, ']')
		p.setPosition(closeIdx + 1)
	} else {
		p.incrementPosition(1)
	}
	root.appendToken(newWildcardToken())
	return p.currentIsTail() || pc.readNextToken(root)
}

func (pc *pathCompiler) readArrayToken(root *rootToken) bool {
	p := pc.path
	if !p.currentCharIs('[') {
		return false
	}
	next := p.nextSignificantChar()
	if !isJavaDigit(next) && next != '-' && next != ':' {
		return false
	}
	begin := p.position + 1
	end := p.nextIndexOf(begin, ']')
	if end == -1 {
		return false
	}
	expression := javaTrim(p.subSequence(begin, end))
	if expression == "*" {
		return false
	}
	for _, c := range toJ(expression) {
		if !isJavaDigit(c) && c != ',' && c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	if strings.Contains(expression, ":") {
		root.appendToken(parseSliceOperation(expression))
	} else {
		root.appendToken(parseIndexOperation(expression))
	}
	p.setPosition(end + 1)
	return p.currentIsTail() || pc.readNextToken(root)
}

func parseIndexOperation(op string) *arrayIndexToken {
	for _, c := range toJ(op) {
		if !isJavaDigit(c) && c != ',' && c != ' ' && c != '-' {
			throw(InvalidPath, "Failed to parse ArrayIndexOperation: %s", op)
		}
	}
	tokens := splitCommaKeepAll(op)
	indexes := make([]int, 0, len(tokens))
	for _, t := range tokens {
		v, err := javafmt.ParseInt(t)
		if err != nil {
			throw(InvalidPath, "Failed to parse token in ArrayIndexOperation: %s", t)
		}
		indexes = append(indexes, int(v))
	}
	return &arrayIndexToken{tokenBase: newBase(), indexes: indexes}
}

func parseSliceOperation(op string) *arraySliceToken {
	for _, c := range toJ(op) {
		if !isJavaDigit(c) && c != '-' && c != ':' {
			throw(InvalidPath, "Failed to parse SliceOperation: %s", op)
		}
	}
	tokens := javaSplit(op, ":")
	tryRead := func(idx int) *int {
		if len(tokens) <= idx || tokens[idx] == "" {
			return nil
		}
		v, err := javafmt.ParseInt(tokens[idx])
		if err != nil {
			throw(NumberFormat, "%s", err.Error())
		}
		n := int(v)
		return &n
	}
	from := tryRead(0)
	to := tryRead(1)
	t := &arraySliceToken{tokenBase: newBase(), from: from, to: to}
	switch {
	case from != nil && to == nil:
		t.kind = sliceFrom
	case from != nil:
		t.kind = sliceBetween
	case to != nil:
		t.kind = sliceTo
	default:
		throw(InvalidPath, "Failed to parse SliceOperation: %s", op)
	}
	return t
}

func (pc *pathCompiler) readBracketPropertyToken(root *rootToken) bool {
	p := pc.path
	if !p.currentCharIs('[') {
		return false
	}
	delim := p.nextSignificantChar()
	if delim != '\'' && delim != '"' {
		return false
	}
	var properties []string
	start := p.position + 1
	read := start
	end := 0
	inProperty, inEscape, lastWasComma := false, false, false
	for ; p.inBounds(read); read++ {
		c := p.charAt(read)
		switch {
		case inEscape:
			inEscape = false
		case c == '\\':
			inEscape = true
		case c == ']' && !inProperty:
			if lastWasComma {
				throw(InvalidPath, "Found empty property at index %d", read)
			}
			goto done
		case c == delim:
			if inProperty {
				nextSig := p.nextSignificantCharFrom(read)
				if nextSig != ']' && nextSig != ',' {
					throw(InvalidPath, "Property must be separated by comma or Property must be terminated close square bracket at index %d", read)
				}
				end = read
				properties = append(properties, unescape(p.subSequence(start, read)))
				inProperty = false
			} else {
				start = read + 1
				inProperty = true
				lastWasComma = false
			}
		case c == ',' && !inProperty:
			if lastWasComma {
				throw(InvalidPath, "Found empty property at index %d", read)
			}
			lastWasComma = true
		}
	}
done:
	if inProperty {
		throw(InvalidPath, "Property has not been closed - missing closing %s", charObj(delim))
	}
	endBracket := p.indexOfNextSignificantCharFrom(end, ']')
	if endBracket == -1 {
		throw(InvalidPath, "Property has not been closed - missing closing ]")
	}
	p.setPosition(endBracket + 1)
	root.appendToken(newPropertyToken(properties, delim))
	return p.currentIsTail() || pc.readNextToken(root)
}
