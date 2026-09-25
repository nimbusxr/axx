// Package javare implements Java (java.util.regex) regular expressions on top
// of github.com/dlclark/regexp2, a pure-Go backtracking engine.
//
// Steps use Java regex syntax and semantics (hamcrest matchesPattern, JsonPath =~ filters). Go's regexp
// package differs in both, and regexp2 follows .NET, which differs in many
// details too. Instead of passing patterns through, [Compile] parses the Java
// syntax with a port of java.util.regex.Pattern's own parser (so exactly the
// patterns Java rejects are rejected) and emits an equivalent regexp2 pattern
// that spells everything out explicitly:
//
//   - \Q...\E quoting, possessive quantifiers (as atomic groups), inline
//     flags (?idmsuxU-idmsuxU) and scoped flags, (?x) comments mode;
//   - Java character classes with union, nesting and && intersection, POSIX
//     (\p{Alpha}), java.lang.Character (\p{javaLowerCase}), category, script,
//     binary-property and block classes, computed as explicit code point sets;
//   - Java's ASCII-only \w, \d, \s and \b (Unicode with (?U)), its line
//     terminator set for ., ^, $ and \Z, \R, \h, \v;
//   - Java's case-insensitive matching, which is ASCII-only unless
//     UNICODE_CASE is set, applied per character;
//   - group numbering in order of the opening parenthesis (named groups
//     included) and back references to groups that do not exist (they never
//     match in Java).
//
// Known approximations: \X uses a simplified grapheme cluster rule; a case
// insensitive back reference folds case the .NET way; \p{javaMirrored} covers
// the common paired brackets only; Unicode blocks (\p{InGreek}) are limited
// to a common subset; \N{name}, \b{g}, CANON_EQ and the Emoji properties are
// not supported (compile errors here); and after an empty match a find loop
// steps a whole code point where Java steps one UTF-16 unit.
//
// Every match runs with a timeout ([MatchTimeout]) so a pathological pattern
// cannot hang a test run.
package javare

import (
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// Flag is a java.util.regex.Pattern flag.
type Flag int

// Pattern flags, with Java's values.
const (
	UnixLines             Flag = 0x01
	CaseInsensitive       Flag = 0x02
	Comments              Flag = 0x04
	Multiline             Flag = 0x08
	Literal               Flag = 0x10
	DotAll                Flag = 0x20
	UnicodeCase           Flag = 0x40
	CanonEq               Flag = 0x80
	UnicodeCharacterClass Flag = 0x100
)

// MatchTimeout bounds every match operation.
var MatchTimeout = 5 * time.Second

// SyntaxError is java.util.regex.PatternSyntaxException.
type SyntaxError struct {
	Description string
	Index       int
	Pattern     string
}

// Error renders the message like PatternSyntaxException.getMessage().
func (e *SyntaxError) Error() string {
	var sb strings.Builder
	sb.WriteString(e.Description)
	if e.Index >= 0 {
		sb.WriteString(" near index ")
		sb.WriteString(strconv.Itoa(e.Index))
	}
	sb.WriteString("\n")
	sb.WriteString(e.Pattern)
	if e.Index >= 0 && e.Index < len([]rune(e.Pattern))+1 {
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat(" ", e.Index))
		sb.WriteString("^")
	}
	return sb.String()
}

// Regexp is a compiled Java regular expression. It is safe for concurrent use.
type Regexp struct {
	pattern string
	find    *regexp2.Regexp
	full    *regexp2.Regexp
	groups  int
	names   map[string]int
	// afterEmpty is the pattern with \G never matching, used by find loops
	// after an empty match (nil when the pattern has no \G).
	afterEmpty *regexp2.Regexp
}

// Compile compiles a Java regular expression, as Pattern.compile(pattern).
func Compile(pattern string) (*Regexp, error) {
	return CompileFlags(pattern, 0)
}

// MustCompile is Compile that panics on error.
func MustCompile(pattern string) *Regexp {
	re, err := Compile(pattern)
	if err != nil {
		panic(err)
	}
	return re
}

// CompileFlags compiles a Java regular expression with Pattern flags, as
// Pattern.compile(pattern, flags).
func CompileFlags(pattern string, flags Flag) (*Regexp, error) {
	if flags&CanonEq != 0 {
		return nil, &SyntaxError{Description: "CANON_EQ is not supported", Index: -1, Pattern: pattern}
	}
	tree, groups, names, err := parse(pattern, flags)
	if err != nil {
		return nil, err
	}
	body := emit(tree, groups)
	find, err := regexp2.Compile(body, regexp2.None)
	if err != nil {
		return nil, &SyntaxError{Description: "unsupported construct: " + err.Error(), Index: -1, Pattern: pattern}
	}
	full, err := regexp2.Compile(`\A(?:`+body+`)\z`, regexp2.None)
	if err != nil {
		return nil, &SyntaxError{Description: "unsupported construct: " + err.Error(), Index: -1, Pattern: pattern}
	}
	find.MatchTimeout = MatchTimeout
	full.MatchTimeout = MatchTimeout
	re := &Regexp{pattern: pattern, find: find, full: full, groups: groups, names: names}
	if noG, hasG := emitWithoutLastMatch(tree, groups); hasG {
		if re.afterEmpty, err = regexp2.Compile(noG, regexp2.None); err != nil {
			return nil, &SyntaxError{Description: "unsupported construct: " + err.Error(), Index: -1, Pattern: pattern}
		}
		re.afterEmpty.MatchTimeout = MatchTimeout
	}
	return re, nil
}

