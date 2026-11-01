package core

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// A value means what it says, in every pack, whether a step sets it or
// checks it (ADR 0014): `42` is an integer, `42L` a long and `1.5` a
// decimal; `true` and `false` are booleans; `null` is null; `undefined` is
// no such property; `{...}` and `[...]` are JSON; a value in double quotes
// is the string inside them (`"42"`); any other text is a string. A value
// that could be read two ways is an error that gives the explicit forms: an
// empty cell, a number in a form JSON does not have (`01067`, `+5`, `1.5f`),
// and, where only text fits, a value that reads as anything else.

// ValueKind is what the value cells of a step's table are.
type ValueKind int

const (
	// NotValues: the cells are names, settings or patterns, which the step
	// reads its own way.
	NotValues ValueKind = iota
	// JSONValues: each value cell is a value by the rule, typed: `42`,
	// `"42"`, `true`, `null`, `{...}`.
	JSONValues
	// TextValues: each value cell is text, so a value that reads as anything
	// else (`42`, `true`, `null`) is an error asking for it in double quotes.
	TextValues
)

// MarshalText names the kind: "json" or "text".
func (k ValueKind) MarshalText() ([]byte, error) {
	switch k {
	case JSONValues:
		return []byte("json"), nil
	case TextValues:
		return []byte("text"), nil
	}
	return []byte(""), nil
}

// UnmarshalText reads a kind MarshalText named.
func (k *ValueKind) UnmarshalText(b []byte) error {
	switch string(b) {
	case "json":
		*k = JSONValues
	case "text":
		*k = TextValues
	case "":
		*k = NotValues
	default:
		return fmt.Errorf("unknown value kind %q", b)
	}
	return nil
}

// ValueCells returns the cells of a row that are values: the last one, in
// a table of named columns, or every cell.
func (d *TableDoc) ValueCells(row []string) []int {
	if d == nil || d.Values == NotValues || len(row) == 0 {
		return nil
	}
	if len(d.Columns) >= 2 {
		if len(row) != len(d.Columns) {
			return nil
		}
		return []int{len(row) - 1}
	}
	all := make([]int, len(row))
	for i := range row {
		all[i] = i
	}
	return all
}

// Notes is the table's note followed by how its value cells are read
// (ValueDoc), for its documentation.
func (d *TableDoc) Notes() string {
	if d == nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimSpace(d.Note) + " " + ValueDoc(d.Values, d.Undefined))
}

// RowIsText says whether the row of a table of JSON values whose first cell
// is key holds text (TextRows).
func (d *TableDoc) RowIsText(key string) bool {
	if d == nil {
		return false
	}
	key = strings.TrimSpace(key)
	for _, k := range d.TextRows {
		if key == k || strings.HasPrefix(key, k+" ") {
			return true
		}
	}
	return false
}

// ValueDoc says how a table's value cells are read, for its documentation.
func ValueDoc(kind ValueKind, undefined bool) string {
	switch kind {
	case JSONValues:
		s := "Values are typed: `42` an integer, `1.5` a decimal, `true` and `false` booleans, `null` null, `{...}` and `[...]` JSON, " +
			"`\"42\"` in double quotes the string, and other text a string; `42` is not `\"42\"`, nor `2` `2.0`."
		if undefined {
			s += " `undefined` is no such property."
		}
		return s
	case TextValues:
		s := "Values are text: one that reads as a number, a boolean or `null` goes in double quotes (`\"3\"`)."
		if undefined {
			s += " `undefined` is none."
		}
		return s
	}
	return ""
}

// Value is a value as a feature writes it, read by the rule.
type Value struct {
	text      string
	kind      valueType
	undefined bool
	json      string
}

type valueType int

const (
	stringValue valueType = iota
	integerValue
	longValue
	decimalValue
	booleanValue
	nullValue
	objectValue
	arrayValue
)

// Text is the value as written.
func (v Value) Text() string { return v.text }

// Undefined says the value is `undefined`: no such property.
func (v Value) Undefined() bool { return v.undefined }

// IsNull says the value is `null`.
func (v Value) IsNull() bool { return v.kind == nullValue && !v.undefined }

// Str returns the value's string and true when the value is a string.
func (v Value) Str() (string, bool) {
	if v.kind != stringValue || v.undefined {
		return "", false
	}
	var s string
	_ = json.Unmarshal([]byte(v.json), &s)
	return s, true
}

// JSON is the value as JSON text: `42`, `"42"`, `true`, `null`, the object
// or array as written; `42L` is `42`. It is empty for `undefined`.
func (v Value) JSON() string { return v.json }

// IsLong says the value was written as a long (`42L`).
func (v Value) IsLong() bool { return v.kind == longValue }

// IsDecimal says the value is a decimal (`1.5`, `2.0`).
func (v Value) IsDecimal() bool { return v.kind == decimalValue }

