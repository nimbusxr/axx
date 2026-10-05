// Package mobilecore is the mobile-core pack: native Android and iOS apps,
// used as people use them. It carries out app-core's steps for apps on
// phones (launch, tap, fill, check), and has the steps of phones alone
// (deep links, the background, swipes, dialogs, notifications); the
// mobile-android and mobile-ios packs register apps, run them on devices
// through Appium, and keep each scenario's device to itself.
package mobilecore

import (
	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// Name is the pack's name.
const Name = "mobile-core"

// Pack returns the mobile-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `Native mobile apps, used as people use them: launched, or opened with a deep link straight at the screen a scenario tests; their controls tapped and filled, found by the names people see; what they show checked, waiting as apps take their time.

The steps every app takes, on a phone or a desktop, are ` + "`app-core`" + `'s, which this pack carries out for phones; its own are the steps of phones alone: deep links, the background, swipes, the system's dialogs and notifications. The apps are registered, and run, by the platform packs: ` + "`mobile-android`" + ` (Android emulators and devices) and, later, ` + "`mobile-ios`" + `. They drive the apps through [Appium](https://appium.io), which axx downloads on first use. **Each scenario has a device to itself, and a clean app:** the platform pack leases a device for the scenario and resets the app, and what it can see, before the scenario starts. Every step names its app, so a scenario can drive two, and use a web app too.

Controls are found by the name people see: a button's text, a field's label, a list item's text. When no name tells a control apart, ` + "`id=`" + ` (the Android resource ID or the iOS accessibility identifier) or ` + "`xpath=`" + ` finds it.`

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         Name,
		Namespace:    Name,
		Doc:          packDoc,
		Requires:     []string{appcore.Name},
		Params:       []core.ParamType{directionParam},
		ConfigSchema: []byte(configSchema),
		Steps:        append(appcore.Paced(append(steps(), dialogSteps()...)), notificationSteps()...),
	}
}
