package oaslevel

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

var testKeys = NewKeys([]string{
	"validation.request.body.missing",
	"validation.request.body.schema.{keyword}",
	"validation.request.parameter.{in}.missing",
	"validation.response.header.missing",
	"validation.response.body.schema.{keyword}",
}, map[string][]string{
	"{keyword}": {"required", "maximum", "minimum", "maxLength", "minLength", "enum"},
	"{in}":      {"query", "header"},
}, map[string]string{"const": "enum", "exclusiveMaximum": "maximum"})

func TestKeysHaveTheirPrefixes(t *testing.T) {
	for _, key := range []string{
		"validation", "validation.request", "validation.request.body", "validation.request.body.schema",
		"validation.request.body.schema.maxLength", "validation.request.parameter.header.missing",
		"validation.response.body.schema.enum",
	} {
		if err := testKeys.Check(key); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
	for _, key := range []string{
		"", "validation.", "validation.request.body.schema.maxlength", "validation.requestX",
		"validation.request.body.schema.{keyword}", "validation.request.parameter.path.missing",
		"request.body", "validation.request.body.schema.required.name",
	} {
		if testKeys.Has(key) {
			t.Errorf("%q should be unknown", key)
		}
	}
}

func TestUnknownKeySuggestsTheClosestKeys(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		// a typo
		{"validation.request.body.shema.maximum", "validation.request.body.schema.maximum"},
		// another case
		{"validation.request.body.schema.maxlength", "validation.request.body.schema.maxLength"},
		// a segment too few, or too many
		{"validation.request.header.missing", "validation.request.parameter.header.missing"},
		{"validation.request.body.required", "validation.request.body.schema.required"},
		{"request.body", "validation.request.body"},
		{"validation.request.body.schema.required.name", "validation.request.body.schema.required"},
		// a keyword the keys name otherwise
		{"validation.request.body.schema.const", "validation.request.body.schema.enum"},
	} {
		err := testKeys.Check(c.key)
		var uk *UnknownKeyError
		if !errors.As(err, &uk) {
			t.Fatalf("%s: %v", c.key, err)
		}
		if uk.Key != c.key || len(uk.Closest) == 0 || len(uk.Closest) > 3 || uk.Closest[0] != c.want {
			t.Errorf("%s: closest %v, want %s first", c.key, uk.Closest, c.want)
		}
	}
}

func TestUnknownKeyMessage(t *testing.T) {
	err := testKeys.Check("validation.request.body.shema.maximum")
	want := `unknown OpenAPI validation key "validation.request.body.shema.maximum"; ` +
		`did you mean validation.request.body.schema.maximum? ` +
		`A key is one the validator reports, or a prefix of such keys, like validation.request.body`
	if err == nil || err.Error() != want {
		t.Fatalf("got  %v\nwant %s", err, want)
	}
	err = testKeys.Check("validation.request.header.missing")
	if err == nil || !strings.Contains(err.Error(), "did you mean validation.request.parameter.header.missing, ") ||
		!strings.Contains(err.Error(), " or ") {
		t.Fatalf("several suggestions: %v", err)
	}
}

func TestParseMapChecksKeys(t *testing.T) {
	padded := " validation.request.body " // keys are trimmed
	got, err := ParseMap(map[string]string{
		padded:                                   "ignore",
		"validation.response.header.missing":     "Warn",
		"validation.request.body.schema.maximum": "FAIL",
	}, testKeys)
	if err != nil {
		t.Fatal(err)
	}
	want := Levels{"validation.request.body": Ignore, "validation.response.header.missing": Warn, "validation.request.body.schema.maximum": Error}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, lv := range want {
		if got[k] != lv {
			t.Errorf("%s: %v, want %v", k, got[k], lv)
		}
	}

	_, err = ParseMap(map[string]string{"validation.request.bdy": "IGNORE"}, testKeys)
	var uk *UnknownKeyError
	if !errors.As(err, &uk) || !slices.Contains(uk.Closest, "validation.request.body") {
		t.Fatalf("unknown key: %v", err)
	}
	if _, err := ParseMap(map[string]string{"validation.request": "LOUD"}, testKeys); err == nil ||
		!strings.Contains(err.Error(), `key "validation.request": invalid level "LOUD"`) {
		t.Fatalf("invalid level: %v", err)
	}
}
