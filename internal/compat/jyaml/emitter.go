package jyaml

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// This file is a port of org.yaml.snakeyaml.emitter.Emitter (SnakeYAML 2.5)
// restricted to what Jackson's YAMLGenerator produces: block collections
// (flow style only for empty ones), untagged scalars, !!binary literals, no
// anchors and no comments. Method and state names follow the Java source so
// the two can be compared side by side. Text is handled as UTF-16 code units
// because the column arithmetic that decides line folding counts Java chars.

type scalarStyle int

const (
	stylePlain scalarStyle = iota
	styleSingleQuoted
	styleDoubleQuoted
	styleLiteral
	styleFolded
)

type eventKind int

const (
	evStreamStart eventKind = iota
	evStreamEnd
	evDocumentStart
	evDocumentEnd
	evScalar
	evSequenceStart
	evSequenceEnd
	evMappingStart
	evMappingEnd
)

type event struct {
	kind eventKind
	// scalar
	value            string
	style            scalarStyle
	tag              string
	implicitPlain    bool // ImplicitTuple.canOmitTagInPlainScalar
	implicitNonPlain bool // ImplicitTuple.canOmitTagInNonPlainScalar
	// document start/end
	explicit bool
}

const (
	bestIndent         = 2
	indicatorIndent    = 0
	bestWidth          = 80
	maxSimpleKeyLength = 128
	noIndent           = -1
)

// Constant sets of org.yaml.snakeyaml.scanner.Constant.
const (
	constLinebr        = "\n\u0085\u2028\u2029"
	constNullBlTLinebr = "\t \x00\r\n\u0085\u2028\u2029"
	constNullBlT       = "\x00 \t"
)

func has(set string, c int) bool { return strings.ContainsRune(set, rune(c)) }

var escapeReplacements = map[uint16]string{
	0x00: "0", 0x07: "a", 0x08: "b", 0x09: "t", 0x0A: "n", 0x0B: "v", 0x0C: "f", 0x0D: "r",
	0x1B: "e", '"': "\"", '\\': "\\", 0x85: "N", 0xA0: "_", 0x2028: "L", 0x2029: "P",
}

type scalarAnalysis struct {
	scalar            []uint16
	empty             bool
	multiline         bool
	allowFlowPlain    bool
	allowBlockPlain   bool
	allowSingleQuoted bool
	allowBlock        bool
}

type emitter struct {
	out    strings.Builder
	events []event
	next   int // index of the event after the current one
	ev     *event

	states []func()
	state  func()

	indents []int
	indent  int

	flowLevel        int
	rootContext      bool
	mappingContext   bool
	simpleKeyContext bool

	column     int
	whitespace bool
	indention  bool
	openEnded  bool
	splitLines bool

	preparedTag string
	analysis    *scalarAnalysis
	style       *scalarStyle
}

func emit(events []event) string {
	e := &emitter{events: events, indent: noIndent, whitespace: true, indention: true, splitLines: true}
	e.state = e.expectStreamStart
	for i := range events {
		e.ev = &events[i]
		e.next = i + 1
		e.state()
	}
	return e.out.String()
}

func (e *emitter) peek() *event {
	if e.next < len(e.events) {
		return &e.events[e.next]
	}
	return nil
}

func (e *emitter) push(s func()) { e.states = append(e.states, s) }

func (e *emitter) pop() func() {
	s := e.states[len(e.states)-1]
	e.states = e.states[:len(e.states)-1]
	return s
}

func (e *emitter) popIndent() int {
	i := e.indents[len(e.indents)-1]
	e.indents = e.indents[:len(e.indents)-1]
	return i
}

func (e *emitter) increaseIndent(flow, indentless bool) {
	e.indents = append(e.indents, e.indent)
	if e.indent == noIndent {
		if flow {
			e.indent = bestIndent
		} else {
			e.indent = 0
		}
	} else if !indentless {
		e.indent += bestIndent
	}
}

// States.

func (e *emitter) expectStreamStart() {
	if e.ev.kind != evStreamStart {
		panic("expected StreamStartEvent")
	}
	e.state = func() { e.expectDocumentStart(true) }
}

func (e *emitter) expectNothing() { panic("expecting nothing") }

