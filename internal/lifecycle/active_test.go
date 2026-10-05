package lifecycle

import (
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func TestSelectActive(t *testing.T) {
	tagged := func(app config.Service, tags ...string) config.Service {
		app.Active.Tags = tags
		return app
	}
	apps := config.Services{
		dep("db"),                       // no tags: always
		tagged(dep("kafka"), "@events"), // with @
		tagged(dep("api", "kafka"), "api", "@web"), // without @; needs kafka
		tagged(dep("admin"), "@admin"),
		disabled(tagged(dep("legacy"), "@api")),
	}
	all := []string{"db", "kafka", "api", "admin"}
	on := config.Active{Enabled: true}

	tests := []struct {
		name     string
		active   config.Active
		tags     [][]string
		want     []string
		wantCode string
	}{
		{"disabled selection starts all enabled", config.Active{}, [][]string{{"@admin"}}, all, ""},
		{"no scenarios, fallback by default", on, nil, all, ""},
		{"untagged scenarios, explicit fallback", config.Active{Enabled: true, OnNoTags: "fallback"}, [][]string{{}, {}}, all, ""},
		{"untagged scenarios, error", config.Active{Enabled: true, OnNoTags: "error"}, [][]string{{}}, nil, CodeNoActiveTags},
		{"unknown onNoTags", config.Active{Enabled: true, OnNoTags: "maybe"}, nil, nil, CodeInvalidConfig},
		{"tag match with @ in config", on, [][]string{{"@events"}}, []string{"db", "kafka"}, ""},
		{"config tag without @ matches", on, [][]string{{"@api"}}, []string{"db", "kafka", "api"}, ""},
		{"dependencies included", on, [][]string{{"@web"}}, []string{"db", "kafka", "api"}, ""},
		{"scenario tags without @ normalized", on, [][]string{{"admin"}}, []string{"db", "admin"}, ""},
		{"union across scenarios", on, [][]string{{"@admin"}, {"@smoke", "@events"}}, []string{"db", "kafka", "admin"}, ""},
		{"no match keeps untagged services", on, [][]string{{"@other"}}, []string{"db"}, ""},
		{"disabled service never selected", on, [][]string{{"@api"}}, []string{"db", "kafka", "api"}, ""},
		{"tags are case sensitive", on, [][]string{{"@API"}}, []string{"db"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectActive(apps, tt.active, tt.tags, nil)
			if tt.wantCode != "" {
				wantCode(t, err, tt.wantCode)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("SelectActive = %q, want %q", got, tt.want)
			}
		})
	}
}

// A service can wait for the packs the scenarios use, like an emulator for the
// scenarios with Android steps: no tag needed.
func TestSelectActiveByUses(t *testing.T) {
	usedBy := func(app config.Service, packs ...string) config.Service {
		app.Active.Uses = packs
		return app
	}
	tagged := func(app config.Service, tags ...string) config.Service {
		app.Active.Tags = tags
		return app
	}
	apps := config.Services{
		dep("db"),
		usedBy(dep("emulator"), "mobile-android"),
		tagged(dep("portal"), "@web"),
		usedBy(tagged(dep("stub", "db"), "@stub"), "mobile-ios"),
	}
	on := config.Active{Enabled: true}
	tests := []struct {
		name   string
		active config.Active
		tags   [][]string
		packs  []string
		want   []string
	}{
		{"a pack the scenarios use", on, [][]string{{"@checkout"}}, []string{"rest", "mobile-android"}, []string{"db", "emulator"}},
		{"no pack, a tag", on, [][]string{{"@web"}}, []string{"rest"}, []string{"db", "portal"}},
		{"a tag or a pack", on, [][]string{{"@x"}}, []string{"mobile-ios"}, []string{"db", "stub"}},
		{"untagged scenarios fall back for tagged services", on, [][]string{{}}, []string{"mobile-android"}, []string{"db", "emulator", "portal", "stub"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectActive(apps, tt.active, tt.tags, tt.packs)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("SelectActive = %q, want %q", got, tt.want)
			}
		})
	}
	// Services that wait for packs alone need no tags: untagged scenarios are no error.
	onlyPacks := config.Services{dep("db"), usedBy(dep("emulator"), "mobile-android")}
	got, err := SelectActive(onlyPacks, config.Active{Enabled: true, OnNoTags: "error"}, [][]string{{}}, []string{"rest"})
	if err != nil || !slices.Equal(got, []string{"db"}) {
		t.Errorf("SelectActive = %q, %v", got, err)
	}
}
