package gcpbigquery

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"

	"cloud.google.com/go/bigquery"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

// A row's values keep their types (ADR 0014): an INT64 is an integer, a
// FLOAT64 a decimal, a whole one too, and a NUMERIC or a BIGNUMERIC the
// number it is, all of its digits.
func TestValuesKeepTheirTypes(t *testing.T) {
	price, _ := new(big.Rat).SetString("1.35")
	whole, _ := new(big.Rat).SetString("2")
	big38, _ := new(big.Rat).SetString("0.12345678901234567890123456789012345678")
	b, err := json.Marshal(plain(map[string]bigquery.Value{
		"lines": int64(2), "billed": 12.0, "rate": 0.5, "price": price, "minimum": whole, "share": big38,
		"ratio": math.NaN(), "route": map[string]bigquery.Value{"km": 140.0},
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"billed":12.0,"lines":2,"minimum":2,"price":1.35,"rate":0.5,"ratio":"NaN","route":{"km":140.0},"share":0.12345678901234567890123456789012345678}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	rs := cloudstep.Rows{{Key: "billed", Value: "12.0"}, {Key: "lines", Value: "2"}, {Key: "price", Value: "1.35"}, {Key: "route.km", Value: "140.0"}}
	if ok, err := rs.Match(string(b)); !ok || err != nil {
		t.Errorf("the row should match: %v", err)
	}
	if ok, _ := (cloudstep.Rows{{Key: "billed", Value: "12"}}).Match(string(b)); ok {
		t.Error("a whole FLOAT64 should not match an integer")
	}
}

// A row has every column of its table's schema, NULL or not: `undefined` is
// no value of a row's, and fails at once with the rule's message, as axx
// validate reports it, rather than as a query of a column the table lacks.
func TestARowHasEveryColumn(t *testing.T) {
	h := cloudtest.New(t, gcpcore.Pack(), Pack())
	for _, step := range []string{
		"the billing.invoice_lines bigquery table has a row where:",
		"the billing.invoice_lines bigquery table has 1 row where:",
	} {
		_ = h.Fails(step, "no_such_column: undefined (no such property) is not a value here", [][]string{{"no_such_column", "undefined"}})
		_ = h.Fails(step, "route.to: undefined (no such property) is not a value here", [][]string{{"route.to", "undefined"}})
	}
	checks := 0
	for _, s := range Pack().Manifest().Steps {
		if s.Table == nil {
			continue
		}
		checks++
		if _, err := core.ParseValue("undefined", false, s.Table.Undefined); err == nil {
			t.Errorf("%s takes undefined, so axx validate would not report it", s.ID)
		}
	}
	if checks != 2 {
		t.Errorf("%d steps have a table, not 2", checks)
	}
}

func TestColumns(t *testing.T) {
	rows := func(keys ...string) cloudstep.Rows {
		var rs cloudstep.Rows
		for _, k := range keys {
			rs = append(rs, core.Pair{Key: k, Value: "x"})
		}
		return rs
	}
	for _, c := range []struct {
		keys []string
		want string
	}{
		{[]string{"invoice", "status"}, "invoice, status"},
		{[]string{"route.to", "route.from", "$.billed"}, "route, billed"},
		{[]string{"from", "to"}, "`from`, `to`"},
		{[]string{"$['route']['to']"}, "route"},
		{[]string{"lines[0].parcel"}, "lines"},
		{[]string{"invoice", "$..parcel"}, "*"},
		{[]string{"weird col"}, "*"},
	} {
		if got := columns(rows(c.keys...)); got != c.want {
			t.Errorf("%v: %s, want %s", c.keys, got, c.want)
		}
	}
}
