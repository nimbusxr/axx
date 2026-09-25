// Package oracletest loads the behavior oracles in testdata/oracles
// and compares Go results against them. It is used by the compat packages'
// tests only.
//
// Every case where axx deliberately differs from an oracle is listed in
// [Deviations], with the reason. A test consults the table with
// [Check]: a mismatch that is not listed fails, and so does a listed
// deviation that no longer occurs (so the table cannot go stale).
package oracletest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// OracleDir returns the directory holding the oracle files.
func OracleDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "testdata", "oracles")
}

// Load decodes oracle <name>.json into v.
func Load(t testing.TB, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(OracleDir(), name+".json"))
	if err != nil {
		t.Fatalf("read oracle %s: %v", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode oracle %s: %v", name, err)
	}
}

// JavaError is an exception as the oracles record it.
type JavaError struct {
	Class   string  `json:"class"`
	Message *string `json:"message"`
}

func (e *JavaError) String() string {
	if e == nil {
		return "<no error>"
	}
	if e.Message == nil {
		return e.Class + " (null message)"
	}
	return e.Class + ": " + *e.Message
}

// Deviation is one intended difference from an oracle.
type Deviation struct {
	// Oracle is the oracle file name, Case the case key the test builds.
	Oracle, Case string
	Reason       string
}

// Deviations lists every intended difference from the oracles.
var Deviations = append([]Deviation{
	// Integer overflow: Java's Integer.parseInt throws NumberFormatException
	// for integers beyond 32 bits; axx falls back to a 64-bit Long instead,
	// so such values compare equal to json-smart's Long.
	{"reststeps", "coerce|2147483648", "Integer overflow falls back to int64"},
	{"reststeps", "coerce|-2147483649", "Integer overflow falls back to int64"},
	{"reststeps", "coerce|9223372036854775807", "Integer overflow falls back to int64"},
	{"reststeps", "request|profile|single|age|3000000000", "Integer overflow falls back to int64"},
	{"reststeps", "request|profile|table|age|3000000000", "Integer overflow falls back to int64"},
	{"reststeps", "response|person|is|big|3000000000", "Integer overflow falls back to int64 (and now matches the Long)"},
	{"reststeps", "response|person|table|big|3000000000", "Integer overflow falls back to int64 (and now matches the Long)"},
	{"reststeps", "response|person|is|age|3000000000", "Integer overflow falls back to int64"},
	{"reststeps", "response|person|table|age|3000000000", "Integer overflow falls back to int64"},
	{"kafka", "coerce|2147483648", "Integer overflow falls back to int64"},
	{"kafka", "coerce|-2147483649", "Integer overflow falls back to int64"},
	{"kafka", "case|event|big|3000000000", "Integer overflow falls back to int64 (and now matches the Long)"},
	{"kafka", "coerce|123456789012", "Integer overflow falls back to int64"},
	{"kafka", "case|nested|ts|1700000000000", "Integer overflow falls back to int64 (an epoch-millis timestamp now matches)"},

	// Regular expressions.
	{"javaregex", `compile|\N{LATIN SMALL LETTER A}`, `\N{name} needs the Unicode character name table, which axx does not embed`},
	{"javaregex", `compile|\p{IsEmoji}`, "Go's unicode tables lack the Emoji properties"},
	// After an empty match Java's find() loop steps one UTF-16 unit, into the
	// middle of a surrogate pair, and finds one more empty match there; a Go
	// string cannot address half a code point.
	{"javaregex", `findAll|\Q\E|😀`, "find() loop inside a surrogate pair"},
	{"javaregex", `findAll|\Q\E|x😀y`, "find() loop inside a surrogate pair"},
	{"javaregex", `findAll|\Q|😀`, "find() loop inside a surrogate pair"},

	// Form encoding: the oracle renders a nested object with String.valueOf,
	// i.e. in Java's map notation "{a=1, b=[1,2]}" that no server can parse; axx
	// sends the object's JSON text. (Nested arrays were already JSON.)
	{"reststeps", `form|{"obj":{"a":1,"b":[1,2]}}`, "nested objects are form-encoded as JSON"},
	{"reststeps", `form|{"nested":{"deeper":{"x":[{"y":null}]}}}`, "nested objects are form-encoded as JSON"},
	{"reststeps", `form|{"obj":{"s":"x/y","q":"say \"hi\""}}`, "nested objects are form-encoded as JSON"},
	// YAML aliases: Jackson's tree reader returns an alias as the text of its
	// anchor name ("x" for *x); axx resolves it to a copy of the anchored
	// value, which is what a YAML author means. "<<" stays an ordinary key.
	{"jacksonyaml", "parse|a: &x 1\nb: *x", "aliases resolve to the anchored value"},
	{"jacksonyaml", "parse|base: &b {x: 1}\nother:\n  <<: *b\n  y: 2", "aliases resolve to the anchored value"},
}, nullPropertyDeviations()...)

