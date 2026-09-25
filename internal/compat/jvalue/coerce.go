// Package jvalue holds the value semantics the step packs share: how the
// text of a feature file becomes typed values, how they are compared, and
// how values render back to text.
//
// Values use the jsonx model (nil, bool, string, jsonx.Number, *jsonx.Object,
// *jsonx.Array for json-smart lists, []any for Jackson lists).
//
// Errors are either a *jsonx.Error, standing for a Java exception the step
// let escape (NumberFormatException, PathNotFoundException, ...), or a
// *StepError, a failure the step raises itself.
//
// Three deliberate differences from the Java libraries (each listed in the
// oracle Deviations table):
//
//   - Where Java parsed an integer with Integer.parseInt and failed with a
//     NumberFormatException for values beyond 32 bits, axx falls back to a
//     64-bit Long (json-smart itself reads such numbers as Long, so the
//     comparison can succeed). Values beyond 64 bits still fail as in Java.
//   - Setting a request payload property whose current value is JSON null
//     works: the oracle records a NullPointerException; axx infers
//     the new value's type from the text, as for a property that does not
//     exist yet (see [CoerceToExisting]).
//   - Form encoding renders nested objects as JSON instead of Java's
//     "{a=1, b=2}" map notation (see [FormURLEncode]).
package jvalue

import (
	"regexp"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// StepError is a failure the step raised itself.
type StepError struct {
	Message string
	Cause   error
}

func (e *StepError) Error() string { return e.Message }

// Unwrap returns the underlying exception, if any.
func (e *StepError) Unwrap() error { return e.Cause }

func isQuoted(s string) bool {
	return len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"'
}

// InferRequestValue is the REST request step's inferValue: the typed value a
// new payload property gets from its feature-file text. In order: a
// double-quoted text is the string inside the quotes; "true"/"false" (any
// case) a Boolean; then Integer, Long and Double as Java parses them
// (leading '+', Unicode digits, "NaN", "1e5", " 12 ", "1.5f", ...); then, for
// text starting with '{' or '[', whatever json-smart parses it to; otherwise
// the text itself.
func InferRequestValue(s string) any {
	if isQuoted(s) {
		return s[1 : len(s)-1]
	}
	if strings.EqualFold(s, "true") || strings.EqualFold(s, "false") {
		return javafmt.ParseBoolean(s)
	}
	if v, err := javafmt.ParseInt(s); err == nil {
		return jsonx.IntegerNumber(v)
	}
	if v, err := javafmt.ParseLong(s); err == nil {
		return jsonx.LongNumber(v)
	}
	if v, err := javafmt.ParseDouble(s); err == nil {
		return jsonx.DoubleNumber(v)
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		if v, err := jsonx.Parse(s); err == nil {
			return v
		}
	}
	return s
}

// CoerceToExisting converts text to the Java type of the value a request
// payload property already has, like the REST request step's setProperty:
// String keeps the text, Boolean uses Boolean.parseBoolean (anything but
// "true" is false), Integer, Long and Double parse the text (Integer falls
// back to Long on overflow), and objects and arrays parse it with json-smart.
// A null value has no type, so the text is inferred with [InferRequestValue]
// (Java failed with a NullPointerException here). Parse failures are
// NumberFormat or IllegalArgument errors; the step wraps those in "Invalid
// value" messages.
func CoerceToExisting(existing any, s string) (any, error) {
	switch x := existing.(type) {
	case string:
		return s, nil
	case bool:
		return javafmt.ParseBoolean(s), nil
	case jsonx.Number:
		switch x.Kind() {
		case jsonx.Integer:
			if v, err := javafmt.ParseInt(s); err == nil {
				return jsonx.IntegerNumber(v), nil
			}
			v, err := javafmt.ParseLong(s)
			if err != nil {
				return nil, numberFormat(err)
			}
			return jsonx.LongNumber(v), nil
		case jsonx.Long:
			v, err := javafmt.ParseLong(s)
			if err != nil {
				return nil, numberFormat(err)
			}
			return jsonx.LongNumber(v), nil
		case jsonx.Double:
			v, err := javafmt.ParseDouble(s)
			if err != nil {
				return nil, numberFormat(err)
			}
			return jsonx.DoubleNumber(v), nil
		}
		return nil, &StepError{Message: "Unsupported type for value: class " + x.Kind().JavaClassName()}
	case *jsonx.Object, *jsonx.Array, []any:
		return jsonx.Parse(s)
	case nil:
		return InferRequestValue(s), nil
	}
	return nil, &StepError{Message: "Unsupported type for value: class " + jsonx.JavaClassName(existing)}
}

func numberFormat(err error) error {
	return &jsonx.Error{Kind: jsonx.NumberFormat, Message: err.Error()}
}

var (
	intPattern    = regexp.MustCompile(`^-?[0-9]+$`)
	longPattern   = regexp.MustCompile(`^-?[0-9]+[lL]$`)
	doublePattern = regexp.MustCompile(`^-?[0-9]*\.[0-9]+$`)
)

// parseExpectedInt parses an integer literal as Integer, falling back to Long
// (the documented overflow fix).
func parseExpectedInt(s string) (any, error) {
	if v, err := javafmt.ParseInt(s); err == nil {
		return jsonx.IntegerNumber(v), nil
	}
	v, err := javafmt.ParseLong(s)
	if err != nil {
		return nil, numberFormat(err)
	}
	return jsonx.LongNumber(v), nil
}

// CoerceExpected is the REST response step's coerceValue: the typed value a
// response property is compared against. A text in double quotes is the
// string inside; -?\d+ is an Integer (Long on overflow), -?\d+[lL] a Long,
// -?\d*\.\d+ a Double, true/false (any case) a Boolean, {...} a Jackson map
// and [...] a Jackson list; anything else is the text itself.
func CoerceExpected(s string) (any, error) {
	switch {
	case strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`):
		return unquoteNoLengthCheck(s)
	case intPattern.MatchString(s):
		return parseExpectedInt(s)
	case longPattern.MatchString(s):
		v, err := javafmt.ParseLong(s[:len(s)-1])
		if err != nil {
			return nil, numberFormat(err)
		}
		return jsonx.LongNumber(v), nil
	case doublePattern.MatchString(s):
		v, _ := javafmt.ParseDouble(s)
		return jsonx.DoubleNumber(v), nil
	case strings.EqualFold(s, "true") || strings.EqualFold(s, "false"):
		return javafmt.ParseBoolean(s), nil
	case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
		return jacksonTyped(s, true, "Failed to parse JSON object")
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		return jacksonTyped(s, false, "Failed to parse JSON array")
	}
	return s, nil
}

// CoerceKafkaExpected is the Kafka consumer step's value coercion: like
// CoerceExpected, except that "null" (any case) means JSON null (nil) and
// there is no Long suffix form.
func CoerceKafkaExpected(s string) (any, error) {
	switch {
	case strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`):
		return unquoteNoLengthCheck(s)
	case strings.EqualFold(s, "null"):
		return nil, nil
	case intPattern.MatchString(s):
		return parseExpectedInt(s)
	case doublePattern.MatchString(s):
		v, _ := javafmt.ParseDouble(s)
		return jsonx.DoubleNumber(v), nil
	case strings.EqualFold(s, "true") || strings.EqualFold(s, "false"):
		return javafmt.ParseBoolean(s), nil
	case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
		return jacksonTyped(s, true, "Failed to parse JSON object")
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		return jacksonTyped(s, false, "Failed to parse JSON object")
	}
	return s, nil
}