// String returns the Java source pattern.
func (re *Regexp) String() string { return re.pattern }

// NumGroups returns the number of capturing groups.
func (re *Regexp) NumGroups() int { return re.groups }

// GroupIndex returns the number of a named group, or -1.
func (re *Regexp) GroupIndex(name string) int {
	if i, ok := re.names[name]; ok {
		return i
	}
	return -1
}

// FullMatch reports whether the whole of s matches, like Matcher.matches()
// (and hamcrest's matchesPattern).
func (re *Regexp) FullMatch(s string) (bool, error) {
	return re.full.MatchString(s)
}

// MatchString reports whether s contains a match, like Matcher.find().
func (re *Regexp) MatchString(s string) (bool, error) {
	return re.find.MatchString(s)
}

// Match is one match: Groups[0] is the whole match, Groups[i] group i.
// Matched[i] is false for groups that did not participate (Java's null).
// Start[i] and End[i] are the offsets of group i in the searched text,
// counted in code points (runes), like Matcher.start(i)/end(i) but in code
// points rather than UTF-16 units; both are -1 for a group that did not
// participate.
type Match struct {
	Groups  []string
	Matched []bool
	Start   []int
	End     []int
}

// Find returns the first match in s, like the first Matcher.find(), or nil.
func (re *Regexp) Find(s string) (*Match, error) {
	m, err := re.find.FindStringMatch(s)
	if err != nil || m == nil {
		return nil, err
	}
	return re.toMatch(m), nil
}

func (re *Regexp) toMatch(m *regexp2.Match) *Match {
	n := re.groups + 1
	out := &Match{Groups: make([]string, n), Matched: make([]bool, n), Start: make([]int, n), End: make([]int, n)}
	for i := 0; i < n; i++ {
		out.Start[i], out.End[i] = -1, -1
		g := m.GroupByNumber(i)
		if g != nil && len(g.Captures) > 0 {
			out.Groups[i] = g.String()
			out.Matched[i] = true
			out.Start[i], out.End[i] = g.Index, g.Index+g.Length
		}
	}
	return out
}

// FindSubmatch returns the text of the first match and of its groups, or nil
// when there is no match. Groups that did not participate are "".
func (re *Regexp) FindSubmatch(s string) ([]string, error) {
	m, err := re.Find(s)
	if err != nil || m == nil {
		return nil, err
	}
	return m.Groups, nil
}

// FindAllString returns the text of every successive match, like repeated
// Matcher.find() calls: after an empty match the next search starts one
// character later. (Java steps one UTF-16 unit, which after an empty match
// in front of a supplementary character lands inside the surrogate pair; Go
// steps a whole code point, so such loops can yield one empty match less.)
func (re *Regexp) FindAllString(s string) ([]string, error) {
	var out []string
	runes := []rune(s)
	pos := 0
	prevEmpty := false
	for pos <= len(runes) {
		r := re.find
		if prevEmpty && re.afterEmpty != nil {
			r = re.afterEmpty
		}
		m, err := r.FindRunesMatchStartingAt(runes, pos)
		if err != nil {
			return out, err
		}
		if m == nil {
			break
		}
		out = append(out, m.String())
		end := m.Index + m.Length
		prevEmpty = m.Length == 0
		if prevEmpty {
			end++
		}
		pos = end
	}
	return out, nil
}

// FindAll returns every successive match in s with its groups and offsets,
// like repeated Matcher.find() calls (see FindAllString for how the search
// continues after an empty match).
func (re *Regexp) FindAll(s string) ([]*Match, error) {
	var out []*Match
	runes := []rune(s)
	pos := 0
	prevEmpty := false
	for pos <= len(runes) {
		r := re.find
		if prevEmpty && re.afterEmpty != nil {
			r = re.afterEmpty
		}
		m, err := r.FindRunesMatchStartingAt(runes, pos)
		if err != nil {
			return out, err
		}
		if m == nil {
			break
		}
		out = append(out, re.toMatch(m))
		end := m.Index + m.Length
		prevEmpty = m.Length == 0
		if prevEmpty {
			end++
		}
		pos = end
	}
	return out, nil
}
