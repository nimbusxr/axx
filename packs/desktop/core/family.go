package desktopcore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// family carries out app-core's steps for desktop apps.
type family struct{}

// running is the step's app's process: started if start is true.
func running(sc *core.Scenario, a *appcore.App, start bool) (Process, error) {
	app, ok := a.Data.(*App)
	if !ok {
		return nil, fmt.Errorf("the %s app is not a desktop app", a.Name)
	}
	return process(sc, app, start)
}

func (family) Launch(sc *core.Scenario, a *appcore.App) error {
	if app, ok := a.Data.(*App); ok && app.System {
		return fmt.Errorf("the %s app is the system's: it runs already, and the scenario does not launch it", a.Name)
	}
	p, err := running(sc, a, true)
	if err != nil {
		return err
	}
	return opened(p)
}

// opened brings the app that opened to the front, with the pointer away
// from it, wherever the scenario before left it: an app that opens under a
// still pointer shows what is under it hovered (Preview a tooltip).
func opened(p Process) error {
	if err := p.Front(); err != nil {
		return err
	}
	return p.Away()
}

func (family) Restart(sc *core.Scenario, a *appcore.App) error {
	app, ok := a.Data.(*App)
	if !ok {
		return fmt.Errorf("the %s app is not a desktop app", a.Name)
	}
	if err := stop(sc, app); err != nil {
		return fmt.Errorf("cannot stop the %s app: %w", a.Name, err)
	}
	p, err := running(sc, a, true)
	if err != nil {
		return err
	}
	return opened(p)
}

func (family) Press(sc *core.Scenario, a *appcore.App, k appcore.Kind, name string) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		_, err := again(sc, a.Name, p, k, name, p.Click)
		return err
	})
}

// fillTries is how often a field is filled before the step gives up: a
// click while the window moves (a tab sliding in) misses the field.
const fillTries = 4

func (family) Fill(sc *core.Scenario, a *appcore.App, field, text string) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		var held string
		for range fillTries {
			c, err := again(sc, a.Name, p, appcore.Field, field, p.Click)
			if err != nil {
				return err
			}
			if err := p.Key("ControlOrMeta+A"); err != nil {
				return err
			}
			if err := p.Type(text); err != nil {
				return err
			}
			if _, reported := c.Value(); !reported {
				sc.Log("the %s field does not say what it holds: typed, and not read back", quoted(field))
				return nil
			}
			ok, err := waitUntil(sc, 2*time.Second, func() (bool, error) {
				held, _ = c.Value()
				return held == text, nil
			})
			if err != nil || ok {
				return err
			}
			sc.Log("the %s field holds %q, not the text typed: filling it again", quoted(field), held)
		}
		return core.Fail(fmt.Sprintf("The %s field in the %s app was filled %d times, and does not hold the text typed", quoted(field), a.Name, fillTries), text, held)
	})
}

func (family) ScrollIntoView(sc *core.Scenario, a *appcore.App, k appcore.Kind, name string) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		if _, ok := selector(name); ok {
			c, err := control(sc, a.Name, p, k, name, false, actionWait)
			if err != nil {
				return err
			}
			return p.ScrollIntoView(c)
		}
		// It waits for the control, as every action does: a web view builds
		// its tree a moment after its window shows.
		found, err := waitUntil(sc, actionWait, func() (bool, error) {
			c, err := p.ScrollTo(k, name)
			return c != nil, err
		})
		if err != nil {
			return err
		}
		if !found {
			return missing(a.Name, p, k, name)
		}
		return nil
	})
}

func (family) Shows(sc *core.Scenario, a *appcore.App, text string, wait time.Duration) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		ok, err := waitUntil(sc, wait, func() (bool, error) { return p.Shows(text) })
		if err != nil || ok {
			return err
		}
		return core.Fail(fmt.Sprintf("The %s app does not show %q", a.Name, text), text, cloudstep.Shown("texts", p.Texts(), 30))
	})
}

