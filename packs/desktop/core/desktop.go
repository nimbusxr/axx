// Package desktopcore is the desktop-core pack: the family of app-core's
// packs that run apps on desktops (ADR 0011, 0012). It carries out
// app-core's steps for desktop apps, has the steps only desktops take (a key
// pressed, a click at a place, a drag), and what every OS shares: the
// desktop each scenario has, the app's clean home, waiting, failures and
// screenshots. The packs of the OSes (desktop-macos, desktop-windows,
// desktop-linux) register apps and drive them, each through its
// accessibility API.
package desktopcore

import (
	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// Name is the pack's name.
const Name = "desktop-core"

const since = "0.2.0"

// Pack returns the desktop-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `Desktop apps on macOS, Windows and Linux, used as people use them, through the operating system's accessibility tree: what screen readers read. axx starts the app as it ships, with no build for testing, and clicks and types with the pointer and the keyboard, as a person does.

An app is registered for each OS it runs on, by that OS's pack (` + "`desktop-macos`" + `, ` + "`desktop-windows`" + `, ` + "`desktop-linux`" + `), and every other step is ` + "`app-core`" + `'s: a feature lists the app once for each OS and runs unchanged on each. This pack adds what only desktops do: a key pressed, a click at a place on a control, a drag.

**Each scenario has the desktop, and a clean app.** A desktop has one screen, one pointer and one keyboard focus, so on macOS and Windows desktop scenarios take the machine's desktop in turns (other runs on the machine wait too), while other scenarios run alongside them; on Linux, axx runs desktops of its own. Before an app starts in a scenario, its home (a folder of the project's ` + "`.axx/desktop`" + `) is emptied, and what its OS keeps of it elsewhere is reset; after the scenario, the app and every process it started are stopped. An app's files are a folder of the files pack whose ` + "`owner`" + ` is the app: ` + "`./`" + ` is where its OS keeps an app's data, ` + "`~/`" + ` its home.`

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         Name,
		Namespace:    Name,
		Doc:          packDoc,
		Requires:     []string{appcore.Name},
		ConfigSchema: []byte(configSchema),
		Steps:        appcore.Paced(steps()),
	}
}

// on runs a step on the named app as it runs; its failure carries what the
// app showed.
func on(sc *core.Scenario, name string, fn func(p Process) error) error {
	app, err := desktopApp(sc, name)
	if err != nil {
		return err
	}
	p, err := process(sc, app, false)
	if err != nil {
		return err
	}
	return secrets.Hide(sc, failing(sc, name, p, func() error { return fn(p) }))
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".key", Keyword: "When", Since: since,
			Expr: "the {word} key is pressed in the {word} app",
			Doc: "Press a key, with the keys held with it, in the app's focused window: `Enter`, `Escape`, `Tab`, `Control+Shift+S`, " +
				"as the web pack names them. `ControlOrMeta+Z` is Command on macOS and Control elsewhere, so a feature runs unchanged on each OS.",
			Examples: []string{"When the Escape key is pressed in the depot app", "When the ControlOrMeta+S key is pressed in the depot app"},
			Run: func(sc *core.Scenario, a core.Args) error {
				key := a.String(0)
				return on(sc, a.String(1), func(p Process) error {
					if err := p.Front(); err != nil {
						return err
					}
					return p.Key(key)
				})
			},
		},
		{
			ID: Name + ".click-at", Keyword: "When", Since: since,
			Expr: "the {string} {control} in the {word} app is clicked at {int}, {int}",
			Doc: "Click at a place on a control, in points from its top left, for what an app draws rather than names: " +
				"a signature pad, a map, a canvas.",
			Examples: []string{`When the "Courier signature" element in the depot app is clicked at 40, 40`},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, k, x, y := secrets.Expand(sc, a.String(0)), a.Value(1).(appcore.Kind), a.Int(3), a.Int(4)
				return on(sc, a.String(2), func(p Process) error {
					c, err := usable(sc, a.String(2), p, k, name)
					if err != nil {
						return err
					}
					return p.ClickAt(c, float64(x), float64(y))
				})
			},
		},
		{
			ID: Name + ".drag", Keyword: "When", Since: since,
			Expr: "the pointer is dragged from {int}, {int} to {int}, {int} on the {string} {control} in the {word} app",
			Doc: "Press the pointer at a place on a control, move it to another in steps, as a hand does, and release it there; " +
				"places are in points from the control's top left.",
			Examples: []string{`When the pointer is dragged from 40, 80 to 300, 80 on the "Courier signature" element in the depot app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				x1, y1, x2, y2 := a.Int(0), a.Int(1), a.Int(2), a.Int(3)
				name, k := secrets.Expand(sc, a.String(4)), a.Value(5).(appcore.Kind)
				return on(sc, a.String(6), func(p Process) error {
					c, err := usable(sc, a.String(6), p, k, name)
					if err != nil {
						return err
					}
					return p.Drag(c, float64(x1), float64(y1), float64(x2), float64(y2))
				})
			},
		},
	}
}
