package engine

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func TestPackConfigMergesOpenAPILevels(t *testing.T) {
	cases := []struct {
		name   string
		levels map[string]string
		packs  map[string]json.RawMessage
		want   string // packs.rest after merging ("" = absent)
	}{
		{"no openapi section", nil, map[string]json.RawMessage{"rest": json.RawMessage(`{"tls":{"verify":true}}`)}, `{"tls":{"verify":true}}`},
		{
			"top-level only",
			map[string]string{"validation.request": "WARN"},
			nil,
			`{"openapi":{"levels":{"validation.request":"WARN"}}}`,
		},
		{
			"merged under pack levels",
			map[string]string{"validation.request": "WARN", "validation.response": "INFO"},
			map[string]json.RawMessage{"rest": json.RawMessage(`{"tls":{"verify":true},"openapi":{"levels":{"validation.request":"IGNORE"}}}`)},
			`{"openapi":{"levels":{"validation.request":"IGNORE","validation.response":"INFO"}},"tls":{"verify":true}}`,
		},
		{
			"malformed rest section is left alone",
			map[string]string{"validation": "WARN"},
			map[string]json.RawMessage{"rest": json.RawMessage(`[1]`)},
			`[1]`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{OpenAPI: config.OpenAPI{Levels: c.levels}, Packs: c.packs}
			got := packConfig(cfg)
			if c.want == "" {
				if _, ok := got["rest"]; ok {
					t.Fatalf("unexpected rest section %s", got["rest"])
				}
				return
			}
			var a, b any
			if err := json.Unmarshal(got["rest"], &a); err != nil {
				t.Fatalf("rest section %s: %v", got["rest"], err)
			}
			_ = json.Unmarshal([]byte(c.want), &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("rest section %s, want %s", got["rest"], c.want)
			}
		})
	}
	// The configured map itself is not modified.
	packs := map[string]json.RawMessage{"rest": json.RawMessage(`{}`)}
	packConfig(&config.Config{OpenAPI: config.OpenAPI{Levels: map[string]string{"validation": "WARN"}}, Packs: packs})
	if string(packs["rest"]) != `{}` {
		t.Fatalf("packs modified: %s", packs["rest"])
	}
}
