package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedFilesUpToDate fails when a task's generated files (Dockerfiles,
// compose files, test.sh, the initial manifest) are stale: run
// `go run ./cmd/evals sync`.
func TestGeneratedFilesUpToDate(t *testing.T) {
	stale, err := syncTasks(filepath.Join("..", ".."), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("stale generated files (run `go run ./cmd/evals sync`):\n  %s", strings.Join(stale, "\n  "))
	}
}

// TestWorkspaceCopies keeps the starting project's copies of the service's
// contract and stubs identical to the originals in app/.
func TestWorkspaceCopies(t *testing.T) {
	root := filepath.Join("..", "..")
	pairs := map[string]string{
		"workspace/openapi.yaml": "app/openapi.yaml",
		"tasks/init-first-feature/environment/workspace/openapi.yaml":          "app/openapi.yaml",
		"workspace/infra/address-service/mappings/postcode-deliverable.json":   "app/wiremock/mappings/postcode-deliverable.json",
		"workspace/infra/address-service/mappings/postcode-undeliverable.json": "app/wiremock/mappings/postcode-undeliverable.json",
		"tasks/init-first-feature/environment/workspace/docs/registration.md":  "workspace/docs/registration.md",
	}
	for cp, orig := range pairs {
		a, err := os.ReadFile(filepath.Join(root, cp))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, orig))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from %s: copy it again", cp, orig)
		}
	}
}

func TestApplyCondition(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(p, []byte("FROM x\n"+conditionMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyCondition(p, Condition{Name: "both", AgentsMD: true, Skills: true, MCP: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "RUN project-setup finish --agents-md --skills\n") {
		t.Fatalf("Dockerfile:\n%s", b)
	}
}
