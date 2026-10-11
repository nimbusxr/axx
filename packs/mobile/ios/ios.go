// Package mobileios is the mobile-ios pack: iOS apps on simulators, which
// axx drives through WebDriverAgent. Each scenario leases a simulator of its
// own and starts from a clean app.
package mobileios

import (
	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// Name is the pack's name.
const Name = "mobile-ios"

const since = "0.1.5"

// Pack returns the mobile-ios pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `iOS apps, on simulators axx runs, driven through WebDriverAgent, the app on the simulator that drives yours: axx downloads the build of it that the [Appium](https://appium.io) project publishes on first use, and starts it on each simulator itself. It needs a Mac with Xcode and an iOS simulator runtime.

**Each scenario has a simulator to itself.** axx makes simulators as scenarios need them, up to ` + "`devices`" + ` at once (1 by default: iOS scenarios then take turns), each a clone, deleted when the run ends: for a device type, like iPhone 16, of axx's own simulator of it, which it sets up once and keeps in its cache (no scenario ever runs on it); for a simulator the project set up, of that one. A scenario leases a simulator for its whole run.

**Each scenario starts from a clean app.** Before the app starts, it is installed afresh (its data and its notifications go with the old one), the simulator's keychain and the app's permissions are reset, the permissions the registration names are granted, and the location is set. A dialog a scenario leaves unanswered, like a request for notifications, is dismissed as it ends: it would outlive the app and take the next scenario's taps. The app starts in the registration's language, region and time zone. There is no switch that skips it: a scenario is one journey, and that journey is its own.`

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         Name,
		Namespace:    Name,
		Doc:          packDoc,
		Requires:     []string{mobilecore.Name},
		ConfigSchema: []byte(configSchema),
		Steps:        steps(),
		Checks:       checks(),
	}
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mobile-ios.app", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} ios app with the following properties:",
			Doc:  "Register an iOS app, and the simulator it runs on. The app starts when a step launches it or opens a link in it.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "app", Takes: "the app: a simulator build (a `.app` folder, or it zipped), a file of the project"},
					{Name: "bundle id", Takes: "the app's bundle identifier; with no `app`, an app the simulator has", Default: "the app's own"},
					{Name: "device", Takes: "a device type axx makes simulators of, like `iPhone 16` or `iPhone 16, iOS 18.1` (the newest iOS Xcode has, by default); a simulator the project set up, by its name, which axx clones; or a simulator's UDID", Required: true},
					{Name: "permissions", Takes: "what the app may use from the start, like `location, photos` (`xcrun simctl help privacy` lists them); notifications cannot be granted: the app asks, and the scenario answers the dialog"},
					{Name: "locale", Takes: "the app's language and region, like `de-DE`", Default: "en-US"},
					{Name: "timezone", Takes: "the app's time zone, like `Europe/Berlin`", Default: "UTC"},
					{Name: "location", Takes: "where the simulator says it is: a latitude and a longitude, like `51.3397, 12.3731`", Default: "none"},
					{Name: "appium", Takes: "an Appium server of the project's own or a device farm's, which then runs the device, in place of `device`"},
					{Name: "capability.<name>", Takes: "an Appium capability for that server, like a device farm's options, as text or JSON"},
				},
			},
			Examples: []string{"Given the courier ios app with the following properties:\n  | app    | ../courier/ios/build/Build/Products/Debug-iphonesimulator/Courier.app |\n  | device | iPhone 16                                                             |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				app, err := parseApp(sc, func(v string) string { return secrets.Expand(sc, v) }, a.String(0), a.Table)
				if err != nil {
					return err
				}
				return mobilecore.Register(sc, &mobilecore.App{Name: app.name, Platform: runner{app: app}})
			},
		},
	}
}
