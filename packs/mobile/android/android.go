// Package mobileandroid is the mobile-android pack: Android apps on
// emulators and devices, which Appium's UiAutomator2 driver runs. Each
// scenario leases a device of its own and starts from a clean app.
package mobileandroid

import (
	"context"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// Name is the pack's name.
const Name = "mobile-android"

const since = "0.1.5"

// Pack returns the mobile-android pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `Android apps, on emulators axx starts or on devices adb lists, driven by [Appium](https://appium.io)'s UiAutomator2 driver, which axx downloads on first use. It needs the Android SDK (ANDROID_HOME): its emulator and its devices (AVDs), and adb.

**Each scenario has a device to itself.** axx starts emulators of a device (AVD) as scenarios need them, read-only so that several run at once and nothing a scenario changes outlives it, up to ` + "`devices`" + ` at once (1 by default: Android scenarios then take turns). A scenario leases a device for its whole run.

**Each scenario starts from a clean app.** Before the app starts, its data is cleared (and with it what it stored, its permissions and its notifications), the permissions the registration names are granted, and the device's language, time zone and location are set. There is no switch that skips it: a scenario is one journey, and that journey is its own.`

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
			ID: "mobile-android.app", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} android app with the following properties:",
			Doc:  "Register an Android app, and the device it runs on. The app starts when a step launches it or opens a link in it.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "apk", Takes: "the app: an APK, a file of the project"},
					{Name: "package", Takes: "the app's package; with no `apk`, an app the device has"},
					{Name: "activity", Takes: "the activity the app starts with", Default: "the APK's launcher activity"},
					{Name: "device", Takes: "an emulator's device (AVD) axx starts, like `Pixel_9`, or a device adb lists, by its serial", Required: true},
					{Name: "permissions", Takes: "the permissions the app has from the start, like `POST_NOTIFICATIONS, ACCESS_FINE_LOCATION`"},
					{Name: "locale", Takes: "the device's language and region, like `de-DE`", Default: "en-US"},
					{Name: "timezone", Takes: "the device's time zone, like `Europe/Berlin`", Default: "UTC"},
					{Name: "location", Takes: "where an emulator says it is: a latitude and a longitude, like `51.3397, 12.3731`", Default: "where an emulator starts (37.4219983, -122.084), whatever the scenario before set"},
					{Name: "appium", Takes: "an Appium server of the project's own or a device farm's, which then runs the device, in place of `device`"},
					{Name: "capability.<name>", Takes: "an Appium capability for that server, like a device farm's options, as text or JSON"},
				},
			},
			Examples: []string{"Given the courier android app with the following properties:\n  | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |\n  | device      | Pixel_9                                                      |\n  | permissions | POST_NOTIFICATIONS                                           |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				app, err := parseApp(sc, func(v string) string { return secrets.Expand(sc, v) }, a.String(0), a.Table)
				if err != nil {
					return err
				}
				return mobilecore.Register(sc, &mobilecore.App{Name: app.name, Platform: runner{app: app}})
			},
		},
		{
			ID: "mobile-android.back", Keyword: "When", Since: since,
			Expr:     "the {word} app's back button is pressed",
			Doc:      "Press Android's back button: back to the previous screen, or out of the app from its first.",
			Examples: []string{"When the courier app's back button is pressed"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return mobilecore.OnDevice(sc, a.String(0), func(ctx context.Context, d mobilecore.Device) error {
					return d.Session().Back(ctx)
				})
			},
		},
	}
}
