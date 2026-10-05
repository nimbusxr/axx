// Package appcore is the app-core pack: the steps every app takes, used as
// people use it, whatever it runs on (ADR 0012): launched, its controls
// tapped or clicked and filled, found by the names people see, what it shows
// checked. The families under it run the apps: mobile-core (with
// mobile-android and mobile-ios) and desktop-core (with desktop-macos,
// desktop-windows and desktop-linux), whose platform packs register them.
package appcore

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "app-core"

// Pack returns the app-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = `Apps, used as people use them, whatever they run on: launched, their controls tapped or clicked and filled, found by the names people see; what they show checked, waiting as apps take their time.

An app is registered by the pack of the platform it runs on: ` + "`mobile-android`" + ` and ` + "`mobile-ios`" + ` for phones, ` + "`desktop-macos`" + `, ` + "`desktop-windows`" + ` and ` + "`desktop-linux`" + ` for desktops (` + "`the courier android app with the following properties:`" + `, ` + "`the depot macos app with the following properties:`" + `). Every other step names the app the same way on every platform (` + "`the courier app`" + `, ` + "`the depot app`" + `), so a feature for a desktop app lists its registration for each OS and runs unchanged on each: a registration for a platform the machine cannot run does nothing there. An app runs on one platform in a scenario; two apps may run on two, and a scenario can use a web app too.

**Each scenario has a clean app:** the platform pack resets the app, and what it keeps, before the scenario starts. Controls are found by the name people see: a button's text, a field's label, a list item's or a row's text. When no name tells a control apart, ` + "`id=`" + ` or ` + "`xpath=`" + ` finds it. An app's files are a folder of the files pack whose ` + "`owner`" + ` is the app (` + "`app:depot`" + `).`

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      Name,
		Namespace: Name,
		Doc:       packDoc,
		Params:    []core.ParamType{controlParam},
		Steps:     append(Paced(steps()), checkSteps()...),
	}
}

// Init makes the apps' files reachable as folders of the files pack: a
// folder whose owner is an app is the app's files, wherever it runs.
func (pack) Init(_ context.Context, s *core.Suite) error {
	s.SetFileOwner("app", func(sc *core.Scenario, name, path string) (core.Files, error) {
		app, err := Get(sc, name)
		if err != nil {
			return nil, err
		}
		return app.Family.Files(sc, app, path)
	})
	return nil
}

// Paced makes each action (a launch, a tap, a typed field) pause after it in
// a watched run, for the run's slowdown (axx run --watch --slowdown 500ms),
// so a person can follow the app as web-core's slowdown lets them follow a
// browser.
func Paced(steps []core.StepDef) []core.StepDef {
	for i := range steps {
		if steps[i].Keyword != "When" || steps[i].Run == nil {
			continue
		}
		run := steps[i].Run
		steps[i].Run = func(sc *core.Scenario, a core.Args) error {
			err := run(sc, a)
			if watching, d := sc.Suite().Watching(); err == nil && watching && d > 0 {
				select {
				case <-sc.Context().Done():
				case <-time.After(d):
				}
			}
			return err
		}
	}
	return steps
}

// since is when the steps came to be, in mobile-core; sinceClicked when
// "clicked" came, with desktops.
const (
	since        = "0.1.5"
	sinceClicked = "0.2.0"
)

// on runs a step on the named app: its family carries it out, and its error
// masks the scenario's secrets.
func on(sc *core.Scenario, name string, fn func(app *App) error) error {
	app, err := Get(sc, name)
	if err != nil {
		return err
	}
	return secrets.Hide(sc, fn(app))
}

func steps() []core.StepDef {
	press := func(word, since string, examples ...string) core.StepDef {
		return core.StepDef{
			ID: Name + "." + word, Keyword: "When", Since: since,
			Expr: "the {string} {control} is " + word + " in the {word} app",
			Doc: "Press a control, found by the name people see, or by an `id=` or `xpath=` selector: a tap on a phone, a click on a desktop. " +
				"\"tapped\" and \"clicked\" are the same step: either word works on every platform.",
			Examples: examples,
			Run: func(sc *core.Scenario, a core.Args) error {
				name, k := secrets.Expand(sc, a.String(0)), a.Value(1).(Kind)
				return on(sc, a.String(2), func(app *App) error { return app.Family.Press(sc, app, k, name) })
			},
		}
	}
	return []core.StepDef{
		{
			ID: Name + ".launch", Keyword: "When", Since: since,
			Expr: "the {word} app is launched",
			Doc: "Launch the app, reset for the scenario by its platform's pack (see the pack), " +
				"or bring it to the front if it is already running.",
			Examples: []string{"When the courier app is launched", "When the depot app is launched"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return on(sc, a.String(0), func(app *App) error { return app.Family.Launch(sc, app) })
			},
		},
		{
			ID: Name + ".restart", Keyword: "When", Since: since,
			Expr:     "the {word} app is restarted",
			Doc:      "Stop the app and start it again, keeping what it stored: what survives a restart, like a sign-in, is still there.",
			Examples: []string{"When the courier app is restarted"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return on(sc, a.String(0), func(app *App) error { return app.Family.Restart(sc, app) })
			},
		},
		press("tapped", since, `When the "Sign in" button is tapped in the courier app`, `When the "PX-MOB-9401" list item is tapped in the courier app`),
		press("clicked", sinceClicked, `When the "Register" button is clicked in the depot app`, `When the "PX-DSK-4138" row is clicked in the depot app`),
		{
			ID: Name + ".fill", Keyword: "When", Since: since,
			Expr:     "the {string} field in the {word} app is filled with {string}",
			Doc:      "Replace what a field holds with a text, as typing it would. `${env:..}` values are secrets: masked in logs and failures.",
			Examples: []string{`When the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, text := secrets.Expand(sc, a.String(0)), secrets.Expand(sc, a.String(2))
				return on(sc, a.String(1), func(app *App) error { return app.Family.Fill(sc, app, name, text) })
			},
		},
		{
			ID: Name + ".scroll", Keyword: "When", Since: since,
			Expr:     "the {string} {control} is scrolled into view in the {word} app",
			Doc:      "Scroll until a control shows: on a desktop, every area it is in, the outermost first; on a phone, the screen, down first, then up.",
			Examples: []string{`When the "PX-MOB-9412" list item is scrolled into view in the courier app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, k := secrets.Expand(sc, a.String(0)), a.Value(1).(Kind)
				return on(sc, a.String(2), func(app *App) error { return app.Family.ScrollIntoView(sc, app, k, name) })
			},
		},
	}
}

