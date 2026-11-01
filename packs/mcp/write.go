package mcp

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// An argument is what its cell says (ADR 0014): nothing converts it to its
// schema's type. withWrites adds to a finding of another type what to
// write: `arguments $.postcode: got number, want string: write "10115"`.

var (
	integerText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	numberText  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
)

// withWrites adds what to write to the type findings of v, which
// schemadoc.Validate keyed prefix.type and named what.
func withWrites(found []finding, prefix, what string, v any) []finding {
	for i, f := range found {
		if f.Key != prefix+".type" {
			continue
		}
		rest, ok := strings.CutPrefix(f.Message, what+" ")
		if !ok {
			continue
		}
		loc, kind, ok := strings.Cut(rest, ": ")
		if !ok {
			continue
		}
		_, want, ok := strings.Cut(kind, ", want ")
		if !ok {
			continue
		}
		if x, ok := valueAt(v, loc); ok {
			if w := write(x, strings.Split(want, " or ")); w != "" {
				found[i].Message += ": write " + w
			}
		}
	}
	return found
}

// valueAt is the value at a finding's location: `$`, `$.address.postcode`,
// `$.lines.0`.
func valueAt(v any, loc string) (any, bool) {
	rest, ok := strings.CutPrefix(loc, "$")
	if !ok {
		return nil, false
	}
	for _, seg := range strings.Split(strings.TrimPrefix(rest, "."), ".") {
		if seg == "" {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			if v, ok = x[seg]; !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// write is what to write for a value of another type than the schema's:
// a number or a boolean in double quotes for a string, a number's or a
// boolean's text without them.
func write(x any, want []string) string {
	switch y := x.(type) {
	case json.Number:
		if slices.Contains(want, "string") {
			return `"` + y.String() + `"`
		}
	case bool:
		if slices.Contains(want, "string") {
			return `"` + strconv.FormatBool(y) + `"`
		}
	case string:
		switch {
		case slices.Contains(want, "integer") && integerText.MatchString(y),
			slices.Contains(want, "number") && numberText.MatchString(y),
			slices.Contains(want, "boolean") && (y == "true" || y == "false"):
			return y
		}
	}
	return ""
}