func (family) DoesNotShow(sc *core.Scenario, a *appcore.App, text string, wait time.Duration) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			shows, err := p.Shows(text)
			return !shows, err
		})
		if err != nil || ok {
			return err
		}
		return core.Failf("The %s app shows %q", a.Name, text)
	})
}

func (family) Shown(sc *core.Scenario, a *appcore.App, k appcore.Kind, name string, wait time.Duration) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		_, err := control(sc, a.Name, p, k, name, true, wait)
		return err
	})
}

func (family) Enabled(sc *core.Scenario, a *appcore.App, k appcore.Kind, name string, enabled bool, wait time.Duration) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	state := map[bool]string{true: "enabled", false: "disabled"}
	return failing(sc, a.Name, p, func() error {
		deadline := time.Now().Add(wait)
		c, err := control(sc, a.Name, p, k, name, true, wait)
		if err != nil {
			return err
		}
		if _, reported := c.Enabled(); !reported {
			return core.Failf("The %s %s in the %s app does not say whether it is enabled: its toolkit does not report it on this OS; check what pressing it does instead",
				quoted(name), k.Noun, a.Name)
		}
		var now bool
		ok, err := waitUntil(sc, time.Until(deadline), func() (bool, error) {
			now, _ = c.Enabled()
			return now == enabled, nil
		})
		if err != nil || ok {
			return err
		}
		return core.Fail(fmt.Sprintf("The %s %s in the %s app is %s", quoted(name), k.Noun, a.Name, state[now]), state[enabled], state[now])
	})
}

func (family) Value(sc *core.Scenario, a *appcore.App, field, want string, wait time.Duration) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return failing(sc, a.Name, p, func() error {
		deadline := time.Now().Add(wait)
		// A field's value is read wherever the field is: one scrolled away
		// is still the app's.
		c, err := control(sc, a.Name, p, appcore.Field, field, false, wait)
		if err != nil {
			return err
		}
		if _, reported := c.Value(); !reported {
			return core.Failf("The %s field in the %s app does not say what it holds: its toolkit does not report it on this OS; check what the app shows instead",
				quoted(field), a.Name)
		}
		var held string
		ok, err := waitUntil(sc, time.Until(deadline), func() (bool, error) {
			held, _ = c.Value()
			return held == want, nil
		})
		if err != nil || ok {
			return err
		}
		return core.Fail(fmt.Sprintf("The %s field in the %s app holds %q", quoted(field), a.Name, held), want, held)
	})
}

func (family) LooksLike(sc *core.Scenario, a *appcore.App, shot string, wait time.Duration) error {
	p, err := running(sc, a, false)
	if err != nil {
		return err
	}
	return looksLike(sc, a.Name, p, shot, wait)
}

// takesInput are the kinds of control an action waits to be enabled: those
// a person uses. A row, a list item or a text says nothing by being
// disabled (Java reports its table's rows so, and a click selects them).
var takesInput = map[string]bool{
	"button": true, "field": true, "checkbox": true, "radio button": true, "switch": true, "tab": true, "menu": true, "menu item": true,
}

