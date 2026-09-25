package lifecycle

import (
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

// SelectActive returns the names of the apps to start for a run, in
// declaration order.
//
// Without active startup (active.enabled false) every enabled app is
// selected. Otherwise the tags of all selected scenarios are pooled: an
// enabled app is selected when it lists no active.tags or when any of its
// tags occurs in the pool, and the apps it depends on come with it. When the
// scenarios have no tags at all, onNoTags decides: "fallback" (the default)
// selects every enabled app, "error" fails.
//
// Tags compare with their leading "@"; tags in axx.yaml may omit it.
func SelectActive(apps config.Apps, active config.Active, scenarioTags [][]string) ([]string, error) {
	enabled := make([]string, 0, len(apps))
	for _, a := range apps {
		if a.IsEnabled() {
			enabled = append(enabled, a.Name)
		}
	}
	if !active.Enabled {
		return enabled, nil
	}

	pool := make(map[string]bool)
	for _, tags := range scenarioTags {
		for _, t := range tags {
			if t = normalizeTag(t); t != "" {
				pool[t] = true
			}
		}
	}
	if len(pool) == 0 {
		switch active.OnNoTags {
		case "", "fallback":
			return enabled, nil
		case "error":
			return nil, configErr(CodeNoActiveTags, "active startup cannot choose apps: the selected scenarios have no tags").
				WithHint("tag the scenarios (for example @api), or set active.onNoTags: fallback to start every enabled app")
		default:
			return nil, configErr(CodeInvalidConfig, "active.onNoTags is %q", active.OnNoTags).
				WithHint(`use "fallback" or "error"`)
		}
	}

	var picked []string
	for _, a := range apps {
		if a.IsEnabled() && wantsApp(a, pool) {
			picked = append(picked, a.Name)
		}
	}
	return withDependencies(apps, picked), nil
}

// wantsApp reports whether the pooled scenario tags call for app.
func wantsApp(app config.App, pool map[string]bool) bool {
	if len(app.Active.Tags) == 0 {
		return true
	}
	for _, t := range app.Active.Tags {
		if pool[normalizeTag(t)] {
			return true
		}
	}
	return false
}

// normalizeTag trims t and gives it a leading "@".
func normalizeTag(t string) string {
	t = strings.TrimSpace(t)
	if t == "" || strings.HasPrefix(t, "@") {
		return t
	}
	return "@" + t
}
