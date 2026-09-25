package feature

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/axxerr"
)

const launches = `@launches
Feature: Launches

  Background:
    Given the api service with the following properties:
      | url | http://localhost:8080 |

  @smoke
  Scenario: list launches
    Given a GET request to /api/launches
    When the request is executed
    Then the response status code is 200

  Scenario Outline: get launch <id>
    Given a GET request to /api/launches/<id>
    When the request is executed
    Then the response status code is <status>

    @found
    Examples:
      | id | status |
      | 1  | 200    |
      | 2  | 200    |

    Examples:
      | id | status |
      | 99 | 404    |

  Rule: writes
    @wip
    Scenario: create launch
      Given a POST request to /api/launches
`

func setup(t *testing.T) (string, *Set) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "features", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", "launches.feature"), []byte(launches), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", "nested", "other.feature"), []byte("Feature: Other\n  Scenario: x\n    Given y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := Load([]string{"features"}, dir, (&messages.Incrementing{}).NewId)
	if err != nil {
		t.Fatal(err)
	}
	return dir, set
}

func names(ps []*Pickle) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestLoadAndPickles(t *testing.T) {
	_, set := setup(t)
	if len(set.Docs) != 2 || set.Docs[0].URI != "features/launches.feature" || set.Docs[1].URI != "features/nested/other.feature" {
		t.Fatalf("docs: %v", []string{set.Docs[0].URI})
	}
	if len(set.Pickles) != 6 {
		t.Fatalf("want 6 pickles, got %d: %v", len(set.Pickles), names(set.Pickles))
	}
	p := set.Pickles[0]
	if p.Name != "list launches" || p.Line != 9 || len(p.Steps) != 4 {
		t.Fatalf("first pickle: %s line %d steps %d", p.Name, p.Line, len(p.Steps))
	}
	bg := p.StepSource(p.Steps[0])
	if !bg.Background || bg.Line != 5 || bg.Keyword != "Given " {
		t.Errorf("background step source = %+v", bg)
	}
	if s := p.StepSource(p.Steps[3]); s.Background || s.Line != 12 {
		t.Errorf("scenario step source = %+v", s)
	}
	if set.Pickles[1].Name != "get launch 1" || set.Pickles[1].Line != 22 || set.Pickles[1].ScenarioLine != 14 {
		t.Errorf("outline pickle: %s line %d/%d", set.Pickles[1].Name, set.Pickles[1].Line, set.Pickles[1].ScenarioLine)
	}
}

func TestTagFilter(t *testing.T) {
	_, set := setup(t)
	cases := map[string]int{
		"@smoke":                 1,
		"@found":                 2,
		"@launches and not @wip": 4,
		"not @launches":          1,
	}
	for expr, want := range cases {
		ps, err := set.Apply(Filter{Tags: expr})
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != want {
			t.Errorf("%q: got %d (%v), want %d", expr, len(ps), names(ps), want)
		}
	}
	_, err := set.Apply(Filter{Tags: "@a and ("})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeTags {
		t.Errorf("bad tag expression: %v", err)
	}
}

func TestLineFilter(t *testing.T) {
	dir, set := setup(t)
	cases := map[string][]string{
		"features/launches.feature:9":     {"list launches"},
		"features/launches.feature:12":    {"list launches"}, // a step line inside the scenario
		"features/launches.feature:22":    {"get launch 1"},  // example row
		"features/launches.feature:14":    {"get launch 1", "get launch 2", "get launch 99"},
		"features/launches.feature:20":    {"get launch 1", "get launch 2"}, // Examples keyword line
		"features/launches.feature:22:31": {"get launch 1", "create launch"},
	}
	for spec, want := range cases {
		_, lines := ParseLineSpecs([]string{spec}, dir)
		ps, err := set.Apply(Filter{Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		if got := names(ps); !equal(got, want) {
			t.Errorf("%s: got %v, want %v", spec, got, want)
		}
	}
}

func TestNameFilter(t *testing.T) {
	_, set := setup(t)
	ps, err := set.Apply(Filter{Names: []string{"^get launch (1|2)$"}})
	if err != nil || len(ps) != 2 {
		t.Fatalf("got %v %v", names(ps), err)
	}
}

func TestParseErrorsAggregate(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.feature", "b.feature"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("Feature: x\n  Scenario: y\n    Given z\n      | a | b |\n      | c |\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Load([]string{"."}, dir, (&messages.Incrementing{}).NewId)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeParse {
		t.Fatalf("want parse error, got %v", err)
	}
	for _, f := range []string{"a.feature", "b.feature"} {
		if !contains(err.Error(), f) {
			t.Errorf("error should mention %s: %v", f, err)
		}
	}
}

func TestMissingPath(t *testing.T) {
	_, err := Load([]string{"nope"}, t.TempDir(), (&messages.Incrementing{}).NewId)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestSplitLineSpec(t *testing.T) {
	p, l := splitLineSpec("features/x.feature:12:30")
	if p != "features/x.feature" || len(l) != 2 || l[1] != 30 {
		t.Fatalf("%s %v", p, l)
	}
	if p, l := splitLineSpec(`C:\x.feature`); p != `C:\x.feature` || l != nil {
		t.Fatalf("windows path mangled: %s %v", p, l)
	}
}

func equal(a, b []string) bool {
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

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
