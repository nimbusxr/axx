package javafmt_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

func TestDouble(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{1, "1.0"},
		{1.5, "1.5"},
		{100, "100.0"},
		{1e7, "1.0E7"},
		{9999999, "9999999.0"},
		{1e10, "1.0E10"},
		{0.001, "0.001"},
		{1e-4, "1.0E-4"},
		{1e-5, "1.0E-5"},
		{123456789, "1.23456789E8"},
		{0.30000000000000004, "0.30000000000000004"},
		{math.MaxFloat64, "1.7976931348623157E308"},
		{math.SmallestNonzeroFloat64, "4.9E-324"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
	}
	for _, tt := range tests {
		if got := javafmt.Double(tt.in); got != tt.want {
			t.Errorf("Double(%v) = %s, want %s", tt.in, got, tt.want)
		}
	}
	if got := javafmt.Float(1.1); got != "1.1" {
		t.Errorf("Float(1.1) = %s", got)
	}
	if got := javafmt.Float(1e10); got != "1.0E10" {
		t.Errorf("Float(1e10) = %s", got)
	}
}

func TestValueOf(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, "null"},
		{"text", "text"},
		{true, "true"},
		{int32(-5), "-5"},
		{int64(3000000000), "3000000000"},
		{1.0, "1.0"},
		{float32(1.5), "1.5"},
		{big.NewInt(7), "7"},
		{[]any{int32(1), "x", nil, []any{2.5}}, "[1, x, null, [2.5]]"},
		{map[string]any{"b": []any{int32(1), int32(2)}, "a": int32(1)}, "{a=1, b=[1, 2]}"},
	}
	for _, tt := range tests {
		if got := javafmt.ValueOf(tt.in); got != tt.want {
			t.Errorf("ValueOf(%#v) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	ints := map[string]int64{"0": 0, "+5": 5, "-2147483648": math.MinInt32, "007": 7, "١٢": 12}
	for in, want := range ints {
		got, err := javafmt.ParseInt(in)
		if err != nil || int64(got) != want {
			t.Errorf("ParseInt(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "-", "2147483648", " 1", "1.0", "0x10"} {
		if _, err := javafmt.ParseInt(in); err == nil {
			t.Errorf("ParseInt(%q) should fail", in)
		}
	}
	if v, err := javafmt.ParseLong("-9223372036854775808"); err != nil || v != math.MinInt64 {
		t.Errorf("ParseLong min = %d, %v", v, err)
	}
	doubles := []struct {
		in   string
		want float64
	}{{" 12 ", 12}, {"1e5", 1e5}, {".5", 0.5}, {"5.", 5}, {"1.5f", 1.5}, {"0x1p3", 8}, {"-Infinity", math.Inf(-1)}}
	for _, tt := range doubles {
		got, err := javafmt.ParseDouble(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseDouble(%q) = %v, %v", tt.in, got, err)
		}
	}
	for _, in := range []string{"", "1e", "infinity", "0x10", "1_000", "--1"} {
		if _, err := javafmt.ParseDouble(in); err == nil {
			t.Errorf("ParseDouble(%q) should fail", in)
		}
	}
	if !javafmt.ParseBoolean("TRUE") || javafmt.ParseBoolean("yes") {
		t.Error("ParseBoolean")
	}
}

func TestBigDecimal(t *testing.T) {
	tests := []struct{ in, str, plain string }{
		{"1.50", "1.50", "1.50"},
		{"1e5", "1E+5", "100000"},
		{"0.00000001", "1E-8", "0.00000001"},
		{"-1.23E-7", "-1.23E-7", "-0.000000123"},
		{"123.4567e10", "1.234567E+12", "1234567000000"},
	}
	for _, tt := range tests {
		d, err := javafmt.ParseBigDecimal(tt.in)
		if err != nil || d.String() != tt.str || d.PlainString() != tt.plain {
			t.Errorf("%s: %s %s %v", tt.in, d.String(), d.PlainString(), err)
		}
	}
	a, _ := javafmt.ParseBigDecimal("1.0")
	b, _ := javafmt.ParseBigDecimal("1.00")
	if a.Cmp(b) != 0 || a.Equal(b) {
		t.Error("1.0 and 1.00 compare equal but are not equals()")
	}
}