func (e *emitter) expectDocumentStart(first bool) {
	switch e.ev.kind {
	case evDocumentStart:
		// Jackson passes an empty (non-null) tag map, so an open-ended
		// previous document is closed with "...".
		if e.openEnded {
			e.writeIndicator("...", true, false, false)
			e.writeIndent()
		}
		implicit := first && !e.ev.explicit && !e.checkEmptyDocument()
		if !implicit {
			e.writeIndent()
			e.writeIndicator("---", true, false, false)
		}
		e.state = e.expectDocumentRoot
	case evStreamEnd:
		e.state = e.expectNothing
	default:
		panic("expected DocumentStartEvent")
	}
}

func (e *emitter) expectDocumentEnd() {
	if e.ev.kind != evDocumentEnd {
		panic("expected DocumentEndEvent")
	}
	e.writeIndent()
	if e.ev.explicit {
		e.writeIndicator("...", true, false, false)
		e.writeIndent()
	}
	e.state = func() { e.expectDocumentStart(false) }
}

func (e *emitter) expectDocumentRoot() {
	e.push(e.expectDocumentEnd)
	e.expectNode(true, false, false)
}

func (e *emitter) expectNode(root, mapping, simpleKey bool) {
	e.rootContext = root
	e.mappingContext = mapping
	e.simpleKeyContext = simpleKey
	switch e.ev.kind {
	case evScalar:
		e.processTag()
		e.expectScalar()
	case evSequenceStart:
		if e.flowLevel == 0 && !e.checkEmptySequence() {
			e.expectBlockSequence()
		} else {
			e.expectFlowSequence()
		}
	case evMappingStart:
		if e.flowLevel == 0 && !e.checkEmptyMapping() {
			e.expectBlockMapping()
		} else {
			e.expectFlowMapping()
		}
	default:
		panic("expected NodeEvent")
	}
}

func (e *emitter) expectScalar() {
	e.increaseIndent(true, false)
	e.processScalar()
	e.indent = e.popIndent()
	e.state = e.pop()
}

func (e *emitter) expectFlowSequence() {
	e.writeIndicator("[", true, true, false)
	e.flowLevel++
	e.increaseIndent(true, false)
	e.state = e.expectFirstFlowSequenceItem
}

func (e *emitter) expectFirstFlowSequenceItem() {
	if e.ev.kind == evSequenceEnd {
		e.indent = e.popIndent()
		e.flowLevel--
		e.writeIndicator("]", false, false, false)
		e.state = e.pop()
		return
	}
	if e.column > bestWidth && e.splitLines {
		e.writeIndent()
	}
	e.push(e.expectFlowSequenceItem)
	e.expectNode(false, false, false)
}

func (e *emitter) expectFlowSequenceItem() {
	if e.ev.kind == evSequenceEnd {
		e.indent = e.popIndent()
		e.flowLevel--
		e.writeIndicator("]", false, false, false)
		e.state = e.pop()
		return
	}
	e.writeIndicator(",", false, false, false)
	if e.column > bestWidth && e.splitLines {
		e.writeIndent()
	}
	e.push(e.expectFlowSequenceItem)
	e.expectNode(false, false, false)
}

func (e *emitter) expectFlowMapping() {
	e.writeIndicator("{", true, true, false)
	e.flowLevel++
	e.increaseIndent(true, false)
	e.state = e.expectFirstFlowMappingKey
}

func (e *emitter) expectFirstFlowMappingKey() {
	if e.ev.kind == evMappingEnd {
		e.indent = e.popIndent()
		e.flowLevel--
		e.writeIndicator("}", false, false, false)
		e.state = e.pop()
		return
	}
	e.flowMappingKey()
}

func (e *emitter) expectFlowMappingKey() {
	if e.ev.kind == evMappingEnd {
		e.indent = e.popIndent()
		e.flowLevel--
		e.writeIndicator("}", false, false, false)
		e.state = e.pop()
		return
	}
	e.writeIndicator(",", false, false, false)
	e.flowMappingKey()
}

func (e *emitter) flowMappingKey() {
	if e.column > bestWidth && e.splitLines {
		e.writeIndent()
	}
	if e.checkSimpleKey() {
		e.push(e.expectFlowMappingSimpleValue)
		e.expectNode(false, true, true)
	} else {
		e.writeIndicator("?", true, false, false)
		e.push(e.expectFlowMappingValue)
		e.expectNode(false, true, false)
	}
}

