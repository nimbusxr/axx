package featurefmt

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// Each testdata/*.feature, laid out, is its .golden: the golden is laid out
// already, and says what the feature says.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.feature"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden cases: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(f, src, Options{})
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(f, ".feature") + ".golden"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("laid out:\n%s\nwant:\n%s", got, want)
			}
			again, err := Format(golden, want, Options{})
			if err != nil || !bytes.Equal(again, want) {
				t.Errorf("the golden is not laid out (%v):\n%s", err, again)
			}
		})
	}
}

func format(t *testing.T, src string) string {
	t.Helper()
	out, err := Format("x.feature", []byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestTrailingSpacesGoButDocStringContentStays(t *testing.T) {
	src := "Feature: F   \n  Scenario: S \t\n    Given a step  \n      \"\"\"text  \n      two spaces after:  \n      \"\"\"  \n"
	want := "Feature: F\n\n  Scenario: S\n    Given a step\n      \"\"\"text\n      two spaces after:  \n      \"\"\"\n"
	if got := format(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestTabsAreIndentation(t *testing.T) {
	src := "Feature: F\n\tScenario: S\n\t\tGiven a step\n\t\t\t| a | b |\n"
	want := "Feature: F\n\n  Scenario: S\n    Given a step\n      | a | b |\n"
	if got := format(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestCRLFStaysCRLF(t *testing.T) {
	src := "Feature: F\r\n  Scenario: S\r\n      Given a step\r\n"
	want := "Feature: F\r\n\r\n  Scenario: S\r\n    Given a step\r\n"
	if got := format(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestOneLineBreakAtTheEnd(t *testing.T) {
	for _, src := range []string{"Feature: F\n  Scenario: S\n    Given a step", "Feature: F\n  Scenario: S\n    Given a step\n\n\n"} {
		if got := format(t, src); got != "Feature: F\n\n  Scenario: S\n    Given a step\n" {
			t.Errorf("%q: got %q", src, got)
		}
	}
}

func TestAStepWithNoTextKeepsItsKeyword(t *testing.T) {
	src := "Feature: F\n  Scenario: S\n    Given \n"
	if got := format(t, src); got != "Feature: F\n\n  Scenario: S\n    Given \n" {
		t.Errorf("got %q", got)
	}
}

func TestEmptyAndCommentOnlyFiles(t *testing.T) {
	if got := format(t, "\n\n"); got != "" {
		t.Errorf("blank lines: got %q", got)
	}
	if got := format(t, "  # just a note\n\n"); got != "# just a note\n" {
		t.Errorf("a comment: got %q", got)
	}
}

func TestAFeatureThatDoesNotParseNamesItsLine(t *testing.T) {
	_, err := Format("claims.feature", []byte("Feature: F\n  Scenario: S\n    Given a\n      | a | b |\n      | c |\n"), Options{})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeParse {
		t.Fatalf("got %v", err)
	}
	if ae.Location == nil || ae.Location.File != "claims.feature" || ae.Location.Line != 5 {
		t.Errorf("location %+v", ae.Location)
	}
	if !strings.Contains(ae.Message, "inconsistent cell count") || ae.Hint == "" {
		t.Errorf("%s (hint %q)", ae.Message, ae.Hint)
	}
}

// A layout that would read differently is refused: compare names where.
func TestCompareSaysWhereTwoFeaturesDiffer(t *testing.T) {
	a, err := parse([]byte("Feature: F\n  Scenario: S\n    Given a\n      | a  | b |\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := parse([]byte("Feature: F\n  Scenario: S\n    Given a\n      | a | b |\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d := compare(a, b); d != "" {
		t.Errorf("the same feature differs: %s", d)
	}
	c, err := parse([]byte("Feature: F\n  Scenario: S\n    Given a\n      | a | c |\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d := compare(a, c); !strings.Contains(d, "cells[1].value") {
		t.Errorf("got %q", d)
	}
	if err := changed("x.feature", compare(a, c)); err == nil || !strings.Contains(err.Error(), "x.feature") {
		t.Errorf("got %v", err)
	}
}

func TestWidth(t *testing.T) {
	for s, want := range map[string]int{
		"abc":       3,
		"已送达":       6,
		"📦":         2,
		"e\u0301":   1,
		"Ｆｕｌｌ":      8,
		"a\u200db":  2,
		`a \| b`:    6,
		"":          0,
		"Straße 12": 9,
	} {
		if got := Width(s); got != want {
			t.Errorf("Width(%q) = %d, want %d", s, got, want)
		}
	}
}

// With an indent of 4, each testdata/*.feature is its indent4/*.golden: the
// same layout, four spaces to a level.
func TestGoldenIndentFour(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.feature"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden cases: %v", err)
	}
	four := Options{Indent: 4}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(f, src, four)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "indent4", strings.TrimSuffix(filepath.Base(f), ".feature")+".golden")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("laid out:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// No indent is axx's: two spaces to a level.
func TestTheDefaultIndentIsTwo(t *testing.T) {
	src := []byte("Feature: Claims\nScenario: A claim\nGiven a thing\n")
	def, err := Format("x.feature", src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	two, err := Format("x.feature", src, Options{Indent: 2})
	if err != nil || !bytes.Equal(def, two) {
		t.Errorf("default:\n%s\nindent 2 (%v):\n%s", def, err, two)
	}
}
