// Package oaslevel holds OpenAPI validation levels: the ERROR, WARN, INFO and
// IGNORE a finding is reported at, and the maps from validation keys
// (validation.request.body.schema.required, ...) to levels. The REST pack
// uses them for the service under test's own contract and the mock pack for
// mocked dependencies' contracts; the settings of the two are separate.
package oaslevel

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

// Level is how an OpenAPI validation finding is reported.
type Level int

// Levels, from the least to the most severe.
const (
	Ignore Level = iota
	Info
	Warn
	Error
)

func (l Level) String() string {
	switch l {
	case Ignore:
		return "IGNORE"
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	default:
		return "ERROR"
	}
}

// Supported is the list shown in errors.
const Supported = "ERROR (or FAIL), WARN, INFO, IGNORE"

// Parse parses a level name. FAIL is an alias of ERROR. Names are
// case-insensitive.
func Parse(s string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ERROR", "FAIL":
		return Error, nil
	case "WARN":
		return Warn, nil
	case "INFO":
		return Info, nil
	case "IGNORE":
		return Ignore, nil
	}
	return 0, fmt.Errorf("invalid level %q; supported levels: %s", s, Supported)
}

// Levels maps validation keys to levels.
type Levels map[string]Level

// ParseMap parses a key -> level-name map.
func ParseMap(m map[string]string) (Levels, error) {
	out := make(Levels, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lv, err := Parse(m[k])
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		out[strings.TrimSpace(k)] = lv
	}
	return out, nil
}

// Merge returns l overlaid with over (over wins for equal keys).
func (l Levels) Merge(over Levels) Levels {
	out := make(Levels, len(l)+len(over))
	maps.Copy(out, l)
	maps.Copy(out, over)
	return out
}

// Find returns the level of the most specific configured key that is the
// key itself or one of its dotted prefixes (validation.request.body.schema.required
// falls back to validation.request.body.schema, validation.request.body,
// validation.request and validation), and false when none is configured.
func (l Levels) Find(key string) (Level, bool) {
	for k := key; k != ""; {
		if lv, ok := l[k]; ok {
			return lv, true
		}
		i := strings.LastIndexByte(k, '.')
		if i < 0 {
			break
		}
		k = k[:i]
	}
	return Error, false
}

// Resolve is Find with ERROR when no key is configured.
func (l Levels) Resolve(key string) Level {
	lv, _ := l.Find(key)
	return lv
}