func (e *emitter) expectFlowMappingSimpleValue() {
	e.writeIndicator(":", false, false, false)
	e.push(e.expectFlowMappingKey)
	e.expectNode(false, true, false)
}

func (e *emitter) expectFlowMappingValue() {
	if e.column > bestWidth {
		e.writeIndent()
	}
	e.writeIndicator(":", true, false, false)
	e.push(e.expectFlowMappingKey)
	e.expectNode(false, true, false)
}

func (e *emitter) expectBlockSequence() {
	indentless := e.mappingContext && !e.indention
	e.increaseIndent(false, indentless)
	e.state = func() { e.expectBlockSequenceItem(true) }
}

func (e *emitter) expectBlockSequenceItem(first bool) {
	if !first && e.ev.kind == evSequenceEnd {
		e.indent = e.popIndent()
		e.state = e.pop()
		return
	}
	e.writeIndent()
	e.writeWhitespace(indicatorIndent)
	e.writeIndicator("-", true, false, true)
	e.push(func() { e.expectBlockSequenceItem(false) })
	e.expectNode(false, false, false)
}

func (e *emitter) expectBlockMapping() {
	e.increaseIndent(false, false)
	e.state = func() { e.expectBlockMappingKey(true) }
}

func (e *emitter) expectBlockMappingKey(first bool) {
	if !first && e.ev.kind == evMappingEnd {
		e.indent = e.popIndent()
		e.state = e.pop()
		return
	}
	e.writeIndent()
	if e.checkSimpleKey() {
		e.push(e.expectBlockMappingSimpleValue)
		e.expectNode(false, true, true)
	} else {
		e.writeIndicator("?", true, false, true)
		e.push(e.expectBlockMappingValue)
		e.expectNode(false, true, false)
	}
}

func (e *emitter) expectBlockMappingSimpleValue() {
	e.writeIndicator(":", false, false, false)
	e.push(func() { e.expectBlockMappingKey(false) })
	e.expectNode(false, true, false)
}

func (e *emitter) expectBlockMappingValue() {
	e.writeIndent()
	e.writeIndicator(":", true, false, true)
	e.push(func() { e.expectBlockMappingKey(false) })
	e.expectNode(false, true, false)
}

// Checkers.

func (e *emitter) checkEmptySequence() bool {
	n := e.peek()
	return e.ev.kind == evSequenceStart && n != nil && n.kind == evSequenceEnd
}

func (e *emitter) checkEmptyMapping() bool {
	n := e.peek()
	return e.ev.kind == evMappingStart && n != nil && n.kind == evMappingEnd
}

func (e *emitter) checkEmptyDocument() bool {
	n := e.peek()
	if e.ev.kind != evDocumentStart || n == nil || n.kind != evScalar {
		return false
	}
	return n.tag == "" && n.value == ""
}

func (e *emitter) checkSimpleKey() bool {
	length := 0
	if e.ev.kind == evScalar && e.ev.tag != "" {
		if e.preparedTag == "" {
			e.preparedTag = prepareTag(e.ev.tag)
		}
		length += len(utf16.Encode([]rune(e.preparedTag)))
	}
	if e.ev.kind == evScalar {
		if e.analysis == nil {
			e.analysis = analyzeScalar(e.ev.value)
		}
		length += len(e.analysis.scalar)
	}
	return length < maxSimpleKeyLength &&
		(e.ev.kind == evScalar && !e.analysis.empty && !e.analysis.multiline ||
			e.checkEmptySequence() || e.checkEmptyMapping())
}

// Processors.

func (e *emitter) processTag() {
	ev := e.ev
	if e.style == nil {
		s := e.chooseScalarStyle()
		e.style = &s
	}
	if (*e.style == stylePlain && ev.implicitPlain) || (*e.style != stylePlain && ev.implicitNonPlain) {
		e.preparedTag = ""
		return
	}
	tag := ev.tag
	if ev.implicitPlain && tag == "" {
		tag = "!"
		e.preparedTag = ""
	}
	if e.preparedTag == "" {
		e.preparedTag = prepareTag(tag)
	}
	e.writeIndicator(e.preparedTag, true, false, false)
	e.preparedTag = ""
}