var (
	integerText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	longText    = regexp.MustCompile(`^-?(0|[1-9][0-9]*)[lL]$`)
	decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]+$`)
	// numberLike is text a reader could take for a number JSON does not
	// write that way: a leading zero, a sign, a dot first or last, an
	// exponent, a float or double suffix, hexadecimal.
	numberLike = regexp.MustCompile(`^[-+]?(([0-9]+\.?[0-9]*|\.[0-9]+)([eE][-+]?[0-9]+)?[fFdD]?|0[xX][0-9a-fA-F]+)$`)
	hexText    = regexp.MustCompile(`^(-?)0[xX]([0-9a-fA-F]+)$`)
)

// ParseValue reads a value by the rule. empty says the cell was empty, an
// error that names the explicit forms; undefined says the step takes
// `undefined`.
func ParseValue(text string, empty, undefined bool) (Value, error) {
	if empty {
		return Value{}, emptyError(undefined, false)
	}
	v := Value{text: text}
	switch {
	case len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"':
		v.kind, v.json = stringValue, quote(text[1:len(text)-1])
	case strings.EqualFold(text, "undefined"):
		if !undefined {
			return Value{}, fmt.Errorf("undefined (no such property) is not a value here: write \"undefined\" for the text")
		}
		v.kind, v.undefined = nullValue, true
	case strings.EqualFold(text, "null"):
		v.kind, v.json = nullValue, "null"
	case strings.EqualFold(text, "true"), strings.EqualFold(text, "false"):
		v.kind, v.json = booleanValue, strings.ToLower(text)
	case integerText.MatchString(text):
		v.kind, v.json = integerValue, text
	case longText.MatchString(text):
		v.kind, v.json = longValue, text[:len(text)-1]
	case decimalText.MatchString(text):
		v.kind, v.json = decimalValue, text
	case strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}"),
		strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]"):
		if !json.Valid([]byte(text)) {
			return Value{}, fmt.Errorf("%s is not valid JSON: write it as JSON, or in double quotes for the text", clipText(text))
		}
		v.kind, v.json = objectValue, text
		if text[0] == '[' {
			v.kind = arrayValue
		}
	case numberLike.MatchString(text) || isSpecialNumber(text):
		return Value{}, numberError(text)
	default:
		v.kind, v.json = stringValue, quote(text)
	}
	return v, nil
}

// ParseText reads a value where only text fits: double quotes give the text
// inside, and a value that reads as anything else is an error asking for
// it in double quotes. undefined says the step takes `undefined` (none),
// reported as isUndefined.
func ParseText(text string, empty, undefined bool) (s string, isUndefined bool, err error) {
	if empty {
		return "", false, emptyError(undefined, true)
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		return text[1 : len(text)-1], false, nil
	}
	if undefined && strings.EqualFold(text, "undefined") {
		return "", true, nil
	}
	v, err := ParseValue(text, false, true)
	switch {
	case err != nil:
		return "", false, fmt.Errorf("%s is text here: write \"%s\"", clipText(text), text)
	case v.kind == stringValue:
		return text, false, nil
	}
	return "", false, fmt.Errorf("%s reads as %s, and only text fits here: write \"%s\" for the text", clipText(text), v.kindName(), text)
}

func (v Value) kindName() string {
	switch {
	case v.undefined:
		return "undefined (no such property)"
	case v.kind == integerValue:
		return "an integer"
	case v.kind == longValue:
		return "a long"
	case v.kind == decimalValue:
		return "a decimal"
	case v.kind == booleanValue:
		return "a boolean"
	case v.kind == nullValue:
		return "null"
	case v.kind == objectValue:
		return "a JSON object"
	case v.kind == arrayValue:
		return "a JSON array"
	}
	return "a string"
}

func emptyError(undefined, text bool) error {
	switch {
	case text && undefined:
		return fmt.Errorf("the cell is empty: write \"\" for the empty text, or undefined for none")
	case text:
		return fmt.Errorf("the cell is empty: write \"\" for the empty text")
	case undefined:
		return fmt.Errorf("the cell is empty: write \"\" for the empty string, null for null, or undefined for no such property")
	}
	return fmt.Errorf("the cell is empty: write \"\" for the empty string, or null for null")
}

func isSpecialNumber(s string) bool {
	switch strings.ToLower(strings.TrimLeft(s, "+-")) {
	case "nan", "infinity", "inf":
		return true
	}
	return false
}

// numberError names the number a text could be and its explicit forms.
func numberError(text string) error {
	if n, ok := plainNumber(text); ok {
		return fmt.Errorf("%s could be the number %s or the text %s: write %s for the number, or \"%s\" for the text", text, n, text, n, text)
	}
	return fmt.Errorf("%s is not a JSON number: write \"%s\" for the text", text, text)
}

// plainNumber is the JSON form of a number written another way.
func plainNumber(text string) (string, bool) {
	t := strings.TrimPrefix(text, "+")
	if m := hexText.FindStringSubmatch(t); m != nil {
		n, err := strconv.ParseInt(m[2], 16, 64)
		if err != nil {
			return "", false
		}
		if m[1] == "-" {
			n = -n
		}
		return strconv.FormatInt(n, 10), true
	}
	t = strings.TrimRight(t, "fFdD")
	if !strings.ContainsAny(text, ".eEfFdD") {
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return strconv.FormatInt(n, 10), true
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return "", false
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s, true
}

func quote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func clipText(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}
