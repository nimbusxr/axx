package jyaml_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

type yamlOracle struct {
	Strings []struct {
		Input    string            `json:"input"`
		Factory  map[string]string `json:"factory"`
		Manifest map[string]string `json:"manifest"`
		Default  map[string]string `json:"default"`
	} `json:"strings"`
	Documents []struct {
		Input    json.RawMessage `json:"input"`
		Factory  string          `json:"factory"`
		Manifest string          `json:"manifest"`
		Default  string          `json:"default"`
	} `json:"documents"`
	Parse []struct {
		Input  string          `json:"input"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	} `json:"parse"`
}

// Feature sets of the oracle.
var writers = map[string]jyaml.Options{
	"factory":  {MinimizeQuotes: true, QuoteNumericStrings: true},
	"manifest": {MinimizeQuotes: true},
	"default":  {DocumentStart: true},
}

func object(kv ...any) *jsonx.Object {
	o := jsonx.NewObject()
	for i := 0; i < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func TestWriteOracle(t *testing.T) {
	var o yamlOracle
	oracletest.Load(t, "jacksonyaml", &o)
	if len(o.Strings) < 200 || len(o.Documents) < 10 {
		t.Fatalf("oracle too small: %d strings, %d documents", len(o.Strings), len(o.Documents))
	}
	for _, c := range o.Strings {
		contexts := map[string]any{
			"value": object("k", c.Input),
			"key":   object(c.Input, "v"),
			"item":  object("list", []any{c.Input}),
			"deep":  object("a", object("b", []any{object("c", c.Input)})),
		}
		for set, want := range map[string]map[string]string{"factory": c.Factory, "manifest": c.Manifest, "default": c.Default} {
			for ctx, doc := range contexts {
				got, err := jyaml.Marshal(doc, writers[set])
				if err != nil {
					t.Fatal(err)
				}
				key := "string|" + set + "|" + ctx + "|" + c.Input
				oracletest.Check(t, "jacksonyaml", key, string(got) == want[ctx], func() string {
					return fmt.Sprintf("want:\n%s\ngot:\n%s", want[ctx], got)
				})
			}
		}
	}
	for i, c := range o.Documents {
		v := typed(t, c.Input)
		for set, want := range map[string]string{"factory": c.Factory, "manifest": c.Manifest, "default": c.Default} {
			got, err := jyaml.Marshal(v, writers[set])
			if err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprintf("document|%s|%d", set, i)
			oracletest.Check(t, "jacksonyaml", key, string(got) == want, func() string {
				return fmt.Sprintf("want:\n%s\ngot:\n%s", want, got)
			})
		}
	}
}

func TestParseOracle(t *testing.T) {
	var o yamlOracle
	oracletest.Load(t, "jacksonyaml", &o)
	for _, c := range o.Parse {
		got, err := jyaml.Unmarshal([]byte(c.Input))
		key := "parse|" + c.Input
		if c.Error != "" {
			oracletest.Check(t, "jacksonyaml", key, err != nil, func() string {
				return fmt.Sprintf("want error %s, got %s", c.Error, oracletest.Repr(got))
			})
			continue
		}
		if err != nil {
			oracletest.Check(t, "jacksonyaml", key, false, func() string { return fmt.Sprintf("unexpected error: %v", err) })
			continue
		}
		want := typed(t, c.Result)
		oracletest.Check(t, "jacksonyaml", key, repr(got) == repr(want), func() string {
			return fmt.Sprintf("want %s, got %s", repr(want), repr(got))
		})
	}
	oracletest.CheckAllDeviationsSeen(t, "jacksonyaml")
}

// repr extends oracletest.Repr with byte arrays.
func repr(v any) string {
	switch x := v.(type) {
	case []byte:
		return "bytes:" + base64.StdEncoding.EncodeToString(x)
	case *jsonx.Object:
		s := "{"
		for i, k := range x.Keys() {
			if i > 0 {
				s += ","
			}
			e, _ := x.Get(k)
			s += oracletest.Quote(k) + ":" + repr(e)
		}
		return s + "}"
	case []any:
		s := "["
		for i, e := range x {
			if i > 0 {
				s += ","
			}
			s += repr(e)
		}
		return s + "]"
	}
	return oracletest.Repr(v)
}

// typed decodes the oracle's typed value notation.
func typed(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var n any
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	return fromTyped(t, n)
}

func fromTyped(t *testing.T, n any) any {
	t.Helper()
	if n == nil {
		return nil
	}
	m := n.(map[string]any)
	for k, v := range m {
		switch k {
		case "s":
			return v.(string)
		case "i":
			i, _ := strconv.ParseInt(v.(string), 10, 32)
			return jsonx.IntegerNumber(int32(i))
		case "l":
			i, _ := strconv.ParseInt(v.(string), 10, 64)
			return jsonx.LongNumber(i)
		case "b":
			b, _ := new(big.Int).SetString(v.(string), 10)
			return jsonx.BigIntegerNumber(b)
		case "d":
			f, err := javafmt.ParseDouble(v.(string))
			if err != nil {
				t.Fatal(err)
			}
			return jsonx.DoubleNumber(f)
		case "z":
			return v.(bool)
		case "x":
			b, _ := base64.StdEncoding.DecodeString(v.(string))
			return b
		case "m":
			o := jsonx.NewObject()
			for _, e := range v.([]any) {
				pair := e.([]any)
				o.Set(pair[0].(string), fromTyped(t, pair[1]))
			}
			return o
		case "a":
			out := []any{}
			for _, e := range v.([]any) {
				out = append(out, fromTyped(t, e))
			}
			return out
		}
	}
	t.Fatalf("bad typed value %v", n)
	return nil
}
