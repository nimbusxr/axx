package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

func TestNodeJSON(t *testing.T) {
	cases := map[string]string{
		`{"b": 1.0, "a": [1, true, null, "x"], "big": 12345678901234567890}`:                     `{"b":1.0,"a":[1,true,null,"x"],"big":12345678901234567890}`,
		"name: Falcon\nflight_number: 7\nfuel_ratio: .5\nhex: 0x10\nok: yes\nnone: ~\ninf: .inf": `{"name":"Falcon","flight_number":7,"fuel_ratio":0.5,"hex":16,"ok":"yes","none":null,"inf":".inf"}`,
		"base: &b {x: 1}\ncopy: *b":          `{"base":{"x":1},"copy":{"x":1}}`,
		`"just a string <&>"`:                `just a string <&>`, // a string example is the payload itself
		"when: 2024-01-01T00:00:00Z":         `{"when":"2024-01-01T00:00:00Z"}`,
		`{"html": "<b>&</b>", "u": "é"}`:     `{"html":"<b>&</b>","u":"é"}`,
		"[]":                                 `[]`,
		"- 1\n- -2.5e3":                      `[1,-2.5e3]`,
		"":                                   `null`,
		`{"k": "line\nbreak \"q\""}`:         `{"k":"line\nbreak \"q\""}`,
		"n: null\nb: False\ni: 1_000\nf: 1.": `{"n":null,"b":false,"i":1000,"f":1}`,
	}
	for in, want := range cases {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(in), &n); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		got, err := nodeJSON(&n)
		if err != nil || got != want {
			t.Errorf("nodeJSON(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
}

func TestJSONText(t *testing.T) {
	doc, err := jsonx.Parse(`{"s":"x/y","n":1e10,"d":1.50,"big":12345678901234567890,"l":[1,{"a":null}],"b":false,"nan":NaN}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"s":"x/y","n":1e10,"d":1.50,"big":12345678901234567890,"l":[1,{"a":null}],"b":false,"nan":"NaN"}`
	if got := jsonText(doc); got != want {
		t.Errorf("jsonText = %s\nwant       %s", got, want)
	}
	jackson, err := jvalue.CoerceExpected(`[{"a":1},2.5,"s"]`)
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonText(jackson); got != `[{"a":1},2.5,"s"]` {
		t.Errorf("jsonText(jackson) = %s", got)
	}
	if got := jsonText(jsonx.IntegerNumber(7)); got != "7" {
		t.Errorf("jsonText(Integer) = %s", got)
	}
}

// A scalar in a failure is shown quoted once, and --json carries the value
// itself. Objects stay JSON text, which reporters pretty-print and diff.
func TestPropertyFailureValues(t *testing.T) {
	ex := &Exchange{
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   []byte(`{"message":"Hello, axx","count":2,"meta":{"a":1}}`),
	}
	for _, c := range []struct {
		path, value, expected, actual string
		scalar                        bool
	}{
		{"message", "Hello, world", `"Hello, world"`, `"Hello, axx"`, true},
		{"count", "3", `3`, `2`, true},
		{"meta", `{"a":2}`, `{"a":2}`, `{"a":1}`, false},
	} {
		f, err := propertyIs(ex, c.path, c.value)
		if err != nil || f == nil {
			t.Fatalf("%s: failure %v, err %v", c.path, f, err)
		}
		if got := fmt.Sprint(f.expected); got != c.expected {
			t.Errorf("%s: expected shown as %s, want %s", c.path, got, c.expected)
		}
		if got := fmt.Sprint(f.actual); got != c.actual {
			t.Errorf("%s: actual shown as %s, want %s", c.path, got, c.actual)
		}
		if !c.scalar {
			continue
		}
		if b, err := json.Marshal(f.expected); err != nil || string(b) != c.expected {
			t.Errorf("%s: --json value %s, %v; want %s", c.path, b, err, c.expected)
		}
	}
}

func TestParseSpecRejectsSwagger2(t *testing.T) {
	_, err := parseSpec("/tmp/s.yaml", []byte("swagger: '2.0'\ninfo: {title: t, version: '1'}\npaths: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "OpenAPI 3") {
		t.Fatalf("swagger 2.0: %v", err)
	}
	if _, err := parseSpec("/tmp/s.yaml", []byte("not: [valid")); err == nil {
		t.Fatal("malformed YAML must be rejected")
	}
}

func TestSingleExampleAndPathTemplates(t *testing.T) {
	sp, err := parseSpec("/tmp/s.yaml", []byte(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /items/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: integer}}
    put:
      requestBody:
        content:
          application/json;charset=UTF-8:
            example: {id: 1, tags: [a]}
  /items/search:
    put:
      requestBody:
        content:
          application/json:
            examples:
              only: {value: {q: x}}
  /flags/{on}/v{version}:
    parameters:
      - {name: on, in: path, required: true, schema: {type: boolean}}
      - {name: version, in: path, required: true, schema: {type: number}}
    post:
      requestBody:
        content:
          application/json:
            examples:
              e: {summary: no value}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path, want, err string }{
		{"PUT", "/items/12?x=1", `{"id":1,"tags":["a"]}`, ""},
		{"put", "/items/search", `{"q":"x"}`, ""}, // {id} is an integer, so "search" is the literal path
		{"PUT", "/items/-3", `{"id":1,"tags":["a"]}`, ""},
		{"POST", "/flags/true/v1.5", "", `No value or externalValue found for example with name "e"`},
		{"POST", "/flags/maybe/v1", "", "No path found"},
		{"GET", "/items/1", "", "no GET operation found"},
	} {
		text, _, err := sp.pickExample(c.method, c.path, "application/json", "")
		switch {
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s %s: error %v, want %q", c.method, c.path, err, c.err)
		case c.err == "" && (err != nil || text != c.want):
			t.Errorf("%s %s: %s, %v; want %s", c.method, c.path, text, err, c.want)
		}
	}
}