func (e *emitter) chooseScalarStyle() scalarStyle {
	ev := e.ev
	if e.analysis == nil {
		e.analysis = analyzeScalar(ev.value)
	}
	a := e.analysis
	plain := ev.style == stylePlain
	if !plain && ev.style == styleDoubleQuoted {
		return styleDoubleQuoted
	}
	if !plain || !ev.implicitPlain ||
		e.simpleKeyContext && (a.empty || a.multiline) ||
		(e.flowLevel == 0 || !a.allowFlowPlain) && (e.flowLevel != 0 || !a.allowBlockPlain) {
		if (ev.style == styleLiteral || ev.style == styleFolded) && e.flowLevel == 0 && !e.simpleKeyContext && a.allowBlock {
			return ev.style
		}
		if !plain && ev.style != styleSingleQuoted || !a.allowSingleQuoted || e.simpleKeyContext && a.multiline {
			return styleDoubleQuoted
		}
		return styleSingleQuoted
	}
	return stylePlain
}

func (e *emitter) processScalar() {
	if e.analysis == nil {
		e.analysis = analyzeScalar(e.ev.value)
	}
	split := !e.simpleKeyContext && e.splitLines
	text := e.analysis.scalar
	switch *e.style {
	case stylePlain:
		e.writePlain(text, split)
	case styleDoubleQuoted:
		e.writeDoubleQuoted(text, split)
	case styleSingleQuoted:
		e.writeSingleQuoted(text, split)
	case styleFolded:
		e.writeFolded(text, split)
	case styleLiteral:
		e.writeLiteral(text)
	}
	e.analysis = nil
	e.style = nil
}

func prepareTag(tag string) string {
	if tag == "!" {
		return tag
	}
	// DEFAULT_TAG_PREFIXES: "!" -> "!", "tag:yaml.org,2002:" -> "!!"
	var handle, prefix string
	for _, p := range []struct{ prefix, handle string }{{"!", "!"}, {"tag:yaml.org,2002:", "!!"}} {
		if strings.HasPrefix(tag, p.prefix) && (p.prefix == "!" || len(p.prefix) < len(tag)) {
			prefix, handle = p.prefix, p.handle
		}
	}
	if handle != "" {
		return handle + tag[len(prefix):]
	}
	return "!<" + tag + ">"
}

// Scalar analysis.

