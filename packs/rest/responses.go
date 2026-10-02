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

// valueDoc says how the response property steps compare values.
var valueDoc = []string{
	"A value is compared with its JSON type. Text, like `REGISTERED`, is a string, and so is a value in double quotes, like `\"42\"`.",
	"`42` is an integer, `42L` a long and `1.5` a number. An integer never equals a decimal: `5` is not `5.0`.",
	"`true` and `false` are booleans. `{...}` and `[...]` are a JSON object and a JSON array; an object's members may come in any order.",
}

// regexpDoc and regexpsDoc say how the steps that match regular
// expressions match them.
const (
	regexpDoc  = "The regular expression is in Java syntax, and must match the whole value."
	regexpsDoc = "Each regular expression is in Java syntax, and must match the whole value."
)

func responseSteps() []core.StepDef {
	out := []core.StepDef{{
		ID: "rest.response.status", Keyword: "Then",
		Expr: "the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]",
		Doc: docList("Check the HTTP status code of a response.",
			"Without an ordinal it checks the response to the first (default) request; `the 2nd ordered response`, "+
				"the response to the second.",
			serviceDoc),
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
			doc:          "Check that the response body contains the text.",
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
			doc: "Check that a response header has the value.",
			details: []string{
				"The header's name is matched in any case.",
				"With the header repeated, one of its values must be the value.",
			},
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
			doc: "Check that a response header matches a regular expression.",
			details: []string{
				regexpDoc,
				"With the header repeated, one of its values must match.",
			},
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
			doc:          "Check that the response has no header of that name.",
			example:      "Then the response header Content-Length is missing",
			namedExample: "Then the response header Retry-After is missing for response on parcels",
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
			doc: "Check response headers, a row each, like the single-header step.",
			details: []string{
				"A name may repeat.",
				"Every row is checked, and every mismatch reported.",
			},
			table: &core.TableDoc{Columns: []string{"header", "value"}, Note: "A row is a header's name and the value it must have."},
			example: "Then the response headers are:\n" +
				"  | Content-Type | application/json     |\n" +
				"  | Location     | /api/parcels/PX-4101 |",
			namedExample: "Then the response headers for 1st ordered response on parcels are:\n" +
				"  | Content-Type | application/problem+json |\n" +
				"  | Retry-After  | 5                        |",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return headerTable(sc, a, t, func(h http.Header, row [2]string) (*failure, error) {
					return headerIs(h, row[0], row[1]), nil
				})
			},
		},
		{
			id: "rest.response.headers.match", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response headers", tail: " match:",
			doc: "Check that response headers match regular expressions, a row each.",
			details: []string{
				regexpsDoc,
				"Every row is checked, and every mismatch reported.",
			},
			table: &core.TableDoc{
				Columns: []string{"header", "regular expression"},
				Note:    "A row is a header's name and a regular expression its value must match.",
			},
			example: "Then the response headers match:\n" +
				"  | Content-Type | application/json.*       |\n" +
				"  | Location     | /api/parcels/PX-[0-9]{4} |",
			namedExample: "Then the response headers for response on parcels match:\n" +
				"  | Retry-After | [0-9]+ |",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return headerTable(sc, a, t, func(h http.Header, row [2]string) (*failure, error) {
					return headerMatches(h, row[0], row[1])
				})
			},
		},
		{
			id: "rest.response.headers.missing", keyword: "Then", arg: core.ArgTable, noun: "response",
			head: "the response headers", tail: " are missing:",
			doc:   "Check that the response has none of the headers the table names.",
			table: &core.TableDoc{Columns: []string{"header"}, Note: "A row names a header, in its first cell."},
			example: "Then the response headers are missing:\n" +
				"  | Retry-After |\n" +
				"  | Set-Cookie  |",
			namedExample: "Then the response headers for response on parcels are missing:\n" +
				"  | Retry-After |",
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
			doc: "Check a property of a JSON response, by its JSONPath, like `status`, `recipient.postcode` or " +
				"`[?(@.sender=='kestrel-books')].reference`.",
			details: append([]string{
				"An indefinite path, like a filter, reads as a list.",
				"The response must be JSON: `application/json`, `text/json` or any `+json` type, whatever its charset.",
			}, valueDoc...),
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
			doc:          "Check that a property of the response payload exists, and is JSON null.",
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
			doc: "Check that a property of the response payload does not exist.",
			details: []string{
				"An indefinite path always exists: it reads as a list, which may be empty.",
			},
			example:      "Then the response payload property recipient.street is undefined",
			namedExample: "Then the response payload property recipient.street is undefined for 1st ordered response on parcels",
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
			doc:          "Check that a property of the response payload is a string that matches a regular expression.",
			details:      []string{regexpDoc},
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
			doc: "Check properties of a JSON response, a row each, like the single-property step.",
			details: append([]string{
				"`null` and `undefined`, in any case, check for JSON null and for no such property; `\"null\"`, " +
					"in double quotes, is the string.",
				"Every row is checked, and every mismatch reported.",
			}, valueDoc...),
			table: &core.TableDoc{Columns: []string{"JSONPath", "value"}, Note: "A row is a property's JSONPath and the value it must have."},
			example: "Then the response payload properties are:\n" +
				"  | reference | PX-4101    |\n" +
				"  | status    | REGISTERED |\n" +
				"  | zone      | DE-1       |",
			namedExample: "Then the response payload properties for 1st ordered response on parcels are:\n" +
				"  | serviceLevel     | STANDARD     |\n" +
				"  | recipient.name   | Ada Lovelace |\n" +
				"  | recipient.street | undefined    |",
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
			doc: "Check that properties of the response payload are strings that match regular expressions, a row each.",
			details: []string{
				regexpsDoc,
				"Every row is checked, and every mismatch reported.",
			},
			table: &core.TableDoc{
				Columns: []string{"JSONPath", "regular expression"},
				Note:    "A row is a property's JSONPath and a regular expression its value must match.",
			},
			example: "Then the response payload properties match:\n" +
				"  | barcode   | ^PX[0-9]{11}$  |\n" +
				"  | signature | ^[0-9a-f]{64}$ |",
			namedExample: "Then the response payload properties for 2nd ordered response on parcels match:\n" +
				"  | [0].reference | PX-[0-9]{4} |",
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
// The message names the response, since a step without an ordinal reads the
// first request's, which may not be the one meant.
func checkJSONResponse(ex *Exchange) error {
	ct := ex.Header.Get("Content-Type")
	if !isJSONMediaType(mediaTypeOf(ct)) {
		if ct == "" {
			ct = "none"
		}
		return fmt.Errorf("Response content type is not supported for payload property validation: %s (the response to %s %s, status %d)", ct, ex.Method, ex.URL, ex.Status) //nolint:staticcheck // user-facing message
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
