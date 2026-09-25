// Package interp expands ${prefix:name} references in strings.
//
// Semantics follow Apache Commons Text's StringSubstitutor with env/sys
// lookups:
//
//   - ${env:NAME}   environment variable
//   - ${sys:name}   property (axx.yaml `properties`, overridden by -D name=value)
//   - ${p:name:-fallback} uses fallback when unresolved; fallbacks and names
//     may themselves contain references (nesting)
//   - $${...} is an escape producing a literal ${...}
//   - unresolved references are left verbatim (unless Strict is set)
package interp

import (
	"fmt"
	"strings"
)

// Lookup resolves a name within one prefix.
type Lookup func(name string) (string, bool)

// Resolver expands references using per-prefix lookups.
type Resolver struct {
	Lookups map[string]Lookup
	// Strict makes unresolved references an error instead of literal text.
	Strict bool
}

// UnresolvedError reports a reference that could not be resolved in Strict mode.
type UnresolvedError struct{ Ref string }

func (e *UnresolvedError) Error() string { return fmt.Sprintf("unresolved reference %s", e.Ref) }

const maxDepth = 16

// Expand returns s with references expanded.
func (r *Resolver) Expand(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	return r.expand(s, 0)
}

// MustExpand expands and ignores errors (non-strict callers).
func (r *Resolver) MustExpand(s string) string {
	out, err := r.Expand(s)
	if err != nil {
		return s
	}
	return out
}

func (r *Resolver) expand(s string, depth int) (string, error) {
	if depth > maxDepth {
		return "", fmt.Errorf("interpolation nested too deeply in %q", s)
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		// escape: $${ -> literal ${
		if strings.HasPrefix(s[i:], "$${") {
			end := matchBrace(s, i+2)
			if end < 0 {
				b.WriteString(s[i:])
				break
			}
			b.WriteString(s[i+1 : end+1])
			i = end + 1
			continue
		}
		if strings.HasPrefix(s[i:], "${") {
			end := matchBrace(s, i+1)
			if end < 0 { // unterminated: literal
				b.WriteString(s[i:])
				break
			}
			ref := s[i : end+1]
			val, err := r.resolve(s[i+2:end], ref, depth)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i = end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}

// matchBrace returns the index of the '}' closing the '{' at open, honoring
// nested ${...} references.
func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (r *Resolver) resolve(body, ref string, depth int) (string, error) {
	key, def, hasDef := splitDefault(body)
	key, err := r.expand(key, depth+1)
	if err != nil {
		return "", err
	}
	prefix, name, ok := strings.Cut(key, ":")
	if ok {
		if lookup, found := r.Lookups[prefix]; found && lookup != nil {
			if v, found := lookup(name); found {
				return v, nil
			}
		}
	}
	if hasDef {
		return r.expand(def, depth+1)
	}
	if r.Strict {
		return "", &UnresolvedError{Ref: ref}
	}
	// Keep the reference verbatim (with any nested references expanded).
	return "${" + key + "}", nil
}

// splitDefault splits "name:-default" at the first top-level ":-".
func splitDefault(body string) (name, def string, ok bool) {
	depth := 0
	for i := 0; i < len(body)-1; i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ':':
			if depth == 0 && body[i+1] == '-' {
				return body[:i], body[i+2:], true
			}
		}
	}
	return body, "", false
}

// MapLookup adapts a map to a Lookup.
func MapLookup(m map[string]string) Lookup {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

// EnvLookup adapts an environment getter (e.g. os.LookupEnv).
func EnvLookup(get func(string) (string, bool)) Lookup { return Lookup(get) }
