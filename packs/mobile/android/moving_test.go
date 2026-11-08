package mobileandroid

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
	"Prager Str. 3, 01069 Dresden", "Karl-Liebknecht-Str. 12, 04107 Leipzig",
)

// A list still scrolling as the screen is read has another row where the tapped one was by the
// time it is tapped, as the file picker's did on a busy emulator (the neighbouring photo was
// attached): the tap finds the row where it landed.
func TestARowOfAListStillScrollingIsTappedWhereItLands(t *testing.T) {
	sc := screens(t)
	sc["deliveries-moved-on"] = movedOn.Replace(sc["deliveries"])
	app := appiumtest.Start(t, appiumtest.App{
		Screens: sc,
		Start:   "deliveries",
		Taps: map[string]string{
			at(t, sc, "deliveries-moved-on", "PX-MOB-9401"): "delivery",
			at(t, sc, "deliveries-moved-on", "PX-MOB-9400"): "delivered",
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

// A control still moving, as the file picker's roots do while its drawer slides in, takes no tap:
// the tap waits until it lands.
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
// own text or description, or else its first descendant's, quoted as XPath quotes it.
func TestALocatorSaysWhatTheNodeShows(t *testing.T) {
	s, err := parseSource(`<hierarchy width="1080" height="2400">` +
		`<android.view.View class="android.view.View" text="" clickable="true" bounds="[0,534][1080,750]" displayed="true">` +
		`<android.widget.TextView class="android.widget.TextView" text="Hanna's &quot;fragile&quot; parcel" bounds="[48,576][900,648]" displayed="true"/>` +
		`</android.view.View>` +
		`<android.widget.Button class="android.widget.Button" text="Sign in" content-desc="Sign in to Parcels" bounds="[48,800][500,900]" displayed="true"/>` +
		`<android.view.View class="android.view.View" text="" bounds="[0,0][10,10]" displayed="true"/>` +
		`</hierarchy>`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range s.Nodes {
		got = append(got, n.Value)
	}
	want := []string{
		`//android.view.View[@bounds="[0,534][1080,750]" and .//*[@text=concat("Hanna's ", '"', "fragile", '"', " parcel")]]`,
		`//android.widget.TextView[@bounds="[48,576][900,648]" and @text=concat("Hanna's ", '"', "fragile", '"', " parcel")]`,
		`//android.widget.Button[@bounds="[48,800][500,900]" and @content-desc="Sign in to Parcels"]`,
		`//android.view.View[@bounds="[0,0][10,10]"]`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("locators:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
