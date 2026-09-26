package rest

import (
	"slices"

	"github.com/nimbusxr/axx/internal/oaslevel"
)

// Level is how an OpenAPI validation finding is reported.
type Level = oaslevel.Level

// Levels, from the least to the most severe.
const (
	LevelIgnore = oaslevel.Ignore
	LevelInfo   = oaslevel.Info
	LevelWarn   = oaslevel.Warn
	LevelError  = oaslevel.Error
)

// supportedLevels is the list shown in errors.
const supportedLevels = oaslevel.Supported

// ParseLevel parses a level name. FAIL is an alias of ERROR. Names are
// case-insensitive.
func ParseLevel(s string) (Level, error) { return oaslevel.Parse(s) }

// Levels maps validation keys to levels; Resolve returns the level of the
// most specific configured key covering a finding key, and ERROR when none
// is configured.
type Levels = oaslevel.Levels

// ParseLevels parses a key -> level-name map. Every key must be one this
// pack's findings have, or a prefix of such keys (validation.request.body);
// any other is an error that names the closest keys.
func ParseLevels(m map[string]string) (Levels, error) { return oaslevel.ParseMap(m, levelKeys) }

// levelKeys are the keys levels can be set on: the keys of knownKeys, with
// {keyword} standing for every schema keyword a finding is keyed by, and
// {in} for every parameter location; and their prefixes. A newer schema
// keyword name suggests the draft-4 one the findings use (const -> enum).
var levelKeys = func() *oaslevel.Keys {
	patterns := make([]string, len(knownKeys))
	for i, k := range knownKeys {
		patterns[i] = k.key
	}
	keywords := []string{"unknownError"}
	aliases := map[string]string{}
	for name, kw := range schemaKeywords {
		if !slices.Contains(keywords, kw) {
			keywords = append(keywords, kw)
		}
		if name != kw {
			aliases[name] = kw
		}
	}
	return oaslevel.NewKeys(patterns, map[string][]string{
		"{keyword}": keywords,
		"{in}":      {"path", "query", "header", "cookie"},
	}, aliases)
}()