func hasLeadingZero(s []uint16) bool {
	if len(s) <= 1 || s[0] != '0' {
		return false
	}
	for _, ch := range s[1:] {
		if (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

// codePointAt is Java's String.codePointAt.
func codePointAt(s []uint16, i int) int {
	c := s[i]
	if utf16.IsSurrogate(rune(c)) && c < 0xDC00 && i+1 < len(s) {
		if d := s[i+1]; d >= 0xDC00 && d <= 0xDFFF {
			return int(utf16.DecodeRune(rune(c), rune(d)))
		}
	}
	return int(c)
}

func charCount(c int) int {
	if c >= 0x10000 {
		return 2
	}
	return 1
}

func analyzeScalar(value string) *scalarAnalysis {
	s := utf16.Encode([]rune(value))
	if len(s) == 0 {
		return &scalarAnalysis{scalar: s, empty: true, allowBlockPlain: true, allowSingleQuoted: true}
	}
	var blockIndicators, flowIndicators, lineBreaks, specialCharacters bool
	leadingZeroNumber := hasLeadingZero(s)
	var leadingSpace, leadingBreak, trailingSpace, trailingBreak, breakSpace, spaceBreak bool
	if strings.HasPrefix(value, "---") || strings.HasPrefix(value, "...") {
		blockIndicators = true
		flowIndicators = true
	}
	preceededByWhitespace := true
	followedByWhitespace := len(s) == 1 || has(constNullBlTLinebr, codePointAt(s, 1))
	previousSpace := false
	previousBreak := false
	index := 0
	for index < len(s) {
		c := codePointAt(s, index)
		if index == 0 {
			if has("#,[]{}&*!|>'\"%@`", c) {
				flowIndicators = true
				blockIndicators = true
			}
			if c == '?' || c == ':' {
				flowIndicators = true
				if followedByWhitespace {
					blockIndicators = true
				}
			}
			if c == '-' && followedByWhitespace {
				flowIndicators = true
				blockIndicators = true
			}
		} else {
			if has(",?[]{}", c) {
				flowIndicators = true
			}
			if c == ':' {
				flowIndicators = true
				if followedByWhitespace {
					blockIndicators = true
				}
			}
			if c == '#' && preceededByWhitespace {
				flowIndicators = true
				blockIndicators = true
			}
		}
		isLineBreak := has(constLinebr, c)
		if isLineBreak {
			lineBreaks = true
		}
		if c != '\n' && (c < 0x20 || c > 0x7E) {
			printable := c == 0x85 || c >= 0xA0 && c <= 0xD7FF || c >= 0xE000 && c <= 0xFFFD || c >= 0x10000 && c <= 0x10FFFF
			if !printable {
				specialCharacters = true
			}
		}
		switch {
		case c == ' ':
			if index == 0 {
				leadingSpace = true
			}
			if index == len(s)-1 {
				trailingSpace = true
			}
			if previousBreak {
				breakSpace = true
			}
			previousSpace = true
			previousBreak = false
		case isLineBreak:
			if index == 0 {
				leadingBreak = true
			}
			if index == len(s)-1 {
				trailingBreak = true
			}
			if previousSpace {
				spaceBreak = true
			}
			previousSpace = false
			previousBreak = true
		default:
			previousSpace = false
			previousBreak = false
		}
		index += charCount(c)
		preceededByWhitespace = has(constNullBlT, c) || isLineBreak
		followedByWhitespace = true
		if index+1 < len(s) {
			nextIndex := index + charCount(codePointAt(s, index))
			if nextIndex < len(s) {
				followedByWhitespace = has(constNullBlT, codePointAt(s, nextIndex)) || isLineBreak
			}
		}
	}
	allowFlowPlain, allowBlockPlain, allowSingleQuoted, allowBlock := true, true, true, true
	if leadingSpace || leadingBreak || trailingSpace || trailingBreak || leadingZeroNumber {
		allowFlowPlain = false
		allowBlockPlain = false
	}
	if trailingSpace {
		allowBlock = false
	}
	if breakSpace {
		allowFlowPlain = false
		allowBlockPlain = false
		allowSingleQuoted = false
	}
	if spaceBreak || specialCharacters {
		allowFlowPlain = false
		allowBlockPlain = false
		allowSingleQuoted = false
		allowBlock = false
	}
	if lineBreaks {
		allowFlowPlain = false
	}
	if flowIndicators {
		allowFlowPlain = false
	}
	if blockIndicators {
		allowBlockPlain = false
	}
	return &scalarAnalysis{
		scalar: s, multiline: lineBreaks,
		allowFlowPlain: allowFlowPlain, allowBlockPlain: allowBlockPlain,
		allowSingleQuoted: allowSingleQuoted, allowBlock: allowBlock,
	}
}

// Writers.

func (e *emitter) write(s []uint16) { e.out.WriteString(string(utf16.Decode(s))) }

func (e *emitter) writeString(s string) { e.out.WriteString(s) }

func (e *emitter) writeIndicator(indicator string, needWhitespace, whitespace, indentation bool) {
	if !e.whitespace && needWhitespace {
		e.column++
		e.out.WriteByte(' ')
	}
	e.whitespace = whitespace
	e.indention = e.indention && indentation
	e.column += len(utf16.Encode([]rune(indicator)))
	e.openEnded = false
	e.out.WriteString(indicator)
}

func (e *emitter) writeIndent() {
	indent := e.indent
	if indent == noIndent {
		indent = 0
	}
	if !e.indention || e.column > indent || e.column == indent && !e.whitespace {
		e.writeLineBreak(nil)
	}
	e.writeWhitespace(indent - e.column)
}

func (e *emitter) writeWhitespace(length int) {
	if length <= 0 {
		return
	}
	e.whitespace = true
	e.out.WriteString(strings.Repeat(" ", length))
	e.column += length
}

func (e *emitter) writeLineBreak(data []uint16) {
	e.whitespace = true
	e.indention = true
	e.column = 0
	if data == nil {
		e.out.WriteByte('\n')
	} else {
		e.write(data)
	}
}

// writeBreaks writes each line break char of data: '\n' as the configured
// line break, others verbatim.
func (e *emitter) writeBreaks(data []uint16) {
	for _, br := range data {
		if br == '\n' {
			e.writeLineBreak(nil)
		} else {
			e.writeLineBreak([]uint16{br})
		}
	}
}

func isLinebr(ch uint16) bool { return has(constLinebr, int(ch)) }

func (e *emitter) writeSingleQuoted(text []uint16, split bool) {
	e.writeIndicator("'", true, false, false)
	spaces := false
	breaks := false
	start := 0
	for end := 0; end <= len(text); end++ {
		var ch uint16
		if end < len(text) {
			ch = text[end]
		}
		switch {
		case spaces:
			if ch == 0 || ch != ' ' {
				if start+1 == end && e.column > bestWidth && split && start != 0 && end != len(text) {
					e.writeIndent()
				} else {
					e.column += end - start
					e.write(text[start:end])
				}
				start = end
			}
		case !breaks:
			if (isLinebr(ch) || ch == 0 || ch == ' ' || ch == '\'') && start < end {
				e.column += end - start
				e.write(text[start:end])
				start = end
			}
		default:
			if ch == 0 || !isLinebr(ch) {
				if text[start] == '\n' {
					e.writeLineBreak(nil)
				}
				e.writeBreaks(text[start:end])
				e.writeIndent()
				start = end
			}
		}
		if ch == '\'' {
			e.column += 2
			e.writeString("''")
			start = end + 1
		}
		if ch != 0 {
			spaces = ch == ' '
			breaks = isLinebr(ch)
		}
	}
	e.writeIndicator("'", false, false, false)
}

func isPrintable(c int) bool {
	return c >= 0x20 && c <= 0x7E || c == 0x09 || c == 0x0A || c == 0x0D || c == 0x85 ||
		c >= 0xA0 && c <= 0xD7FF || c >= 0xE000 && c <= 0xFFFD || c >= 0x10000 && c <= 0x10FFFF
}

func (e *emitter) writeDoubleQuoted(text []uint16, split bool) {
	e.writeIndicator("\"", true, false, false)
	start := 0
	for end := 0; end <= len(text); end++ {
		isNull := end >= len(text)
		var ch uint16
		if !isNull {
			ch = text[end]
		}
		if isNull || ch == '"' || ch == '\\' || ch == 0x85 || ch == 0x2028 || ch == 0x2029 || ch == 0xFEFF || ch < ' ' || ch > '~' {
			if start < end {
				e.column += end - start
				e.write(text[start:end])
				start = end
			}
			if !isNull {
				var data string
				if r, ok := escapeReplacements[ch]; ok {
					data = "\\" + r
				} else {
					codePoint := int(ch)
					if ch >= 0xD800 && ch <= 0xDBFF && end+1 < len(text) {
						codePoint = (int(ch)-0xD800)<<10 + (int(text[end+1]) - 0xDC00) + 0x10000
					}
					switch {
					case isPrintable(codePoint):
						data = string(utf16.Decode(text[end : end+charCount(codePoint)]))
						if charCount(codePoint) == 2 {
							end++
						}
					case ch <= 0xFF:
						s := "0" + strconv.FormatInt(int64(ch), 16)
						data = "\\x" + s[len(s)-2:]
					case charCount(codePoint) == 2:
						end++
						s := "000" + strconv.FormatInt(int64(codePoint), 16)
						data = "\\U" + s[len(s)-8:]
					default:
						s := "000" + strconv.FormatInt(int64(ch), 16)
						data = "\\u" + s[len(s)-4:]
					}
				}
				e.column += len(utf16.Encode([]rune(data)))
				e.writeString(data)
				start = end + 1
			}
		}
		if 0 < end && end < len(text)-1 && (ch == ' ' || start >= end) && e.column+(end-start) > bestWidth && split {
			var data []uint16
			if start >= end {
				data = []uint16{'\\'}
			} else {
				data = append(append([]uint16(nil), text[start:end]...), '\\')
			}
			if start < end {
				start = end
			}
			e.column += len(data)
			e.write(data)
			e.writeIndent()
			e.whitespace = false
			e.indention = false
			if text[start] == ' ' {
				e.column++
				e.writeString("\\")
			}
		}
	}
	e.writeIndicator("\"", false, false, false)
}

func (e *emitter) determineBlockHints(text []uint16) string {
	var hints strings.Builder
	if isLinebr(text[0]) || text[0] == ' ' {
		hints.WriteString(strconv.Itoa(bestIndent))
	}
	ch1 := text[len(text)-1]
	if !isLinebr(ch1) {
		hints.WriteString("-")
	} else if len(text) == 1 || isLinebr(text[len(text)-2]) {
		hints.WriteString("+")
	}
	return hints.String()
}

func (e *emitter) writeFolded(text []uint16, split bool) {
	hints := e.determineBlockHints(text)
	e.writeIndicator(">"+hints, true, false, false)
	if strings.HasSuffix(hints, "+") {
		e.openEnded = true
	}
	e.writeLineBreak(nil)
	leadingSpace := true
	spaces := false
	breaks := true
	start := 0
	for end := 0; end <= len(text); end++ {
		var ch uint16
		if end < len(text) {
			ch = text[end]
		}
		switch {
		case breaks:
			if ch == 0 || !isLinebr(ch) {
				if !leadingSpace && ch != 0 && ch != ' ' && text[start] == '\n' {
					e.writeLineBreak(nil)
				}
				leadingSpace = ch == ' '
				e.writeBreaks(text[start:end])
				if ch != 0 {
					e.writeIndent()
				}
				start = end
			}
		case spaces:
			if ch != ' ' {
				if start+1 == end && e.column > bestWidth && split {
					e.writeIndent()
				} else {
					e.column += end - start
					e.write(text[start:end])
				}
				start = end
			}
		default:
			if isLinebr(ch) || ch == 0 || ch == ' ' {
				e.column += end - start
				e.write(text[start:end])
				if ch == 0 {
					e.writeLineBreak(nil)
				}
				start = end
			}
		}
		if ch != 0 {
			breaks = isLinebr(ch)
			spaces = ch == ' '
		}
	}
}

func (e *emitter) writeLiteral(text []uint16) {
	hints := e.determineBlockHints(text)
	e.writeIndicator("|"+hints, true, false, false)
	if strings.HasSuffix(hints, "+") {
		e.openEnded = true
	}
	e.writeLineBreak(nil)
	breaks := true
	start := 0
	for end := 0; end <= len(text); end++ {
		var ch uint16
		if end < len(text) {
			ch = text[end]
		}
		if !breaks {
			if ch == 0 || isLinebr(ch) {
				e.write(text[start:end])
				if ch == 0 {
					e.writeLineBreak(nil)
				}
				start = end
			}
		} else if ch == 0 || !isLinebr(ch) {
			e.writeBreaks(text[start:end])
			if ch != 0 {
				e.writeIndent()
			}
			start = end
		}
		if ch != 0 {
			breaks = isLinebr(ch)
		}
	}
}

func (e *emitter) writePlain(text []uint16, split bool) {
	if e.rootContext {
		e.openEnded = true
	}
	if len(text) == 0 {
		return
	}
	if !e.whitespace {
		e.column++
		e.out.WriteByte(' ')
	}
	e.whitespace = false
	e.indention = false
	spaces := false
	breaks := false
	start := 0
	for end := 0; end <= len(text); end++ {
		var ch uint16
		if end < len(text) {
			ch = text[end]
		}
		switch {
		case spaces:
			if ch != ' ' {
				if start+1 == end && e.column > bestWidth && split {
					e.writeIndent()
					e.whitespace = false
					e.indention = false
				} else {
					e.column += end - start
					e.write(text[start:end])
				}
				start = end
			}
		case !breaks:
			if isLinebr(ch) || ch == 0 || ch == ' ' {
				e.column += end - start
				e.write(text[start:end])
				start = end
			}
		default:
			if !isLinebr(ch) {
				if text[start] == '\n' {
					e.writeLineBreak(nil)
				}
				e.writeBreaks(text[start:end])
				e.writeIndent()
				e.whitespace = false
				e.indention = false
				start = end
			}
		}
		if ch != 0 {
			spaces = ch == ' '
			breaks = isLinebr(ch)
		}
	}
}
