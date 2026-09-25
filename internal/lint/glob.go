package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javare"
)

// Glob patterns follow java.nio.file's "glob:" syntax (sun.nio.fs.Globs):
//
//   - `*` matches within one path segment, `**` matches across segments
//     wherever it appears (so `**.fixture.yaml` matches `a/b/x.fixture.yaml`),
//     and `?` matches one character other than `/`;
//   - `[abc]`, `[a-z]` and `[!abc]` are character classes that never match
//     `/`; `{a,b}` is a group of alternatives (groups do not nest);
//   - `\` escapes the next character.
//
// Common glob libraries (doublestar among them) treat a `**` that is not a
// whole segment like `*`, which would silently break the `**.fixture.yaml`
// exclude idiom, so the patterns are translated as sun.nio.fs.Globs does.

const (
	regexMetaChars = ".^$+{[]|()"
	globMetaChars  = "\\*?[{"
)

// GlobError is an invalid glob pattern (java.util.regex.PatternSyntaxException).
type GlobError struct {
	Pattern     string
	Description string
	Index       int
}

func (e *GlobError) Error() string {
	return fmt.Sprintf("%s at position %d of glob %q", e.Description, e.Index, e.Pattern)
}

// globToRegex translates a glob into a Java regular expression, as
// sun.nio.fs.Globs.toUnixRegexPattern does.
func globToRegex(glob string) (string, error) {
	g := []rune(glob)
	next := func(i int) rune {
		if i < len(g) {
			return g[i]
		}
		return 0
	}
	fail := func(desc string, idx int) (string, error) {
		return "", &GlobError{Pattern: glob, Description: desc, Index: idx}
	}
	var re strings.Builder
	re.WriteByte('^')
	inGroup := false
	i := 0
	for i < len(g) {
		c := g[i]
		i++
		switch c {
		case '\\':
			if i == len(g) {
				return fail("no character to escape", i-1)
			}
			n := g[i]
			if strings.ContainsRune(globMetaChars, n) || strings.ContainsRune(regexMetaChars, n) {
				re.WriteByte('\\')
			}
			re.WriteRune(n)
			i++
		case '/':
			re.WriteRune(c)
		case '[':
			re.WriteString("[[^/]&&[")
			if next(i) == '^' {
				re.WriteString(`\^`)
				i++
			} else {
				if next(i) == '!' {
					re.WriteByte('^')
					i++
				}
				if next(i) == '-' {
					re.WriteByte('-')
					i++
				}
			}
			hasRangeStart := false
			var last rune
			for i < len(g) {
				c = g[i]
				i++
				if c == ']' {
					break
				}
				if c == '/' {
					return fail("explicit 'name separator' in class", i-1)
				}
				if c == '\\' || c == '[' || c == '&' && next(i) == '&' {
					re.WriteByte('\\')
				}
				re.WriteRune(c)
				if c == '-' {
					if !hasRangeStart {
						return fail("invalid range", i-1)
					}
					c = next(i)
					i++
					if c == 0 || c == ']' {
						break
					}
					if c < last {
						return fail("invalid range", i-3)
					}
					re.WriteRune(c)
					hasRangeStart = false
				} else {
					hasRangeStart = true
					last = c
				}
			}
			if c != ']' {
				return fail("missing ']'", i-1)
			}
			re.WriteString("]]")
		case '{':
			if inGroup {
				return fail("cannot nest groups", i-1)
			}
			re.WriteString("(?:(?:")
			inGroup = true
		case '}':
			if inGroup {
				re.WriteString("))")
				inGroup = false
			} else {
				re.WriteByte('}')
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
			if strings.ContainsRune(regexMetaChars, c) {
				re.WriteByte('\\')
			}
			re.WriteRune(c)
		}
	}
	if inGroup {
		return fail("missing '}'", i-1)
	}
	re.WriteByte('$')
	return re.String(), nil
}

// globMatcher matches slash-separated paths against a glob.
type globMatcher struct {
	glob string
	re   *javare.Regexp
}

func compileGlob(glob string) (*globMatcher, error) {
	src, err := globToRegex(glob)
	if err != nil {
		return nil, err
	}
	re, err := javare.Compile(src)
	if err != nil {
		return nil, &GlobError{Pattern: glob, Description: err.Error(), Index: -1}
	}
	return &globMatcher{glob: glob, re: re}, nil
}

// match reports whether the whole of path matches.
func (m *globMatcher) match(path string) bool {
	ok, err := m.re.FullMatch(path)
	return err == nil && ok
}

// matchTail reports whether the glob matches the absolute path, its file
// name, or any trailing run of its segments.
func (m *globMatcher) matchTail(abs string) bool {
	if m.match(abs) {
		return true
	}
	rest := strings.TrimLeft(abs, "/")
	for {
		if m.match(rest) {
			return true
		}
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			return false
		}
		rest = rest[i+1:]
	}
}

// filePattern is a compiled filePatterns entry: the static directory prefix
// (everything before the last '/' preceding the first wildcard) and the glob
// matched below it.
type filePattern struct {
	raw    string
	prefix string // "" when the walk starts at baseDir
	glob   *globMatcher
}

func compileFilePattern(pattern string) (*filePattern, error) {
	fp := &filePattern{raw: pattern}
	glob := pattern
	if strings.Contains(pattern, "/") {
		firstMeta := strings.IndexAny(pattern, "*?[{")
		var lastSlash int
		if firstMeta < 0 {
			lastSlash = strings.LastIndexByte(pattern, '/')
		} else {
			lastSlash = strings.LastIndexByte(pattern[:firstMeta], '/')
		}
		if lastSlash > 0 {
			fp.prefix = pattern[:lastSlash]
			glob = pattern[lastSlash+1:]
		}
	}
	m, err := compileGlob(glob)
	if err != nil {
		return nil, err
	}
	fp.glob = m
	return fp, nil
}

// skipDirs are never descended into while searching for files: version
// control metadata and axx's own state and logs.
var skipDirs = map[string]bool{".git": true, ".axx": true}

// findFiles returns the regular files matched by any of patterns, as
// absolute paths in lexical order. A pattern whose directory prefix does not
// exist matches nothing (generated fixtures may not exist yet).
func findFiles(baseDir string, patterns []*filePattern) ([]string, error) {
	seen := map[string]bool{}
	for _, fp := range patterns {
		root := baseDir
		if fp.prefix != "" {
			root = filepath.FromSlash(fp.prefix)
			if !filepath.IsAbs(root) {
				root = filepath.Join(baseDir, root)
			}
			root = filepath.Clean(root)
		}
		if _, err := os.Stat(root); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root {
					return err
				}
				return nil // unreadable subdirectories are skipped
			}
			if d.IsDir() {
				if p != root && skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				st, serr := os.Stat(p) // follow symlinks to files, as Files.isRegularFile does
				if serr != nil || !st.Mode().IsRegular() {
					return nil //nolint:nilerr // dangling links and devices are not files
				}
			}
			if fp.glob.matchTail(filepath.ToSlash(p)) {
				seen[p] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// relSlash renders p relative to base with forward slashes (possibly
// starting with ../), or p itself when no relative path exists.
func relSlash(base, p string) string {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}
