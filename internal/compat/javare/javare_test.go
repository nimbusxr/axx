package javare_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/compat/javare"
)

func TestJavaSemantics(t *testing.T) {
	tests := []struct {
		pattern, input string
		full, find     bool
	}{
		// \w, \d, \s and \b are ASCII-only in Java.
		{`\w+`, "héllo", false, true},
		{`\d`, "١", false, false},
		{`(?U)\w+`, "héllo", true, true},
		{`\bé`, "é", false, false},
		// Case-insensitivity is ASCII-only unless UNICODE_CASE is set.
		{`(?i)é`, "É", false, false},
		{`(?iu)é`, "É", true, true},
		{`(?i)k`, "K", false, false},
		// Quoting, possessive quantifiers, POSIX classes, intersections.
		{`\Qa.b\E`, "a.b", true, true},
		{`\Qa.b\E`, "axb", false, false},
		{`a*+a`, "aaa", false, false},
		{`\p{Alpha}+`, "abc", true, true},
		{`[a-z&&[^aeiou]]+`, "bcd", true, true},
		{`[a-z&&[^aeiou]]+`, "bad", false, true},
		// Line terminators: '.' excludes \r and U+2028, $ matches before a
		// final line terminator.
		{`a.b`, "a\rb", false, false},
		{`(?s)a.b`, "a\rb", true, true},
		{`end$`, "end\r\n", false, true},
		{`(?x) a b # comment`, "ab", true, true},
		// Named groups count in order with unnamed ones.
		{`(?<first>a)(b)\k<first>\2`, "abab", true, true},
		// Back references to groups that do not exist never match.
		{`(a)\2`, "aa", false, false},
	}
	for _, tt := range tests {
		re, err := javare.Compile(tt.pattern)
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.pattern, err)
			continue
		}
		full, err := re.FullMatch(tt.input)
		if err != nil || full != tt.full {
			t.Errorf("%q FullMatch(%q) = %v, %v; want %v", tt.pattern, tt.input, full, err, tt.full)
		}
		find, err := re.MatchString(tt.input)
		if err != nil || find != tt.find {
			t.Errorf("%q MatchString(%q) = %v, %v; want %v", tt.pattern, tt.input, find, err, tt.find)
		}
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, p := range []string{`[`, `(`, `)`, `*a`, `a{`, `a{,2}`, `a{2,1}`, `\y`, `(?<n>a)(?<n>b)`, `\k<x>`, `[z-a]`, `(?z)`, `(?<=(ab)+)c`} {
		_, err := javare.Compile(p)
		var se *javare.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("Compile(%q): expected a syntax error, got %v", p, err)
		}
	}
	// Java accepts these, some surprisingly.
	for _, p := range []string{`{1}`, `x{2}{3}`, `(?<=a+)b`, `\1`, `[]a]`, `a++`} {
		if _, err := javare.Compile(p); err != nil {
			t.Errorf("Compile(%q): %v", p, err)
		}
	}
}

func TestFindSubmatch(t *testing.T) {
	re := javare.MustCompile(`(\d{4})-(\d{2})(?:-(\d{2}))?`)
	got, err := re.FindSubmatch("on 2024-06 and later")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "2024-06|2024|06|" {
		t.Errorf("FindSubmatch = %q", got)
	}
	m, _ := re.Find("2024-06")
	if !m.Matched[2] || m.Matched[3] {
		t.Errorf("participation = %v", m.Matched)
	}
	if got, _ := re.FindSubmatch("none"); got != nil {
		t.Errorf("no match should be nil, got %q", got)
	}
	all, _ := javare.MustCompile(`a*`).FindAllString("baaa")
	if strings.Join(all, ",") != ",aaa," {
		t.Errorf("FindAllString = %q", all)
	}
}

func TestFindAll(t *testing.T) {
	// Offsets count code points: "é" is one position, as it is one UTF-16 unit.
	re, err := javare.CompileFlags(`^id: (\w+)|^name: (\w+)`, javare.Multiline)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := re.FindAll("id: a\né name: x\nname: b\nid: c")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		g := 1
		if !m.Matched[1] {
			g = 2
		}
		got = append(got, fmt.Sprintf("%s@%d-%d/%d", m.Groups[g], m.Start[g], m.End[g], m.Start[0]))
		if m.Matched[1] == m.Matched[2] || m.Start[3-g] != -1 {
			t.Errorf("participation: %+v", m)
		}
	}
	if want := "a@4-5/0,b@22-23/16,c@28-29/24"; strings.Join(got, ",") != want {
		t.Errorf("FindAll = %s, want %s", strings.Join(got, ","), want)
	}
	empty, _ := javare.MustCompile(`a*`).FindAll("baaa")
	if len(empty) != 3 || empty[1].Start[0] != 1 || empty[1].End[0] != 4 || empty[2].Start[0] != 4 {
		t.Errorf("empty matches: %+v", empty)
	}
}

func TestFlags(t *testing.T) {
	re, err := javare.CompileFlags("a.c", javare.CaseInsensitive|javare.DotAll)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := re.FullMatch("A\nC"); !ok {
		t.Error("flags not applied")
	}
	re, err = javare.CompileFlags("a.c", javare.Literal)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := re.FullMatch("abc"); ok {
		t.Error("LITERAL should match the text literally")
	}
}

func TestMatchTimeout(t *testing.T) {
	saved := javare.MatchTimeout
	javare.MatchTimeout = 50 * time.Millisecond
	defer func() { javare.MatchTimeout = saved }()
	re := javare.MustCompile(`(a+)+$`)
	start := time.Now()
	_, err := re.FullMatch(strings.Repeat("a", 40) + "!")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}