// nullPropertyDeviations lists the oracle cases that set a request payload
// property whose current value is null ("nothing" in the profile document).
// The oracle records a NullPointerException for these; axx infers
// the new value's type from the text, as for a property that does not exist.
// A double-quoted value and the table's null/undefined keywords never reach
// that code, so they are not deviations.
func nullPropertyDeviations() []Deviation {
	const reason = "setting a property whose value is null infers the type instead of failing"
	values := []string{
		"new", "30", "-7", "3000000000", "12345678901234567890", "19.99", "1e3", "NaN", "true", "FALSE", "yes",
		`{"city":"LA"}`, "{city:LA}", `["b","c"]`, "[]", "notjson", "", `"`, " 5", "+5", "1.5f", "0x10",
	}
	out := make([]Deviation, 0, 2*len(values)+2)
	for _, v := range values {
		for _, mode := range []string{"single", "table"} {
			out = append(out, Deviation{"reststeps", "request|profile|" + mode + "|nothing|" + v, reason})
		}
	}
	// The table routes null and undefined to their own steps; only the single
	// step hands them to setProperty.
	for _, v := range []string{"null", "undefined"} {
		out = append(out, Deviation{"reststeps", "request|profile|single|nothing|" + v, reason})
	}
	return out
}

var (
	seenMu sync.Mutex
	seen   = map[string]bool{}
)

// Check reports a mismatch with the oracle unless the case is a listed deviation, and
// reports a listed deviation that now matches.
func Check(t testing.TB, oracle, key string, equal bool, detail func() string) {
	t.Helper()
	dev := lookup(oracle, key)
	switch {
	case equal && dev != nil:
		t.Errorf("%s %q: listed as a deviation (%s) but now matches the oracle", oracle, key, dev.Reason)
	case !equal && dev == nil:
		t.Errorf("%s %q: differs from the oracle:\n%s", oracle, key, detail())
	case !equal:
		seenMu.Lock()
		seen[oracle+"\x00"+key] = true
		seenMu.Unlock()
	}
}

func lookup(oracle, key string) *Deviation {
	for i := range Deviations {
		if Deviations[i].Oracle == oracle && Deviations[i].Case == key {
			return &Deviations[i]
		}
	}
	return nil
}

// CheckAllDeviationsSeen fails for deviations of an oracle that no test case
// produced.
func CheckAllDeviationsSeen(t testing.TB, oracle string) {
	t.Helper()
	seenMu.Lock()
	defer seenMu.Unlock()
	for _, d := range Deviations {
		if d.Oracle == oracle && !seen[oracle+"\x00"+d.Case] {
			t.Errorf("deviation %s %q was not exercised", oracle, d.Case)
		}
	}
}

// Repr renders a value with its Java runtime types in the oracle format,
// e.g. {"a":I1,"b":[D1.5]}.
func Repr(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return Quote(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case jsonx.Number:
		prefix := map[jsonx.NumberKind]string{
			jsonx.Integer: "I", jsonx.Long: "L", jsonx.BigInteger: "BI",
			jsonx.Double: "D", jsonx.Float: "F", jsonx.BigDecimal: "BD",
		}[x.Kind()]
		return prefix + x.String()
	case *jsonx.Object:
		var sb strings.Builder
		sb.WriteByte('{')
		for i, k := range x.Keys() {
			if i > 0 {
				sb.WriteByte(',')
			}
			v, _ := x.Get(k)
			sb.WriteString(Quote(k) + ":" + Repr(v))
		}
		sb.WriteByte('}')
		return sb.String()
	case *jsonx.Array:
		return "[" + reprItems(x.Items()) + "]"
	case []any:
		return "AL[" + reprItems(x) + "]"
	case jsonx.KeySet:
		return "<java.util.LinkedHashMap$LinkedKeySet>" + Quote(x.JavaString())
	}
	return fmt.Sprintf("<go:%T>%v", v, v)
}

func reprItems(items []any) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = Repr(it)
	}
	return strings.Join(parts, ",")
}

// Quote quotes a string like the oracles' q(): quotes and backslashes
// escaped, UTF-16 units outside printable ASCII as \uXXXX.
func Quote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, ch := range utf16.Encode([]rune(s)) {
		switch {
		case ch == '"' || ch == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(byte(ch))
		case ch < 0x20 || ch >= 0x7f:
			fmt.Fprintf(&sb, `\u%04X`, ch)
		default:
			sb.WriteByte(byte(ch))
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// ErrorOf converts a Go error from the compat packages to the oracle form.
func ErrorOf(err error) *JavaError {
	if err == nil {
		return nil
	}
	var e *jsonx.Error
	if errors.As(err, &e) {
		je := &JavaError{Class: e.Kind.JavaClass()}
		if !e.NoMessage {
			m := e.Message
			je.Message = &m
		}
		return je
	}
	m := err.Error()
	return &JavaError{Class: fmt.Sprintf("%T", err), Message: &m}
}

// SameError compares two recorded errors.
func SameError(a, b *JavaError) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Class != b.Class {
		return false
	}
	if a.Message == nil || b.Message == nil {
		return a.Message == b.Message
	}
	return *a.Message == *b.Message
}
