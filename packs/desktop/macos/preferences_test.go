package desktopmacos

import (
	"slices"
	"testing"

	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

func TestPreferenceDomains(t *testing.T) {
	app := func(prefs string) *desktopcore.App {
		return &desktopcore.App{Name: "depot", Extra: map[string]string{"preferences": prefs}}
	}
	got, err := preferenceDomains(app(" com.parcels-example.Depot desk , example.parcels.depotdesk.qt,"))
	if want := []string{"com.parcels-example.Depot desk", "example.parcels.depotdesk.qt"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
	if got, err := preferenceDomains(app("")); err != nil || len(got) != 0 {
		t.Errorf("none named: %q, %v", got, err)
	}
	for _, bad := range []string{"NSGlobalDomain", "-g", "/Users/clerk/Library/Preferences/com.apple.finder", "~/x", ".hidden"} {
		if _, err := preferenceDomains(app(bad)); err == nil {
			t.Errorf("%q: emptied", bad)
		}
	}
}
