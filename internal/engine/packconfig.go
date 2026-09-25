package engine

import (
	"encoding/json"
	"maps"

	"github.com/nimbusxr/axx/internal/config"
)

// packConfig returns the configuration sections handed to packs (the packs
// map of axx.yaml). The top-level openapi section configures the rest
// pack: its levels reach it as packs.rest.openapi.levels, underneath any
// levels set there. A malformed rest section is passed on unchanged for the
// pack to report.
func packConfig(cfg *config.Config) map[string]json.RawMessage {
	if len(cfg.OpenAPI.Levels) == 0 {
		return cfg.Packs
	}
	out := maps.Clone(cfg.Packs)
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	section := map[string]json.RawMessage{}
	if raw := out["rest"]; len(raw) > 0 && json.Unmarshal(raw, &section) != nil {
		return out
	}
	openapi := map[string]json.RawMessage{}
	if raw := section["openapi"]; len(raw) > 0 && json.Unmarshal(raw, &openapi) != nil {
		return out
	}
	levels := maps.Clone(cfg.OpenAPI.Levels)
	if raw := openapi["levels"]; len(raw) > 0 {
		var over map[string]string
		if json.Unmarshal(raw, &over) != nil {
			return out
		}
		maps.Copy(levels, over)
	}
	var err error
	if openapi["levels"], err = json.Marshal(levels); err != nil {
		return out
	}
	if section["openapi"], err = json.Marshal(openapi); err != nil {
		return out
	}
	if out["rest"], err = json.Marshal(section); err != nil {
		return cfg.Packs
	}
	return out
}
