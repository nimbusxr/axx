package md

import (
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

func registry(t *testing.T) (*match.Registry, PackInfo) {
	t.Helper()
	m := core.Manifest{
		Name: "demo", Doc: "Demo steps.\n\nMore text.",
		Params: []core.ParamType{{Name: "thing", Regexps: []string{`([^\s]+)`}, Doc: "A thing."}},
		Steps: []core.StepDef{
			{ID: "demo.status", Keyword: "Then", Expr: "the status is {int}[[ on {thing}]]", Doc: "Check status.", Examples: []string{"Then the status is 200"}},
			{ID: "demo.table", Keyword: "Given", Expr: "these values:", Arg: core.ArgTable, DeprecatedBy: "use demo.status", Since: "0.2.0"},
		},
	}
	reg := match.NewRegistry()
	if err := reg.AddPack("demo", m); err != nil {
		t.Fatal(err)
	}
	return reg, PackInfo{Name: "demo", Manifest: m}
}

func TestStepsPage(t *testing.T) {
	reg, p := registry(t)
	out, err := StepsPage(reg, p, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"title: demo steps", `description: "Demo steps."`, GeneratedHeader,
		"## `demo.status`", "Then the status is {int}[[ on {thing}]]", "- `the status is {int} on {thing}`",
		"`{thing}` (A thing)", "**Example:**", "> **Deprecated:** use demo.status", "| ... | ... |", "_Since 0.2.0._",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestEmptyIsAnError(t *testing.T) {
	reg := match.NewRegistry()
	if _, err := StepsPage(reg, PackInfo{Name: "nothing"}, false); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
	if _, err := StepIndex(reg); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty for index, got %v", err)
	}
}

func TestIndexAndParams(t *testing.T) {
	reg, _ := registry(t)
	idx, err := StepIndex(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(idx, "demo | demo.status | the status is {int} on {thing}") || !strings.Contains(idx, "these values:  [+table]") {
		t.Errorf("index:\n%s", idx)
	}
	ps, err := ParamsPage(reg, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ps, "| `{thing}` | `([^\\s]+)` | A thing. | demo |") {
		t.Errorf("params:\n%s", ps)
	}
}
