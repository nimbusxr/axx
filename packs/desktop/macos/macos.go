// Package desktopmacos is the desktop-macos pack: macOS apps, through AX,
// the accessibility tree screen readers read, with the pointer and the
// keyboard as a person uses them (ADR 0011, 0012).
package desktopmacos

import (
	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Name is the pack's name.
const Name = "desktop-macos"

const since = "0.2.0"

// Pack returns the desktop-macos pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `macOS apps, through AX, the accessibility tree VoiceOver reads: native apps (AppKit, SwiftUI), web views (Tauri, Wails), Electron, Qt, Java and Flutter, as they ship.

macOS asks that the app running axx be allowed to control the computer: the terminal, the IDE or the CI agent at the top of axx's process tree, in System Settings > Privacy & Security > Accessibility (Device Control & Data Access, on macOS 27), and Screen Recording for screenshots. ` + "`axx doctor`" + ` checks both, and names the app. Hosted macOS runners allow both already.

**Each scenario has the desktop, and a clean app.** Desktop scenarios take the Mac's desktop in turns: while they run, the screen, the pointer and the keyboard are theirs, so leave the Mac be, or run them in CI. Before the app starts, its home (` + "`HOME`" + ` and ` + "`CFFIXED_USER_HOME`" + `, which Foundation follows; Java's ` + "`user.home`" + `) is emptied, and its preferences, which macOS keeps for the user whatever the home, are emptied too: the domain of the app's bundle identifier, and the domains its registration names (` + "`preferences`" + `: Qt's ` + "`QSettings`" + ` names its own, after the app's organization). Your own are kept aside during the run and put back when it ends (or as the next run starts, after one that was stopped). Window restoration is off. The keychain is not reset: a sign-in an app keeps there outlives the scenario. An app's ` + "`./`" + ` files are its home's ` + "`Library/Application Support`" + `.`

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      Name,
		Namespace: Name,
		Doc:       packDoc,
		Requires:  []string{desktopcore.Name},
		Steps:     steps(),
		Checks:    checks(),
	}
}

// driver runs macOS apps.
type driver struct{}

func (driver) Platform() string { return "macos" }

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".app", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} macos app with the following properties:",
			Doc: "Register a macOS app. The app starts when a step launches it. On another OS, the registration does nothing, " +
				"so a feature registers the app once for each OS it runs on.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "app", Takes: "the app: a `.app` of the project, an installed app's bundle identifier (`com.apple.TextEdit`), or an executable (a file of the project, or a command)", Required: true},
					{Name: "args", Takes: "the arguments it starts with, a quoted one with spaces; a file of the project is relative to axx.yaml"},
					{Name: "env.<name>", Takes: "an environment variable it starts with, like `env.PARCELS_API`"},
					{Name: "preferences", Takes: "the preferences domains the app keeps its settings in beyond its bundle identifier's, like `com.parcels-example.Depot desk` (Qt's `QSettings` names its own), separated by commas: emptied before each scenario"},
					{Name: "locale", Takes: "its language and region, like `de-DE`", Default: "en-US"},
					{Name: "timezone", Takes: "its time zone, like `Europe/Berlin`", Default: "UTC"},
					{Name: "owner", Takes: "`system` for an app the system runs already, like Finder (`com.apple.finder`): the scenario reads it as it runs, and never resets, launches or stops it; the windows it shows during the scenario are closed as the scenario ends"},
				},
			},
			Examples: []string{"Given the depot macos app with the following properties:\n  | app | ../depot-desk/appkit/build/Depot desk.app |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				app, err := desktopcore.Parse(sc, "macos", a.String(0), a.Table, "preferences")
				if err != nil {
					return err
				}
				if _, err := preferenceDomains(app); err != nil {
					return err
				}
				app.Driver = driver{}
				return desktopcore.Register(sc, app)
			},
		},
	}
}
