package javafmt_test

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

type formatOracle struct {
	Doubles []struct {
		Bits   string `json:"bits"`
		String string `json:"string"`
	} `json:"doubles"`
	Floats []struct {
		Bits   string `json:"bits"`
		String string `json:"string"`
	} `json:"floats"`
	Parse []struct {
		Input      string `json:"input"`
		Int        string `json:"int"`
		Long       string `json:"long"`
		Double     string `json:"double"`
		Float      string `json:"float"`
		Boolean    bool   `json:"boolean"`
		BigInteger string `json:"bigInteger"`
		BigDecimal string `json:"bigDecimal"`
	} `json:"parse"`
	ValueOf []struct {
		Spec   string `json:"spec"`
		Result string `json:"result"`
	} `json:"valueOf"`
	DecimalCompare []struct {
		A       string `json:"a"`
		B       string `json:"b"`
		Compare int    `json:"compare"`
		Equals  bool   `json:"equals"`
	} `json:"decimalCompare"`
}

func TestFormatOracle(t *testing.T) {
	var o formatOracle
	oracletest.Load(t, "javafmt", &o)
	if len(o.Doubles) < 3000 || len(o.Floats) < 900 {
		t.Fatalf("oracle too small: %d doubles, %d floats", len(o.Doubles), len(o.Floats))
	}
	for _, c := range o.Doubles {
		bits, err := strconv.ParseUint(c.Bits, 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		got := javafmt.Double(math.Float64frombits(bits))
		oracletest.Check(t, "javafmt", "double|"+c.Bits, got == c.String, func() string {
			return fmt.Sprintf("want %s, got %s", c.String, got)
		})
	}
	for _, c := range o.Floats {
		bits, err := strconv.ParseUint(c.Bits, 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		got := javafmt.Float(math.Float32frombits(uint32(bits)))
		oracletest.Check(t, "javafmt", "float|"+c.Bits, got == c.String, func() string {
			return fmt.Sprintf("want %s, got %s", c.String, got)
		})
	}
	for _, c := range o.Parse {
		check := func(kind, want, got string) {
			oracletest.Check(t, "javafmt", kind+"|"+c.Input, want == got, func() string {
				return fmt.Sprintf("want %s, got %s", want, got)
			})
		}
		check("int", c.Int, attempt(func() (string, error) {
			v, err := javafmt.ParseInt(c.Input)
			return strconv.FormatInt(int64(v), 10), err
		}))
		check("long", c.Long, attempt(func() (string, error) {
			v, err := javafmt.ParseLong(c.Input)
			return strconv.FormatInt(v, 10), err
		}))
		check("double", c.Double, attempt(func() (string, error) {
			v, err := javafmt.ParseDouble(c.Input)
			return javafmt.Double(v), err
		}))
		check("float", c.Float, attempt(func() (string, error) {
			v, err := javafmt.ParseFloat(c.Input)
			return javafmt.Float(v), err
		}))
		check("boolean", strconv.FormatBool(c.Boolean), strconv.FormatBool(javafmt.ParseBoolean(c.Input)))
		check("bigInteger", c.BigInteger, attempt(func() (string, error) {
			v, err := javafmt.ParseBigInteger(c.Input)
			if err != nil {
				return "", err
			}
			return v.String(), nil
		}))
		check("bigDecimal", c.BigDecimal, attempt(func() (string, error) {
			d, err := javafmt.ParseBigDecimal(c.Input)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s scale=%d int=%d plain=%s", d.String(), d.Scale(), d.IntValue(), d.PlainString()), nil
		}))
	}
	for _, c := range o.ValueOf {
		got := javafmt.ValueOf(valueSpec(t, c.Spec))
		oracletest.Check(t, "javafmt", "valueOf|"+c.Spec, got == c.Result, func() string {
			return fmt.Sprintf("want %s, got %s", c.Result, got)
		})
	}
	for _, c := range o.DecimalCompare {
		a, err := javafmt.ParseBigDecimal(c.A)
		if err != nil {
			t.Fatal(err)
		}
		b, err := javafmt.ParseBigDecimal(c.B)
		if err != nil {
			t.Fatal(err)
		}
		cmp := a.Cmp(b)
		ok := sign(cmp) == sign(c.Compare) && a.Equal(b) == c.Equals
		oracletest.Check(t, "javafmt", "decimal|"+c.A+"|"+c.B, ok, func() string {
			return fmt.Sprintf("want compare %d equals %v, got %d %v", c.Compare, c.Equals, cmp, a.Equal(b))
		})
	}
	oracletest.CheckAllDeviationsSeen(t, "javafmt")
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func attempt(f func() (string, error)) string {
	s, err := f()
	if err != nil {
		return "!NumberFormatException"
	}
	return s
}

// valueSpec decodes the oracle's value specs; "J:" is json-smart, "K:" Jackson.
func valueSpec(t *testing.T, s string) any {
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
		n, _ := strconv.ParseInt(v, 10, 32)
		return int32(n)
	case "L":
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case "BI":
		n, _ := new(big.Int).SetString(v, 10)
		return n
	case "D":
		f, _ := javafmt.ParseDouble(v)
		return f
	case "F":
		f, _ := javafmt.ParseFloat(v)
		return f
	case "BD":
		d, _ := javafmt.ParseBigDecimal(v)
		return d
	case "J":
		j, err := jsonx.Parse(v)
		if err != nil {
			t.Fatal(err)
		}
		return j
	case "K":
		return jacksonLike(t, v)
	}
	t.Fatalf("bad spec %q", s)
	return nil
}

// jacksonLike builds what Jackson's untyped deserialization produces
// (LinkedHashMap and ArrayList) from json-smart's parse of v.
func jacksonLike(t *testing.T, v string) any {
	t.Helper()
	j, err := jsonx.Parse(v)
	if err != nil {
		t.Fatal(err)
	}
	var conv func(any) any
	conv = func(x any) any {
		switch y := x.(type) {
		case *jsonx.Array:
			out := make([]any, y.Len())
			for i, e := range y.Items() {
				out[i] = conv(e)
			}
			return out
		case *jsonx.Object:
			o := jsonx.NewObject()
			for _, k := range y.Keys() {
				e, _ := y.Get(k)
				o.Set(k, conv(e))
			}
			return o
		}
		return x
	}
	return conv(j)
}
