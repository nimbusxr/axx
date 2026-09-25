package jsonx_test

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

type jsonSmartOracle struct {
	Cases []struct {
		Input           string                `json:"input"`
		Error           *oracletest.JavaError `json:"error"`
		Typed           string                `json:"typed"`
		JSONString      *string               `json:"jsonString"`
		JSONStringError *oracletest.JavaError `json:"jsonStringError"`
		ValueOf         string                `json:"valueOf"`
		NoCompress      string                `json:"noCompress"`
	} `json:"cases"`
}

func TestJSONSmartOracle(t *testing.T) {
	var o jsonSmartOracle
	oracletest.Load(t, "jsonsmart", &o)
	for _, c := range o.Cases {
		key := "parse|" + c.Input
		v, err := jsonx.Parse(c.Input)
		if c.Error != nil || err != nil {
			got := oracletest.ErrorOf(err)
			oracletest.Check(t, "jsonsmart", key, oracletest.SameError(got, c.Error), func() string {
				return fmt.Sprintf("want error %v, got %v (value %s)", c.Error, got, oracletest.Repr(v))
			})
			continue
		}
		typed := oracletest.Repr(v)
		oracletest.Check(t, "jsonsmart", key, typed == c.Typed, func() string {
			return fmt.Sprintf("typed: want %s\n got %s", c.Typed, typed)
		})
		s, err := jsonx.Marshal(v)
		if c.JSONStringError != nil || err != nil {
			got := oracletest.ErrorOf(err)
			oracletest.Check(t, "jsonsmart", "marshal|"+c.Input, oracletest.SameError(got, c.JSONStringError), func() string {
				return fmt.Sprintf("want error %v, got %v (%q)", c.JSONStringError, got, s)
			})
		} else {
			oracletest.Check(t, "jsonsmart", "marshal|"+c.Input, s == *c.JSONString, func() string {
				return fmt.Sprintf("want %s\n got %s", *c.JSONString, s)
			})
		}
		vo := javafmt.ValueOf(v)
		oracletest.Check(t, "jsonsmart", "valueOf|"+c.Input, vo == c.ValueOf, func() string {
			return fmt.Sprintf("want %s\n got %s", c.ValueOf, vo)
		})
		nc := jsonx.MarshalValue(v)
		oracletest.Check(t, "jsonsmart", "noCompress|"+c.Input, nc == c.NoCompress, func() string {
			return fmt.Sprintf("want %s\n got %s", c.NoCompress, nc)
		})
	}
	oracletest.CheckAllDeviationsSeen(t, "jsonsmart")
}

type jsonPathCase struct {
	Doc           string                `json:"doc"`
	Path          string                `json:"path"`
	Op            string                `json:"op"`
	Definite      *bool                 `json:"definite"`
	Normalized    *string               `json:"normalized"`
	CompileError  *oracletest.JavaError `json:"compileError"`
	Result        *string               `json:"result"`
	Typed         *string               `json:"typed"`
	Error         *oracletest.JavaError `json:"error"`
	HasNoJSONPath *bool                 `json:"hasNoJsonPath"`
	HasNoError    *oracletest.JavaError `json:"hasNoJsonPathError"`
}

type jsonPathOracle struct {
	Docs  map[string]string `json:"docs"`
	Cases []jsonPathCase    `json:"cases"`
}

func TestJSONPathOracle(t *testing.T) {
	var o jsonPathOracle
	oracletest.Load(t, "jsonpath", &o)
	for _, c := range o.Cases {
		checkJSONPathCase(t, o.Docs[c.Doc], c)
	}
	oracletest.CheckAllDeviationsSeen(t, "jsonpath")
}

