package jsonx

import (
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// This file ports com.jayway.jsonpath.internal.filter.FilterCompiler, which
// compiles "[?(<expression>)]" into a predicate tree.

type filterCompiler struct {
	filter *charIndex
}

// compileFilter is FilterCompiler.compile.
func compileFilter(filterString string) predicate {
	fc := &filterCompiler{filter: newCharIndex(filterString)}
	f := fc.filter
	f.trim()
	if !f.currentCharIs('[') || !f.lastCharIs(']') {
		throw(InvalidPath, "Filter must start with '[' and end with ']'. %s", filterString)
	}
	f.incrementPosition(1)
	f.decrementEndPosition(1)
	f.trim()
	if !f.currentCharIs('?') {
		throw(InvalidPath, "Filter must start with '[?' and end with ']'. %s", filterString)
	}
	f.incrementPosition(1)
	f.trim()
	if !f.currentCharIs('(') || !f.lastCharIs(')') {
		throw(InvalidPath, "Filter must start with '[?(' and end with ')]'. %s", filterString)
	}
	return &compiledFilter{pred: fc.compile()}
}

type compiledFilter struct{ pred predicate }

func (c *compiledFilter) apply(ctx *predicateContext) bool { return c.pred.apply(ctx) }

func (c *compiledFilter) String() string {
	s := c.pred.String()
	if strings.HasPrefix(s, "(") {
		return "[?" + s + "]"
	}
	return "[?(" + s + ")]"
}

func (fc *filterCompiler) compile() (result predicate) {
	err := catch(func() {
		result = fc.readLogicalOR()
		fc.filter.skipBlanks()
		if fc.filter.inBoundsCur() {
			throw(InvalidPath, "Expected end of filter expression instead of: %s",
				fc.filter.subSequence(fc.filter.position, fc.filter.length()))
		}
	})
	if err == nil {
		return result
	}
	if err.Kind.Is(InvalidPath) {
		panic(err)
	}
	// Java builds this message with currentChar(), which itself may throw.
	panic(newErr(InvalidPath, "Failed to parse filter: %s, error on position: %d, char: %s",
		fc.filter.String(), fc.filter.position, charObj(fc.filter.currentChar())))
}

func (fc *filterCompiler) readValueNode() valueNode {
	switch fc.filter.skipBlanks().currentChar() {
	case '!':
		fc.filter.incrementPosition(1)
		switch fc.filter.skipBlanks().currentChar() {
		case '$', '@':
			return fc.readPath()
		default:
			throw(InvalidPath, "Unexpected character: %s", "!")
		}
	case '$', '@':
		return fc.readPath()
	}
	return fc.readLiteral()
}

func (fc *filterCompiler) readLiteral() valueNode {
	switch fc.filter.skipBlanks().currentChar() {
	case '"':
		return fc.readStringLiteral('"')
	case '\'':
		return fc.readStringLiteral('\'')
	case '-':
		return fc.readNumberLiteral()
	case '/':
		return fc.readPattern()
	case '[', '{':
		return fc.readJSONLiteral()
	case 'f', 't':
		return fc.readBooleanLiteral()
	case 'n':
		return fc.readNullLiteral()
	}
	return fc.readNumberLiteral()
}

func (fc *filterCompiler) readLogicalOR() predicate {
	ops := []predicate{fc.readLogicalAND()}
	for {
		save := fc.filter.position
		if !fc.filter.hasSignificantSubSequence("||") {
			fc.filter.setPosition(save)
			if len(ops) == 1 {
				return ops[0]
			}
			return &logicalExpr{op: "||", chain: ops}
		}
		ops = append(ops, fc.readLogicalAND())
	}
}

func (fc *filterCompiler) readLogicalAND() predicate {
	ops := []predicate{fc.readLogicalANDOperand()}
	for {
		save := fc.filter.position
		if !fc.filter.hasSignificantSubSequence("&&") {
			fc.filter.setPosition(save)
			if len(ops) == 1 {
				return ops[0]
			}
			return &logicalExpr{op: "&&", chain: ops}
		}
		ops = append(ops, fc.readLogicalANDOperand())
	}
}

func (fc *filterCompiler) readLogicalANDOperand() predicate {
	save := fc.filter.skipBlanks().position
	if fc.filter.skipBlanks().currentCharIs('!') {
		fc.filter.readSignificantChar('!')
		switch fc.filter.skipBlanks().currentChar() {
		case '$', '@':
			fc.filter.setPosition(save)
		default:
			return &logicalExpr{op: "!", chain: []predicate{fc.readLogicalANDOperand()}}
		}
	}
	if fc.filter.skipBlanks().currentCharIs('(') {
		fc.filter.readSignificantChar('(')
		op := fc.readLogicalOR()
		fc.filter.readSignificantChar(')')
		return op
	}
	return fc.readExpression()
}

func (fc *filterCompiler) readExpression() predicate {
	left := fc.readValueNode()
	save := fc.filter.position
	var expr predicate
	err := catchKind(InvalidPath, func() {
		op := fc.readRelationalOperator()
		right := fc.readValueNode()
		expr = &relationalExpr{left: left, op: op, right: right}
	})
	if err == nil {
		return expr
	}
	fc.filter.setPosition(save)
	pn := asPathNode(left)
	check := &pathNode{path: pn.path, existsCheck: true, shouldExist: pn.shouldExist}
	right := valueNode(falseNode)
	if check.shouldExist {
		right = trueNode
	}
	return &relationalExpr{left: check, op: "EXISTS", right: right}
}

var relationalOperators = []string{
	">=", "<=", "==", "===", "!=", "!==", "<", ">", "=~", "NIN", "IN", "CONTAINS", "ALL", "SIZE",
	"EXISTS", "TYPE", "MATCHES", "EMPTY", "SUBSETOF", "ANYOF", "NONEOF",
}

func (fc *filterCompiler) readRelationalOperator() string {
	f := fc.filter
	begin := f.skipBlanks().position
	if isRelationalOperatorChar(f.currentChar()) {
		for f.inBoundsCur() && isRelationalOperatorChar(f.currentChar()) {
			f.incrementPosition(1)
		}
	} else {
		for f.inBoundsCur() && f.currentChar() != ' ' {
			f.incrementPosition(1)
		}
	}
	operator := f.subSequence(begin, f.position)
	upper := strings.ToUpper(operator)
	for _, op := range relationalOperators {
		if op == upper {
			return op
		}
	}
	throw(InvalidPath, "Filter operator %s is not supported!", operator)
	return ""
}

func (fc *filterCompiler) readNullLiteral() valueNode {
	f := fc.filter
	if f.currentChar() == 'n' && f.inBounds(f.position+3) {
		if f.subSequence(f.position, f.position+4) == "null" {
			f.incrementPosition(4)
			return nullNode{}
		}
	}
	throw(InvalidPath, "Expected <null> value")
	return nil
}

func (fc *filterCompiler) readJSONLiteral() valueNode {
	f := fc.filter
	begin := f.position
	open := f.currentChar()
	closeCh := uint16(']')
	if open == '{' {
		closeCh = '}'
	}
	closing := f.indexOfMatchingCloseChar(f.position, open, closeCh, true, false)
	if closing == -1 {
		throw(InvalidPath, "String not closed. Expected ' in %s", f.String())
	}
	f.setPosition(closing + 1)
	return &jsonNode{json: f.subSequence(begin, f.position)}
}

func (fc *filterCompiler) endOfFlags(position int) int {
	end := position
	for fc.filter.inBounds(end) {
		if patternFlagCode(fc.filter.charAt(end)) <= 0 {
			break
		}
		end++
	}
	return end
}

func (fc *filterCompiler) readPattern() valueNode {
	f := fc.filter
	begin := f.position
	closing := f.nextIndexOfUnescaped(f.position, '/')
	if closing == -1 {
		throw(InvalidPath, "Pattern not closed. Expected / in %s", f.String())
	}
	if f.inBounds(closing + 1) {
		endFlags := fc.endOfFlags(closing + 1)
		if endFlags > closing {
			closing += endFlags - (closing + 1)
		}
	}
	f.setPosition(closing + 1)
	return newPatternNode(f.subSequence(begin, f.position))
}

func (fc *filterCompiler) readStringLiteral(endChar uint16) valueNode {
	f := fc.filter
	begin := f.position
	closing := f.nextIndexOfUnescaped(f.position, endChar)
	if closing == -1 {
		throw(InvalidPath, "String literal does not have matching quotes. Expected %s in %s", charObj(endChar), f.String())
	}
	f.setPosition(closing + 1)
	return newStringNode(f.subSequence(begin, f.position), true)
}

func (fc *filterCompiler) readNumberLiteral() valueNode {
	f := fc.filter
	begin := f.position
	for f.inBoundsCur() && f.isNumberCharacter(f.position) {
		f.incrementPosition(1)
	}
	literal := f.subSequence(begin, f.position)
	d, err := javafmt.ParseBigDecimal(literal)
	if err != nil {
		throw(NumberFormat, "%s", err.Error())
	}
	return &numberNode{number: d}
}

func (fc *filterCompiler) readBooleanLiteral() valueNode {
	f := fc.filter
	begin := f.position
	end := f.position + 4
	if f.currentChar() == 't' {
		end = f.position + 3
	}
	if !f.inBounds(end) {
		throw(InvalidPath, "Expected boolean literal")
	}
	b := f.subSequence(begin, end+1)
	if b != "true" && b != "false" {
		throw(InvalidPath, "Expected boolean literal")
	}
	f.incrementPosition(len(toJ(b)))
	if b == "true" {
		return trueNode
	}
	return falseNode
}

func (fc *filterCompiler) readPath() valueNode {
	f := fc.filter
	previous := f.previousSignificantChar()
	begin := f.position
	f.incrementPosition(1)
	for f.inBoundsCur() {
		if f.currentChar() == '[' {
			closing := f.indexOfMatchingCloseChar(f.position, '[', ']', true, false)
			if closing == -1 {
				throw(InvalidPath, "Square brackets does not match in filter %s", f.String())
			}
			f.setPosition(closing + 1)
		}
		closingFunction := f.currentChar() == ')' && fc.currentCharIsClosingFunctionBracket(begin)
		closingLogical := f.currentChar() == ')' && !closingFunction
		if !f.inBoundsCur() || isRelationalOperatorChar(f.currentChar()) || f.currentChar() == ' ' || closingLogical {
			break
		}
		f.incrementPosition(1)
	}
	shouldExist := previous != '!'
	return newPathNode(f.subSequence(begin, f.position), false, shouldExist)
}

func (fc *filterCompiler) currentCharIsClosingFunctionBracket(lowerBound int) bool {
	f := fc.filter
	if f.currentChar() != ')' {
		return false
	}
	idx := f.indexOfPreviousSignificantChar()
	if idx == -1 || f.charAt(idx) != '(' {
		return false
	}
	idx--
	for f.inBounds(idx) && idx > lowerBound {
		if f.charAt(idx) == '.' {
			return true
		}
		idx--
	}
	return false
}

func isRelationalOperatorChar(c uint16) bool {
	return c == '<' || c == '>' || c == '=' || c == '~' || c == '!'
}

// patternFlagCode is PatternFlag.getCodeByFlag.
func patternFlagCode(c uint16) int {
	switch c {
	case 'd':
		return 1
	case 'i':
		return 2
	case 'x':
		return 4
	case 'm':
		return 8
	case 's':
		return 32
	case 'u':
		return 64
	case 'U':
		return 256
	}
	return 0
}
