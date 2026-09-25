package rest

import "github.com/nimbusxr/axx/internal/oaslevel"

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

// ParseLevels parses a key -> level-name map.
func ParseLevels(m map[string]string) (Levels, error) { return oaslevel.ParseMap(m) }
