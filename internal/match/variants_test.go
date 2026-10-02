package match

import (
	"slices"
	"testing"
)

func TestExpandGivesEveryConcreteExpression(t *testing.T) {
	got := Expand("the[[ {ordinal} ordered]] response status code is {int}")
	want := []string{"the response status code is {int}", "the {ordinal} ordered response status code is {int}"}
	if !slices.Equal(got, want) {
		t.Errorf("Expand = %q, want %q", got, want)
	}
	if got := Expand("plain text"); !slices.Equal(got, []string{"plain text"}) {
		t.Errorf("no optional segments: %q", got)
	}
	if got := Expand("broken [[ segment"); got != nil {
		t.Errorf("invalid expression: %q", got)
	}
}