// unquoteNoLengthCheck is value.substring(1, value.length() - 1) without the
// length check the request step has: a lone `"` fails as in Java.
func unquoteNoLengthCheck(s string) (any, error) {
	if len(s) < 2 {
		return nil, &jsonx.Error{Kind: jsonx.StringIndexOutOfBounds, Message: "Range [1, 0) out of bounds for length 1"}
	}
	return s[1 : len(s)-1], nil
}

// jacksonTyped is ObjectMapper.readValue into Map<String,Object> or
// List<Object>.
func jacksonTyped(s string, wantObject bool, failure string) (any, error) {
	v, err := ParseJackson(s)
	if err == nil {
		_, isObj := v.(*jsonx.Object)
		_, isList := v.([]any)
		if wantObject && isObj || !wantObject && isList {
			return v, nil
		}
		err = &jsonx.Error{Kind: jsonx.JSONParse, Message: "mismatched input"}
	}
	return nil, &StepError{Message: failure, Cause: err}
}

// JavaEquals is Java's actual.equals(expected) as hamcrest's equalTo applies
// it: types must match exactly (a JSON 5 is an Integer and does not equal the
// Double 5.0), maps compare regardless of order, lists element by element.
func JavaEquals(actual, expected any) bool {
	return jsonx.JavaEquals(actual, expected)
}
