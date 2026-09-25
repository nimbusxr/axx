package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/javare"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

const valueDoc = " Values are compared with their JSON type: `'John'` or `\"42\"` (double quotes inside) are strings, " +
	"`42` an integer, `42L` a long, `1.5` a number, `true`/`false` booleans, `{...}` and `[...]` JSON objects and arrays " +
	"(compared regardless of member order). An integer never equals a decimal (`5` is not `5.0`)."

func responseSteps() []core.StepDef {
	out := []core.StepDef{{
		ID: "rest.response.status", Keyword: "Then",
		Expr:     "the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]",
		Doc:      "Assert the HTTP status code of a response." + respOrdinalDoc,
		Examples: []string{"Then the response status code is 200", "Then the 2nd ordered response status code is 201 on parcels"},
		Run: func(sc *core.Scenario, a core.Args) error {
			ex, err := target{ord: 0, svc: 2}.exchange(sc, a)
			if err != nil {
				return err
			}
			if want := a.Int(1); ex.Status != want {
				return core.Fail(fmt.Sprintf("Expected status code <%d> but was <%d>.", want, ex.Status), want, ex.Status)
			}
			return nil
		},
	}}
	for _, f := range []family{
		{
			id: "rest.response.body.contains", keyword: "Then", noun: "response",
			head: "the response body contains {string}", nHead: 1,
			doc:          "Assert that the response body contains the text." + respOrdinalDoc,
			example:      "Then the response body contains 'already registered'",
			namedExample: "Then the response body contains 'already registered' for 2nd ordered response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				if !strings.Contains(string(ex.Body), a.String(0)) {
					return core.Fail("Response body doesn't contain the expected text", a.String(0), truncated(ex.Body))
				}
				return nil
			},
		},
		{
			id: "rest.response.header.is", keyword: "Then", noun: "response",
			head: "the response header {word} is {string}", nHead: 2,
			doc: "Assert that a response header (name matched case-insensitively) has the value; with repeated " +
				"headers, one of them must." + respOrdinalDoc,
			example:      "Then the response header Content-Type is 'application/json'",
			namedExample: "Then the response header Content-Type is 'application/json' for response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				return failures(headerIs(ex.Header, a.String(0), a.String(1)))
			},
		},
		{
			id: "rest.response.header.matches", keyword: "Then", noun: "response",
			head: "the response header {word} matches {pattern}", nHead: 2,
			doc: "Assert that a response header matches a regular expression (Java syntax; it must match the " +
				"whole value). With repeated headers, one of them must match." + respOrdinalDoc,
			example:      "Then the response header Content-Type matches ^application/json.*$",
			namedExample: "Then the response header Content-Type matches ^application/json$ for 1st ordered response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				f, err := headerMatches(ex.Header, a.String(0), a.String(1))
				if err != nil {
					return err
				}
				return failures(f)
			},
		},
		{
			id: "rest.response.header.missing", keyword: "Then", noun: "response",
			head: "the response header {word} is missing", nHead: 1,
			doc:          "Assert that the response has no header with the name." + respOrdinalDoc,
			example:      "Then the response header Content-Length is missing",
			namedExample: "Then the response header X-Custom is missing for response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				return failures(headerMissing(ex.Header, a.String(0)))
			},
		},
		{
			id: "rest.response.headers.are", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response headers", tail: " are:",
			doc:          "Assert response headers from a `name | value` table, each like the single-header step (a name may repeat)." + respOrdinalDoc,
			example:      "Then the response headers are:",
			namedExample: "Then the response headers for 1st ordered response on parcels are:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return headerTable(sc, a, t, func(h http.Header, row [2]string) (*failure, error) {
					return headerIs(h, row[0], row[1]), nil
				})
			},
		},
		{
			id: "rest.response.headers.match", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response headers", tail: " match:",
			doc:          "Assert response headers from a `name | regular expression` table (full match, Java syntax)." + respOrdinalDoc,
			example:      "Then the response headers match:",
			namedExample: "Then the response headers for response on parcels match:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return headerTable(sc, a, t, func(h http.Header, row [2]string) (*failure, error) {
					return headerMatches(h, row[0], row[1])
				})
			},
		},
		{
			id: "rest.response.headers.missing", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response headers", tail: " are missing:",
			doc:          "Assert that the response has none of the headers named in the table's first column." + respOrdinalDoc,
			example:      "Then the response headers are missing:",
			namedExample: "Then the response headers for response on parcels are missing:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				if a.Table == nil {
					return fmt.Errorf("a data table is required")
				}
				var fs []*failure
				for _, row := range a.Table.Rows {
					if len(row) > 0 {
						fs = append(fs, headerMissing(ex.Header, row[0]))
					}
				}
				return failures(fs...)
			},
		},
		{
			id: "rest.response.property.is", keyword: "Then", noun: "response",
			head: "the response payload property {word} is {string}", nHead: 2,
			doc: "Assert a property of a JSON response (a JSONPath such as `status`, `recipient.postcode` or " +
				"`[?(@.sender=='kestrel-books')].reference`; an indefinite path yields a list). The response must be JSON " +
				"(`application/json`, `text/json` or any `+json` type, charset ignored)." + valueDoc + respOrdinalDoc,
			example:      "Then the response payload property status is 'REGISTERED'",
			namedExample: "Then the response payload property status is 'REGISTERED' for 1st ordered response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				f, err := propertyIs(ex, a.String(0), a.String(1))
				if err != nil {
					return err
				}
				return failures(f)
			},
		},
		{
			id: "rest.response.property.null", keyword: "Then", noun: "response",
			head: "the response payload property {word} is null", nHead: 1,
			doc:          "Assert that a response payload property exists and is JSON null." + respOrdinalDoc,
			example:      "Then the response payload property lastLocation is null",
			namedExample: "Then the response payload property lastLocation is null for response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				f, err := propertyNull(ex, a.String(0))
				if err != nil {
					return err
				}
				return failures(f)
			},
		},
		{
			id: "rest.response.property.undefined", keyword: "Then", noun: "response",
			head: "the response payload property {word} is undefined", nHead: 1,
			doc: "Assert that a response payload property does not exist. (An indefinite path always exists: it " +
				"reads as a possibly empty list.)" + respOrdinalDoc,
			example:      "Then the response payload property nonexistent is undefined",
			namedExample: "Then the response payload property nonexistent is undefined for 1st ordered response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				f, err := propertyUndefined(ex, a.String(0))
				if err != nil {
					return err
				}
				return failures(f)
			},
		},
		{
			id: "rest.response.property.matches", keyword: "Then", noun: "response",
			head: "the response payload property {word} matches {pattern}", nHead: 2,
			doc: "Assert that a response payload property is a string that matches a regular expression (Java " +
				"syntax; it must match the whole value)." + respOrdinalDoc,
			example:      "Then the response payload property barcode matches ^PX[0-9]{11}$",
			namedExample: "Then the response payload property barcode matches ^PX[0-9]{11}$ for response on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				ex, err := t.exchange(sc, a)
				if err != nil {
					return err
				}
				f, err := propertyMatches(ex, a.String(0), a.String(1))
				if err != nil {
					return err
				}
				return failures(f)
			},
		},
		{
			id: "rest.response.properties.are", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response payload properties", tail: " are:",
			doc: "Assert response payload properties from a `path | value` table. `null` and `undefined` (any case) " +
				"check for JSON null and absence; `\"null\"` in double quotes is the string. Every other value is " +
				"compared like the single-property step. All rows are checked and every mismatch is reported." + valueDoc + respOrdinalDoc,
			example:      "Then the response payload properties are:",
			namedExample: "Then the response payload properties for 1st ordered response on parcels are:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return propertyTable(sc, a, t, func(ex *Exchange, path, value string) (*failure, error) {
					switch {
					case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
						return propertyIs(ex, path, value)
					case strings.EqualFold(value, "undefined"):
						return propertyUndefined(ex, path)
					case strings.EqualFold(value, "null"):
						return propertyNull(ex, path)
					}
					return propertyIs(ex, path, value)
				})
			},
		},
		{
			id: "rest.response.properties.match", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response payload properties", tail: " match:",
			doc:          "Assert response payload properties from a `path | regular expression` table (full match, Java syntax)." + respOrdinalDoc,
			example:      "Then the response payload properties match:",
			namedExample: "Then the response payload properties for response on parcels match:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return propertyTable(sc, a, t, propertyMatches)
			},
		},
	} {
		out = append(out, f.defs()...)
	}
	return out
}

