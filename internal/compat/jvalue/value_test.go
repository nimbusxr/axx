package jvalue

import (
	"testing"

	"github.com/nimbusxr/axx/core"
)

// The rule (core.ParseValue) and REST's checks (CoerceExpected) read every
// value the rule accepts the same way: ADR 0014 made REST's reading every
// pack's.
func TestTheRuleIsRESTsReading(t *testing.T) {
	for _, text := range []string{
		"42", "-7", "0", "2147483648", "42L", "1.5", "-0.25", "2.0", "true", "FALSE",
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
