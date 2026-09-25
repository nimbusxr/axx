package jsonassert

import (
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

const doc = `{"priority": "high", "crew": {"size": 3, "lead": "Ada"}, "ratio": 2.50, "tags": ["a", "b"], "note": null, "ok": true}`

func table(rows ...[]string) *core.Table { return &core.Table{Rows: rows} }

func TestPropertiesPass(t *testing.T) {
	err := Properties(doc, table(
		[]string{"priority", "high"},
		[]string{"crew.size", "3"},
		[]string{"$.crew.lead", "Ada"},
		[]string{"ratio", "2.5"},
		[]string{"tags[1]", "b"},
		[]string{"note", "NULL"},
		[]string{"missing", "undefined"},
		[]string{"ok", "true"},
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Properties(doc, table([]string{"crew.lead", "A.a"}, []string{"crew.size", `\d+`}), true); err != nil {
		t.Fatal(err)
	}
}

func TestPropertiesFailTogether(t *testing.T) {
	err := Properties(doc, table(
		[]string{"priority", "low"},
		[]string{"crew.size", "3"},
		[]string{"note", "undefined"},
		[]string{"gone", "x"},
	), false)
	var ae *core.AssertionError
	if !errors.As(err, &ae) {
		t.Fatalf("want an assertion failure, got %v", err)
	}
	if !strings.Contains(ae.Message, "priority, note, gone") {
		t.Errorf("message %q", ae.Message)
	}
	act := ae.Actual.(map[string]any)
	if act["priority"] != "high" || act["note"] != nil || act["gone"] != "<absent>" {
		t.Errorf("actual %#v", act)
	}
	if err := Properties(doc, table([]string{"crew.lead", "A"}), true); !core.IsAssertion(err) {
		t.Errorf("a partial regex match must fail: %v", err)
	}
}

func TestPropertiesErrors(t *testing.T) {
	if err := Properties("not json", table([]string{"a", "b"}), false); err == nil || !strings.Contains(err.Error(), "Could not perform selection") {
		t.Errorf("invalid JSON: %v", err)
	}
	if err := Properties(doc, table([]string{"a", "("}), true); err == nil || core.IsAssertion(err) {
		t.Errorf("invalid pattern must be an error, not an assertion: %v", err)
	}
}
