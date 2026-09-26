package rest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/rest"
)

// TestTopLevelLevelKeysAreChecked checks that the levels of axx.yaml's
// top-level openapi section, which reach the pack as its own, have their
// keys checked when the run starts; their levels take any case.
func TestTopLevelLevelKeysAreChecked(t *testing.T) {
	start := func(levels map[string]string) error {
		t.Helper()
		cfg := &config.Config{Dir: t.TempDir(), OpenAPI: config.OpenAPI{Levels: levels}}
		e, err := engine.New(engine.Options{Config: cfg, Packs: engine.Ordered(map[string]core.Pack{"rest": rest.Pack()})})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.Close(context.Background()) })
		return e.Init(t.Context())
	}
	if err := start(map[string]string{"validation.response": "warn", "validation.request.body.schema.maximum": "Ignore"}); err != nil {
		t.Fatalf("known keys: %v", err)
	}
	err := start(map[string]string{"validation.response": "warn", "validation.response.body.shema.required": "IGNORE"})
	if err == nil || !strings.Contains(err.Error(), `openapi.levels: unknown OpenAPI validation key "validation.response.body.shema.required"; `+
		"did you mean validation.response.body.schema.required?") {
		t.Fatalf("Init: %v", err)
	}
}
