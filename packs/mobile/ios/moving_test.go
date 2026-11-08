package mobileios

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium/appiumtest"
)

// movedOn is the deliveries list a row further on, as a list still scrolling lands: a parcel
// above PX-MOB-9401 came into view, and each row shows the parcel of the row before it.
var movedOn = strings.NewReplacer(
	"PX-MOB-9401", "PX-MOB-9400",
	"Karl-Liebknecht-Str. 12, 04107 Leipzig", "Augustusplatz 9, 04109 Leipzig",
	"PX-MOB-9402", "PX-MOB-9401",
	"Prager Str. 3, 04103 Leipzig", "Karl-Liebknecht-Str. 12, 04107 Leipzig",
)

// A list still scrolling as the screen is read has another row where the tapped one was by the
// time it is tapped: the tap finds the row where it landed.
func TestARowOfAListStillScrollingIsTappedWhereItLands(t *testing.T) {
	sc := screens(t)
	sc["deliveries-moved-on"] = movedOn.Replace(sc["deliveries"])
	app := appiumtest.Start(t, appiumtest.App{
		Screens: sc,
		Start:   "deliveries",
		Taps: map[string]string{
			at(t, sc, "deliveries-moved-on", mobilecore.RoleListItem, "PX-MOB-9401"): "delivery",
			at(t, sc, "deliveries-moved-on", mobilecore.RoleListItem, "PX-MOB-9400"): "delivered",
		},
	})
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Lands("deliveries-moved-on")
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	if got := app.Screen(); got != "delivery" {
		t.Fatalf("the tap opened %s, not PX-MOB-9401's delivery: %v", got, app.Commands())
	}
}

// A control still moving, as a list settling after a scroll moves its rows, takes no tap: the tap
// waits until it lands.
func TestAControlStillMovingIsTappedOnceItLands(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Show("deliveries")
	app.Drifting(600 * time.Millisecond)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	if got := app.Screen(); got != "delivery" || slices.ContainsFunc(app.Commands(), func(c string) bool { return strings.HasPrefix(c, "lost tap") }) {
		t.Fatalf("on %s after %v", got, app.Commands())
	}
}

// What a node shows is in its locator, so that the driver finds no other node in its place: its
// own label or value, or else its first visible descendant's.
func TestALocatorSaysWhatTheNodeShows(t *testing.T) {
	s, err := parseSource(`<AppiumAUT>` +
		`<XCUIElementTypeCell type="XCUIElementTypeCell" visible="true" x="0" y="200" width="393" height="60">` +
		`<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="PX-MOB-9401" label="PX-MOB-9401" visible="true" x="40" y="211" width="116" height="21"/>` +
		`</XCUIElementTypeCell>` +
		`<XCUIElementTypeTextField type="XCUIElementTypeTextField" value="CR-LEJ-12" visible="true" x="20" y="300" width="353" height="44"/>` +
		`</AppiumAUT>`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range s.Nodes {
		got = append(got, n.Value)
	}
	want := []string{
		`//XCUIElementTypeCell[@x="0" and @y="200" and @width="393" and @height="60" and .//*[@label="PX-MOB-9401"]]`,
		`//XCUIElementTypeStaticText[@x="40" and @y="211" and @width="116" and @height="21" and @label="PX-MOB-9401"]`,
		`//XCUIElementTypeTextField[@x="20" and @y="300" and @width="353" and @height="44" and @value="CR-LEJ-12"]`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("locators:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
