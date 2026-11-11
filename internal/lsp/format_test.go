package lsp

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/featurefmt"
)

// apply applies edits to text as an editor does: positions are lines (each
// ended by \r\n, \r or \n) and UTF-16 offsets in the text before any edit.
func apply(t *testing.T, text string, edits []textEdit) string {
	t.Helper()
	starts := []int{0} // where each line starts
	for i := 0; i < len(text); i++ {
		switch {
		case text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n':
			i++
			starts = append(starts, i+1)
		case text[i] == '\r' || text[i] == '\n':
			starts = append(starts, i+1)
		}
	}
	offset := func(p position) int {
		if p.Line >= len(starts) {
			t.Fatalf("line %d is past the end", p.Line)
		}
		end := len(text)
		if p.Line+1 < len(starts) {
			end = starts[p.Line+1]
		}
		line := strings.TrimRight(text[starts[p.Line]:end], "\r\n")
		return starts[p.Line] + byteOffset(line, p.Character)
	}
	sorted := append([]textEdit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool { return offset(sorted[i].Range.Start) > offset(sorted[j].Range.Start) })
	for _, e := range sorted {
		from, to := offset(e.Range.Start), offset(e.Range.End)
		text = text[:from] + e.NewText + text[to:]
	}
	return text
}

func TestLineEditsChangeOnlyTheLinesThatChange(t *testing.T) {
	for name, tc := range map[string]struct {
		text, out string
		edits     int
	}{
		"a table":            {"Feature: F\n\n  Scenario: S\n    Given a\n      | a | bb |\n      | ccc | d |\n", "Feature: F\n\n  Scenario: S\n    Given a\n      | a   | bb |\n      | ccc | d  |\n", 1},
		"a blank line added": {"Feature: F\n  Scenario: S\n    Given a\n", "Feature: F\n\n  Scenario: S\n    Given a\n", 1},
		"two places":         {"Feature: F\n  Scenario: S\n    Given a\n  Scenario: T\n    Given b\n", "Feature: F\n\n  Scenario: S\n    Given a\n\n  Scenario: T\n    Given b\n", 2},
		"blank lines gone":   {"Feature: F\n\n\n\n  Scenario: S\n    Given a\n\n\n", "Feature: F\n\n  Scenario: S\n    Given a\n", 2},
		"no last line break": {"Feature: F\n\n  Scenario: S\n    Given  a", "Feature: F\n\n  Scenario: S\n    Given a\n", 1},
		"carriage returns":   {"Feature: F\r\n  Scenario: S\r\n    Given a\r\n", "Feature: F\r\n\r\n  Scenario: S\r\n    Given a\r\n", 1},
		"a lone return":      {"Feature: F\r  Scenario: S\n", "Feature: F\n", 1},
		"nothing to do":      {"Feature: F\n", "Feature: F\n", 0},
		"wide characters":    {"Feature: F 📦\n  Scenario: S\n    Given 📦  a\n", "Feature: F 📦\n\n  Scenario: S\n    Given 📦 a\n", 2},
	} {
		t.Run(name, func(t *testing.T) {
			edits := lineEdits(tc.text, tc.out)
			if len(edits) != tc.edits {
				t.Errorf("%d edits, want %d: %+v", len(edits), tc.edits, edits)
			}
			if got := apply(t, tc.text, edits); got != tc.out {
				t.Errorf("got %q\nwant %q", got, tc.out)
			}
		})
	}
}

const messyFeature = "Feature: Claims\n Scenario: A claim\n  Given  a GET request to /claims\n   |a|bb|\n   |ccc|d|\n"

func formatting(t *testing.T, c *client, uri string) []textEdit {
	t.Helper()
	raw := c.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": false},
	})
	var edits []textEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return edits
}

