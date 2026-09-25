package javare_test

import (
	"fmt"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/javare"
)

type regexOracle struct {
	Cases []struct {
		Pattern string `json:"pattern"`
		Error   *struct {
			Description string `json:"description"`
			Index       int    `json:"index"`
		} `json:"error"`
		Results []struct {
			Input   string    `json:"input"`
			Matches bool      `json:"matches"`
			Find    []*string `json:"find"`
			FindAll []string  `json:"findAll"`
		} `json:"results"`
	} `json:"cases"`
}

func TestRegexOracle(t *testing.T) {
	var o regexOracle
	oracletest.Load(t, "javaregex", &o)
	if len(o.Cases) < 300 {
		t.Fatalf("oracle too small: %d patterns", len(o.Cases))
	}
	for _, c := range o.Cases {
		re, err := javare.Compile(c.Pattern)
		key := c.Pattern
		if c.Error != nil || err != nil {
			oracletest.Check(t, "javaregex", "compile|"+key, (c.Error != nil) == (err != nil), func() string {
				if c.Error != nil {
					return fmt.Sprintf("Java rejects it (%s at %d) but it compiled", c.Error.Description, c.Error.Index)
				}
				return fmt.Sprintf("Java accepts it but compiling failed: %v", err)
			})
			continue
		}
		for _, r := range c.Results {
			rkey := key + "|" + r.Input
			full, err := re.FullMatch(r.Input)
			if err != nil {
				t.Fatalf("%q on %q: %v", c.Pattern, r.Input, err)
			}
			oracletest.Check(t, "javaregex", "matches|"+rkey, full == r.Matches, func() string {
				return fmt.Sprintf("matches(): want %v, got %v", r.Matches, full)
			})
			m, err := re.Find(r.Input)
			if err != nil {
				t.Fatalf("%q on %q: %v", c.Pattern, r.Input, err)
			}
			oracletest.Check(t, "javaregex", "find|"+rkey, sameGroups(m, r.Find), func() string {
				return fmt.Sprintf("find(): want %s, got %s", fmtGroups(r.Find), fmtMatch(m))
			})
			all, err := re.FindAllString(r.Input)
			if err != nil {
				t.Fatalf("%q on %q: %v", c.Pattern, r.Input, err)
			}
			oracletest.Check(t, "javaregex", "findAll|"+rkey, sameStrings(all, r.FindAll), func() string {
				return fmt.Sprintf("find() loop: want %q, got %q", r.FindAll, all)
			})
		}
	}
	oracletest.CheckAllDeviationsSeen(t, "javaregex")
}

func sameGroups(m *javare.Match, want []*string) bool {
	if m == nil || want == nil {
		return m == nil && want == nil
	}
	if len(m.Groups) != len(want) {
		return false
	}
	for i, w := range want {
		if (w == nil) != !m.Matched[i] || w != nil && *w != m.Groups[i] {
			return false
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fmtGroups(g []*string) string {
	if g == nil {
		return "no match"
	}
	s := "["
	for i, x := range g {
		if i > 0 {
			s += ", "
		}
		if x == nil {
			s += "null"
		} else {
			s += fmt.Sprintf("%q", *x)
		}
	}
	return s + "]"
}

func fmtMatch(m *javare.Match) string {
	if m == nil {
		return "no match"
	}
	s := "["
	for i, g := range m.Groups {
		if i > 0 {
			s += ", "
		}
		if !m.Matched[i] {
			s += "null"
		} else {
			s += fmt.Sprintf("%q", g)
		}
	}
	return s + "]"
}
