package jvalue_test

import (
	"errors"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

func TestInferRequestValue(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", `"hello"`},
		{`"123"`, `"123"`},
		{"123", "I123"},
		{"+123", "I123"},
		{"3000000000", "L3000000000"},
		{"12.5", "D12.5"},
		{" 12 ", "D12.0"},
		{"NaN", "DNaN"},
		{"TRUE", "true"},
		{"null", `"null"`},
		{`{"a":[1,2.5]}`, `{"a":[I1,D2.5]}`},
		{"{a:'b'}", `{"a":"b"}`},
		{"[1,", `"[1,"`},
	}
	for _, tt := range tests {
		if got := oracletest.Repr(jvalue.InferRequestValue(tt.in)); got != tt.want {
			t.Errorf("InferRequestValue(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestCoerceToExisting(t *testing.T) {
	tests := []struct {
		existing any
		in       string
		want     string
		err      bool
	}{
		{"old", "30", `"30"`, false},
		{true, "yes", "false", false},
		{jsonx.IntegerNumber(1), "30", "I30", false},
		{jsonx.IntegerNumber(1), "3000000000", "L3000000000", false},
		{jsonx.IntegerNumber(1), "abc", "", true},
		{jsonx.LongNumber(1), "5", "L5", false},
		{jsonx.DoubleNumber(1), "20", "D20.0", false},
		{jsonx.NewObject(), `{"a":1}`, `{"a":I1}`, false},
		{jsonx.NewObject(), "", "", true},
		// Java failed with a NullPointerException; axx infers the type.
		{nil, "x", `"x"`, false},
		{nil, "30", "I30", false},
		{nil, `{"a":1}`, `{"a":I1}`, false},
	}
	for _, tt := range tests {
		got, err := jvalue.CoerceToExisting(tt.existing, tt.in)
		if (err != nil) != tt.err || err == nil && oracletest.Repr(got) != tt.want {
			t.Errorf("CoerceToExisting(%s, %q) = %s, %v", oracletest.Repr(tt.existing), tt.in, oracletest.Repr(got), err)
		}
	}
}

func TestCoerceExpected(t *testing.T) {
	tests := map[string]string{
		"5":             "I5",
		"5L":            "L5",
		"3000000000":    "L3000000000",
		"5.0":           "D5.0",
		"1e5":           `"1e5"`,
		`"5"`:           `"5"`,
		"False":         "false",
		`{"a":[1,2.0]}`: `{"a":AL[I1,D2.0]}`,
		"[1,2]":         "AL[I1,I2]",
		"null":          `"null"`,
	}
	for in, want := range tests {
		got, err := jvalue.CoerceExpected(in)
		if err != nil || oracletest.Repr(got) != want {
			t.Errorf("CoerceExpected(%q) = %s, %v; want %s", in, oracletest.Repr(got), err, want)
		}
	}
	var se *jvalue.StepError
	if _, err := jvalue.CoerceExpected("{a:1}"); !errors.As(err, &se) || se.Message != "Failed to parse JSON object" {
		t.Errorf("lenient JSON must fail like Jackson: %v", err)
	}
	if v, err := jvalue.CoerceKafkaExpected("NULL"); v != nil || err != nil {
		t.Errorf("Kafka null = %v, %v", v, err)
	}
}

func TestJavaEquals(t *testing.T) {
	body, _ := jsonx.Parse(`{"i":5,"d":5.0,"m":{"a":[1,2]}}`)
	obj := body.(*jsonx.Object)
	i, _ := obj.Get("i")
	d, _ := obj.Get("d")
	m, _ := obj.Get("m")
	exp, _ := jvalue.CoerceExpected(`{"a":[1,2]}`)
	switch {
	case !jvalue.JavaEquals(i, jsonx.IntegerNumber(5)):
		t.Error("Integer 5 equals Integer 5")
	case jvalue.JavaEquals(i, jsonx.DoubleNumber(5)):
		t.Error("Integer 5 must not equal Double 5.0")
	case jvalue.JavaEquals(d, jsonx.IntegerNumber(5)):
		t.Error("Double 5.0 must not equal Integer 5")
	case !jvalue.JavaEquals(m, exp):
		t.Error("a json-smart map equals the same Jackson map")
	}
}

func TestStringify(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1,"b":1.5,"c":true,"d":null,"e":[1e10,"s"]}`: `{"a":"1","b":"1.5","c":"true","d":null,"e":["1.0E10","s"]}`,
		`5`: `5`,
		``:  `null`,
	} {
		got, err := jvalue.StringifyJSON(in)
		if err != nil || got != want {
			t.Errorf("StringifyJSON(%q) = %s, %v", in, got, err)
		}
	}
	if _, err := jvalue.StringifyJSON(`{'a':1}`); err == nil {
		t.Error("Jackson rejects single quotes")
	}
	if got := jvalue.Stringify(jsonx.DoubleNumber(12345678)); got != "1.2345678E7" {
		t.Errorf("Stringify = %s", got)
	}
}

func TestFormURLEncode(t *testing.T) {
	got, err := jvalue.FormURLEncode(`{"grant_type":"client_credentials","scope":"a b","obj":{"x":[1,2]}}`)
	// Nested objects are sent as JSON (Java used its map notation "{x=[1,2]}").
	want := "grant_type=client_credentials&scope=a+b&obj=%7B%22x%22%3A%5B1%2C2%5D%7D"
	if err != nil || got != want {
		t.Errorf("FormURLEncode = %s, %v", got, err)
	}
	if _, err := jvalue.FormURLEncode(`[1]`); err == nil {
		t.Error("an array payload cannot be form encoded")
	}
}

func TestRequestProperty(t *testing.T) {
	doc, _ := jsonx.Parse(`{"name":"old","age":25,"tags":["a"]}`)
	steps := []struct{ path, value string }{
		{"name", "new"},
		{"age", "30"},
		{"age", `"thirty"`},
		{"tags", `["b","c"]`},
		{"address.city", "LA"},
		{"added", "1.5"},
	}
	for _, s := range steps {
		err := jvalue.SetRequestProperty(doc, s.path, s.value)
		if s.path == "address.city" {
			if !errors.Is(err, jsonx.ErrPathNotFound) {
				t.Errorf("a missing parent fails: %v", err)
			}
			continue
		}
		if err != nil {
			t.Errorf("SetRequestProperty(%s, %s): %v", s.path, s.value, err)
		}
	}
	got, _ := jsonx.Marshal(doc)
	if want := `{"name":"new","age":"thirty","tags":["b","c"],"added":1.5}`; got != want {
		t.Errorf("payload = %s, want %s", got, want)
	}
	var se *jvalue.StepError
	err := jvalue.SetRequestProperty(doc, "added", "abc")
	if !errors.As(err, &se) || se.Message != `Invalid value "abc" for property "added": For input string: "abc"` {
		t.Errorf("invalid number: %v", err)
	}
	if err := jvalue.ApplyRequestTableRow(doc, "name", "undefined"); err != nil {
		t.Error(err)
	}
	if err := jvalue.ApplyRequestTableRow(doc, "missing", "null"); !errors.As(err, &se) ||
		se.Message != "Property not found in payload: null" {
		t.Errorf("null on a missing property: %v", err)
	}
}

func TestResponseProperty(t *testing.T) {
	body := `{"name":"John","count":42,"price":19.99,"tags":["a","b"],"none":null}`
	tests := []struct {
		name string
		fn   func() (bool, error)
		want bool
	}{
		{"string", func() (bool, error) { return jvalue.ResponsePropertyIs(body, "name", "John") }, true},
		{"int", func() (bool, error) { return jvalue.ResponsePropertyIs(body, "count", "42") }, true},
		{"int vs double", func() (bool, error) { return jvalue.ResponsePropertyIs(body, "count", "42.0") }, false},
		{"list", func() (bool, error) { return jvalue.ResponsePropertyIs(body, "tags", `["a","b"]`) }, true},
		{"null", func() (bool, error) { return jvalue.ResponsePropertyIsNull(body, "none") }, true},
		{"undefined", func() (bool, error) { return jvalue.ResponsePropertyIsUndefined(body, "missing") }, true},
		{"matches", func() (bool, error) { return jvalue.ResponsePropertyMatches(body, "name", `\w+`) }, true},
		{"matches number", func() (bool, error) { return jvalue.ResponsePropertyMatches(body, "count", `\d+`) }, false},
		{"table null", func() (bool, error) { return jvalue.ResponseTableRow(body, "none", "NULL") }, true},
		{"kafka null", func() (bool, error) { return jvalue.KafkaPropertyMatches(body, "none", "null") }, true},
		{"postgres text", func() (bool, error) { return jvalue.PostgresPropertyIs(body, "count", "42") }, true},
		{"postgres match", func() (bool, error) { return jvalue.PostgresPropertyMatches(body, "price", `\d+\.\d+`) }, true},
	}
	for _, tt := range tests {
		got, err := tt.fn()
		if err != nil || got != tt.want {
			t.Errorf("%s: %v, %v", tt.name, got, err)
		}
	}
}