func TestFormattingLaysOutAsAxxFmtDoes(t *testing.T) {
	if caps := capabilities("test")["capabilities"].(map[string]any); caps["documentFormattingProvider"] != true {
		t.Errorf("formatting is not announced: %+v", caps)
	}
	c := startServer(t)
	uri := newProject(t)
	c.open(uri, messyFeature)
	want, err := featurefmt.Format("x.feature", []byte(messyFeature), featurefmt.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := apply(t, messyFeature, formatting(t, c, uri)); got != string(want) {
		t.Errorf("formatted:\n%s\nwant:\n%s", got, want)
	}
	// A feature that does not parse is left alone: its diagnostics say why.
	c.open(uri, "Feature: x\n  Scenario: y\n    Given a\n  | a |\n  | b | c |\n")
	if edits := formatting(t, c, uri); len(edits) != 0 {
		t.Errorf("edits for a feature that does not parse: %+v", edits)
	}
	// A document that is not open has nothing to format.
	if edits := formatting(t, c, uri+".other"); len(edits) != 0 {
		t.Errorf("edits for a document that is not open: %+v", edits)
	}
}

// Formatting a range lays out the lines in it, and leaves the others.
func TestRangeFormattingLaysOutTheLinesInTheRange(t *testing.T) {
	if caps := capabilities("test")["capabilities"].(map[string]any); caps["documentRangeFormattingProvider"] != true {
		t.Errorf("range formatting is not announced: %+v", caps)
	}
	c := startServer(t)
	uri := newProject(t)
	text := "Feature: Claims\n\n  Scenario: A claim\n    Given  a GET request to /claims\n\n  Scenario: Another\n    Given  a GET request to /claims\n"
	c.open(uri, text)
	raw := c.request("textDocument/rangeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 6, "character": 0}, "end": map[string]any{"line": 7, "character": 0}},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	})
	var edits []textEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		t.Fatal(err)
	}
	want := "Feature: Claims\n\n  Scenario: A claim\n    Given  a GET request to /claims\n\n  Scenario: Another\n    Given a GET request to /claims\n"
	if got := apply(t, text, edits); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The router formats itself: a project's server may still be preparing its
// packs, or have failed to.
func TestRouterFormatsWithoutAProjectServer(t *testing.T) {
	s := &servers{fail: errors.New("error[AXX-E0910]: preparing the packs failed")}
	c := startRouter(t, s)
	_, uri := packsProject(t, "./steps")
	c.open(uri, messyFeature)
	want, err := featurefmt.Format("x.feature", []byte(messyFeature), featurefmt.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := apply(t, messyFeature, formatting(t, c, uri)); got != string(want) {
		t.Errorf("formatted:\n%s\nwant:\n%s", got, want)
	}
}

// An editor lays out a feature as its project's axx.yaml says (fmt.indent),
// whether the project's server or the router answers, as axx fmt does.
func TestFormattingFollowsTheProjectsAxxYaml(t *testing.T) {
	want, err := featurefmt.Format("x.feature", []byte(messyFeature), featurefmt.Options{Indent: 4})
	if err != nil {
		t.Fatal(err)
	}
	if two, _ := featurefmt.Format("x.feature", []byte(messyFeature), featurefmt.Options{}); string(two) == string(want) {
		t.Fatal("the feature reads the same with either indent")
	}
	axxYaml := "version: 1\nfmt:\n  indent: 4\n"

	c := startServer(t)
	_, uri := writeProject(t, map[string]string{"axx.yaml": axxYaml, "axx-packs.yaml": packsFile})
	c.open(uri, messyFeature)
	if got := apply(t, messyFeature, formatting(t, c, uri)); got != string(want) {
		t.Errorf("the project's server:\n%s\nwant:\n%s", got, want)
	}

	r := startRouter(t, &servers{fail: errors.New("error[AXX-E0910]: preparing the packs failed")})
	_, uri = writeProject(t, map[string]string{"axx.yaml": axxYaml, "axx-packs.yaml": "packs: [./steps]\n"})
	r.open(uri, messyFeature)
	if got := apply(t, messyFeature, formatting(t, r, uri)); got != string(want) {
		t.Errorf("the router:\n%s\nwant:\n%s", got, want)
	}
}
