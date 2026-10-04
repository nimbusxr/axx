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

// A step line with its ordinal where another step has it gets the line with
// the ordinal moved as its first suggestion.
func TestReordered(t *testing.T) {
	r := NewRegistry()
	must(t, r.AddParams("core", []core.ParamType{{Name: "ordinal", Regexps: []string{`(\d+)(?:st|nd|rd|th)`}}}))
	must(t, r.AddSteps("rest", []core.StepDef{
		{ID: "status", Expr: "the response status code is {int}"},
		{ID: "status.nth", Expr: "the {ordinal} ordered response status code is {int}"},
		{ID: "property", Expr: "the response payload property {word} is {string}"},
		{ID: "property.nth", Expr: "the response payload property {word} is {string} for {ordinal} ordered response"},
		{ID: "properties", Expr: "the response payload properties are:"},
		{ID: "properties.nth", Expr: "the response payload properties for {ordinal} ordered response are:"},
	}))
	for text, want := range map[string]string{
		"the 2nd ordered response payload property status is 'REGISTERED'": "the response payload property status is 'REGISTERED' for 2nd ordered response",
		"the 2nd ordered response payload property detail is 'Not found'":  "the response payload property detail is 'Not found' for 2nd ordered response",
		"the 2nd ordered response payload properties are:":                 "the response payload properties for 2nd ordered response are:",
		"the response status code is 200 for 2nd ordered response":         "the 2nd ordered response status code is 200",
	} {
		line, _, ok := r.Reordered(text)
		if !ok || line != want {
			t.Errorf("Reordered(%q) = %q, %v; want %q", text, line, ok, want)
		}
		if s := r.Suggest(text, 3); len(s) == 0 || s[0].Expr != want {
			t.Errorf("Suggest(%q)[0] = %+v", text, s)
		}
	}
	for _, text := range []string{
		"the 2nd ordered response status code is 200",       // a step as it is
		"the response payload property status is 409",       // no ordinal to move
		"the 2nd ordered response payload property is open", // moved, it still matches nothing
	} {
		if line, _, ok := r.Reordered(text); ok {
			t.Errorf("Reordered(%q) = %q", text, line)
		}
	}
}
