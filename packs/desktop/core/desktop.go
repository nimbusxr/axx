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
	"errors"

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

**Each scenario has the desktop, and a clean app.** A desktop has one screen, one pointer and one keyboard focus, so on macOS and Windows desktop scenarios take the machine's desktop in turns (other runs on the machine wait too), while other scenarios run alongside them; on Linux, axx runs desktops of its own. Before an app starts in a scenario, its home (a folder of the project's ` + "`.axx/desktop`" + `) is emptied, and what its OS keeps of it elsewhere is reset; after the scenario, the app and every process it started are stopped. An app's files are a folder of the files pack whose ` + "`owner`" + ` is the app: ` + "`./`" + ` is where its OS keeps an app's data, ` + "`~/`" + ` its home.

**Screenshots** are of the app's front window, as it draws itself, one for each platform and display scale (` + "`arrivals.darwin@2x.png`" + `). A focused field's text cursor is left out of them, as the web pack leaves it out of its screenshots: it blinks.`

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         Name,
		Namespace:    Name,
		Doc:          packDoc,
		Requires:     []string{appcore.Name},
		Params:       []core.ParamType{anchorParam},
		ConfigSchema: []byte(configSchema),
		Steps:        appcore.Paced(steps()),
		Hooks:        []core.Hook{{ID: Name + ".trace", Phase: core.AfterStep, Run: traceStepHook}},
	}
}

// on runs a step on the named app as it runs; its failure carries what the
// app showed.
func on(sc *core.Scenario, name string, fn func(p Process) error) error {
	name = appcore.Named(sc, name)
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
				return clickAt(sc, a, TopLeft)
			},
		},
		{
			ID: Name + ".click-at-anchor", Keyword: "When", Since: "0.2.2",
			Expr: "the {string} {control} in the {word} app is clicked at {int}, {int} from its {anchor}",
			Doc: "Click at a place on a control, in points from its middle or a corner, for what keeps its place there as the control " +
				"grows with the window or the screen: a picture an app shows in its middle, a button in its bottom right corner. " +
				"Places run right and down, so one left of or above the point is negative.",
			Examples: []string{`When the "Courier signature" element in the depot app is clicked at -40, -20 from its bottom right`},
			Run: func(sc *core.Scenario, a core.Args) error {
				return clickAt(sc, a, a.Value(5).(Anchor))
			},
		},
		{
			ID: Name + ".drag", Keyword: "When", Since: since,
			Expr: "the pointer is dragged from {int}, {int} to {int}, {int} on the {string} {control} in the {word} app",
			Doc: "Press the pointer at a place on a control, move it to another in steps, as a hand does, and release it there; " +
				"places are in points from the control's top left.",
			Examples: []string{`When the pointer is dragged from 40, 30 to 140, 30 on the "Courier signature" element in the depot app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				return drag(sc, a, TopLeft, 4)
			},
		},
		{
			ID: Name + ".drag-from-anchor", Keyword: "When", Since: "0.2.2",
			Expr: "the pointer is dragged from {int}, {int} to {int}, {int} from the {anchor} of the {string} {control} in the {word} app",
			Doc: "Drag on a control as a hand does, with places in points from its middle or a corner, for what keeps its place there " +
				"as the control grows with the window or the screen. Places run right and down, so one left of or above the point is negative.",
			Examples: []string{`When the pointer is dragged from -60, 0 to 60, 0 from the middle of the "Courier signature" element in the depot app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				return drag(sc, a, a.Value(4).(Anchor), 5)
			},
		},
	}
}

// clickAt clicks at the step's place on its control, from the anchor.
func clickAt(sc *core.Scenario, a core.Args, from Anchor) error {
	name, k, x, y := secrets.Expand(sc, a.String(0)), a.Value(1).(appcore.Kind), a.Int(3), a.Int(4)
	app := appcore.Named(sc, a.String(2))
	return on(sc, app, func(p Process) error {
		_, err := again(sc, app, p, k, name, func(c Control) error {
			return p.ClickAt(c, from, float64(x), float64(y))
		})
		return offControl(err, x, y, from, name, k, app)
	})
}

// offControl is a failure for a place a step names off its control, which
// the driver measured: a click there would reach something else.
func offControl(err error, x, y int, from Anchor, name string, k appcore.Kind, app string) error {
	var off *Off
	if !errors.As(err, &off) {
		return err
	}
	return core.Failf("%d, %d from its %s is off the %s %s in the %s app, which is %.0f by %.0f points: a click there reaches something else",
		x, y, from, quoted(name), k.Noun, app, off.Width, off.Height)
}

// drag drags between the step's places on its control, from the anchor; the
// control's name is the argument at named.
func drag(sc *core.Scenario, a core.Args, from Anchor, named int) error {
	x1, y1, x2, y2 := a.Int(0), a.Int(1), a.Int(2), a.Int(3)
	name, k, app := secrets.Expand(sc, a.String(named)), a.Value(named+1).(appcore.Kind), appcore.Named(sc, a.String(named+2))
	return on(sc, app, func(p Process) error {
		_, err := again(sc, app, p, k, name, func(c Control) error {
			return p.Drag(c, from, float64(x1), float64(y1), float64(x2), float64(y2))
		})
		return offControl(err, x1, y1, from, name, k, app)
	})
}
