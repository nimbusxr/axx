package lsp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nimbusxr/axx/internal/check"
)

// A value's message starts with its row's key, as the table writes it.
func TestProblemMessageKeepsAKeysCase(t *testing.T) {
	for key, want := range map[string]string{
		"postcode": "postcode: the cell is empty.",
		"über":     "über: the cell is empty.",
		"Depot":    "Depot: the cell is empty.",
	} {
		got := problemMessage(check.Problem{Kind: check.Value, Message: key + ": the cell is empty"}, false)
		if got != want || !utf8.ValidString(got) {
			t.Errorf("%s: %q", key, got)
		}
	}
	got := problemMessage(check.Problem{Kind: check.Argument, Message: "this step requires a data table"}, false)
	if !strings.HasPrefix(got, "This step requires a data table.") {
		t.Errorf("an argument problem: %q", got)
	}
}