// ---- failures ----

// failure is one unmet expectation of a (table) assertion.
type failure struct {
	msg              string
	expected, actual any
}

// failures turns unmet expectations into one assertion error (nil when
// every expectation held).
func failures(fs ...*failure) error {
	var list []*failure
	for _, f := range fs {
		if f != nil {
			list = append(list, f)
		}
	}
	switch len(list) {
	case 0:
		return nil
	case 1:
		return core.Fail(list[0].msg, list[0].expected, list[0].actual)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d expectations failed:", len(list))
	exp := make([]any, len(list))
	act := make([]any, len(list))
	for i, f := range list {
		fmt.Fprintf(&b, "\n  - %s (expected %s, actual %s)", f.msg, show(f.expected), show(f.actual))
		exp[i], act[i] = f.expected, f.actual
	}
	return core.Fail(b.String(), exp, act)
}

func show(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}

// ---- headers ----

func headerValues(h http.Header, name string) any {
	v := h.Values(name)
	if len(v) == 0 {
		return "<missing>"
	}
	if len(v) == 1 {
		return v[0]
	}
	return v
}

func headerIs(h http.Header, name, value string) *failure {
	for _, v := range h.Values(name) {
		if v == value {
			return nil
		}
	}
	return &failure{fmt.Sprintf("Response header %s does not have the expected value", name), value, headerValues(h, name)}
}

func headerMatches(h http.Header, name, pattern string) (*failure, error) {
	re, err := javare.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression %q: %w", pattern, err)
	}
	for _, v := range h.Values(name) {
		ok, err := re.FullMatch(v)
		if err != nil {
			return nil, err
		}
		if ok {
			return nil, nil
		}
	}
	return &failure{fmt.Sprintf("Response header %s does not match %s", name, pattern), pattern, headerValues(h, name)}, nil
}

