package detmath

import (
	"math"
	"testing"
)

// ulps is how many floats apart a and b are.
func ulps(a, b float64) uint64 {
	if a == b {
		return 0
	}
	ia, ib := int64(math.Float64bits(a)), int64(math.Float64bits(b))
	if (ia < 0) != (ib < 0) {
		return math.MaxUint64
	}
	if ia > ib {
		return uint64(ia - ib)
	}
	return uint64(ib - ia)
}

// The functions are within a few ulps of math's, over the ranges stubs use
// them in, and agree with it on special values.
func TestTheFunctionsAreMathsWithinAFewUlps(t *testing.T) {
	for _, c := range []struct {
		name     string
		f, want  func(float64) float64
		from, to float64
		max      uint64
	}{
		{"Exp", Exp, math.Exp, -700, 700, 2},
		{"Exp near 0", Exp, math.Exp, -1, 1, 2},
		{"Log", Log, math.Log, 1e-300, 1e300, 2},
		{"Log near 1", Log, math.Log, 0.5, 2, 2},
		{"Log10", Log10, math.Log10, 1e-30, 1e30, 3},
		{"Sin", Sin, math.Sin, -1e5, 1e5, 2},
		{"Sin of a turn", Sin, math.Sin, -2 * math.Pi, 2 * math.Pi, 2},
		{"Cos", Cos, math.Cos, -1e5, 1e5, 2},
		{"Cos of a turn", Cos, math.Cos, -2 * math.Pi, 2 * math.Pi, 2},
	} {
		var worst uint64
		var at float64
		const n = 200000
		for i := range n {
			x := c.from + (c.to-c.from)*float64(i)/n
			if c.name == "Log" || c.name == "Log10" {
				x = math.Exp(math.Log(c.from) + (math.Log(c.to)-math.Log(c.from))*float64(i)/n)
			}
			got, want := c.f(x), c.want(x)
			// Sin's and Cos's results are at most 1: their error is absolute (an
			// ulp of 1 is far more than one of a result near a zero).
			if c.name[:3] == "Sin" || c.name[:3] == "Cos" {
				if math.Abs(got-want) > 0x1p-50 {
					t.Errorf("%s(%g) = %v, %g from math's", c.name, x, got, got-want)
				}
				continue
			}
			if u := ulps(got, want); u > worst {
				worst, at = u, x
			}
		}
		if worst > c.max {
			t.Errorf("%s: %d ulps from math's at %g, want %d at most", c.name, worst, at, c.max)
		}
	}
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"Exp(0)", Exp(0), 1},
		{"Exp(-Inf)", Exp(math.Inf(-1)), 0},
		{"Exp(800)", Exp(800), math.Inf(1)},
		{"Exp(-800)", Exp(-800), 0},
		{"Log(1)", Log(1), 0},
		{"Log(0)", Log(0), math.Inf(-1)},
		{"Log(Inf)", Log(math.Inf(1)), math.Inf(1)},
		{"Sin(0)", Sin(0), 0},
		{"Cos(0)", Cos(0), 1},
		{"Pow(2, 10)", Pow(2, 10), 1024},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if !math.IsNaN(Log(-1)) || !math.IsNaN(Exp(math.NaN())) || !math.IsNaN(Sin(math.Inf(1))) {
		t.Error("NaN in, or a log of a negative, is not NaN")
	}
	if u := ulps(Exp(1), math.E); u > 1 {
		t.Errorf("Exp(1) is %d ulps from e", u)
	}
	if u := ulps(Pow(10000, -0.25), math.Pow(10000, -0.25)); u > 4 {
		t.Errorf("Pow(10000, -0.25) is %d ulps from math's", u)
	}
}
