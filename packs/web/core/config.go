package webcore

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// configSchema is the pack's section of axx.yaml, packs.web-core.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "watch": {"type": "boolean", "description": "Show the browsers in windows on your screen, instead of running them headless."},
    "slowdown": {"type": "string", "description": "Wait this long after each browser action, to follow what happens (like 500ms)."},
    "pauseOnFailure": {"type": "boolean", "description": "Pause a failed scenario where it failed, in Playwright's Inspector, until you resume it; the browsers show."},
    "traces": {"type": "string", "enum": ["failed", "always", "never"], "description": "Which scenarios keep a Playwright trace in .axx/web/traces (default failed)."},
    "videos": {"type": "string", "enum": ["failed", "always", "never"], "description": "Which scenarios keep a video in .axx/web/videos (default never)."},
    "testIdAttribute": {"type": "string", "description": "The attribute testid= selectors look at (default data-testid)."},
    "scriptErrors": {"type": "string", "enum": ["report", "fail"], "description": "What errors in the pages' scripts do: report lists them with a failed scenario (the default), fail fails the scenario."}
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	Watch           bool   `json:"watch"`
	Slowdown        string `json:"slowdown"`
	PauseOnFailure  bool   `json:"pauseOnFailure"`
	Traces          string `json:"traces"`
	Videos          string `json:"videos"`
	TestIDAttribute string `json:"testIdAttribute"`
	ScriptErrors    string `json:"scriptErrors"`
}

// settings are the run's web settings, from Config.
type settings struct {
	watch    bool
	slowdown time.Duration
	pause    bool
	traces   string // failed, always or never
	videos   string
	// testIDAttribute is what testid= selectors look at.
	testIDAttribute string
	// failOnScriptErrors fails a scenario whose pages' scripts had errors.
	failOnScriptErrors bool
}

var keeps = []string{"failed", "always", "never"}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		return parseConfig(c)
	})
}

func parseConfig(c Config) (*settings, error) {
	st := &settings{watch: c.Watch, pause: c.PauseOnFailure, traces: "failed", videos: "never"}
	if c.Slowdown != "" {
		d, err := time.ParseDuration(c.Slowdown)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("packs.web-core.slowdown: %q is not a duration like 500ms", c.Slowdown)
		}
		st.slowdown = d
	}
	for _, k := range []struct {
		name, value string
		to          *string
	}{{"traces", c.Traces, &st.traces}, {"videos", c.Videos, &st.videos}} {
		if k.value == "" {
			continue
		}
		if !slices.Contains(keeps, k.value) {
			return nil, fmt.Errorf("packs.web-core.%s: %q is not one of %s", k.name, k.value, strings.Join(keeps, ", "))
		}
		*k.to = k.value
	}
	st.testIDAttribute = c.TestIDAttribute
	if st.testIDAttribute == "" {
		st.testIDAttribute = "data-testid"
	}
	switch c.ScriptErrors {
	case "", "report":
	case "fail":
		st.failOnScriptErrors = true
	default:
		return nil, fmt.Errorf("packs.web-core.scriptErrors: %q is not one of report, fail", c.ScriptErrors)
	}
	return st, nil
}

// keep reports whether a scenario keeps what the policy (failed, always,
// never) says.
func keep(policy string, failed bool) bool {
	return policy == "always" || (policy == "failed" && failed)
}
