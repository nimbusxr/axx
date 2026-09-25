package jvalue

import (
	"errors"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javare"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// The functions in this file reproduce complete step behaviors of the Java
// framework, including the order in which its failures surfaced, so the step
// packs can call them instead of re-deriving the sequence.

func isKind(err error, kind jsonx.ErrorKind) bool {
	var e *jsonx.Error
	return errors.As(err, &e) && e.Kind.Is(kind)
}

// ---- REST request payload properties ----

// SetRequestProperty applies "the request payload property <path> is
// <value>" to doc, like the Java step's setProperty:
//
//   - A double-quoted value sets the string inside the quotes.
//   - Otherwise the current value at path (read from a serialize/parse round
//     trip of doc, as Java does) decides the type: see [CoerceToExisting].
//   - When path does not exist, the property is added to its parent (split
//     at the last '.', see [jsonx.ParentAndKey]) with the type
//     [InferRequestValue] infers.
//
// Invalid numbers and empty or "null" JSON produce a StepError
// "Invalid value ..."; other failures propagate as Java's did.
func SetRequestProperty(doc any, path, value string) error {
	if isQuoted(value) {
		str := value[1 : len(value)-1]
		err := jsonx.Set(doc, path, str)
		if isKind(err, jsonx.PathNotFound) {
			parent, key := jsonx.ParentAndKey(path)
			return jsonx.Put(doc, parent, key, str)
		}
		return err
	}
	err := setTyped(doc, path, value)
	switch {
	case err == nil:
		return nil
	case isKind(err, jsonx.PathNotFound):
		parent, key := jsonx.ParentAndKey(path)
		return jsonx.Put(doc, parent, key, InferRequestValue(value))
	case isKind(err, jsonx.IllegalArgument):
		var e *jsonx.Error
		errors.As(err, &e)
		return &StepError{
			Message: `Invalid value "` + value + `" for property "` + path + `": ` + e.JavaMessage(),
			Cause:   err,
		}
	}
	return err
}

// setTyped is the try block of setProperty.
func setTyped(doc any, path, value string) error {
	text, err := jsonx.Marshal(doc)
	if err != nil {
		return err
	}
	snapshot, err := jsonx.Parse(text)
	if err != nil {
		return err
	}
	existing, _, err := jsonx.Read(snapshot, path)
	if err != nil {
		return err
	}
	v, err := CoerceToExisting(existing, value)
	if err != nil {
		return err
	}
	return jsonx.Set(doc, path, v)
}

// SetRequestPropertyNull sets the property at path to JSON null, like the
// Java step's setPropertyNull: any failure becomes "Property not found in
// payload: <message>".
func SetRequestPropertyNull(doc any, path string) error {
	return notFoundWrap(jsonx.Set(doc, path, nil))
}

// DeleteRequestProperty removes the property at path, like the Java step's
// deleteProperty.
func DeleteRequestProperty(doc any, path string) error {
	return notFoundWrap(jsonx.Delete(doc, path))
}

func notFoundWrap(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var e *jsonx.Error
	if errors.As(err, &e) {
		msg = e.JavaMessage()
	}
	return &StepError{Message: "Property not found in payload: " + msg, Cause: err}
}

// ApplyRequestTableRow applies one row of "the request payload properties
// are:": "undefined" (any case) deletes the property, "null" (any case) sets
// it to null, anything else (including the quoted "\"null\"") is set with
// SetRequestProperty.
func ApplyRequestTableRow(doc any, path, value string) error {
	switch {
	case isQuoted(value):
		return SetRequestProperty(doc, path, value)
	case strings.EqualFold(value, "undefined"):
		return DeleteRequestProperty(doc, path)
	case strings.EqualFold(value, "null"):
		return SetRequestPropertyNull(doc, path)
	}
	return SetRequestProperty(doc, path, value)
}

// ---- REST response payload properties ----

// ResponsePropertyIs is "the response payload property <path> is <value>":
// hasJsonPath(path, equalToObject(CoerceExpected(value))) on the body. The
// value is coerced before the path is compiled, so its errors come first.
func ResponsePropertyIs(body, path, value string) (bool, error) {
	expected, err := CoerceExpected(value)
	if err != nil {
		return false, err
	}
	return jsonx.HasJSONPath(body, path, func(v any) bool { return JavaEquals(v, expected) })
}

// ResponsePropertyIsNull is "the response payload property <path> is null".
func ResponsePropertyIsNull(body, path string) (bool, error) {
	return jsonx.HasJSONPath(body, path, func(v any) bool { return v == nil })
}

// ResponsePropertyIsUndefined is "the response payload property <path> is
// undefined": hasNoJsonPath. An indefinite path always reads (as a list), so
// it is never undefined.
func ResponsePropertyIsUndefined(body, path string) (bool, error) {
	return jsonx.HasNoJSONPath(body, path)
}

// ResponsePropertyMatches is "the response payload property <path> matches
// <pattern>": the value must be a string the Java regular expression fully
// matches.
func ResponsePropertyMatches(body, path, pattern string) (bool, error) {
	return propertyMatches(body, path, pattern)
}

func propertyMatches(body, path, pattern string) (bool, error) {
	re, err := compileRegex(pattern)
	if err != nil {
		return false, err
	}
	var matchErr error
	ok, err := jsonx.HasJSONPath(body, path, func(v any) bool {
		s, isString := v.(string)
		if !isString {
			return false
		}
		m, err := re.FullMatch(s)
		if err != nil {
			matchErr = err
		}
		return m
	})
	if err == nil {
		err = matchErr
	}
	return ok, err
}

func compileRegex(pattern string) (*javare.Regexp, error) {
	re, err := javare.Compile(pattern)
	if err != nil {
		return nil, &jsonx.Error{Kind: jsonx.PatternSyntax, Message: err.Error()}
	}
	return re, nil
}

// ResponseTableRow is one row of "the response payload properties are:":
// quoted values compare as strings, "undefined" and "null" (any case) check
// absence and null, anything else is ResponsePropertyIs.
func ResponseTableRow(body, path, value string) (bool, error) {
	switch {
	case isQuoted(value):
		return ResponsePropertyIs(body, path, value)
	case strings.EqualFold(value, "undefined"):
		return ResponsePropertyIsUndefined(body, path)
	case strings.EqualFold(value, "null"):
		return ResponsePropertyIsNull(body, path)
	}
	return ResponsePropertyIs(body, path, value)
}

// ---- Kafka consumer payload properties ----

// KafkaPropertyMatches is one row of the Kafka consumer payload-property
// expectation on a message payload: hasJsonPath(path, equalToObject(
// CoerceKafkaExpected(value))), where "null" expects JSON null.
func KafkaPropertyMatches(payload, path, value string) (bool, error) {
	expected, err := CoerceKafkaExpected(value)
	if err != nil {
		return false, err
	}
	return jsonx.HasJSONPath(payload, path, func(v any) bool { return JavaEquals(v, expected) })
}

// ---- Postgres JSON column properties ----

// PostgresPropertyIs is one row of the Postgres "JSON properties are"
// expectation on a column's JSON text: every scalar is compared as text
// (see StringifyJSON); "undefined" and "null" (any case) check absence and
// null. Every failure is a StepError "Could not perform selection".
func PostgresPropertyIs(column, path, value string) (bool, error) {
	modified, err := StringifyJSON(column)
	if err != nil {
		return false, selectionError(err)
	}
	var ok bool
	switch {
	case strings.EqualFold(value, "undefined"):
		ok, err = jsonx.HasNoJSONPath(modified, path)
	case strings.EqualFold(value, "null"):
		ok, err = jsonx.HasJSONPath(modified, path, func(v any) bool { return v == nil })
	default:
		ok, err = jsonx.HasJSONPath(modified, path, func(v any) bool { return v == value })
	}
	if err != nil {
		return false, selectionError(err)
	}
	return ok, nil
}

// PostgresPropertyMatches is one row of the Postgres "JSON properties match"
// expectation: the stringified value must fully match the Java pattern.
func PostgresPropertyMatches(column, path, pattern string) (bool, error) {
	modified, err := StringifyJSON(column)
	if err != nil {
		return false, selectionError(err)
	}
	ok, err := propertyMatches(modified, path, pattern)
	if err != nil {
		return false, selectionError(err)
	}
	return ok, nil
}

func selectionError(err error) error {
	return &StepError{Message: "Could not perform selection", Cause: err}
}
