package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/interp"
)

const password = `Fjord & "north" 2026/ü`

func suite() *core.Suite {
	env := interp.MapLookup(map[string]string{"SHOP_PASSWORD": password, "SHOP_EMAIL": "orders@fjord-outdoor.example", "EMPTY": ""})
	r := &interp.Resolver{Lookups: map[string]interp.Lookup{"env": env}}
	return core.NewSuite(core.SuiteOptions{Interpolate: r.MustExpand})
}

func scenario() *core.Scenario {
	return core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "sign in"}, suite(), nil)
}

func TestSecretsAreWhatEnvReferencesExpandTo(t *testing.T) {
	s := suite()
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"${env:SHOP_PASSWORD}", []string{password}},
		{"user ${env:SHOP_EMAIL}, password ${env:SHOP_PASSWORD}", []string{"orders@fjord-outdoor.example", password}},
		{"${env:MISSING:-${env:SHOP_PASSWORD}}", []string{password}},
		{"${env:MISSING:-local-dev}", []string{"local-dev"}},
		{"$${env:SHOP_PASSWORD}", nil},
		{"${env:MISSING}", nil},
		{"${env:EMPTY}", nil},
		{"${sys:portal.url}", nil},
		{"Fjord Outdoor", nil},
		{"${env:SHOP_PASSWORD", nil},
	} {
		if got := In(s, c.in); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSecretsAreMaskedInFailures(t *testing.T) {
	sc := scenario()
	if v := Expand(sc, "${env:SHOP_PASSWORD}"); v != password {
		t.Fatalf("expanded to %q", v)
	}
	if err := Hide(sc, core.Fail(`The page does not show "`+password+`"`, password, "Log in to Your Account")); err.Error() !=
		"The page does not show \"********\"\n  expected: \"********\"\n  actual:   \"Log in to Your Account\"" {
		t.Errorf("assertion: %v", err)
	}
	err := Hide(sc, fmt.Errorf("wrapped: %w", core.Fail("no "+password, nil, nil)))
	if strings.Contains(err.Error(), password) || !core.IsAssertion(err) {
		t.Errorf("wrapped assertion: %v", err)
	}
	if err := Hide(sc, io.EOF); !errors.Is(err, io.EOF) {
		t.Errorf("an error without secrets changed: %v", err)
	}
	if err := Hide(sc, os.ErrNotExist); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an error without secrets changed: %v", err)
	}
	if got := Mask(sc, "token="+password); got != "token="+Masked {
		t.Errorf("mask: %q", got)
	}
}

func TestMaskingCoversEncodings(t *testing.T) {
	sc := scenario()
	Keep(sc, password)
	r := Replacer(sc)
	for _, s := range []string{
		password,
		"login=orders%40fjord-outdoor.example&password=Fjord+%26+%22north%22+2026%2F%C3%BC",
		"/reset/Fjord%20&%20%22north%22%202026%2F%C3%BC",
		`<input value="Fjord &amp; &#34;north&#34; 2026/ü">`,
		`{"value":"Fjord & \"north\" 2026/ü"}`,
		`{"value":"Fjord & \"north\" 2026/ü"}`,
	} {
		if got := r.Replace(s); !strings.Contains(got, Masked) {
			t.Errorf("not masked: %s", got)
		}
	}
	if Replacer(scenario()) != nil {
		t.Error("a masker without secrets")
	}
	if got := Mask(scenario(), password); got != password {
		t.Errorf("masked without secrets: %q", got)
	}
}

// A scenario's secrets are its own.
func TestSecretsBelongToTheirScenario(t *testing.T) {
	a, b := scenario(), scenario()
	Keep(a, password)
	if Replacer(b) != nil {
		t.Error("a scenario sees another's secrets")
	}
}
