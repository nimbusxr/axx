package core

import (
	"errors"
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
		{"1e3", "1e3", "a decimal"},
		{"1E+3", "1E+3", "a decimal"},
		{"-1.5e-3", "-1.5e-3", "a decimal"},
		{"1e400", "1e400", "a decimal"},
		{"1e-400", "1e-400", "a decimal"},
		{"1e99999999999", "1e99999999999", "a decimal"},
		{"true", "true", "a boolean"},
		{"false", "false", "a boolean"},
		{"null", "null", "null"},
		// Only these words, as they are written here, are these values.
		{"True", `"True"`, "a string"},
		{"FALSE", `"FALSE"`, "a string"},
		{"Null", `"Null"`, "a string"},
		{"NULL", `"NULL"`, "a string"},
		{"Undefined", `"Undefined"`, "a string"},
		{"UNDEFINED", `"UNDEFINED"`, "a string"},
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
		{"0x1F", false, true, `write 31 for the number, or "0x1F" for the text`},
		// A suffix, a sign or a leading zero fails, with a long's too.
		{"1.5L", false, true, `write 1.5 for the number, or "1.5L" for the text`},
		{"+5L", false, true, `write 5L for the number, or "+5L" for the text`},
		{"042L", false, true, `write 42L for the number, or "042L" for the text`},
		{"1e5f", false, true, `write 1e5 for the number, or "1e5f" for the text`},
		{"+1e5", false, true, `write 1e5 for the number, or "+1e5" for the text`},
		{"01e5", false, true, `write 1e5 for the number, or "01e5" for the text`},
		{".5e3", false, true, `write 0.5e3 for the number, or ".5e3" for the text`},
		{"5.e3", false, true, `write 5.0e3 for the number, or "5.e3" for the text`},
		{"-007.50", false, true, `write -7.50 for the number, or "-007.50" for the text`},
		// What it advises is the same number, however long.
		{"0123456789012345678901", false, true, `write 123456789012345678901 for the number`},
		{"000.1234567890123456789012", false, true, `write 0.1234567890123456789012 for the number`},
		{"01e-400", false, true, `write 1e-400 for the number`},
		{"01e400", false, true, `write 1e400 for the number`},
		{"0xFFFFFFFFFFFFFFFFFFFF", false, true, `write 1208925819614629174706175 for the number`},
		{"00", false, true, `write 0 for the number`},
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

// Every value the rule cannot read says so with the code axx validate gives
// it, wherever it is reported from.
func TestValueErrorsCarryTheirCode(t *testing.T) {
	for _, read := range []func() error{
		func() error { _, err := ParseValue("", true, true); return err },
		func() error { _, err := ParseValue("01067", false, true); return err },
		func() error { _, err := ParseValue("NaN", false, true); return err },
		func() error { _, err := ParseValue("undefined", false, false); return err },
		func() error { _, err := ParseValue("{oops}", false, true); return err },
		func() error { _, _, err := ParseText("", true, false); return err },
		func() error { _, _, err := ParseText("3", false, false); return err },
		func() error { _, _, err := ParseText("01067", false, false); return err },
	} {
		err := read()
		if err == nil || !strings.HasSuffix(err.Error(), " ["+ValueErrorCode+"]") {
			t.Errorf("%v: no code", err)
		}
		var ve *ValueError
		if !errors.As(err, &ve) || strings.Contains(ve.Message(), ValueErrorCode) {
			t.Errorf("%v: the message without its code", err)
		}
	}
}

// A hint says what to write where the value is: a {string} parameter's quotes
// delimit it, so a table cell's `"2500"` is `'"2500"'` in a step's own text.
func TestInStepText(t *testing.T) {
	for _, c := range []struct {
		text              string
		empty, undefined  bool
		cell, inStepsText string
	}{
		{
			"01067", false, true,
			`01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`,
			`01067 could be the number 1067 or the text 01067: write '1067' for the number, or '"01067"' for the text`,
		},
		{"NaN", false, true, `NaN is not a JSON number: write "NaN" for the text`, `NaN is not a JSON number: write '"NaN"' for the text`},
		{
			"undefined", false, false,
			`undefined (no such property) is not a value here: write "undefined" for the text`,
			`undefined (no such property) is not a value here: write '"undefined"' for the text`,
		},
		{
			"", true, false,
			`the cell is empty: write "" for the empty string, or null for null`,
			`the value is empty: write '""' for the empty string, or 'null' for null`,
		},
		{
			"{oops}", false, true,
			`{oops} is not valid JSON: write it as JSON, or in double quotes for the text`,
			`{oops} is not valid JSON: write it as JSON, or as '"{oops}"' for the text`,
		},
	} {
		_, err := ParseValue(c.text, c.empty, c.undefined)
		if got := err.Error(); got != c.cell+" [AXX-E0207]" {
			t.Errorf("%q in a cell: %s", c.text, got)
		}
		if got := InStepText(err).Error(); got != c.inStepsText+" [AXX-E0207]" {
			t.Errorf("%q in a step's text: %s", c.text, got)
		}
	}
	if err := errors.New("other"); !errors.Is(InStepText(err), err) {
		t.Error("another error comes back as it is")
	}
	if got := StepText("it's"); got != `'it\'s'` {
		t.Errorf("a quote in a form: %s", got)
	}
}

// A row holds text as the steps read it, and only then: a bare key of a named
// row, or more words after a row's whole name, is a path into the JSON.
func TestRowIsText(t *testing.T) {
	d := &TableDoc{TextRows: []string{"attribute <name>", "routing key"}}
	for key, want := range map[string]bool{
		"attribute x":       true,
		"attribute":         false,
		"attributes x":      false,
		"routing key":       true,
		"routing key extra": false,
		"routing":           false,
		"status":            false,
	} {
		if got := d.RowIsText(key); got != want {
			t.Errorf("%q: %v", key, got)
		}
	}
	if (*TableDoc)(nil).RowIsText("x") {
		t.Error("no table")
	}
}

// A reference is a string, whatever it expands to, in a string or in the
// strings of a JSON value; numbers stay as written, and the value's text.
func TestValueExpand(t *testing.T) {
	expand := func(s string) (string, error) {
		return strings.NewReplacer("${T}", `a"b`, "${N}", "01067").Replace(s), nil
	}
	for _, c := range []struct{ text, want string }{
		{"${N}", `"01067"`},
		{`"${N}"`, `"01067"`},
		{`"x ${T}"`, `"x a\"b"`},
		{`{"token": "${T}", "n": 100000000.0, "\u0024{N}": 1, "${N}": [" ${N}"]}`, `{"token": "a\"b", "n": 100000000.0, "\u0024{N}": 1, "01067": [" 01067"]}`},
		{`["\"${N}\"", 1.50, true, null]`, `["\"01067\"", 1.50, true, null]`},
		{"42", "42"},
		{"1.50", "1.50"},
		{"true", "true"},
		{"null", "null"},
		{"undefined", ""},
	} {
		v := mustValue(t, c.text)
		got, err := v.Expand(expand)
		if err != nil || got.JSON() != c.want || got.Text() != c.text {
			t.Errorf("%s: %s (%s), %v, want %s", c.text, got.JSON(), got.Text(), err, c.want)
		}
	}
	failing := func(string) (string, error) { return "", errors.New("no such token") }
	if _, err := mustValue(t, `{"a": "${token:x}"}`).Expand(failing); err == nil {
		t.Error("an expansion's error")
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

// A step's argument that holds a value reads by the rule, as a table's cell
// does, and says what to write in the step's quotes (ADR 0014).
func TestReadArg(t *testing.T) {
	s := StepDef{TextArgs: []int{0}, TypedArgs: []int{1}}
	for _, c := range []struct {
		arg         int
		param, text string
		want        string // what Run gets
		fails       string
	}{
		{arg: 0, param: "string", text: "application/json", want: "application/json"},
		{arg: 0, param: "string", text: `"5"`, want: "5"},
		{arg: 0, param: "string", text: `"{"a":1}"`, want: `{"a":1}`},
		{arg: 0, param: "string", text: `"status":"ok"`, want: `status":"ok`},
		{arg: 0, param: "string", text: `""status":"ok""`, want: `"status":"ok"`},
		{arg: 0, param: "string", text: `""`, want: ""},
		{arg: 0, param: "string", text: "${env:X}", want: "${env:X}"},
		{arg: 0, param: "string", text: "5", fails: `5 reads as an integer, and only text fits here: write '"5"' for the text`},
		{arg: 0, param: "string", text: "true", fails: `true reads as a boolean, and only text fits here: write '"true"' for the text`},
		{arg: 0, param: "string", text: "null", fails: `null reads as null, and only text fits here: write '"null"' for the text`},
		{arg: 0, param: "string", text: "undefined", fails: `undefined reads as undefined (no such property), and only text fits here: write '"undefined"' for the text`},
		{arg: 0, param: "string", text: "01067", fails: `01067 is text here: write '"01067"'`},
		{arg: 0, param: "string", text: `{"a":1}`, fails: `{"a":1} reads as a JSON object, and only text fits here: write '"{"a":1}"' for the text`},
		{arg: 0, param: "string", text: `{a}`, fails: `{a} is text here: write '"{a}"'`},
		{arg: 0, param: "string", text: "it's 5", want: "it's 5"},
		{arg: 0, param: "string", text: "", fails: `the value is empty: write '""' for the empty text`},
		// A {word} has no quotes of its own: what it writes is what a cell writes.
		{arg: 0, param: "word", text: "5", fails: `5 reads as an integer, and only text fits here: write "5" for the text`},
		{arg: 0, param: "word", text: `"5"`, want: "5"},
		// A typed value stays as written, for Run to read.
		{arg: 1, param: "string", text: `"5"`, want: `"5"`},
		{arg: 1, param: "string", text: "5", want: "5"},
		{arg: 1, param: "string", text: "01067", fails: `01067 could be the number 1067 or the text 01067: write '1067' for the number, or '"01067"' for the text`},
		{arg: 1, param: "string", text: "undefined", fails: `undefined (no such property) is not a value here: write '"undefined"' for the text`},
		{arg: 1, param: "string", text: "", fails: `the value is empty: write '""' for the empty string, or 'null' for null`},
		{arg: 1, param: "integer", text: "2500", want: "2500"},
		// Any other argument is not a value: a name, a pattern.
		{arg: 2, param: "string", text: "5", want: "5"},
		{arg: 2, param: "string", text: "", want: ""},
		{arg: 2, param: "string", text: `"5"`, want: `"5"`},
	} {
		got, err := s.ReadArg(c.arg, c.param, c.text)
		switch {
		case c.fails != "":
			var ve *ValueError
			if !errors.As(err, &ve) || ve.Message() != c.fails || err.Error() != c.fails+" ["+ValueErrorCode+"]" {
				t.Errorf("%d %q: %v, want %s", c.arg, c.text, err, c.fails)
			}
		case err != nil || got != c.want:
			t.Errorf("%d %q: %q %v, want %q", c.arg, c.text, got, err, c.want)
		}
	}
	undefined := StepDef{TypedArgs: []int{0}, TypedArgsUndefined: true}
	if _, err := undefined.ReadArg(0, "string", "undefined"); err != nil {
		t.Error(err)
	}
}
