package match

import (
	"testing"

	"github.com/nimbusxr/axx/core"
)

// A step line that keeps the notation of the expression it was copied from
// gets the line written out as its first suggestion.
func TestWrittenOut(t *testing.T) {
	r := NewRegistry()
	// {word} and {int} are Cucumber's own.
	must(t, r.AddParams("core", []core.ParamType{{Name: "ordinal", Regexps: []string{`(\d+)(?:st|nd|rd|th)`}}}))
	must(t, r.AddSteps("p", []core.StepDef{
		{ID: "payload", Expr: "a request payload using a(n) {word} empty content template"},
		{ID: "rows", Expr: "the[[ {ordinal}]] selection has {int} row(s)"},
		{ID: "times", Expr: "the mocked request was received exactly {int} time(s)"},
	}))
	for text, want := range map[string]string{
		"a request payload using a(n) application/json empty content template": "a request payload using an application/json empty content template",
		"a request payload using a(n) text/plain empty content template":       "a request payload using a text/plain empty content template",
		"the[[ 2nd]] selection has 2 row(s)":                                   "the 2nd selection has 2 rows",
		"the selection has 1 row(s)":                                           "the selection has 1 row",
		"the mocked request was received exactly 1 time(s)":                    "the mocked request was received exactly 1 time",
	} {
		line, _, ok := r.WrittenOut(text)
		if !ok || line != want {
			t.Errorf("WrittenOut(%q) = %q, %v; want %q", text, line, ok, want)
		}
		if s := r.Suggest(text, 3); len(s) == 0 || s[0].Expr != want {
			t.Errorf("Suggest(%q)[0] = %+v", text, s)
		}
	}
	if _, _, ok := r.WrittenOut("a request payload using an application/json empty content template"); ok {
		t.Error("a line without notation has nothing to write out")
	}
}
