package coveragecore

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// minimumSchema is a part's, or the whole run's, minimums: an object's
// members, which its description follows.
const minimumSchema = `"type": "object", "additionalProperties": false, "properties": {` +
	`"lines": {"type": "number", "minimum": 0, "maximum": 100}, ` +
	`"branches": {"type": "number", "minimum": 0, "maximum": 100}, ` +
	`"functions": {"type": "number", "minimum": 0, "maximum": 100}}`

// configSchema is the pack's section of axx.yaml, packs.coverage-core.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "folder": {"type": "string", "description": "The folder the run's coverage reports go to, in the project: lcov.info, coverage-final.json, coverage-summary.json and run.json (default .axx/coverage)."},
    "minimum": {` + minimumSchema + `, "description": "The whole run's minimums, in percent: a run of every scenario under one exits 6."},
    "parts": {
      "type": "object",
      "description": "Settings of each measured part: a service, by its name in axx.yaml, or web, the web apps' JavaScript (web-coverage).",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "minimum": {` + minimumSchema + `, "description": "The part's minimums, in percent."},
          "paths": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Where the part's files are in the project, by where they were as it ran: {\"/app/\": \"../orders/\"}, the start of a path in its container mapped to a folder relative to the project. The longest start that matches wins; a relative path is relative to the service's dir."}
        }
      }
    }
  }
}`

// config is the pack's settings.
type config struct {
	Folder  string              `json:"folder"`
	Minimum minimum             `json:"minimum"`
	Parts   map[string]partConf `json:"parts"`
}

// minimum is a part's, or the whole run's, minimums in percent; nil is
// none.
type minimum struct {
	Lines     *float64 `json:"lines"`
	Branches  *float64 `json:"branches"`
	Functions *float64 `json:"functions"`
}

func (m minimum) set() bool { return m.Lines != nil || m.Branches != nil || m.Functions != nil }

type partConf struct {
	Minimum minimum           `json:"minimum"`
	Paths   map[string]string `json:"paths"`
}

func readConfig(settings func(pack string, v any) error) (config, error) {
	var c config
	if err := settings(Name, &c); err != nil {
		return c, err
	}
	if c.Folder == "" {
		c.Folder = filepath.Join(".axx", "coverage")
	}
	return c, nil
}

// minimums reports whether any minimum is set.
func (c config) minimums() bool {
	if c.Minimum.set() {
		return true
	}
	for _, p := range c.Parts {
		if p.Minimum.set() {
			return true
		}
	}
	return false
}

// pathOf maps a path a service named, as it ran, to where the file is on
// this machine: the longest start of it its part's paths name, else the
// path relative to its working directory.
func (c config) pathOf(project string, s core.CoverageService, p string) string {
	paths := c.Parts[s.Name].Paths
	starts := make([]string, 0, len(paths))
	for from := range paths {
		if strings.HasPrefix(p, from) {
			starts = append(starts, from)
		}
	}
	if len(starts) > 0 {
		from := slices.MaxFunc(starts, func(a, b string) int { return len(a) - len(b) })
		to := paths[from]
		if !filepath.IsAbs(to) {
			to = filepath.Join(project, to)
		}
		return filepath.Join(to, filepath.FromSlash(strings.TrimPrefix(p, from)))
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return filepath.FromSlash(p)
	}
	return filepath.Join(s.WorkDir, filepath.FromSlash(p))
}
