package avrojson

import (
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/iskorotkov/avro/v2"
)

func mustSchema(t *testing.T, text string) avro.Schema {
	t.Helper()
	s, err := avro.ParseWithCache(text, "", &avro.SchemaCache{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const orderSchema = `{"type":"record","name":"Order","namespace":"shop","fields":[
  {"name":"id","type":"string"},
  {"name":"lines","type":{"type":"array","items":{"type":"record","name":"Line","fields":[
    {"name":"sku","type":"string"},{"name":"qty","type":"int"}]}}},
  {"name":"note","type":["null","string"]},
  {"name":"attrs","type":{"type":"map","values":"long"}},
  {"name":"status","type":{"type":"enum","name":"Status","symbols":["NEW","DONE"]}},
  {"name":"extra","type":["null","Line",{"type":"array","items":"int"}]}
]}`

func TestDecodeErrorsNameThePath(t *testing.T) {
	s := mustSchema(t, orderSchema)
	const ok = `"note":null,"attrs":{},"status":"NEW","extra":null`
	cases := []struct {
		json, path, msg string
	}{
		{`{"lines":[],` + ok + `}`, "$.id", `missing field "id" (string) of record shop.Order`},
		{`{"id":"o1","lines":[{"sku":"a","qty":1},{"sku":"b","qty":"2"}],` + ok + `}`, "$.lines[1].qty", `expected int, got string "2"`},
		{`{"id":"o1","lines":[],"note":"x","attrs":{},"status":"NEW","extra":null}`, "$.note", `expected a union value`},
		{`{"id":"o1","lines":[],"note":{"int":1},"attrs":{},"status":"NEW","extra":null}`, "$.note", `unknown union branch "int" (branches: null, string)`},
		{`{"id":"o1","lines":[],"note":null,"attrs":{"a b":"x"},"status":"NEW","extra":null}`, "$.attrs['a b']", `expected long, got string "x"`},
		{`{"id":"o1","lines":[],"note":null,"attrs":{},"status":"GONE","extra":null}`, "$.status", `unknown symbol "GONE" for enum shop.Status (symbols: NEW, DONE)`},
		{`{"id":"o1","lines":[],"note":null,"attrs":{},"status":"NEW","extra":{"Line":{"sku":"a","qty":1}}}`, "$.extra", `named types are labelled with their full name, e.g. "shop.Line"`},
		{`{"id":"o1","lines":[],"note":null,"attrs":{},"status":"NEW","extra":{"shop.Line":{"sku":"a","qty":1.5}}}`, "$.extra.qty", `expected int, got number 1.5`},
		{`{"id":"o1","lines":{}}`, "$.lines", `expected array, got an object`},
		{`{"id": 01}`, "$", `invalid JSON`},
		{``, "$", `empty input`},
		{`[1,2]`, "$", `expected record shop.Order, got an array`},
	}
	for _, c := range cases {
		_, err := Decode(s, []byte(c.json), Options{})
		var de *Error
		if !errors.As(err, &de) {
			t.Errorf("%s: want *Error, got %v", c.json, err)
			continue
		}
		if de.Path != c.path || !strings.Contains(de.Msg, c.msg) {
			t.Errorf("%s:\n got  %s: %s\n want %s: ...%s...", c.json, de.Path, de.Msg, c.path, c.msg)
		}
	}
}

func TestDecodeLenientUnions(t *testing.T) {
	s := mustSchema(t, `{"type":"record","name":"R","fields":[
	  {"name":"a","type":["null","string"]},
	  {"name":"b","type":["null","int","long"]},
	  {"name":"c","type":["null",{"type":"record","name":"P","fields":[{"name":"x","type":"int"}]},{"type":"map","values":"string"}]},
	  {"name":"d","type":["null",{"type":"enum","name":"E","symbols":["A"]},"string"]}]}`)
	cases := []struct {
		field, json string
		want        any
		errSub      string
	}{
		{"a", `"x"`, map[string]any{"string": "x"}, ""},
		{"a", `{"string":"x"}`, map[string]any{"string": "x"}, ""},
		{"a", `null`, nil, ""},
		{"a", `5`, nil, "fits no branch"},
		{"b", `5`, nil, "fits several branches of the union (int, long)"},
		{"b", `{"long":5}`, map[string]any{"long": int64(5)}, ""},
		{"c", `{"x":1}`, map[string]any{"P": map[string]any{"x": 1}}, ""}, // the map branch needs string values
		{"c", `{"k":"v"}`, map[string]any{"map": map[string]any{"k": "v"}}, ""},
		{"d", `"A"`, nil, "fits several branches of the union (E, string)"},
		{"d", `"B"`, map[string]any{"string": "B"}, ""},
	}
	for _, c := range cases {
		doc := map[string]string{"a": "null", "b": "null", "c": "null", "d": "null"}
		doc[c.field] = c.json
		text := `{"a":` + doc["a"] + `,"b":` + doc["b"] + `,"c":` + doc["c"] + `,"d":` + doc["d"] + `}`
		v, err := Decode(s, []byte(text), Options{LenientUnions: true})
		if c.errSub != "" {
			if err == nil || !strings.Contains(err.Error(), c.errSub) {
				t.Errorf("%s=%s: want error containing %q, got %v", c.field, c.json, c.errSub, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s=%s: %v", c.field, c.json, err)
			continue
		}
		got := v.(map[string]any)[c.field]
		if Render(nil, got) != Render(nil, c.want) {
			t.Errorf("%s=%s: got %#v, want %#v", c.field, c.json, got, c.want)
		}
		if _, err := Marshal(s, v); err != nil {
			t.Errorf("%s=%s: marshal: %v", c.field, c.json, err)
		}
	}
	if _, err := Decode(s, []byte(`{"a":"x","b":null,"c":null,"d":null}`), Options{}); err == nil {
		t.Error("strict decoding must reject a bare union value")
	}
}

func TestDecodeLogicalTypesMarshal(t *testing.T) {
	s := mustSchema(t, `{"type":"record","name":"L","fields":[
	  {"name":"ts","type":{"type":"long","logicalType":"timestamp-millis"}},
	  {"name":"lts","type":{"type":"long","logicalType":"local-timestamp-micros"}},
	  {"name":"d","type":{"type":"int","logicalType":"date"}},
	  {"name":"tm","type":{"type":"int","logicalType":"time-millis"}},
	  {"name":"tu","type":{"type":"long","logicalType":"time-micros"}},
	  {"name":"dec","type":{"type":"bytes","logicalType":"decimal","precision":6,"scale":2}},
	  {"name":"u","type":["null",{"type":"long","logicalType":"timestamp-micros"}]}]}`)
	v, err := Decode(s, []byte(`{"ts":1700000000000,"lts":5,"d":19000,"tm":1000,"tu":1500,"dec":"\u0004\u00d2","u":{"long":7}}`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["tu"] != 1500*time.Microsecond {
		t.Errorf("time-micros must decode to a time.Duration for the avro library, got %#v", m["tu"])
	}
	if got := m["u"].(map[string]any)["long.timestamp-micros"]; got != int64(7) {
		t.Errorf("union branch key must be the avro library's type name, got %#v", m["u"])
	}
	b, err := Marshal(s, v)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeBinary(s, b)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ts": 1700000000000, "lts": 5, "d": 19000, "tm": 1000, "tu": 1500, "dec": "\u0004Ò", "u": 7}`
	if got := Render(s, back); got != want {
		t.Errorf("round trip:\n got  %s\n want %s", got, want)
	}
	// the avro library's converted values render as the raw numbers Java prints.
	var generic any
	if err := avro.Unmarshal(s, b, &generic); err != nil {
		t.Fatal(err)
	}
	if got := Render(s, generic); got != want {
		t.Errorf("the avro library generic values:\n got  %s\n want %s", got, want)
	}
}

func TestRenderGoValues(t *testing.T) {
	fx := mustSchema(t, `{"type":"fixed","name":"F","size":4,"logicalType":"decimal","precision":8,"scale":2}`)
	dur := mustSchema(t, `{"type":"fixed","name":"D","size":12,"logicalType":"duration"}`)
	dec := mustSchema(t, `{"type":"bytes","logicalType":"decimal","precision":8,"scale":2}`)
	date := mustSchema(t, `{"type":"int","logicalType":"date"}`)
	cases := []struct {
		s    avro.Schema
		v    any
		want string
	}{
		{fx, big.NewRat(-1, 100), "[-1, -1, -1, -1]"},
		{fx, big.NewRat(255, 100), "[0, 0, 0, -1]"},
		{dec, big.NewRat(12345, 100), `"09"`},
		{dec, big.NewRat(-128, 100), `"\u0080"`},
		{dur, avro.LogicalDuration{Months: 1, Days: 2, Milliseconds: 3}, "[1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0]"},
		{date, time.Date(1969, 12, 31, 12, 0, 0, 0, time.UTC), "-1"},
		{nil, map[string]any{"s": "a\"b\\\u2028\u0007\u00e9", "n": nil, "f": float32(0.1), "d": 1e21, "b": []byte{0, 255}}, `{"b": "\u0000ÿ", "s": "a\"b\\\u2028\u0007é", "d": 1.0E21, "f": 0.1, "n": null}`},
	}
	for _, c := range cases {
		if got := Render(c.s, c.v); got != c.want {
			t.Errorf("Render(%v, %#v)\n got  %s\n want %s", c.s, c.v, got, c.want)
		}
	}
}

func TestJavaHashMapOrderCollisions(t *testing.T) {
	// "Aa" and "BB" share a String and a Utf8 hash: insertion order decides.
	s := mustSchema(t, `{"type":"map","values":"int"}`)
	for _, c := range []struct {
		m    OrderedMap
		want string
	}{
		{OrderedMap{{"BB", 1}, {"Aa", 2}}, `{"BB": 1, "Aa": 2}`},
		{OrderedMap{{"Aa", 1}, {"BB", 2}}, `{"Aa": 1, "BB": 2}`},
		{OrderedMap{{"a", 1}, {"a", 2}}, `{"a": 2}`},
	} {
		if got := Render(s, c.m); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
	strKeys := mustSchema(t, `{"type":"map","values":"int","avro.java.string":"String"}`)
	// With String keys "é" (233) hashes to bucket 9 and "a" (97) to 1.
	if got := Render(strKeys, OrderedMap{{"é", 1}, {"a", 2}}); got != `{"a": 2, "é": 1}` {
		t.Errorf("String keys: %s", got)
	}
}

func TestDecodeBinaryErrors(t *testing.T) {
	s := mustSchema(t, orderSchema)
	for _, data := range [][]byte{{}, {0x04, 'o'}, {0x02, 'o', 0x01}} {
		if _, err := DecodeBinary(s, data); err == nil {
			t.Errorf("% x: want error", data)
		}
	}
}
