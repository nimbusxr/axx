// Package desktoplinux is the desktop-linux pack: Linux apps, through
// AT-SPI, in desktops of axx's own, with the pointer and the keyboard as a
// person uses them (ADR 0011, 0012).
package desktoplinux

import (
	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Name is the pack's name.
const Name = "desktop-linux"

const since = "0.2.0"

// Pack returns the desktop-linux pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `Linux apps, through AT-SPI, the accessibility tree Orca reads: native apps (GTK 3, GTK 4), web views (WebKitGTK: Tauri, Wails), Electron, Qt, Java and Flutter, as they ship, under X11.

**axx runs desktops of its own**, ` + "`desktops`" + ` at once (1 by default): each a virtual screen (Xvfb), with its own session bus and accessibility bus for each scenario, and its own view of the file system, in which an app's home is at the same path in every desktop. Nothing shows on your screen, and a feature never names a desktop: a scenario takes one, as a mobile scenario takes a device. It needs Xvfb, dbus-daemon and at-spi2-core (` + "`axx doctor`" + ` checks them); more than one desktop needs user and mount namespaces, which a host that forbids them does not give, and then desktop scenarios run one at a time.

**Each scenario has a desktop, and a clean app.** Before the app starts, its home is emptied, and it starts with ` + "`HOME`" + ` and the XDG folders in it; the scenario's session bus runs with them too, so the app's settings (dconf), keyring and portals start empty. axx switches on what toolkits need to be read: the assistive technology announcement, the accessibility bus's address on the screen (Qt 5), and java-atk-wrapper for Java apps where it is installed. Chromium (Electron, CEF) builds its tree from the environment, but leaves out what shows: an Electron app's registration starts it with ` + "`--force-renderer-accessibility`" + ` in its ` + "`args`" + `. An app's ` + "`./`" + ` files are its home's ` + "`.local/share`" + `.`

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "desktops": {"type": "integer", "minimum": 1, "maximum": 32, "description": "How many Linux desktops run at once: desktop scenarios beyond them wait for one (default 1). More than one needs user and mount namespaces."}
  }
}`

// Config is the pack's settings, packs.desktop-linux in axx.yaml.
type Config struct {
	Desktops int `json:"desktops"`
}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         Name,
		Namespace:    Name,
		Doc:          packDoc,
		Requires:     []string{desktopcore.Name},
		ConfigSchema: []byte(configSchema),
		Steps:        steps(),
		Checks:       checks(),
	}
}

// driver runs Linux apps.
type driver struct{}

func (driver) Platform() string { return "linux" }

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".app", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} linux app with the following properties:",
			Doc: "Register a Linux app. The app starts when a step launches it. On another OS, the registration does nothing, " +
				"so a feature registers the app once for each OS it runs on.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "app", Takes: "the app: an executable of the project (an AppImage too), or a command on PATH", Required: true},
					{Name: "args", Takes: "the arguments it starts with, a quoted one with spaces; a file of the project is relative to axx.yaml"},
					{Name: "env.<name>", Takes: "an environment variable it starts with, like `env.PARCELS_API`"},
					{Name: "locale", Takes: "its language and region, like `de-DE`", Default: "en-US"},
					{Name: "timezone", Takes: "its time zone, like `Europe/Berlin`", Default: "UTC"},
				},
			},
			Examples: []string{"Given the depot linux app with the following properties:\n  | app | ../depot-desk/tauri/src-tauri/target/release/depot-desk |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				app, err := desktopcore.Parse(sc, "linux", a.String(0), a.Table)
				if err != nil {
					return err
				}
				app.Driver = driver{}
				return desktopcore.Register(sc, app)
			},
		},
	}
}