var screenshotName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]*$`)

func checkSteps() []core.StepDef {
	state := func(word, doc string, enabled bool) core.StepDef {
		return core.StepDef{
			ID: Name + ".control." + word, Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} {control} is " + word + " in the {word} app",
			Doc:      doc,
			Examples: []string{`Then the "Sign in" button is ` + word + ` in the courier app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, name, k := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(1)), a.Value(2).(Kind)
				return on(sc, a.String(3), func(app *App) error { return app.Family.Enabled(sc, app, k, name, enabled, wait) })
			},
		}
	}
	return []core.StepDef{
		{
			ID: Name + ".shows", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {word} app shows {string}",
			Doc:      "Check that the app shows a text: in a control's text or label, whitespace collapsed. It waits for the text (10 seconds, or `within`).",
			Examples: []string{`Then the courier app shows "Today's deliveries"`, `Then within 20s the courier app shows "Delivered"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, text := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(2))
				return on(sc, a.String(1), func(app *App) error { return app.Family.Shows(sc, app, text, wait) })
			},
		},
		{
			ID: Name + ".hides", Keyword: "Then", Since: since, Absence: true,
			Expr:     "[[within {duration} ]]the {word} app does not show {string}",
			Doc:      "Check that the app does not show a text, or no longer does: it waits for the text to go (10 seconds, or `within`).",
			Examples: []string{`Then the courier app does not show "PX-MOB-9401"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, text := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(2))
				return on(sc, a.String(1), func(app *App) error { return app.Family.DoesNotShow(sc, app, text, wait) })
			},
		},
		{
			ID: Name + ".control.shown", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} {control} is shown in the {word} app",
			Doc:      "Check that the app shows a control: it waits for it (10 seconds, or `within`).",
			Examples: []string{`Then the "Mark delivered" button is shown in the courier app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, name, k := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(1)), a.Value(2).(Kind)
				return on(sc, a.String(3), func(app *App) error { return app.Family.Shown(sc, app, k, name, wait) })
			},
		},
		state("enabled", "Check that a control can be used: it waits for it (10 seconds, or `within`).", true),
		state("disabled", "Check that a control is shown but cannot be used, like a button until a form is complete.", false),
		{
			ID: Name + ".field.value", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} field in the {word} app has the value {string}",
			Doc:      "Check what a field holds: it waits for the value (10 seconds, or `within`). A password field holds its dots.",
			Examples: []string{`Then the "Courier ID" field in the courier app has the value "CR-LEJ-12"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, name, want := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(1)), secrets.Expand(sc, a.String(3))
				return on(sc, a.String(2), func(app *App) error { return app.Family.Value(sc, app, name, want, wait) })
			},
		},
		{
			ID: Name + ".screenshot", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {word} app looks like the {string} screenshot",
			Doc: "Check that the app looks like its screenshot, pixel by pixel (anti-aliasing aside), once it has settled.\n\n" +
				"- Each platform draws apps its own way, so each device or desktop has its own screenshot, named after it " +
				"(`delivered.android-parcels-pixel.png`); see the platform's family pack for where they are kept.\n" +
				"- Without one, the step takes it and fails: look at it, and keep it.\n" +
				"- When the app looks different, the step attaches the screenshot, the app and their difference.\n" +
				"- The check waits for the app to settle and look right: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the courier app looks like the "delivered" screenshot`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, name := cloudstep.Wait(a, 0), a.String(2)
				if !screenshotName.MatchString(name) {
					return fmt.Errorf("a screenshot's name is letters, digits, dots, dashes, underscores and spaces, not %q", name)
				}
				return on(sc, a.String(1), func(app *App) error { return app.Family.LooksLike(sc, app, name, wait) })
			},
		},
	}
}