func headerMissing(h http.Header, name string) *failure {
	if v := h.Values(name); len(v) > 0 {
		return &failure{fmt.Sprintf("Response header %s is present", name), "<missing>", headerValues(h, name)}
	}
	return nil
}

func headerTable(sc *core.Scenario, a core.Args, t target, check func(http.Header, [2]string) (*failure, error)) error {
	ex, err := t.exchange(sc, a)
	if err != nil {
		return err
	}
	rows, err := rows2(a.Table, "response headers")
	if err != nil {
		return err
	}
	var fs []*failure
	for _, row := range rows {
		f, err := check(ex.Header, row)
		if err != nil {
			return err
		}
		fs = append(fs, f)
	}
	return failures(fs...)
}

// ---- payload properties ----

// checkJSONResponse is the content-type check of the property equality
// steps: the media type (parameters such as charset ignored) must be JSON.
func checkJSONResponse(ex *Exchange) error {
	ct := ex.Header.Get("Content-Type")
	if !isJSONMediaType(mediaTypeOf(ct)) {
		if ct == "" {
			ct = "none"
		}
		return fmt.Errorf("Response content type is not supported for payload property validation: %s", ct) //nolint:staticcheck // user-facing message
	}
	return nil
}

// actualAt renders the value at path for failure messages.
func actualAt(body []byte, path string) any {
	doc, err := jsonx.Parse(string(body))
	if err != nil {
		return "<response body is not JSON>"
	}
	v, _, err := jsonx.Read(doc, path)
	if err != nil {
		return "<no value at " + path + ">"
	}
	return shownJSON(v)
}

