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
		{"express", "TRUE"},                // a boolean
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
	want := `{"postcode":53111,"weightGrams":"1200","express":null,"note":42,"ratio":1.5,` +
		`"recipient":{"address":{"city":"Bonn"}},"tags":["a",1],"meta":{"b":[true]},"empty":""}`
	if got != want {
		t.Errorf("payload\n got %s\nwant %s", got, want)
	}

	// null and undefined change a property the payload has; a path through
	// a missing array has nothing to add to.
	for _, s := range []struct{ path, text string }{{"gone", "null"}, {"gone", "undefined"}} {
		v, _ := core.ParseValue(s.text, false, true)
		var se *StepError
		if err := SetValue(doc, s.path, v); !errors.As(err, &se) || !strings.HasPrefix(se.Message, "Property not found in payload") {
			t.Errorf("%s = %s: %v", s.path, s.text, err)
		}
	}
	v, _ := core.ParseValue("A", false, true)
	if err := SetValue(doc, "items[2].sku", v); !errors.Is(err, jsonx.ErrPathNotFound) {
		t.Errorf("a path through a missing array: %v", err)
	}
}
