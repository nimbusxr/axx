package columntype

import (
	"errors"
	"testing"

	"github.com/nimbusxr/axx/core"
)

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		kind Kind
		cell string
		want string // what to write, "" when it fits
	}{
		{Text, "PX-1", ""},
		{Text, `"10115"`, ""},
		{Text, "10115", `"10115"`},
		{Text, "42L", `"42L"`},
		{Text, "1.5", `"1.5"`},
		{Text, "true", `"true"`},
		{Text, "null", ""},
		{Text, `{"zone": "DE-1"}`, ""},

		{Other, "2026-09-24", ""},
		{Other, `"2026"`, ""},
		{Other, "2026", `"2026"`},
		{Other, "false", `"false"`},
		{Other, "null", ""},
		{Other, `["a"]`, ""},

		{Integer, "850", ""},
		{Integer, "850L", ""},
		{Integer, "null", ""},
		{Integer, `"850"`, "850"},
		{Integer, `"850.0"`, "850"},
		{Integer, "850.0", "850"},
		{Integer, "1e3", "1000"},
		{Integer, "850.5", "an integer, not 850.5"},
		{Integer, "1e999", "an integer, not 1e999"},
		{Integer, "PX-1", "an integer, not PX-1"},
		{Integer, `"01067"`, `an integer, not "01067"`},
		{Integer, "true", "an integer, not true"},
		{Integer, `"true"`, `an integer, not "true"`},

		{Decimal, "850.0", ""},
		{Decimal, "1.5", ""},
		{Decimal, "1e3", ""},
		{Decimal, "850", "850.0"},
		{Decimal, "850L", "850.0"},
		{Decimal, `"12.50"`, "12.50"},
		{Decimal, `"12"`, "12.0"},
		{Decimal, "true", "a decimal, not true"},
		{Decimal, "free", "a decimal, not free"},

		{Boolean, "true", ""},
		{Boolean, "false", ""},
		{Boolean, "null", ""},
		{Boolean, `"true"`, "true"},
		{Boolean, "1", "true"},
		{Boolean, "0", "false"},
		{Boolean, "2", "true or false, not 2"},
		{Boolean, "1.0", "true or false, not 1.0"},
		{Boolean, "yes", "true or false, not yes"},

		{IntegerOrBoolean, "1", ""},
		{IntegerOrBoolean, "true", ""},
		{IntegerOrBoolean, "1.0", "1"},
		{IntegerOrBoolean, `"1"`, "1"},
		{IntegerOrBoolean, `"true"`, "true"},
		{IntegerOrBoolean, "yes", "an integer, not yes"},
	} {
		v, err := core.ParseValue(c.cell, false, false)
		if err != nil {
			t.Fatalf("%s: %v", c.cell, err)
		}
		col := Column{Name: "col", Table: "parcels.pickups", Type: "T", Kind: c.kind}
		got := col.Wrong(v)
		want := ""
		if c.want != "" {
			want = "col " + col.is() + " in parcels.pickups (T): write " + c.want
		}
		if got != want {
			t.Errorf("%v %s:\ngot  %s\nwant %s", c.kind, c.cell, got, want)
		}
	}
}

func TestTheMessageNamesTheColumnItsTypeAndWhatToWrite(t *testing.T) {
	v, _ := core.ParseValue("10115", false, false)
	c := Column{Name: "postcode", Table: "parcels.pickups", Type: "VARCHAR", Kind: Text}
	err := Fail([]string{c.Wrong(v)})
	var ve *core.ValueError
	if !errors.As(err, &ve) {
		t.Fatalf("%v is not a value error, as the rule's are", err)
	}
	if want := `postcode is text in parcels.pickups (VARCHAR): write "10115" [AXX-E0207]`; err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
	if err := Fail(nil); err != nil {
		t.Errorf("nothing wrong: %v", err)
	}
}
