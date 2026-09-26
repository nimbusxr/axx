package a11y

import (
	"slices"
	"strings"
	"testing"
)

func TestTheStandardIncludesTheLevelsBelowIt(t *testing.T) {
	for standard, want := range map[string][]string{
		"":         {"wcag2a", "wcag2aa", "wcag21a", "wcag21aa"},
		"wcag2a":   {"wcag2a"},
		"wcag2aa":  {"wcag2a", "wcag2aa"},
		"wcag21a":  {"wcag2a", "wcag21a"},
		"wcag22aa": {"wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"},
	} {
		st, err := parseConfig(Config{Standard: standard})
		if err != nil || !slices.Equal(st.tags, want) {
			t.Errorf("%q: %v %v, want %v", standard, st, err, want)
		}
	}
	if _, err := parseConfig(Config{Standard: "wcag3"}); err == nil || !strings.Contains(err.Error(), `packs.web-a11y.standard: "wcag3" is not one of`) {
		t.Errorf("wcag3: %v", err)
	}
}

func TestTargetsReadAsSelectors(t *testing.T) {
	if got := target([]any{"#address", "#street"}); got != "#address > #street" {
		t.Errorf("through a frame: %q", got)
	}
	if got := target([]any{[]any{"parcel-card", "button"}}); got != "parcel-card > button" {
		t.Errorf("through a shadow root: %q", got)
	}
}
