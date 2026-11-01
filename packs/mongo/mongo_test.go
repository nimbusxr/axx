package mongo

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

func TestParseSeedKeepsOrderAndTypes(t *testing.T) {
	d, err := parseSeed([]byte(`{
	  "telemetry": [{"_id": {"$oid": "65a1b2c3d4e5f60718293a4b"}, "mission": "m1", "speed": 7.5, "count": 3, "big": 3000000000, "ok": true, "tags": ["a"], "nested": {"x": null}}],
	  "empty": [],
	  "another": [{"a": 1}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 3 || d[0].Key != "telemetry" || d[1].Key != "empty" || d[2].Key != "another" {
		t.Fatalf("collection order: %v", d)
	}
	doc := d[0].Value.(bson.A)[0].(bson.D)
	want := map[string]any{"mission": "m1", "speed": 7.5, "count": int32(3), "big": int64(3000000000), "ok": true}
	got := map[string]any{}
	for _, e := range doc {
		got[e.Key] = e.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v (%T), want %#v (%T)", k, got[k], got[k], v, v)
		}
	}
	if _, ok := got["_id"].(bson.ObjectID); !ok {
		t.Errorf("extended JSON $oid should become an ObjectID, got %T", got["_id"])
	}
}

func TestParseSeedErrors(t *testing.T) {
	if _, err := parseSeed([]byte(`[1,2]`)); err == nil {
		t.Error("top-level array must be rejected")
	}
	if _, err := parseSeed([]byte(`{"a": `)); err == nil {
		t.Error("truncated JSON must be rejected")
	}
}

// A filter's value means what it says (ADR 0014).
func TestTypedValue(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("65a1b2c3d4e5f60718293a4b")
	cases := []struct {
		in   string
		want any
	}{
		{"m1", "m1"},
		{"3", int32(3)},
		{"3000000000", int64(3000000000)},
		{"3L", int64(3)},
		{"7.5", 7.5},
		{"2.0", 2.0},
		{"true", true},
		{"null", nil},
		{`"3"`, "3"},
		{`"true"`, "true"},
		{`{"$oid": "65a1b2c3d4e5f60718293a4b"}`, oid},
		{"not json {", "not json {"},
	}
	for _, c := range cases {
		got, err := typedValue(c.in, false)
		if err != nil || got != c.want {
			t.Errorf("%s: %#v (%T) %v, want %#v (%T)", c.in, got, got, err, c.want, c.want)
		}
	}
	for _, c := range []struct {
		in    string
		empty bool
		want  string
	}{
		{"", true, `the cell is empty: write "" for the empty string, or null for null`},
		{"01067", false, `01067 could be the number 1067 or the text 01067`},
		{"undefined", false, "undefined (no such property) is not a value here"},
		{`{"$oid": 5}`, false, `{"$oid": 5} is not MongoDB's Extended JSON`},
	} {
		if _, err := typedValue(c.in, c.empty); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want an error with %q", c.in, err, c.want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("65a1b2c3d4e5f60718293a4b")
	price, _ := bson.ParseDecimal128("1.50")
	whole, _ := bson.ParseDecimal128("6")
	nan, _ := bson.ParseDecimal128("NaN")
	doc := bson.D{
		{Key: "_id", Value: oid},
		{Key: "z", Value: int64(1)},
		{Key: "a", Value: bson.A{int32(2), 2.5, nil, "x"}},
		{Key: "at", Value: bson.NewDateTimeFromTime(time.Date(2026, 9, 24, 10, 0, 0, 5e6, time.UTC))},
		{Key: "nested", Value: bson.D{{Key: "ok", Value: true}}},
		{Key: "weightKg", Value: 6.0},
		{Key: "doubles", Value: bson.A{math.Copysign(0, -1), 1e21, math.Inf(1), math.NaN()}},
		{Key: "decimals", Value: bson.A{price, whole, nan}},
	}
	var buf bytes.Buffer
	writeJSON(&buf, doc)
	want := `{"_id":"65a1b2c3d4e5f60718293a4b","z":1,"a":[2,2.5,null,"x"],"at":"2026-09-24T10:00:00.005Z","nested":{"ok":true},` +
		`"weightKg":6.0,"doubles":[-0.0,1e+21,"Infinity","NaN"],"decimals":[1.50,6,"NaN"]}`
	if buf.String() != want {
		t.Fatalf("got  %s\nwant %s", buf.String(), want)
	}
}

// A document's numbers keep their types: a whole double is a decimal, so
// `6` does not match it and the failure says what to write.
func TestDocumentNumbersKeepTheirTypes(t *testing.T) {
	price, _ := bson.ParseDecimal128("1.50")
	var buf bytes.Buffer
	writeJSON(&buf, bson.D{{Key: "count", Value: int32(6)}, {Key: "weightKg", Value: 6.0}, {Key: "price", Value: price}})
	ok := &core.Table{Rows: [][]string{{"count", "6"}, {"weightKg", "6.0"}, {"price", "1.5"}}}
	if err := jsonassert.Properties(buf.String(), ok, false); err != nil {
		t.Errorf("%s: %v", buf.String(), err)
	}
	err := jsonassert.Properties(buf.String(), &core.Table{Rows: [][]string{{"weightKg", "6"}}}, false)
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "weightKg is the decimal 6.0: write 6.0") {
		t.Errorf("a whole double should not match an integer: %v", err)
	}
}
