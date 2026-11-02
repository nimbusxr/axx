package jvalue

import (
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// A value is set as written, whatever the property held before: nothing is
// inferred from it (ADR 0014).
func TestSetValueSetsWhatItSays(t *testing.T) {
	doc, err := jsonx.Parse(`{"postcode": "10115", "weightGrams": 800, "express": false, "note": null, "name": "Ada"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct{ path, text string }{
		{"postcode", "53111"},              // a number over a string
		{"weightGrams", `"1200"`},          // a string over a number
		{"express", "true"},                // a boolean
		{"note", "42L"},                    // a long, over null
		{"ratio", "1.50"},                  // added
		{"recipient.address.city", "Bonn"}, // added, with its objects (ADR 0010)
		{"tags", `["a", 1]`},
		{"meta", `{"b": [true]}`},
		{"empty", `""`},
		{"name", "undefined"},
		{"express", "null"},
	} {
		v, err := core.ParseValue(s.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := SetValue(doc, s.path, v); err != nil {
			t.Fatalf("%s = %s: %v", s.path, s.text, err)
		}
	}
	got, _ := jsonx.Marshal(doc)
	want := `{"postcode":53111,"weightGrams":"1200","express":null,"note":42,"ratio":1.50,` +
		`"recipient":{"address":{"city":"Bonn"}},"tags":["a",1],"meta":{"b":[true]},"empty":""}`
	if got != want {
		t.Errorf("payload\n got %s\nwant %s", got, want)
	}

	// A path through a missing array has nothing to add to.
	v, _ := core.ParseValue("A", false, true)
	if err := SetValue(doc, "items[2].sku", v); !errors.Is(err, jsonx.ErrPathNotFound) {
		t.Errorf("a path through a missing array: %v", err)
	}
}

// A property the payload lacks is treated alike whatever the value: `null` is
// added, as `1` is, and `undefined` is already left out. An index past an
// array's end has no name to add.
func TestSetValueOnAnAbsentProperty(t *testing.T) {
	for _, c := range []struct{ path, text, want string }{
		{"b", "1", `{"a":[1,2],"b":1}`},
		{"b", "null", `{"a":[1,2],"b":null}`},
		{"b", "undefined", `{"a":[1,2]}`},
		{"x.y", "null", `{"a":[1,2],"x":{"y":null}}`},
		{"x.y", "undefined", `{"a":[1,2]}`},
		{"a[0]", "null", `{"a":[null,2]}`},
		{"a[0]", "undefined", `{"a":[2]}`},
		{"a[5]", "undefined", `{"a":[1,2]}`},
		{"a[1]", "7", `{"a":[1,7]}`},
	} {
		doc, err := jsonx.Parse(`{"a":[1,2]}`)
		if err != nil {
			t.Fatal(err)
		}
		v, err := core.ParseValue(c.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := SetValue(doc, c.path, v); err != nil {
			t.Errorf("%s = %s: %v", c.path, c.text, err)
			continue
		}
		if got, _ := jsonx.Marshal(doc); got != c.want {
			t.Errorf("%s = %s: %s, want %s", c.path, c.text, got, c.want)
		}
	}
	for _, text := range []string{"1", "null"} {
		doc, _ := jsonx.Parse(`{"a":[1,2]}`)
		v, _ := core.ParseValue(text, false, true)
		if err := SetValue(doc, "a[5]", v); !errors.Is(err, jsonx.ErrPathNotFound) {
			t.Errorf("a[5] = %s: %v", text, err)
		}
		if got, _ := jsonx.Marshal(doc); got != `{"a":[1,2]}` {
			t.Errorf("a[5] = %s: %s", text, got)
		}
	}
}

// A decimal goes out with the digits it was written with, alone or in JSON:
// never rounded, and never in Java's notation.
func TestSetValueSendsADecimalAsWritten(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"9007199254740993.0", "9007199254740993.0"},
		{"100000000.0", "100000000.0"},
		{"1.50", "1.50"},
		{"0.000001", "0.000001"},
		{"-1234567890123456.7", "-1234567890123456.7"},
		{"1.1234567890123456789012", "1.1234567890123456789012"},
		{`[100000000.0, {"b": 9007199254740993.0}]`, `[100000000.0,{"b":9007199254740993.0}]`},
		{"42", "42"},
		{"3000000000", "3000000000"},
		{"123456789012345678901234567890", "123456789012345678901234567890"},
		// An exponent keeps its value, past a double's range too: never a
		// JSON null for an infinity, or a zero.
		{"1e400", "1E+400"},
		{"1e-400", "1E-400"},
		{"1.5e3", "1.5E+3"},
		{"1e2000000000", "1E+2000000000"},
	} {
		doc, err := jsonx.Parse(`{}`)
		if err != nil {
			t.Fatal(err)
		}
		v, err := core.ParseValue(c.text, false, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := SetValue(doc, "a", v); err != nil {
			t.Errorf("%s: %v", c.text, err)
			continue
		}
		if got, _ := jsonx.Marshal(doc); got != `{"a":`+c.want+`}` {
			t.Errorf("%s: %s", c.text, got)
		}
	}
	// A decimal no payload's number can hold is an error, at once.
	doc, _ := jsonx.Parse(`{}`)
	v, err := core.ParseValue("1e99999999999", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetValue(doc, "a", v); err == nil || !strings.Contains(err.Error(), `write "1e99999999999" for the text [AXX-E0207]`) {
		t.Errorf("a huge exponent: %v", err)
	}
}
