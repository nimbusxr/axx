package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// A value means what it says, in every pack, whether a step sets it or
// checks it (ADR 0014): `42` is an integer, `42L` a long, and `1.5` and `1e3`
// decimals; `true` and `false` are booleans; `null` is null; `undefined` is
// no such property; `{...}` and `[...]` are JSON; a value in double quotes
// is the string inside them (`"42"`); any other text is a string, `NULL` and
// `True` too. A value that could be read two ways is an error that gives the
// explicit forms: an empty cell, a number in a form JSON does not have
// (`01067`, `+5`, `1.5f`), and, where only text fits, a value that reads as
// anything else.

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
// is key holds text (TextRows): an entry `routing key` is that row and no
// other, and `attribute <name>` the rows of a name after `attribute `. Any
// other row, `attribute` alone or `routing key extra`, is a path into the
// JSON, as the steps read it.
func (d *TableDoc) RowIsText(key string) bool {
	if d == nil {
		return false
	}
	for _, k := range d.TextRows {
		if head, named := strings.CutSuffix(k, "<name>"); named {
			if strings.HasPrefix(key, head) {
				return true
			}
		} else if key == k {
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

// ValueErrorCode is the error code of a value the rule cannot read.
const ValueErrorCode = "AXX-E0207"

// ValueError is a value the rule cannot read (ADR 0014). Error says what to
// write in a table's cell and ends with the code, as axx validate reports
// it, so that a step that fails on it, and what reports the failure, show
// it too. InStepText says what to write in a step's own text.
type ValueError struct {
	cell, step string
}

// NewValueError is a ValueError of a value a step finds it cannot use, once it
// has read it.
func NewValueError(message string) *ValueError {
	return &ValueError{cell: message, step: message}
}

func (e *ValueError) Error() string { return e.cell + " [" + ValueErrorCode + "]" }

// Message is the error without its code.
func (e *ValueError) Message() string { return e.cell }

// valueError is the error whose message message makes, given how a form of
// the value is written.
func valueError(message func(write func(string) string) string) *ValueError {
	return &ValueError{cell: message(asWritten), step: message(StepText)}
}

func asWritten(form string) string { return form }

// InStepText is err for a value in a step's own text, not a table's cell:
// the quotes of a {string} parameter delimit it, so writing `"01067"` there
// gives the value 01067 again, and the form that works is `'"01067"'`. Any
// other error comes back as it is.
func InStepText(err error) error {
	var ve *ValueError
	if errors.As(err, &ve) {
		return &ValueError{cell: ve.step, step: ve.step}
	}
	return err
}

// StepText is a form of a value as a step's own text writes it: in the single
// quotes of a {string} parameter, whose value then follows the rule. `"42"`
// is `'"42"'`, and `42` is `'42'`.
func StepText(form string) string {
	return "'" + strings.ReplaceAll(form, "'", `\'`) + "'"
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
	// decimalText is JSON's numbers with a fraction, an exponent or both.
	decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+([eE][-+]?[0-9]+)?|[eE][-+]?[0-9]+)$`)
	// numberLike is text a reader could take for a number JSON does not
	// write that way: a leading zero, a sign, a dot first or last, a float,
	// double or long suffix, hexadecimal.
	numberLike = regexp.MustCompile(`^[-+]?(([0-9]+\.?[0-9]*|\.[0-9]+)([eE][-+]?[0-9]+)?[fFdDlL]?|0[xX][0-9a-fA-F]+[lL]?)$`)
	hexText    = regexp.MustCompile(`^0[xX]([0-9a-fA-F]+)([lL]?)$`)
	// numberParts is numberLike's decimal forms, without a sign, in parts.
	numberParts = regexp.MustCompile(`^([0-9]*)(\.?)([0-9]*)([eE][-+]?[0-9]+)?([fFdDlL]?)$`)
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
	case text == "undefined":
		if !undefined {
			return Value{}, valueError(func(write func(string) string) string {
				return "undefined (no such property) is not a value here: write " + write(`"undefined"`) + " for the text"
			})
		}
		v.kind, v.undefined = nullValue, true
	case text == "null":
		v.kind, v.json = nullValue, "null"
	case text == "true", text == "false":
		v.kind, v.json = booleanValue, text
	case integerText.MatchString(text):
		v.kind, v.json = integerValue, text
	case longText.MatchString(text):
		v.kind, v.json = longValue, text[:len(text)-1]
	case decimalText.MatchString(text):
		v.kind, v.json = decimalValue, text
	case strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}"),
		strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]"):
		if !json.Valid([]byte(text)) {
			return Value{}, &ValueError{
				cell: fmt.Sprintf("%s is not valid JSON: write it as JSON, or in double quotes for the text", clipText(text)),
				step: fmt.Sprintf("%s is not valid JSON: write it as JSON, or as %s for the text", clipText(text), StepText(`"`+text+`"`)),
			}
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
	if undefined && text == "undefined" {
		return "", true, nil
	}
	v, err := ParseValue(text, false, true)
	switch {
	case err != nil:
		return "", false, valueError(func(write func(string) string) string {
			return fmt.Sprintf("%s is text here: write %s", clipText(text), write(`"`+text+`"`))
		})
	case v.kind == stringValue:
		return text, false, nil
	}
	return "", false, valueError(func(write func(string) string) string {
		return fmt.Sprintf("%s reads as %s, and only text fits here: write %s for the text", clipText(text), v.kindName(), write(`"`+text+`"`))
	})
}

// Expand is v with the references in its strings expanded: a string's, or
// those in the strings of an object or array, their keys too. It comes after
// the value is read, so a reference is a string, whatever it expands to.
// Text stays as written, which keeps what a reference expands to out of
// messages; JSON has it, and every number as written.
func (v Value) Expand(expand func(string) (string, error)) (Value, error) {
	var b strings.Builder
	rest := v.json
	for {
		start := strings.IndexByte(rest, '"')
		if start < 0 {
			b.WriteString(rest)
			break
		}
		end := start + 1
		for rest[end] != '"' { // v.json is valid JSON
			if rest[end] == '\\' {
				end++
			}
			end++
		}
		b.WriteString(rest[:start])
		str := rest[start : end+1]
		if strings.Contains(str, "${") {
			var s string
			if err := json.Unmarshal([]byte(str), &s); err != nil {
				return Value{}, err
			}
			s, err := expand(s)
			if err != nil {
				return Value{}, err
			}
			str = quote(s)
		}
		b.WriteString(str)
		rest = rest[end+1:]
	}
	v.json = b.String()
	return v, nil
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
	message := func(subject string, write func(string) string) string {
		switch {
		case text && undefined:
			return fmt.Sprintf("%s is empty: write %s for the empty text, or %s for none", subject, write(`""`), write("undefined"))
		case text:
			return fmt.Sprintf("%s is empty: write %s for the empty text", subject, write(`""`))
		case undefined:
			return fmt.Sprintf("%s is empty: write %s for the empty string, %s for null, or %s for no such property", subject, write(`""`), write("null"), write("undefined"))
		}
		return fmt.Sprintf("%s is empty: write %s for the empty string, or %s for null", subject, write(`""`), write("null"))
	}
	return &ValueError{cell: message("the cell", asWritten), step: message("the value", StepText)}
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
		return valueError(func(write func(string) string) string {
			return fmt.Sprintf("%s could be the number %s or the text %s: write %s for the number, or %s for the text", text, n, text, write(n), write(`"`+text+`"`))
		})
	}
	return valueError(func(write func(string) string) string {
		return fmt.Sprintf("%s is not a JSON number: write %s for the text", text, write(`"`+text+`"`))
	})
}

// plainNumber is the rule's form of a number written another way, made from
// its text, so that it is the same number however long or large: its sign and
// its leading zeros go, a dot first or last gets its digit, a float's suffix
// makes it a decimal, and a long's suffix stays on an integer.
func plainNumber(text string) (string, bool) {
	t := strings.TrimPrefix(text, "+")
	sign := ""
	if rest, neg := strings.CutPrefix(t, "-"); neg {
		sign, t = "-", rest
	}
	if m := hexText.FindStringSubmatch(t); m != nil {
		n, ok := new(big.Int).SetString(m[1], 16)
		if !ok {
			return "", false
		}
		return sign + n.String() + m[2], true
	}
	m := numberParts.FindStringSubmatch(t)
	if m == nil {
		return "", false
	}
	whole, dot, fraction, exponent, suffix := strings.TrimLeft(m[1], "0"), m[2], m[3], m[4], m[5]
	if whole == "" {
		whole = "0"
	}
	if dot != "" && fraction == "" {
		fraction = "0"
	}
	isInteger := dot == "" && exponent == ""
	switch suffix {
	case "l", "L":
		if !isInteger {
			suffix = ""
		}
	case "":
	default:
		suffix = ""
		if isInteger {
			dot, fraction = ".", "0"
		}
	}
	return sign + whole + dot + fraction + exponent + suffix, true
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
