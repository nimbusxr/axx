package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
)

func session(t *testing.T, dir string) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	srv := New(Options{WorkDir: dir})
	go func() { _ = srv.Run(ctx, st) }()
	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *sdk.ClientSession, name string, args any) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned an error: %+v", name, res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func TestTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "axx-packs.yaml"), []byte("packs: [rest, sql]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := session(t, dir)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The packs' tools come on top of these.
	if len(tools.Tools) != 10 {
		t.Fatalf("tools: %d (keep the tool list short: at most 10)", len(tools.Tools))
	}
	for _, tl := range tools.Tools {
		if tl.Description == "" {
			t.Errorf("tool %s has no description", tl.Name)
		}
	}

	out := call(t, cs, "steps_search", map[string]any{"query": "response status code"})
	steps, _ := out["steps"].([]any)
	if len(steps) == 0 || steps[0].(map[string]any)["id"] != "rest.response.status" {
		t.Fatalf("steps_search: %v", out)
	}

	out = call(t, cs, "step_explain", map[string]any{"line": "Then the response status code is 200"})
	if out["status"] != "matched" {
		t.Fatalf("step_explain: %v", out)
	}
	out = call(t, cs, "step_explain", map[string]any{"line": "Then the respons status code is 200"})
	if out["status"] != "undefined" || out["suggestions"] == nil {
		t.Fatalf("step_explain undefined: %v", out)
	}

	out = call(t, cs, "feature_validate", map[string]any{"content": "Feature: f\n  Scenario: s\n    Given a GET request to /health\n    Then nothing matches this\n"})
	if out["valid"] != false || len(out["problems"].([]any)) != 1 {
		t.Fatalf("feature_validate: %v", out)
	}
	out = call(t, cs, "feature_validate", map[string]any{"content": "Feature: broken\n  Scenario: s\n    Given x\n      | a | b |\n      | c |\n"})
	if probs := out["problems"].([]any); len(probs) == 0 || probs[0].(map[string]any)["kind"] != "syntax" {
		t.Fatalf("syntax problems: %v", out)
	}

	out = call(t, cs, "feature_validate", map[string]any{"content": "Feature: f\n  Scenario: s\n    Given a GET request to /health\n    Then the 2nd selection has 1 row\n"})
	if ws, _ := out["warnings"].([]any); out["valid"] != true || len(ws) != 1 || ws[0].(map[string]any)["kind"] != "lint" {
		t.Fatalf("feature_validate lint warnings: %v", out)
	}
	out = call(t, cs, "feature_validate", map[string]any{"content": "Feature: f\n  Scenario: s\n    Given a GET request to /health\n    When the request is executed\n    Then the response status code is 200\n"})
	if hs, _ := out["hints"].([]any); out["valid"] != true || len(hs) != 1 || !strings.Contains(hs[0].(string), "only a success status") {
		t.Fatalf("feature_validate hints: %v", out)
	}

	out = call(t, cs, "scaffold", map[string]any{"kind": "feature", "name": "Widget checks"})
	if out["path"] != "features/widget-checks.feature" {
		t.Fatalf("scaffold: %v", out)
	}
	out = call(t, cs, "config_show", map[string]any{})
	if out["file"] == "" {
		t.Fatalf("config_show: %v", out)
	}
}

func TestLintRun(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"axx.yaml":     "version: 1\nlint:\n  rules:\n    - {name: ids, filePatterns: [\"seeds/*.yaml\"], regex: 'id: (\\S+)', validation: cross-file-unique}\n",
		"seeds/a.yaml": "id: m-1\n",
		"seeds/b.yaml": "id: m-1\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs := session(t, dir)
	out := call(t, cs, "lint_run", map[string]any{})
	rep, _ := out["report"].(map[string]any)
	if out["ok"] != false || rep == nil {
		t.Fatalf("lint_run: %v", out)
	}
	rules := rep["rules"].([]any)
	f := rules[0].(map[string]any)["findings"].([]any)[0].(map[string]any)
	if f["value"] != "m-1" || f["code"] != "AXX-E0820" || len(f["locations"].([]any)) != 2 {
		t.Errorf("finding: %v", f)
	}
	if out := call(t, cs, "lint_run", map[string]any{"mode": "warn"}); out["ok"] != true {
		t.Errorf("mode warn: %v", out)
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "lint_run", Arguments: map[string]any{"mode": "loud"}})
	if err != nil || !res.IsError {
		t.Errorf("an invalid mode is a tool error: %v %+v", err, res)
	}
}

