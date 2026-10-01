package lifecycle

import (
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

// SelectActive returns the names of the apps to start for a run, in
// declaration order.
//
// Without active startup (active.enabled false) every enabled app is
// selected. Otherwise the tags of all selected scenarios are pooled, and so
// are the packs whose steps they use: an enabled app is selected when it
// lists no active.tags and no active.packs, when any of its tags occurs in
// the pool, or when the scenarios use any of its packs; the apps it depends
// on come with it. When the scenarios have no tags at all and an app waits
// for tags, onNoTags decides: "fallback" (the default) selects the apps that
// wait for tags, "error" fails.
//
// Tags compare with their leading "@"; tags in axx.yaml may omit it.
func SelectActive(apps config.Apps, active config.Active, scenarioTags [][]string, usedPacks []string) ([]string, error) {
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
	packs := make(map[string]bool, len(usedPacks))
	for _, p := range usedPacks {
		packs[p] = true
	}
	anyTagsWaited := false
	for _, a := range apps {
		if a.IsEnabled() && len(a.Active.Tags) > 0 {
			anyTagsWaited = true
		}
	}
	tagsMet := false
	if len(pool) == 0 && anyTagsWaited {
		switch active.OnNoTags {
		case "", "fallback":
			tagsMet = true
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
		if a.IsEnabled() && wantsApp(a, pool, packs, tagsMet) {
			picked = append(picked, a.Name)
		}
	}
	return withDependencies(apps, picked), nil
}

// wantsApp reports whether the pooled scenario tags, or the packs the
// scenarios use, call for app. tagsMet stands for scenarios that have no
// tags at all, under onNoTags: fallback.
func wantsApp(app config.App, pool, packs map[string]bool, tagsMet bool) bool {
	if len(app.Active.Tags) == 0 && len(app.Active.Packs) == 0 {
		return true
	}
	if tagsMet && len(app.Active.Tags) > 0 {
		return true
	}
	for _, t := range app.Active.Tags {
		if pool[normalizeTag(t)] {
			return true
		}
	}
	for _, p := range app.Active.Packs {
		if packs[p] {
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
