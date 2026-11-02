package jvalue

import (
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// The rule (core.ParseValue) and REST's checks (CoerceExpected) read every
// value the rule accepts the same way: ADR 0014 made REST's reading every
// pack's.
func TestTheRuleIsRESTsReading(t *testing.T) {
	for _, text := range []string{
		"42", "-7", "0", "2147483648", "42L", "1.5", "-0.25", "2.0", "true", "false",
		`"42"`, `"null"`, `""`, "REGISTERED", "PX-4101", "1.2.3", "a b", `{"a": [1, "x"]}`, "[1, 2.5]",
	} {
		v, err := core.ParseValue(text, false, true)
		if err != nil {
			t.Errorf("%s: %v", text, err)
			continue
		}
		got, err := Expected(v)
		if err != nil {
			t.Errorf("%s: %v", text, err)
			continue
		}
		want, err := CoerceExpected(text)
		if err != nil {
			t.Fatal(err)
		}
		if !Equal(got, want) {
			t.Errorf("%s: %v, CoerceExpected %v", text, got, want)
		}
	}
}

func TestValueIsStrict(t *testing.T) {
	doc := `{"zip": "53111", "n": 3, "d": 2.0, "b": true, "z": null}`
	for _, c := range []struct {
		path, text string
		want       bool
	}{
		{"zip", `"53111"`, true},
		{"zip", "53111", false},
		{"n", "3", true},
		{"n", `"3"`, false},
		{"n", "3.0", false},
		{"d", "2.0", true},
		{"d", "2", false},
		{"b", "true", true},
		{"b", `"true"`, false},
		{"z", "null", true},
		{"z", `"null"`, false},
		{"gone", "undefined", true},
		{"n", "undefined", false},
	} {
		v, err := core.ParseValue(c.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := ValueIs(doc, c.path, v); err != nil || ok != c.want {
			t.Errorf("%s is %s: %v, %v", c.path, c.text, ok, err)
		}
	}
}

// Numbers compare by their type, integer or decimal, and their exact value,
// whichever Java class holds them: a long decimal (a BigDecimal) or an
// integer past the long range (a BigInteger) matches the same value.
func TestValueIsNumbers(t *testing.T) {
	doc := `{"big": 123456789012345678901234567890, "long": 3000000000, "dec": 1.1234567890123456789012, "tenth": 0.1, "whole": 6.0}`
	for _, c := range []struct {
		path, text string
		want       bool
	}{
		{"big", "123456789012345678901234567890", true},
		{"big", "123456789012345678901234567891", false},
		{"long", "3000000000", true},
		{"long", "3000000000L", true},
		{"dec", "1.1234567890123456789012", true},
		{"dec", "1.1234567890123456789013", false},
		{"tenth", "0.1", true},
		{"tenth", "0.10", true},
		{"whole", "6.0", true},
		{"whole", "6", false},
	} {
		v, err := core.ParseValue(c.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := ValueIs(doc, c.path, v); err != nil || ok != c.want {
			t.Errorf("%s is %s: %v, %v", c.path, c.text, ok, err)
		}
	}
}

// A decimal compares by its digits, not the double that holds it, alone or in
// a list or an object, and whatever its notation or its exponent's size.
func TestValueIsExact(t *testing.T) {
	for _, c := range []struct {
		doc, text string
		want      bool
	}{
		{`{"a": 9007199254740992.0}`, "9007199254740993.0", false},
		{`{"a": 9007199254740993.0}`, "9007199254740993.0", true},
		{`{"a": 1234567890123456.7}`, "1234567890123456.8", false},
		{`{"a": 1234567890123456.7}`, "1234567890123456.70", true},
		{`{"a": 1.50000000000000000000}`, "1.5", true},
		// In a list, as json-smart holds it, and in an object.
		{`{"a": [1.50000000000000000000]}`, "[1.5]", true},
		{`{"a": [1.5]}`, "[1.50000000000000000000]", true},
		{`{"a": [1.5, 2]}`, "[1.5, 2.0]", false},
		{`{"a": [1.5]}`, "[1.5, 2]", false},
		{`{"a": [9007199254740992.0]}`, "[9007199254740993.0]", false},
		{`{"a": {"b": [1.50000000000000000000]}}`, `{"b": [1.5]}`, true},
		{`{"a": [{"b": 1.50}, "x"]}`, `[{"b": 1.5}, "x"]`, true},
		{`{"a": [1]}`, `{"0": 1}`, false},
		// Notations of a number, which is a decimal in all of them.
		{`{"a": 1000.0}`, "1e3", true},
		{`{"a": 1e3}`, "1E+3", true},
		{`{"a": 1.5e3}`, "1500.0", true},
		{`{"a": 0.0015}`, "1.5e-3", true},
		{`{"a": 1000}`, "1e3", false},
		{`{"a": 1e3}`, "1000", false},
		{`{"a": 0.0}`, "0e5", true},
		// Past a double's range, and past any range: nothing is computed.
		{`{"a": 1e400}`, "1e400", true},
		{`{"a": 1e400}`, "1e401", false},
		{`{"a": 1e400}`, "10e399", true},
		{`{"a": 1e-400}`, "1e-400", true},
		{`{"a": 1e-400}`, "0.0", false},
		{`{"a": 1e99999999999}`, "1e99999999999", true},
		{`{"a": 1e99999999999}`, "10e99999999998", true},
		{`{"a": 1e99999999999}`, "1e99999999998", false},
		{`{"a": 1e2000000000}`, "1e2000000000", true},
		{`{"a": 1e-2000000000}`, "1e2000000000", false},
	} {
		v, err := core.ParseValue(c.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := ValueIs(c.doc, "a", v); err != nil || ok != c.want {
			t.Errorf("%s is %s: %v, %v", c.doc, c.text, ok, err)
		}
	}
}

// What a hint says to write is the number the document has, every digit.
func TestTypeHintKeepsTheDigits(t *testing.T) {
	doc, err := jsonx.Parse(`{"a": 9007199254740993.0}`)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, err := jsonx.Read(doc, "a")
	if err != nil {
		t.Fatal(err)
	}
	v, _ := core.ParseValue(`"9007199254740993.0"`, false, true)
	if got, want := TypeHint("a", v, actual), "a is the decimal 9007199254740993.0: write 9007199254740993.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A hint for a value in a step's own text says what to write there.
func TestStepTypeHint(t *testing.T) {
	v := func(text string) core.Value {
		x, err := core.ParseValue(text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	doc, err := jsonx.Parse(`{"zip": "2500", "n": 3, "d": 2.0, "b": true}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, text, cell, step string }{
		{"zip", "2500", `zip is the string "2500": write "2500"`, `zip is the string "2500": write '"2500"'`},
		{"n", `"3"`, `n is the integer 3: write 3`, `n is the integer 3: write '3'`},
		{"n", "3.0", `n is the integer 3: write 3`, `n is the integer 3: write '3'`},
		{"d", "2", `d is the decimal 2.0: write 2.0`, `d is the decimal 2.0: write '2.0'`},
		{"b", `"true"`, `b is the boolean true: write true`, `b is the boolean true: write 'true'`},
	} {
		actual, _, err := jsonx.Read(doc, c.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := TypeHint(c.path, v(c.text), actual); got != c.cell {
			t.Errorf("%s in a cell: %s", c.text, got)
		}
		if got := StepTypeHint(c.path, v(c.text), actual); got != c.step {
			t.Errorf("%s in a step's text: %s", c.text, got)
		}
	}
}

// A document of null is a value: $ is null, and any path under it is absent.
func TestValueIsOnANullDocument(t *testing.T) {
	for _, c := range []struct {
		doc, path, text string
		want            bool
	}{
		{"null", "$", "null", true},
		{" null\n", "$", "null", true},
		{"null", "$", "true", false},
		{"null", "$", "42", false},
		{"null", "$", `"null"`, false},
		{"null", "$", "undefined", false},
		{"null", "a", "undefined", true},
		{"null", "a.b[0]", "undefined", true},
		{"null", "$.a", "undefined", true},
		{"null", "a", "null", false},
		{"null", "a", "1", false},
	} {
		v, err := core.ParseValue(c.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := ValueIs(c.doc, c.path, v); err != nil || ok != c.want {
			t.Errorf("%q: %s is %s: %v, %v", c.doc, c.path, c.text, ok, err)
		}
	}
	v, _ := core.ParseValue("1", false, true)
	if _, err := ValueIs("null", "$[", v); err == nil {
		t.Error("an invalid path stays an error")
	}
}