func TestRedact(t *testing.T) {
	got := string(redact([]byte(`{"properties":{"db.password":"s3cret","host":"x"},"apps":[{"env":{"API_TOKEN":"t"}}]}`)))
	for _, secret := range []string{"s3cret", `"t"`} {
		if contains(got, secret) {
			t.Errorf("secret %s leaked: %s", secret, got)
		}
	}
	if !contains(got, `"host":"x"`) {
		t.Errorf("non-secret removed: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// shelf is a project's own pack for the session tests: a step puts parcels
// on a shelf, and a tool says what is on it.
type shelf struct{}

var onShelf = core.NewStateKey("shelf.parcels", func(*core.Scenario) *[]string { return &[]string{} }, nil)

func (shelf) Manifest() core.Manifest {
	return core.Manifest{
		Name: "shelf",
		Steps: []core.StepDef{{ID: "shelf.put", Expr: "parcel {word} is put on the shelf", Run: func(sc *core.Scenario, a core.Args) error {
			*onShelf.Of(sc) = append(*onShelf.Of(sc), a.String(0))
			return nil
		}}},
		Tools: []core.Tool{{
			Name: "shelf_list", Description: "The parcels on the shelf.", ReadOnly: true,
			Input: json.RawMessage(`{"type": "object", "properties": {"limit": {"type": "integer"}}, "additionalProperties": false}`),
			Run: func(call *core.ToolCall) (*core.ToolResult, error) {
				return &core.ToolResult{Data: map[string]any{"parcels": *onShelf.Of(call.Scenario)}}, nil
			},
		}},
	}
}

func TestAgentsTryStepsInALiveScenario(t *testing.T) {
	engine.Register("./shelf", shelf{})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "axx-packs.yaml"), []byte("packs: [rest, ./shelf]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := session(t, dir)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	if !slices.Contains(names, "steps_try") || !slices.Contains(names, "shelf_list") {
		t.Fatalf("tools: %v", names)
	}

	out := call(t, cs, "steps_try", map[string]any{"steps": "Given parcel PX-4101 is put on the shelf\nAnd parcel PX-4102 is put on the shelf"})
	if fmt.Sprint(out["steps"]) != "[map[keyword:Given status:passed text:parcel PX-4101 is put on the shelf] map[keyword:And status:passed text:parcel PX-4102 is put on the shelf]]" {
		t.Fatalf("steps_try: %v", out)
	}
	out = call(t, cs, "steps_try", map[string]any{"steps": "When parcel PX-4103 is put on the shelf\nThen the parcel is on the shelf\nAnd parcel PX-4104 is put on the shelf"})
	steps := out["steps"].([]any)
	if steps[1].(map[string]any)["status"] != "undefined" || steps[2].(map[string]any)["status"] != "skipped" {
		t.Fatalf("steps_try undefined: %v", out)
	}
	if out := call(t, cs, "shelf_list", map[string]any{}); fmt.Sprint(out["parcels"]) != "[PX-4101 PX-4102 PX-4103]" {
		t.Fatalf("shelf_list: %v", out)
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "shelf_list", Arguments: map[string]any{"limit": "all"}})
	if err != nil || !res.IsError {
		t.Fatalf("an input against the schema: %v %+v", err, res)
	}

	call(t, cs, "steps_try", map[string]any{"restart": true, "steps": "Given parcel PX-4105 is put on the shelf"})
	if out := call(t, cs, "shelf_list", map[string]any{}); fmt.Sprint(out["parcels"]) != "[PX-4105]" {
		t.Fatalf("after a restart: %v", out)
	}
}

// A pack added after the server started is not in it: the agent is told to
// restart the server.
func TestAServerStartedWithoutAPackSaysToRestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "axx-packs.yaml"), []byte("packs: [rest, ./steps]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := session(t, dir)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "steps_search", Arguments: map[string]any{"query": "status code"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Content)
	if !res.IsError || !strings.Contains(string(b), "restart the axx MCP server") || !strings.Contains(string(b), "./steps") {
		t.Errorf("the error does not say to restart the server: %s", b)
	}
}

// Without a run ID, failure_context reads the latest run; without a
// location, its only failure.
func TestFailureContextOfTheLatestRun(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, ".axx", "runs")
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"axx.yaml", "axx-packs.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n"}[f]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write := func(id, report string, age time.Duration) {
		p := filepath.Join(runs, id+".json")
		if err := os.WriteFile(p, []byte(report), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("20260927T100000-aaaa", `{"failures": [{"location": "features/quotes.feature:9"}, {"location": "features/quotes.feature:20"}]}`, time.Hour)
	write("20260927T110000-bbbb", `{"failures": [{"location": "features/tracking.feature:14", "error": "expected 200"}]}`, time.Minute)
	cs := session(t, dir)

	out := call(t, cs, "failure_context", map[string]any{})
	if out["location"] != "features/tracking.feature:14" || out["runId"] != "20260927T110000-bbbb" {
		t.Errorf("the latest run's failure: %v", out)
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "failure_context", Arguments: map[string]any{"runId": "20260927T100000-aaaa"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Content)
	if !res.IsError || !strings.Contains(string(b), "2 failures") || !strings.Contains(string(b), "features/quotes.feature:20") {
		t.Errorf("a run of two failures lists them: %s", b)
	}
}
