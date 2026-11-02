package rest

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// A payload property is what its cell says (ADR 0014): `10115` is a number,
// even where the contract wants a string. writeHint adds to a request body
// type finding what to write: `$.recipient.postcode: got number, want
// string: write "10115"`.

var (
	typeFinding = regexp.MustCompile(`^(\$[^:]*): got (\w+), want ([a-z ,]+?)( \(|$)`)
	integerText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]+$`)
)

// writeHint is what to write for the value a request body type finding
// names, or "".
func writeHint(key, message string, body []byte) string {
	if key != "validation.request.body.schema.type" {
		return ""
	}
	m := typeFinding.FindStringSubmatch(message)
	if m == nil {
		return ""
	}
	v, ok := bodyValue(body, m[1])
	if !ok {
		return ""
	}
	want := strings.Split(strings.ReplaceAll(m[3], " or ", ","), ",")
	for i := range want {
		want[i] = strings.TrimSpace(want[i])
	}
	for _, w := range want {
		switch x := v.(type) {
		case json.Number:
			if w == "string" {
				return `"` + x.String() + `"`
			}
		case bool:
			if w == "string" {
				return `"` + strconv.FormatBool(x) + `"`
			}
		case string:
			switch {
			case w == "integer" && integerText.MatchString(x),
				w == "number" && (integerText.MatchString(x) || decimalText.MatchString(x)),
				w == "boolean" && (x == "true" || x == "false"):
				return x
			}
		}
	}
	return ""
}

// bodyValue is the value at a finding's location in a JSON body:
// `$.recipient.postcode`, `$.lines[0].reference`, `$.lines.0`.
func bodyValue(body []byte, loc string) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	path := strings.NewReplacer("[", ".", "]", "").Replace(strings.TrimPrefix(loc, "$"))
	for _, seg := range strings.Split(strings.TrimPrefix(path, "."), ".") {
		if seg == "" {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			next, found := x[seg]
			if !found {
				return nil, false
			}
			v = next
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
