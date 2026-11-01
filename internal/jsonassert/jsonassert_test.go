package jsonassert

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

const doc = `{"priority": "high", "crew": {"size": 3, "lead": "Ada"}, "ratio": 2.50, "tags": ["a", "b"], "note": null, "ok": true}`

func table(rows ...[]string) *core.Table { return &core.Table{Rows: rows} }

func TestPropertiesPass(t *testing.T) {
	err := Properties(doc, table(
		[]string{"priority", "high"},
		[]string{"crew.size", "3"},
		[]string{"$.crew.lead", "Ada"},
		[]string{"ratio", "2.5"},
		[]string{"tags[1]", "b"},
		[]string{"note", "NULL"},
		[]string{"missing", "undefined"},
		[]string{"ok", "true"},
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Properties(doc, table([]string{"crew.lead", "A.a"}, []string{"crew.size", `\d+`}), true); err != nil {
		t.Fatal(err)
	}
}

func TestPropertiesFailTogether(t *testing.T) {
	err := Properties(doc, table(
		[]string{"priority", "low"},
		[]string{"crew.size", "3"},
		[]string{"note", "undefined"},
		[]string{"gone", "x"},
	), false)
	var ae *core.AssertionError
	if !errors.As(err, &ae) {
		t.Fatalf("want an assertion failure, got %v", err)
	}
	if !strings.Contains(ae.Message, "priority, note, gone") {
		t.Errorf("message %q", ae.Message)
	}
	act := ae.Actual.(map[string]any)
	if fmt.Sprint(act["priority"]) != `"high"` || fmt.Sprint(act["note"]) != "null" || act["gone"] != "<absent>" {
		t.Errorf("actual %#v", act)
	}
	if err := Properties(doc, table([]string{"crew.lead", "A"}), true); !core.IsAssertion(err) {
		t.Errorf("a partial regex match must fail: %v", err)
	}
}

func TestPropertiesErrors(t *testing.T) {
	if err := Properties("not json", table([]string{"a", "b"}), false); err == nil || !strings.Contains(err.Error(), "Could not perform selection") {
		t.Errorf("invalid JSON: %v", err)
	}
	if err := Properties(doc, table([]string{"a", "("}), true); err == nil || core.IsAssertion(err) {
		t.Errorf("invalid pattern must be an error, not an assertion: %v", err)
	}
}

// Values mean what they say (ADR 0014): compared with their types, with a
// hint when only the type differs, and an error for a cell that could be
// read two ways.
func TestPropertiesTyped(t *testing.T) {
	if err := Properties(doc, table([]string{"priority", `"high"`}, []string{"crew.size", "3"}, []string{"ratio", "2.5"}), false); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		row  []string
		hint string
	}{
		{[]string{"crew.size", `"3"`}, "crew.size is the integer 3: write 3"},
		{[]string{"ok", `"true"`}, "ok is the boolean true: write true"},
	} {
		err := Properties(doc, table(c.row), false)
		var ae *core.AssertionError
		if !errors.As(err, &ae) || !strings.Contains(ae.Message, c.hint) {
			t.Errorf("%v: %v, want the hint %q", c.row, err, c.hint)
		}
	}
	str := `{"postcode": "53111"}`
	err := Properties(str, table([]string{"postcode", "53111"}), false)
	if err == nil || !strings.Contains(err.Error(), `postcode is the string "53111": write "53111"`) {
		t.Errorf("a number for a string: %v", err)
	}
	if err := Properties(`{"whole": 2.0}`, table([]string{"whole", "2"}), false); err == nil || !strings.Contains(err.Error(), "whole is the decimal 2.0: write 2.0") {
		t.Errorf("an integer for a decimal: %v", err)
	}
	if err := Properties(str, table([]string{"postcode", ""}), false); err == nil || core.IsAssertion(err) || !strings.Contains(err.Error(), `postcode: the cell is empty: write "" for the empty string`) {
		t.Errorf("an empty cell: %v", err)
	}
	for _, c := range []struct{ value, want string }{
		{"01067", `postcode: 01067 could be the number 1067 or the text 01067`},
		{"+5", `write 5 for the number, or "+5" for the text`},
	} {
		if err := Properties(str, table([]string{"postcode", c.value}), false); err == nil || core.IsAssertion(err) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.value, err)
		}
	}
}
