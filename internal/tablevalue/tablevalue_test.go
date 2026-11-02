package tablevalue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuild(t *testing.T) {
	got, err := Build([]Row{
		{Path: "reference", Value: "PX-GQL-7201"},
		{Path: "weightGrams", Value: "800"},
		{Path: "express", Value: "true"},
		{Path: "address.postcode", Value: `"01067"`},
		{Path: "address.country", Value: `"DE"`},
		{Path: "lines[1].reference", Value: "PX-GQL-7202"},
		{Path: "lines[0].reference", Value: "PX-GQL-7203"},
		{Path: "tags", Value: `["fragile"]`},
		{Path: "note", Value: "null"},
		{Path: "hold", Value: "true"},
		{Path: "hold", Value: "undefined"},
		{Path: "$.depot", Value: "4"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `{"address":{"country":"DE","postcode":"01067"},"depot":4,"express":true,"lines":[{"reference":"PX-GQL-7203"},{"reference":"PX-GQL-7202"}],` +
		`"note":null,"reference":"PX-GQL-7201","tags":["fragile"],"weightGrams":800}`
	if string(b) != want {
		t.Errorf("built\n got %s\nwant %s", b, want)
	}
}

func TestApply(t *testing.T) {
	base := map[string]any{"reference": "PX-GRPC-5101", "recipient": map[string]any{"name": "Ida Hoffmann"}}
	got, err := Apply(base, []Row{{Path: "recipient.postcode", Value: `"04109"`}, {Path: "reference", Value: "undefined"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if want := `{"recipient":{"name":"Ida Hoffmann","postcode":"04109"}}`; string(b) != want {
		t.Errorf("applied\n got %s\nwant %s", b, want)
	}
}

func TestBuildErrors(t *testing.T) {
	for _, c := range []struct {
		rows []Row
		want string
	}{
		{[]Row{{Path: "", Value: "x"}}, "names nothing"},
		{[]Row{{Path: "a..b", Value: "x"}}, "has an empty step"},
		{[]Row{{Path: "lines[x]", Value: "x"}}, "not a number"},
		{[]Row{{Path: "a", Value: "1"}, {Path: "a.b", Value: "2"}}, "not an object"},
		{[]Row{{Path: "a", Value: "1"}, {Path: "a[0]", Value: "2"}}, "not an array"},
		// A value means what it says (ADR 0014).
		{[]Row{{Path: "depot", Value: "04"}}, `depot: 04 could be the number 4 or the text 04`},
		{[]Row{{Path: "reason", Null: true}}, `reason: the cell is empty`},
	} {
		if _, err := Build(c.rows, nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: got %v, want %q", c.rows, err, c.want)
		}
	}
}

// A reference is expanded in a string, after the value is read: it stays
// text, whatever it expands to.
func TestBuildExpands(t *testing.T) {
	expand := func(v string) (string, error) { return strings.ReplaceAll(v, "${env:PIN}", "0042"), nil }
	got, err := Build([]Row{{Path: "pin", Value: "${env:PIN}"}, {Path: "note", Value: `"pin ${env:PIN}"`}, {Path: "n", Value: "42"}}, expand)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if want := `{"n":42,"note":"pin 0042","pin":"0042"}`; string(b) != want {
		t.Errorf("built\n got %s\nwant %s", b, want)
	}
}

// The strings in a JSON value expand too, with what they expand to kept in a
// string, and an expansion's error fails the row.
func TestBuildExpandsInsideJSON(t *testing.T) {
	expand := func(v string) (string, error) {
		return strings.NewReplacer("${env:TOKEN}", `to"ken`, "${env:KEY}", "k").Replace(v), nil
	}
	got, err := Build([]Row{
		{Path: "auth", Value: `{"token":"${env:TOKEN}","${env:KEY}":[" ${env:TOKEN}",100000000.0],"n":1}`},
		{Path: "list", Value: `["${env:KEY}"]`},
	}, expand)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if want := `{"auth":{"k":[" to\"ken",100000000.0],"n":1,"token":"to\"ken"},"list":["k"]}`; string(b) != want {
		t.Errorf("built\n got %s\nwant %s", b, want)
	}
	boom := errors.New("no such token")
	if _, err := Build([]Row{{Path: "auth", Value: `{"token":"${token:x}"}`}}, func(string) (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Errorf("an expansion's error: %v", err)
	}
}

// `undefined` leaves the path out, and makes nothing on the way to it.
func TestUndefinedLeavesThePathOut(t *testing.T) {
	for _, c := range []struct {
		rows []Row
		want string
	}{
		{[]Row{{Path: "a.b", Value: "undefined"}}, `{}`},
		{[]Row{{Path: "l[0]", Value: "undefined"}}, `{}`},
		{[]Row{{Path: "l[2].x", Value: "undefined"}}, `{}`},
		{[]Row{{Path: "a.b.c", Value: "undefined"}, {Path: "n", Value: "1"}}, `{"n":1}`},
		// On a value that has no such path, nothing changes.
		{[]Row{{Path: "a", Value: "1"}, {Path: "a.b", Value: "undefined"}, {Path: "a[0]", Value: "undefined"}}, `{"a":1}`},
		{[]Row{{Path: "a.b", Value: "1"}, {Path: "a.c.d", Value: "undefined"}}, `{"a":{"b":1}}`},
		{[]Row{{Path: "l[0]", Value: "1"}, {Path: "l[3]", Value: "undefined"}, {Path: "l[0].x", Value: "undefined"}}, `{"l":[1]}`},
		// What is there goes, and only that.
		{[]Row{{Path: "a.b", Value: "1"}, {Path: "a.b", Value: "undefined"}}, `{"a":{}}`},
		{[]Row{{Path: "l[1]", Value: "1"}, {Path: "l[0]", Value: "undefined"}}, `{"l":[1]}`},
		{[]Row{{Path: "l[0].x", Value: "1"}, {Path: "l[0].x", Value: "undefined"}}, `{"l":[{}]}`},
		{[]Row{{Path: "a", Value: "null"}, {Path: "a.b", Value: "undefined"}}, `{"a":null}`},
	} {
		got, err := Build(c.rows, nil)
		if err != nil {
			t.Errorf("%v: %v", c.rows, err)
			continue
		}
		if b, _ := json.Marshal(got); string(b) != c.want {
			t.Errorf("%v: %s, want %s", c.rows, b, c.want)
		}
	}
}
