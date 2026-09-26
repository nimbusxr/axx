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
		Params: []core.ParamType{{Name: "thing", Regexps: []string{`([^\s]+)`}, Doc: "a thing", Values: []string{"crate", "pallet"}, Examples: []string{"crate"}}},
		Steps: []core.StepDef{
			{ID: "demo.status", Keyword: "Then", Expr: "the status is {int}[[ on {thing}]]", Doc: "Check status.", Examples: []string{"Then the status is 200"}},
			{
				ID: "demo.table", Keyword: "Given", Expr: "these values:", Arg: core.ArgTable, DeprecatedBy: "use demo.status", Since: "0.2.0",
				Table: &core.TableDoc{Columns: []string{"property", "value"}, Note: "A property a row.", Rows: []core.TableRow{
					{Name: "url", Takes: "where it is", Required: true},
					{Name: "mode", Takes: "how it goes", Values: []string{"fast", "thorough"}, Default: "fast"},
				}},
				Examples: []string{"Given these values:\n  | url | http://localhost |"},
			},
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
		"| Parameter | Takes | Values | For example |", "| `{thing}` | a thing | `crate`, `pallet` | `crate` |",
		"**Example:**", "> **Deprecated:** use demo.status", "_Since 0.2.0._",
		"Given these values:\n  | property | value |", "| Property | Takes | Values | Default |",
		"| `url` | where it is |  | _required_ |", "| `mode` | how it goes | `fast`, `thorough` | `fast` |", "A property a row.",
		"Given these values:\n  | url | http://localhost |",
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
	if !strings.Contains(ps, "| `{thing}` | a thing | `crate`, `pallet` | `crate` | demo |") {
		t.Errorf("params:\n%s", ps)
	}
}

func TestPackPage(t *testing.T) {
	reg, p := registry(t)
	p.Manifest.Requires = []string{"base"}
	p.Manifest.ConfigSchema = []byte(`{"type": "object", "properties": {
		"mode": {"type": "string", "enum": ["fast", "thorough"], "description": "How it goes."},
		"watch": {"type": "boolean", "description": "Show it."},
		"traces": {"type": "string", "enum": ["failed", "never"], "description": "Which scenarios keep a trace (default failed)."},
		"tls": {"type": "object", "properties": {"verify": {"type": "boolean", "description": "Verify certificates.", "default": false}}},
		"sources": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Where | what."}
	}}`)
	p.Manifest.Tools = []core.Tool{{Name: "demo_look", Description: "What the demo shows."}}
	out, err := PackPage(reg, p, "/references/packs/", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"title: demo", `description: "Demo steps."`, GeneratedHeader,
		"Add it to a project with `axx pack add demo`; it builds on [`base`](/references/packs/base/), which comes with it.",
		"## Settings", "under `packs.demo`", "| `mode` | How it goes. | `fast`, `thorough` | |",
		"| `sources` | Where \\| what. | names and values | |", "| `watch` | Show it. | `true`, `false` | |",
		"| Setting | Takes | Values | Default |", "| `traces` | Which scenarios keep a trace. | `failed`, `never` | `failed` |",
		"| `tls.verify` | Verify certificates. | `true`, `false` | `false` |",
		"## Steps", "### `demo.status`", "## Parameter types", "| `{thing}` | a thing | `crate`, `pallet` | `crate` |",
		"## Tools for agents", "| `demo_look` | What the demo shows. |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// A pack of settings only has its page too.
	settingsOnly := PackInfo{Name: "quiet", Manifest: core.Manifest{Name: "quiet", Doc: "Quiet.", ConfigSchema: p.Manifest.ConfigSchema}}
	out, err = PackPage(reg, settingsOnly, "/references/packs/", false)
	if err != nil || !strings.Contains(out, "## Settings") || strings.Contains(out, "## Steps") {
		t.Errorf("a pack of settings only: %v\n%s", err, out)
	}
	if _, err := PackPage(reg, PackInfo{Name: "nothing"}, "/references/packs/", false); !errors.Is(err, ErrEmpty) {
		t.Errorf("an empty pack: want ErrEmpty, got %v", err)
	}
}

func TestPacksOverview(t *testing.T) {
	out, err := PacksOverview([]PackGroup{
		{Packs: []PackSummary{{Name: "rest", Summary: "REST | OpenAPI"}}},
		{Title: "Azure"},
	}, "/references/packs/", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`description: "The packs axx publishes`, "| [`rest`](/references/packs/rest/) | REST \\| OpenAPI |"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "## Azure") {
		t.Error("an empty group is listed")
	}
	if _, err := PacksOverview(nil, "/", false); !errors.Is(err, ErrEmpty) {
		t.Errorf("no packs: want ErrEmpty, got %v", err)
	}
}

func TestTheCorePage(t *testing.T) {
	reg := match.NewRegistry()
	m := core.ParamsPack().Manifest()
	if err := reg.AddPack("core", m); err != nil {
		t.Fatal(err)
	}
	out, err := PackPage(reg, PackInfo{Name: "core", Manifest: m, Builtin: true}, "/references/packs/", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Every project has it, and every pack builds on it.") || strings.Contains(out, "axx pack add core") || !strings.Contains(out, "`{ordinal}`") {
		t.Errorf("the core's page:\n%s", out)
	}
}
