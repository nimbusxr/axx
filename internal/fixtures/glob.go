package fixtures

import (
	"fmt"
	"regexp"
	"strings"
)

// compileGlob translates a java.nio "glob:" pattern (sun.nio.fs.Globs, Unix
// flavor) into a regular expression matched against a whole slash-separated
// path: "*" stays within a path segment, "**" crosses segments, "?" is one
// character other than '/', "[...]" a class ("[!...]" negated) that never
// matches '/', "{a,b}" alternatives, and "\" escapes.
func compileGlob(glob string) (*regexp.Regexp, error) {
	var re strings.Builder
	re.WriteString(`^`)
	inGroup := false
	rs := []rune(glob)
	next := func(i int) rune {
		if i < len(rs) {
			return rs[i]
		}
		return 0
	}
	for i := 0; i < len(rs); {
		c := rs[i]
		i++
		switch c {
		case '\\':
			if i == len(rs) {
				return nil, fmt.Errorf("no character to escape at the end of %q", glob)
			}
			re.WriteString(regexp.QuoteMeta(string(rs[i])))
			i++
		case '[':
			var class strings.Builder
			negate := false
			switch next(i) {
			case '^':
				class.WriteString(`\^`)
				i++
			case '!':
				negate = true
				i++
				if next(i) == '-' {
					class.WriteString(`\-`)
					i++
				}
			case '-':
				class.WriteString(`\-`)
				i++
			}
			closed := false
			for i < len(rs) {
				c = rs[i]
				i++
				if c == ']' {
					closed = true
					break
				}
				if c == '/' {
					return nil, fmt.Errorf("explicit name separator in class in %q", glob)
				}
				if c == '\\' || c == '[' || c == ']' || c == '^' {
					class.WriteByte('\\')
				}
				class.WriteRune(c)
			}
			if !closed {
				return nil, fmt.Errorf("missing ']' in %q", glob)
			}
			if negate {
				re.WriteString("[^/" + class.String() + "]")
			} else {
				re.WriteString("(?:[" + class.String() + "])")
			}
		case '{':
			if inGroup {
				return nil, fmt.Errorf("cannot nest groups in %q", glob)
			}
			re.WriteString("(?:(?:")
			inGroup = true
		case '}':
			if inGroup {
				re.WriteString("))")
				inGroup = false
			} else {
				re.WriteString(`\}`)
			}
		case ',':
			if inGroup {
				re.WriteString(")|(?:")
			} else {
				re.WriteByte(',')
			}
		case '*':
			if next(i) == '*' {
				re.WriteString(".*")
				i++
			} else {
				re.WriteString("[^/]*")
			}
		case '?':
			re.WriteString("[^/]")
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	if inGroup {
		return nil, fmt.Errorf("missing '}' in %q", glob)
	}
	re.WriteString(`$`)
	return regexp.Compile(re.String())
}

// globMatcher matches slash-separated paths against any of several globs.
type globMatcher []*regexp.Regexp

func newGlobMatcher(globs []string) (globMatcher, error) {
	m := make(globMatcher, 0, len(globs))
	for _, g := range globs {
		re, err := compileGlob(g)
		if err != nil {
			return nil, configError("invalid glob %q: %v", g, err)
		}
		m = append(m, re)
	}
	return m, nil
}

func (m globMatcher) match(path string) bool {
	for _, re := range m {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}
