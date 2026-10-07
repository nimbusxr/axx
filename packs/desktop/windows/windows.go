// Package desktopwindows is the desktop-windows pack: Windows apps, through
// UI Automation and, for Java apps, the Java Access Bridge, with the pointer
// and the keyboard as a person uses them (ADR 0011, 0012).
package desktopwindows

import (
	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Name is the pack's name.
const Name = "desktop-windows"

const since = "0.2.0"

// Pack returns the desktop-windows pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `Windows apps, through UI Automation, the accessibility tree Narrator reads, and the Java Access Bridge for Java apps: native apps (Windows Forms, WPF, WinUI), web views (WebView2: Tauri, Wails), Electron, Qt, Java and Flutter, as they ship.

axx runs apps on the signed-in user's desktop: run it in a terminal there, or as a scheduled task for that user (` + "`schtasks /it`" + `), not as a service or over SSH, which have no desktop. Hosted Windows runners have one. ` + "`axx doctor`" + ` checks it.

**Each scenario has the desktop, and a clean app.** Desktop scenarios take the machine's desktop in turns: while they run, the screen, the pointer and the keyboard are theirs. Before the app starts, its home is emptied: ` + "`USERPROFILE`" + `, and ` + "`APPDATA`" + `, ` + "`LOCALAPPDATA`" + `, ` + "`TEMP`" + ` and ` + "`TMP`" + ` in it, which apps' data folders follow (Java's ` + "`user.home`" + ` too); the registry keys the registration names are emptied too, your own kept aside during the run and put back when it ends. The profile's own folders, like Documents, are Windows' to resolve, and stay yours. Java apps are read through the Java Access Bridge of the Java that runs them, which axx switches on for the launch. An app's ` + "`./`" + ` files are its home's ` + "`AppData\\Roaming`" + `.`

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

// driver runs Windows apps.
type driver struct{}

func (driver) Platform() string { return "windows" }

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".app", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} windows app with the following properties:",
			Doc: "Register a Windows app. The app starts when a step launches it. On another OS, the registration does nothing, " +
				"so a feature registers the app once for each OS it runs on.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "app", Takes: "the app: an `.exe` of the project, or a command", Required: true},
					{Name: "args", Takes: "the arguments it starts with, a quoted one with spaces; a file of the project is relative to axx.yaml"},
					{Name: "env.<name>", Takes: "an environment variable it starts with, like `env.PARCELS_API`"},
					{Name: "registry", Takes: "the keys under `HKEY_CURRENT_USER` the app keeps its settings in, like `Software\\Parcels\\Depot desk`, separated by commas: emptied before each scenario"},
					{Name: "locale", Takes: "its language and region: Windows has none of an app's own, so the machine's apply, and the run says so"},
					{Name: "timezone", Takes: "its time zone: Windows has none of an app's own, so the machine's applies, and the run says so"},
					{Name: "owner", Takes: "`system` for an app the system runs already, like File Explorer (`explorer.exe`): the scenario reads it as it runs, and never resets, launches or stops it; the windows it shows during the scenario are closed as the scenario ends"},
				},
			},
			Examples: []string{"Given the depot windows app with the following properties:\n  | app      | ..\\depot-desk\\winforms\\bin\\Release\\net8.0-windows\\DepotDesk.exe |\n  | registry | Software\\Parcels\\Depot desk                                       |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				app, err := desktopcore.Parse(sc, "windows", a.String(0), a.Table, "registry")
				if err != nil {
					return err
				}
				app.Driver = driver{}
				return desktopcore.Register(sc, app)
			},
		},
	}
}
