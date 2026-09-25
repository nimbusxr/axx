package interp

import (
	"errors"
	"testing"
)

func resolver(strict bool) *Resolver {
	return &Resolver{
		Strict: strict,
		Lookups: map[string]Lookup{
			"env": MapLookup(map[string]string{"HOME": "/home/me", "EMPTY": "", "WHICH": "local.host"}),
			"sys": MapLookup(map[string]string{"local.host": "db.internal", "port": "5432"}),
		},
	}
}

func TestExpand(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"${env:HOME}/x", "/home/me/x"},
		{"http://${sys:local.host}:${sys:port}", "http://db.internal:5432"},
		{"${sys:missing:-localhost}", "localhost"},
		{"${env:EMPTY:-fallback}", ""},
		{"${sys:missing}", "${sys:missing}"},
		{"${unknown:x}", "${unknown:x}"},
		{"${NOPREFIX}", "${NOPREFIX}"},
		{"${sys:missing:-${env:HOME}}", "/home/me"},
		{"${sys:${env:WHICH}}", "db.internal"},
		{"${sys:missing:-${sys:also-missing:-deep}}", "deep"},
		{"$${sys:port}", "${sys:port}"},
		{"cost $5 ${sys:port", "cost $5 ${sys:port"},
		{"${sys:missing:-a:-b}", "a:-b"},
		{"${sys:missing:-}", ""},
	}
	r := resolver(false)
	for _, c := range cases {
		got, err := r.Expand(c.in)
		if err != nil {
			t.Errorf("Expand(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Expand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStrict(t *testing.T) {
	_, err := resolver(true).Expand("x ${sys:nope} y")
	var ue *UnresolvedError
	if !errors.As(err, &ue) || ue.Ref != "${sys:nope}" {
		t.Fatalf("want UnresolvedError for ${sys:nope}, got %v", err)
	}
	if got, err := resolver(true).Expand("${sys:nope:-ok}"); err != nil || got != "ok" {
		t.Fatalf("default should satisfy strict mode: %q %v", got, err)
	}
}

func FuzzExpand(f *testing.F) {
	for _, s := range []string{"${env:HOME}", "$${x}", "${a:${b:-${c}}}", "}}{{${", "${sys:x:-${"} {
		f.Add(s)
	}
	r := resolver(false)
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = r.Expand(s) // must not panic or hang
	})
}
