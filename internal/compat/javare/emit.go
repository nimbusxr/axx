package javare

import (
	"fmt"
	"strconv"
	"strings"
)

// emit renders the AST as a regexp2 (.NET syntax) pattern that relies on no
// regexp2 options and no .NET defaults: every character set is spelled out,
// case-insensitivity has already been expanded into the sets, and anchors
// are written as explicit look-arounds with Java's line-terminator rules.
func emit(n node, groups int) string {
	e := &emitter{groups: groups}
	e.node(n)
	return e.sb.String()
}

// emitWithoutLastMatch renders n with \G as a never-matching assertion, for
// searches that follow an empty match: Java starts those one character
// later while \G still refers to the end of the previous match.
func emitWithoutLastMatch(n node, groups int) (string, bool) {
	e := &emitter{groups: groups, noLastMatch: true}
	e.node(n)
	return e.sb.String(), e.sawLastMatch
}

type emitter struct {
	sb           strings.Builder
	groups       int
	noLastMatch  bool
	sawLastMatch bool
}

func (e *emitter) node(n node) {
	switch x := n.(type) {
	case nil:
	case seqNode:
		for _, c := range x {
			e.node(c)
		}
	case altNode:
		e.sb.WriteString("(?:")
		for i, a := range x {
			if i > 0 {
				e.sb.WriteByte('|')
			}
			e.node(a)
		}
		e.sb.WriteByte(')')
	case *setNode:
		e.sb.WriteString(setPattern(x.cs))
	case *repNode:
		e.repeat(x)
	case *groupNode:
		e.group(x)
	case *assertNode:
		if x.kind == aLastMatch {
			e.sawLastMatch = true
			if e.noLastMatch {
				e.sb.WriteString("(?!)")
				return
			}
		}
		e.sb.WriteString(assertPattern(x))
	case *lineEndNode:
		e.sb.WriteString(`(?:\r\n|[\n\x0B\f\r\u0085\u2028\u2029])`)
	case *graphemeNode:
		// Approximation of an extended grapheme cluster: a character with
		// its combining marks, variation selectors, emoji modifiers and
		// zero-width-joiner continuations.
		e.sb.WriteString(`(?>\r\n|[\s\S](?:[\p{Mn}\p{Me}\p{Mc}\u200C\uFE0E\uFE0F\x{1F3FB}-\x{1F3FF}]|\u200D[\s\S])*)`)
	case *backrefNode:
		if x.index < 1 || x.index > e.groups {
			// Java accepts references to groups that do not exist; they
			// never match.
			e.sb.WriteString("(?!)")
			return
		}
		if x.ci {
			e.sb.WriteString(`(?i:\` + strconv.Itoa(x.index) + ")")
		} else {
			e.sb.WriteString(`(?:\` + strconv.Itoa(x.index) + ")")
		}
	default:
		panic(fmt.Sprintf("javare: unexpected node %T", n))
	}
}

func (e *emitter) repeat(x *repNode) {
	var q string
	switch {
	case x.min == 0 && x.max < 0:
		q = "*"
	case x.min == 1 && x.max < 0:
		q = "+"
	case x.min == 0 && x.max == 1:
		q = "?"
	case x.max < 0:
		q = "{" + strconv.Itoa(x.min) + ",}"
	case x.min == x.max:
		q = "{" + strconv.Itoa(x.min) + "}"
	default:
		q = "{" + strconv.Itoa(x.min) + "," + strconv.Itoa(x.max) + "}"
	}
	if x.kind == lazy {
		q += "?"
	}
	if x.kind == possessive {
		e.sb.WriteString("(?>")
	}
	e.sb.WriteString("(?:")
	e.node(x.sub)
	e.sb.WriteString(")")
	e.sb.WriteString(q)
	if x.kind == possessive {
		e.sb.WriteString(")")
	}
}

func (e *emitter) group(x *groupNode) {
	switch x.kind {
	case gCapture:
		e.sb.WriteString("(")
	case gNonCapture:
		e.sb.WriteString("(?:")
	case gAtomic:
		e.sb.WriteString("(?>")
	case gLookahead:
		e.sb.WriteString("(?=")
	case gNegLookahead:
		e.sb.WriteString("(?!")
	case gLookbehind, gNegLookbehind:
		if x.rmax < x.rmin {
			// Java only tries match lengths between the computed bounds;
			// an overflowed maximum leaves none: the look-behind never
			// matches.
			if x.kind == gLookbehind {
				e.sb.WriteString("(?!)")
			}
			return
		}
		if x.kind == gLookbehind {
			e.sb.WriteString("(?<=")
		} else {
			e.sb.WriteString("(?<!")
		}
	}
	e.node(x.sub)
	e.sb.WriteString(")")
}

const (
	lettersOrDigits = `[\p{L}\p{Nd}]`
)

func assertPattern(a *assertNode) string {
	switch a.kind {
	case aBegin:
		return `\A`
	case aEnd:
		return `\z`
	case aCaret:
		return `(?!\z)(?:\A|(?<=[\n\u0085\u2028\u2029])|(?<=\r)(?!\n))`
	case aUnixCaret:
		return `(?!\z)(?:\A|(?<=\n))`
	case aDollar:
		return `(?=\r\n\z|(?<!\r)\n\z|[\r\u0085\u2028\u2029]\z|\z)`
	case aDollarML:
		return `(?=(?<!\r)\n|[\r\u0085\u2028\u2029]|\z)`
	case aUnixDollar:
		return `(?=\n?\z)`
	case aUnixDollarML:
		return `(?=\n|\z)`
	case aLastMatch:
		return `\G`
	case aBound, aNotBound:
		// Java's Bound: a side counts as a word character when it is one,
		// or when it is a non-spacing mark attached to a letter or digit.
		w := setPattern(wordSet(boolFlag(a.uword)))
		left := `(?:(?<=` + w + `)|(?<=` + lettersOrDigits + `\p{Mn}+))`
		right := `(?:(?=` + w + `)|(?=\p{Mn})(?<=` + lettersOrDigits + `\p{Mn}*))`
		if a.kind == aBound {
			return `(?:(?=` + left + `)(?!` + right + `)|(?!` + left + `)(?=` + right + `))`
		}
		return `(?:(?=` + left + `)(?=` + right + `)|(?!` + left + `)(?!` + right + `))`
	}
	panic("javare: unexpected assertion")
}

func boolFlag(unicodeWords bool) Flag {
	if unicodeWords {
		return UnicodeCharacterClass
	}
	return 0
}

// setPattern renders a code point set as a regexp2 atom.
func setPattern(cs charset) string {
	if cs.isEmpty() {
		return "(?!)"
	}
	if r, ok := cs.single(); ok {
		return literal(r)
	}
	neg := cs.negate()
	if neg.isEmpty() {
		return `[\u0000-\x{10FFFF}]`
	}
	if len(neg) < len(cs) {
		return "[^" + classBody(neg) + "]"
	}
	return "[" + classBody(cs) + "]"
}

func classBody(cs charset) string {
	var sb strings.Builder
	for _, r := range cs {
		sb.WriteString(literal(r.lo))
		if r.hi != r.lo {
			if r.hi > r.lo+1 {
				sb.WriteByte('-')
			}
			sb.WriteString(literal(r.hi))
		}
	}
	return sb.String()
}

// literal renders one code point: ASCII letters and digits as themselves,
// everything else as an escape.
func literal(r rune) string {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return string(r)
	case r <= 0xFFFF:
		return fmt.Sprintf(`\u%04X`, r)
	}
	return fmt.Sprintf(`\x{%X}`, r)
}