func checkJSONPathCase(t *testing.T, docText string, c jsonPathCase) {
	t.Helper()
	key := c.Doc + "|" + c.Op + "|" + c.Path

	// Compilation: definiteness and normalized form.
	if c.Path != "" {
		p, err := jsonx.Compile(c.Path)
		switch {
		case c.CompileError != nil || err != nil:
			got := oracletest.ErrorOf(err)
			oracletest.Check(t, "jsonpath", "compile|"+c.Path, oracletest.SameError(got, c.CompileError), func() string {
				return fmt.Sprintf("want %v, got %v", c.CompileError, got)
			})
		default:
			ok := p.Definite() == *c.Definite && p.String() == *c.Normalized
			oracletest.Check(t, "jsonpath", "compile|"+c.Path, ok, func() string {
				return fmt.Sprintf("want definite=%v normalized=%s, got %v %s", *c.Definite, *c.Normalized, p.Definite(), p.String())
			})
		}
	}

	doc, err := jsonx.Parse(docText)
	if err != nil {
		t.Fatalf("parse doc %s: %v", c.Doc, err)
	}
	if c.Op == "read" {
		v, _, err := jsonx.Read(doc, c.Path)
		checkResult(t, key, c, err, func() string { return oracletest.Repr(v) }, "")
		if c.HasNoJSONPath != nil || c.HasNoError != nil {
			has, err := jsonx.HasNoJSONPath(docText, c.Path)
			if c.HasNoError != nil || err != nil {
				got := oracletest.ErrorOf(err)
				oracletest.Check(t, "jsonpath", "hasNo|"+key, oracletest.SameError(got, c.HasNoError), func() string {
					return fmt.Sprintf("want %v, got %v", c.HasNoError, got)
				})
			} else {
				oracletest.Check(t, "jsonpath", "hasNo|"+key, has == *c.HasNoJSONPath, func() string {
					return fmt.Sprintf("want %v, got %v", *c.HasNoJSONPath, has)
				})
			}
		}
		return
	}
	var opErr error
	switch {
	case strings.HasPrefix(c.Op, "set "):
		opErr = jsonx.Set(doc, c.Path, specValue(t, strings.TrimPrefix(c.Op, "set ")))
	case c.Op == "delete":
		opErr = jsonx.Delete(doc, c.Path)
	case strings.HasPrefix(c.Op, "put "):
		parts := strings.SplitN(c.Op, " ", 3)
		opErr = jsonx.Put(doc, c.Path, parts[1], specValue(t, parts[2]))
	default:
		t.Fatalf("unknown op %q", c.Op)
	}
	checkResult(t, key, c, opErr, func() string {
		s, err := jsonx.Marshal(doc)
		if err != nil {
			return "marshal error: " + err.Error()
		}
		return s
	}, oracletest.Repr(doc))
}

func checkResult(t *testing.T, key string, c jsonPathCase, err error, result func() string, typed string) {
	t.Helper()
	if c.Error != nil || err != nil {
		got := oracletest.ErrorOf(err)
		oracletest.Check(t, "jsonpath", key, oracletest.SameError(got, c.Error), func() string {
			res := ""
			if err == nil {
				res = result()
			}
			return fmt.Sprintf("want error %v\n got %v %s", c.Error, got, res)
		})
		return
	}
	got := result()
	ok := got == *c.Result && (c.Typed == nil || typed == *c.Typed)
	oracletest.Check(t, "jsonpath", key, ok, func() string {
		msg := fmt.Sprintf("want %s\n got %s", *c.Result, got)
		if c.Typed != nil && typed != *c.Typed {
			msg += fmt.Sprintf("\nwant typed %s\n got typed %s", *c.Typed, typed)
		}
		return msg
	})
}

// specValue decodes the oracle's typed value specs ("I:5", "S:x", "N", ...).
func specValue(t *testing.T, s string) any {
	t.Helper()
	if s == "N" {
		return nil
	}
	typ, v, _ := strings.Cut(s, ":")
	switch typ {
	case "S":
		return v
	case "B":
		return javafmt.ParseBoolean(v)
	case "I":
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		return jsonx.IntegerNumber(int32(n))
	case "L":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return jsonx.LongNumber(n)
	case "BI":
		n, ok := new(big.Int).SetString(v, 10)
		if !ok {
			t.Fatalf("bad big integer %q", v)
		}
		return jsonx.BigIntegerNumber(n)
	case "D":
		f, err := javafmt.ParseDouble(v)
		if err != nil {
			t.Fatal(err)
		}
		return jsonx.DoubleNumber(f)
	case "F":
		f, err := javafmt.ParseFloat(v)
		if err != nil {
			t.Fatal(err)
		}
		return jsonx.FloatNumber(f)
	case "BD":
		d, err := javafmt.ParseBigDecimal(v)
		if err != nil {
			t.Fatal(err)
		}
		return jsonx.BigDecimalNumber(d)
	case "J":
		j, err := jsonx.Parse(v)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	t.Fatalf("bad spec %q", s)
	return nil
}