// jsonScalar is the JSON text of a string, number, boolean or null in a
// failure. Reporters print it as it is; as a plain string it would be quoted
// a second time ("\"John\""). In --json output it is the JSON value itself.
type jsonScalar string

func (s jsonScalar) String() string { return string(s) }

func (s jsonScalar) MarshalJSON() ([]byte, error) {
	if json.Valid([]byte(s)) {
		return []byte(s), nil
	}
	return json.Marshal(string(s))
}

// shownJSON is v as a failure value: objects and arrays as JSON text, which
// reporters pretty-print and diff, and scalars as a jsonScalar.
func shownJSON(v any) any {
	text := jsonText(v)
	if t := strings.TrimSpace(text); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return text
	}
	return jsonScalar(text)
}

func propertyIs(ex *Exchange, path, value string) (*failure, error) {
	if err := checkJSONResponse(ex); err != nil {
		return nil, err
	}
	ok, err := jvalue.ResponsePropertyIs(string(ex.Body), path, value)
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, nil
	}
	expected, _ := jvalue.CoerceExpected(value)
	return &failure{fmt.Sprintf("Response payload property %s is not %s", path, value), shownJSON(expected), actualAt(ex.Body, path)}, nil
}

func propertyNull(ex *Exchange, path string) (*failure, error) {
	ok, err := jvalue.ResponsePropertyIsNull(string(ex.Body), path)
	if err != nil || ok {
		return nil, err
	}
	return &failure{fmt.Sprintf("Response payload property %s is not null", path), jsonScalar("null"), actualAt(ex.Body, path)}, nil
}

func propertyUndefined(ex *Exchange, path string) (*failure, error) {
	ok, err := jvalue.ResponsePropertyIsUndefined(string(ex.Body), path)
	if err != nil || ok {
		return nil, err
	}
	return &failure{fmt.Sprintf("Response payload property %s is not undefined", path), "<undefined>", actualAt(ex.Body, path)}, nil
}

func propertyMatches(ex *Exchange, path, pattern string) (*failure, error) {
	ok, err := jvalue.ResponsePropertyMatches(string(ex.Body), path, pattern)
	if err != nil || ok {
		return nil, err
	}
	return &failure{fmt.Sprintf("Response payload property %s does not match %s", path, pattern), pattern, actualAt(ex.Body, path)}, nil
}

func propertyTable(sc *core.Scenario, a core.Args, t target, check func(ex *Exchange, path, value string) (*failure, error)) error {
	ex, err := t.exchange(sc, a)
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	var fs []*failure
	for _, p := range pairs {
		f, err := check(ex, p.Key, p.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Key, err)
		}
		fs = append(fs, f)
	}
	return failures(fs...)
}

// jsonText renders a compat JSON value (json-smart or Jackson model) as
// standard compact JSON, keeping member order and number literals.
func jsonText(v any) string {
	var b bytes.Buffer
	writeJSON(&b, v)
	return b.String()
}

func writeJSON(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		writeString(b, x)
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case jsonx.Number:
		t := x.Text()
		if t == "" || !jsonNumber.MatchString(t) {
			t = x.String()
		}
		if !jsonNumber.MatchString(t) { // NaN, Infinity
			writeString(b, t)
			return
		}
		b.WriteString(t)
	case *jsonx.Object:
		b.WriteByte('{')
		for i, k := range x.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			e, _ := x.Get(k)
			writeJSON(b, e)
		}
		b.WriteByte('}')
	case *jsonx.Array:
		writeList(b, x.Items())
	case []any:
		writeList(b, x)
	default:
		enc, err := json.Marshal(x)
		if err != nil {
			writeString(b, fmt.Sprint(x))
			return
		}
		b.Write(enc)
	}
}

func writeList(b *bytes.Buffer, items []any) {
	b.WriteByte('[')
	for i, e := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSON(b, e)
	}
	b.WriteByte(']')
}
