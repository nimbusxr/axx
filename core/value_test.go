package core

import (
	"strings"
	"testing"
)

// A value means what it says (ADR 0014).
func TestParseValue(t *testing.T) {
	for _, c := range []struct {
		text, json, kind string
	}{
		{"42", "42", "an integer"},
		{"-7", "-7", "an integer"},
		{"0", "0", "an integer"},
		{"42L", "42", "a long"},
		{"1.5", "1.5", "a decimal"},
		{"2.0", "2.0", "a decimal"},
		{"true", "true", "a boolean"},
		{"FALSE", "false", "a boolean"},
		{"null", "null", "null"},
		{"Null", "null", "null"},
		{`"42"`, `"42"`, "a string"},
		{`"null"`, `"null"`, "a string"},
		{`"undefined"`, `"undefined"`, "a string"},
		{`"01067"`, `"01067"`, "a string"},
		{`""`, `""`, "a string"},
		{`"a "quoted" word"`, `"a \"quoted\" word"`, "a string"},
		{"REGISTERED", `"REGISTERED"`, "a string"},
		{"PX-4101", `"PX-4101"`, "a string"},
		{"<b>&", `"<b>&"`, "a string"},
		{"1.2.3", `"1.2.3"`, "a string"},
		{`{"a": 1}`, `{"a": 1}`, "a JSON object"},
		{"[1, 2]", "[1, 2]", "a JSON array"},
	} {
		v, err := ParseValue(c.text, false, true)
		if err != nil {
			t.Errorf("%s: %v", c.text, err)
			continue
		}
		if v.JSON() != c.json || v.kindName() != c.kind {
			t.Errorf("%s: %s (%s), want %s (%s)", c.text, v.JSON(), v.kindName(), c.json, c.kind)
		}
	}
	if v, err := ParseValue("undefined", false, true); err != nil || !v.Undefined() || v.JSON() != "" {
		t.Errorf("undefined: %+v, %v", v, err)
	}
	if s, ok := mustValue(t, `"42"`).Str(); !ok || s != "42" {
		t.Errorf(`"42".Str() = %q, %v`, s, ok)
	}
	if _, ok := mustValue(t, "42").Str(); ok {
		t.Error("42 is not a string")
	}
}

func mustValue(t *testing.T, text string) Value {
	t.Helper()
	v, err := ParseValue(text, false, true)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A value that could be read two ways is an error naming the explicit forms.
func TestParseValueAmbiguous(t *testing.T) {
	for _, c := range []struct {
		text      string
		empty     bool
		undefined bool
		want      string
	}{
		{"", true, true, `write "" for the empty string, null for null, or undefined for no such property`},
		{"", true, false, `write "" for the empty string, or null for null`},
		{"01067", false, true, `01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`},
		{"+5", false, true, `write 5 for the number, or "+5" for the text`},
		{"1.5f", false, true, `write 1.5 for the number, or "1.5f" for the text`},
		{"2d", false, true, `write 2.0 for the number, or "2d" for the text`},
		{".5", false, true, `write 0.5 for the number, or ".5" for the text`},
		{"5.", false, true, `write 5.0 for the number, or "5." for the text`},
		{"1e5", false, true, `write 100000.0 for the number, or "1e5" for the text`},
		{"0x1F", false, true, `write 31 for the number, or "0x1F" for the text`},
		{"NaN", false, true, `NaN is not a JSON number: write "NaN" for the text`},
		{"-Infinity", false, true, `write "-Infinity" for the text`},
		{"undefined", false, false, `undefined (no such property) is not a value here: write "undefined" for the text`},
		{"{oops}", false, true, `{oops} is not valid JSON`},
	} {
		_, err := ParseValue(c.text, c.empty, c.undefined)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.text, err, c.want)
		}
	}
}

// Where only text fits, double quotes give the text inside, and a value that
// reads as anything else asks for them.
func TestParseText(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{`"3"`, "3"},
		{`"true"`, "true"},
		{`"null"`, "null"},
		{`""`, ""},
		{"application/json", "application/json"},
		{"PX-4101", "PX-4101"},
	} {
		s, undef, err := ParseText(c.text, false, false)
		if err != nil || undef || s != c.want {
			t.Errorf("%s: %q, %v, %v", c.text, s, undef, err)
		}
	}
	for _, c := range []struct{ text, want string }{
		{"3", `3 reads as an integer, and only text fits here: write "3" for the text`},
		{"true", `true reads as a boolean, and only text fits here: write "true" for the text`},
		{"null", `null reads as null, and only text fits here: write "null" for the text`},
		{"undefined", `write "undefined" for the text`},
		{"01067", `01067 is text here: write "01067"`},
		{`{"a":1}`, `reads as a JSON object`},
	} {
		if _, _, err := ParseText(c.text, false, false); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.text, err, c.want)
		}
	}
	if _, undef, err := ParseText("undefined", false, true); err != nil || !undef {
		t.Errorf("undefined where the step takes it: %v, %v", undef, err)
	}
	if _, _, err := ParseText("", true, false); err == nil || !strings.Contains(err.Error(), `write "" for the empty text`) {
		t.Errorf("empty text cell: %v", err)
	}
}

func TestValueCells(t *testing.T) {
	named := &TableDoc{Columns: []string{"JSONPath", "value"}, Values: JSONValues}
	if got := named.ValueCells([]string{"a", "b"}); len(got) != 1 || got[0] != 1 {
		t.Errorf("named columns: %v", got)
	}
	rows := &TableDoc{Values: TextValues}
	if got := rows.ValueCells([]string{"a", "b", "c"}); len(got) != 3 {
		t.Errorf("rows: %v", got)
	}
	if got := (&TableDoc{Columns: []string{"property", "value"}}).ValueCells([]string{"a", "b"}); got != nil {
		t.Errorf("settings: %v", got)
	}
}
