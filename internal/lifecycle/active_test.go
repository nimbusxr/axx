package lifecycle

import (
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func TestSelectActive(t *testing.T) {
	tagged := func(app config.App, tags ...string) config.App {
		app.Active.Tags = tags
		return app
	}
	apps := config.Apps{
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
		{"no match keeps untagged apps", on, [][]string{{"@other"}}, []string{"db"}, ""},
		{"disabled app never selected", on, [][]string{{"@api"}}, []string{"db", "kafka", "api"}, ""},
		{"tags are case sensitive", on, [][]string{{"@API"}}, []string{"db"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectActive(apps, tt.active, tt.tags)
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
