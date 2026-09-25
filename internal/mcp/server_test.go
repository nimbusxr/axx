package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	if len(tools.Tools) < 9 || len(tools.Tools) > 10 {
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