// usable is the control an action takes: shown, and enabled when it is a
// control that takes input and the app says whether it is.
func usable(sc *core.Scenario, app string, p Process, k appcore.Kind, name string) (Control, error) {
	deadline := time.Now().Add(actionWait)
	c, err := control(sc, app, p, k, name, true, actionWait)
	if err != nil {
		return nil, err
	}
	if _, reported := c.Enabled(); !reported || !takesInput[k.Noun] {
		return c, nil
	}
	ok, err := waitUntil(sc, time.Until(deadline), func() (bool, error) {
		on, _ := c.Enabled()
		return on, nil
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, core.Failf("The %s %s in the %s app is disabled: it cannot be used", quoted(name), k.Noun, app)
	}
	return c, nil
}

// lostTries is how often an action finds its control again when the driver
// says it lost its place.
const lostTries = 3

// again finds the usable control and acts on it, and finds it again when
// the driver says it lost its place (*Lost): what the action took.
func again(sc *core.Scenario, app string, p Process, k appcore.Kind, name string, act func(Control) error) (Control, error) {
	for try := 1; ; try++ {
		c, err := usable(sc, app, p, k, name)
		if err != nil {
			return nil, err
		}
		err = act(c)
		var lost *Lost
		if !errors.As(err, &lost) || try == lostTries {
			return c, err
		}
		sc.Log("%s: finding the %s %s again", err, quoted(name), k.Noun)
	}
}

// control waits for the one control of kind k named name: one that shows,
// when shown is true.
func control(sc *core.Scenario, app string, p Process, k appcore.Kind, name string, shown bool, wait time.Duration) (Control, error) {
	var found []Control
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		var err error
		found, err = locate(p, k, name, shown)
		if err == nil && len(found) == 0 && shown {
			// The one scrolled out of view, as a person scrolls to it: GTK
			// says what is scrolled away does not show. One that cannot be
			// scrolled into view (on a tab not shown) stays unfound.
			if away, _ := locate(p, k, name, false); len(away) == 1 && p.ScrollIntoView(away[0]) == nil {
				found, err = locate(p, k, name, shown)
			}
		}
		return len(found) > 0, err
	})
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, missing(app, p, k, name)
	case len(found) > 1:
		return nil, core.Failf("%d %s in the %s app are named %s; a step needs exactly one: use an id= or xpath= selector",
			len(found), k.Plural, app, quoted(name))
	}
	return found[0], nil
}

// locate finds the controls of kind k named name, or those an id= or
// xpath= selector finds.
func locate(p Process, k appcore.Kind, name string, shown bool) ([]Control, error) {
	sel, ok := selector(name)
	if !ok {
		return p.Find(k, name, shown)
	}
	tree, err := p.Tree()
	if err != nil {
		return nil, err
	}
	return sel.find(tree)
}

// missing is the failure of a control the app does not have: it names the
// controls of that kind it has.
func missing(app string, p Process, k appcore.Kind, name string) error {
	names := p.Names(k)
	return core.Fail(fmt.Sprintf("No %s named %s in the %s app; %s", k.Noun, quoted(name), app, cloudstep.Shown(k.Plural, names, 20)),
		name, strings.Join(names, ", "))
}

// failing runs a step's work, and attaches to its failure what the app
// showed: a screenshot of its window and an outline of its tree.
func failing(sc *core.Scenario, app string, p Process, fn func() error) error {
	err := fn()
	if err == nil {
		return nil
	}
	var a *core.AssertionError
	if !errors.As(err, &a) {
		return err
	}
	if img, _, werr := p.Window(); werr == nil {
		sc.Attach("image/png", encodePNG(img), "the "+app+" app")
	}
	if tree, terr := p.Tree(); terr == nil {
		sc.Attach("text/plain", []byte(Outline(tree)), "the "+app+" app's controls")
	}
	return err
}

// waitUntil checks until check is true or d has passed, looking at least
// twice.
func waitUntil(sc *core.Scenario, d time.Duration, check func() (bool, error)) (bool, error) {
	deadline := time.Now().Add(d)
	for looks := 1; ; looks++ {
		ok, err := check()
		if err != nil || ok {
			return ok, err
		}
		if !time.Now().Before(deadline) && looks >= 2 {
			return false, nil
		}
		// Soon at first, as most of what is waited for comes at once.
		select {
		case <-sc.Context().Done():
			return false, sc.Context().Err()
		case <-time.After(min(time.Duration(looks)*50*time.Millisecond, 150*time.Millisecond)):
		}
	}
}

var wordSpace = regexp.MustCompile(`\s+`)

// quoted names a control in a message.
func quoted(name string) string { return fmt.Sprintf("%q", wordSpace.ReplaceAllString(name, " ")) }
