// Package desktest is the depot desk's journey (examples/parcels/depot-desk)
// as the desktop packs' integration tests walk it on each OS, with the
// packs' steps, as a feature does.
package desktest

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/files"
)

// Journey works a build of the depot desk with the packs' steps: every step
// a desktop app takes, a restart that keeps the day's arrivals, and a new
// scenario that starts clean. platform is the OS's registration word
// (macos, windows, linux) and pack its pack.
//
// AXX_DESK is the registration's app and AXX_DESK_ARGS its args;
// AXX_DESK_DATA is the folder, in the app's file context, it keeps its
// arrivals in (./Depot desk), AXX_DESK_REGISTRY the registry key it keeps
// its settings in (Windows), and AXX_DESK_PREFERENCES its preferences
// domains when they are not its bundle's (macOS: Qt run by Python).
//
// Toolkits give some controls other roles: AXX_DESK_PRINT_LABEL is the kind
// of Print label ("switch", or "checkbox"), AXX_DESK_LINK that of Handover
// rules ("link", or "text"), AXX_DESK_ARRIVAL that of an arrival ("list
// item", or "row" where a list is a table), AXX_DESK_TAB and AXX_DESK_ROW
// those of a tab and an expected parcel's row, AXX_DESK_RADIO that of a
// radio button (GTK 4.14 reports them as checkboxes), AXX_DESK_MENU and
// AXX_DESK_MENU_ITEM those of the Depot menu and its item. AXX_DESK_FIELD
// names the Reference field for a toolkit that gives it no name (Flutter
// on macOS: xpath=//AXTextField).
//
// What a toolkit does not give fails its step, saying so:
// AXX_DESK_ENABLED=unreported, whether a button is enabled;
// AXX_DESK_MENU_ENABLED=unreported, whether a menu item is (Flutter's own
// menus); AXX_DESK_VALUE=unreported, what a field holds (Java on Linux);
// AXX_DESK_MENU_ITEMS=unreachable, an open menu's items (GTK 4.14's
// popovers); AXX_DESK_POSITIONS=none, places on the screen (Flutter on
// Linux).
func Journey(t *testing.T, platform string, pack core.Pack) {
	t.Helper()
	app := os.Getenv("AXX_DESK")
	if app == "" {
		t.Skip("AXX_DESK names a depot desk build")
	}
	kind := func(env, otherwise string) string { return cmp.Or(os.Getenv(env), otherwise) }
	printLabel, link := kind("AXX_DESK_PRINT_LABEL", "switch"), kind("AXX_DESK_LINK", "link")
	arrival, tab, row := kind("AXX_DESK_ARRIVAL", "list item"), kind("AXX_DESK_TAB", "tab"), kind("AXX_DESK_ROW", "row")
	menu, menuItemKind := kind("AXX_DESK_MENU", "menu"), kind("AXX_DESK_MENU_ITEM", "menu item")
	field, radio := kind("AXX_DESK_FIELD", "Reference"), kind("AXX_DESK_RADIO", "radio button")
	enabledReported := os.Getenv("AXX_DESK_ENABLED") != "unreported"
	menuEnabledReported := os.Getenv("AXX_DESK_MENU_ENABLED") != "unreported"
	valueReported := os.Getenv("AXX_DESK_VALUE") != "unreported"
	menuItems := os.Getenv("AXX_DESK_MENU_ITEMS") != "unreachable"
	positions := os.Getenv("AXX_DESK_POSITIONS") != "none"

	h := cloudtest.New(t, appcore.Pack(), desktopcore.Pack(), pack, files.Pack())
	h.Start(&core.Plan{})
	ended := false
	end := func() {
		ended = true
		if err := h.End("passed"); err != nil {
			t.Fatal(err)
		}
	}
	// A failed step ends the test: its scenario ends too, which stops the app.
	t.Cleanup(func() {
		if !ended {
			_ = h.End("failed")
		}
	})
	t.Cleanup(func() {
		if t.Failed() {
			for _, a := range h.Sink.Attachments {
				if a.MediaType == "text/plain" {
					t.Logf("%s:\n%s", a.Name, a.Body)
				}
			}
		}
	})
	register := func() {
		rows := [][]string{{"app", app}}
		if args := os.Getenv("AXX_DESK_ARGS"); args != "" {
			rows = append(rows, []string{"args", args})
		}
		if key := os.Getenv("AXX_DESK_REGISTRY"); key != "" {
			rows = append(rows, []string{"registry", key})
		}
		if domains := os.Getenv("AXX_DESK_PREFERENCES"); domains != "" {
			rows = append(rows, []string{"preferences", domains})
		}
		h.OK("the depot "+platform+" app with the following properties:", rows)
		if data := os.Getenv("AXX_DESK_DATA"); data != "" {
			h.OK("the desk folder with the following properties:", [][]string{{"owner", "app:depot"}, {"path", data}})
		}
	}
	// either runs a step that passes when the toolkit gives what it checks,
	// and fails saying why when it does not.
	either := func(gives bool, step, why string) {
		t.Helper()
		if gives {
			h.OK(step)
			return
		}
		_ = h.Fails(step, why)
	}
	button := func(name, state string) {
		t.Helper()
		either(enabledReported, `the "`+name+`" button is `+state+` in the depot app`, "does not say whether it is enabled")
	}
	click := func(kind, name string) {
		t.Helper()
		h.OK(`the "` + name + `" ` + kind + ` is clicked in the depot app`)
	}
	shows := func(text string) {
		t.Helper()
		h.OK(`the depot app shows "` + text + `"`)
	}
	key := func(k string) {
		t.Helper()
		h.OK("the " + k + " key is pressed in the depot app")
	}
	fill := func(text string) {
		t.Helper()
		h.OK(`the "` + field + `" field in the depot app is filled with "` + text + `"`)
	}
	value := func(want string) {
		t.Helper()
		either(valueReported, `the "`+field+`" field in the depot app has the value "`+want+`"`, "does not say what it holds")
	}
	menuItem := func(state string) {
		t.Helper()
		step := `within 2s the "Close day" ` + menuItemKind + ` is ` + state + ` in the depot app`
		if !menuItems {
			_ = h.Fails(step, `No `+menuItemKind+` named "Close day"`)
			return
		}
		either(menuEnabledReported, step, "does not say whether it is enabled")
	}
	draw := func(step string) {
		t.Helper()
		either(positions, step, "gives no places on the screen")
	}

	register()
	h.OK("the depot app is launched")
	h.OK(`the "Leipzig depot" image is shown in the depot app`)
	shows("No parcels registered yet")
	button("Register", "disabled")
	click(menu, "Depot")
	menuItem("disabled")
	key("Escape")

	fill("PX-DSK-4201")
	button("Register", "enabled")
	key("Escape")
	value("")

	fill("PX-DSK-4201")
	click("checkbox", "Fragile")
	click(radio, "Express")
	click(printLabel, "Print label")
	click("button", "Register")
	shows("Registered PX-DSK-4201: Express, fragile, label printed")
	value("")

	fill("PX-DSK-4202")
	key("Enter")
	shows("Registered PX-DSK-4202: Express, label printed")
	fill("PX-DSK-4201")
	key("Enter")
	shows("PX-DSK-4201 is already registered")
	key("Escape")

	click(arrival, "PX-DSK-4201")
	shows("PX-DSK-4201: Express, fragile")
	h.OK(`the "PX-DSK-4138" ` + row + ` is scrolled into view in the depot app`)
	click(row, "PX-DSK-4138")
	value("PX-DSK-4138")
	key("Escape")
	if os.Getenv("AXX_DESK_DATA") != "" {
		h.OK(`the "arrivals.json" file in the desk folder contains "PX-DSK-4202"`)
	} else {
		logArrivals(t, h, platform)
	}

	t.Log("restarting: the arrivals and the service level stay")
	h.OK("the depot app is restarted")
	shows("2 parcels registered today")
	h.OK(`the "PX-DSK-4202" ` + arrival + ` is shown in the depot app`)
	click(menu, "Depot")
	menuItem("enabled")
	if menuItems {
		click(menuItemKind, "Close day")
		shows("Day closed: 2 parcels handed over")
		click(menu, "Depot")
		menuItem("disabled")
	}
	key("Escape")

	click(tab, "Handover")
	shows("Not signed")
	draw(`the "Courier signature" element in the depot app is clicked at 40, 40`)
	if positions {
		shows("Signed")
		click("button", "Clear signature")
		shows("Not signed")
	}
	draw(`the pointer is dragged from 40, 30 to 140, 30 on the "Courier signature" element in the depot app`)
	if positions {
		shows("Signed")
	}
	click(link, "Handover rules")
	shows("Parcels are handed over to the courier at 18:00.")

	click(tab, "Arrivals")
	fill("PX-DSK-4203")
	key("Enter")
	shows("Registered PX-DSK-4203: Express")
	end()

	t.Log("a new scenario: the app starts clean")
	h.NewScenario()
	ended = false
	register()
	h.OK("the depot app is launched")
	shows("No parcels registered yet")
	// Its window, captured: the first time, the screenshot is taken.
	_ = h.Fails(`the depot app looks like the "fresh" screenshot`, `There was no "fresh.`)
	h.OK(`within 20s the depot app looks like the "fresh" screenshot`)
	h.OK(`the depot app does not show "PX-DSK-4203"`)
	fill("PX-DSK-4204")
	key("Enter")
	shows("Registered PX-DSK-4204: Standard")
	end()
}

// logArrivals logs where the app keeps its arrivals in its home, to name
// AXX_DESK_DATA.
func logArrivals(t *testing.T, h *cloudtest.Harness, platform string) {
	home := filepath.Join(h.Dir, ".axx", "desktop", platform, "depot")
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == "arrivals.json" {
			rel, _ := filepath.Rel(home, p)
			t.Logf("the arrivals are at ~/%s", strings.ReplaceAll(rel, string(filepath.Separator), "/"))
		}
		return nil
	})
}
