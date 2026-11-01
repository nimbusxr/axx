package tablevalue

import (
	"encoding/json"
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
	})
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
	got, err := Apply(base, []Row{{Path: "recipient.postcode", Value: `"04109"`}, {Path: "reference", Value: "undefined"}})
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
		if _, err := Build(c.rows); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: got %v, want %q", c.rows, err, c.want)
		}
	}
}
