package match

import (
	"fmt"
	"strings"
)

// variant is one concrete Cucumber expression produced from a step
// expression with optional [[...]] segments.
type variant struct {
	expr string
	// argPos maps each argument of this variant to its position in the full
	// expression (the expression with every optional segment present).
	argPos []int
}

// segment is a piece of a step expression: literal text or an optional
// [[...]] group.
type segment struct {
	text     string
	optional bool
	params   int // number of {param} placeholders in text
}

// expandVariants turns "a {int}[[ on {service}]]" into every combination of
// optional segments. It also returns the parameter names of the full
// expression, in order.
func expandVariants(expr string) ([]variant, []string, error) {
	segs, err := splitSegments(expr)
	if err != nil {
		return nil, nil, err
	}
	var optional []int
	for i, s := range segs {
		if s.optional {
			optional = append(optional, i)
		}
	}
	if len(optional) > 6 {
		return nil, nil, fmt.Errorf("expression %q has %d optional [[...]] segments; at most 6 are allowed", expr, len(optional))
	}
	full := strings.Builder{}
	for _, s := range segs {
		full.WriteString(s.text)
	}
	names := paramNames(full.String())

	var out []variant
	for mask := 0; mask < 1<<len(optional); mask++ {
		include := map[int]bool{}
		for bit, idx := range optional {
			if mask&(1<<bit) != 0 {
				include[idx] = true
			}
		}
		var b strings.Builder
		var pos []int
		next := 0
		for i, s := range segs {
			if s.optional && !include[i] {
				next += s.params
				continue
			}
			b.WriteString(s.text)
			for k := 0; k < s.params; k++ {
				pos = append(pos, next)
				next++
			}
		}
		out = append(out, variant{expr: b.String(), argPos: pos})
	}
	return out, names, nil
}

func splitSegments(expr string) ([]segment, error) {
	var segs []segment
	for {
		start := strings.Index(expr, "[[")
		if start < 0 {
			if expr != "" {
				segs = append(segs, segment{text: expr, params: len(paramNames(expr))})
			}
			return segs, nil
		}
		end := strings.Index(expr[start+2:], "]]")
		if end < 0 {
			return nil, fmt.Errorf("unterminated [[ in step expression")
		}
		end += start + 2
		if strings.Contains(expr[start+2:end], "[[") {
			return nil, fmt.Errorf("nested [[ ]] segments are not supported")
		}
		if start > 0 {
			segs = append(segs, segment{text: expr[:start], params: len(paramNames(expr[:start]))})
		}
		inner := expr[start+2 : end]
		segs = append(segs, segment{text: inner, optional: true, params: len(paramNames(inner))})
		expr = expr[end+2:]
	}
}

// paramNames returns the {name} placeholders of a Cucumber expression in
// order, ignoring escaped braces.
func paramNames(expr string) []string {
	var names []string
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '\\':
			i++ // skip escaped character
		case '{':
			end := strings.IndexByte(expr[i:], '}')
			if end < 0 {
				return names
			}
			names = append(names, expr[i+1:i+end])
			i += end
		}
	}
	return names
}
