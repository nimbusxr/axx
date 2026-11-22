package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// family carries out app-core's steps for the apps on phones.
type family struct{}

var _ appcore.Family = family{}

// kindFor is the kind of control a phone finds for an app-core kind: a phone
// has no menu bar, radio buttons, rows or links.
func kindFor(k appcore.Kind) (kind, error) {
	if mk, ok := kinds[k.Noun]; ok {
		return mk, nil
	}
	var have []string
	for _, mk := range controlKinds {
		have = append(have, mk.noun)
	}
	if k.Noun == "notification" {
		return kind{}, errors.New("a notification is tapped, or checked with \"the courier app shows a notification ...\": the other steps take an app's own controls")
	}
	return kind{}, fmt.Errorf("a phone's app has no %s: the kinds of control on a phone are %s", k.Plural, strings.Join(have, ", "))
}

func (family) Launch(sc *core.Scenario, app *appcore.App) error {
	return onDevice(sc, app.Name, true, func(ctx context.Context, d Device) error { return d.Launch(ctx) })
}

func (family) Restart(sc *core.Scenario, app *appcore.App) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error { return d.Restart(ctx) })
}

func (family) Press(sc *core.Scenario, app *appcore.App, ak appcore.Kind, name string) error {
	if ak.Noun == "notification" {
		return openNotification(sc, app.Name, name)
	}
	k, err := kindFor(ak)
	if err != nil {
		return err
	}
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		el, n, came, err := reached(sc, d, app.Name, k, name, actionTimeout)
		if err != nil {
			return err
		}
		// A control that came while the step looked for it, or one tapped where it is (a sheet's
		// or a popover's, which iOS presents with a fade): it may still be coming in.
		if n != nil && (came || n.ByPoint) {
			if err := settle(ctx, d, n.Bounds); err != nil {
				return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
			}
		}
		if n != nil && n.ByPoint {
			// A finger's tap where it is: down and up at its middle.
			x, y := n.Bounds.Center()
			before, _ := looksOf(ctx, d, n.Bounds)
			if err := d.Session().Drag(ctx, x, y, x, y, 0); err != nil {
				return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
			}
			// A sheet can take no notice of a tap even once it looks still (iOS 27's Save
			// Password sheet, on a busy Mac): a tap it heard shows, as the button lights up or the
			// sheet goes. One that shows nothing at all a moment later is tapped again, once, as a
			// person does whose tap did nothing.
			if before != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(700 * time.Millisecond):
				}
				if after, err := looksOf(ctx, d, n.Bounds); err == nil && after != nil && same(before, after) {
					if err := d.Session().Drag(ctx, x, y, x, y, 0); err != nil {
						return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
					}
				}
			}
			return nil
		}
		if el, err = landed(ctx, sc, d, app.Name, k, name, el); err != nil {
			return err
		}
		// A control that came while the step looked for it can take no notice of a tap even once it
		// is still: a drawer that has slid in takes a touch for the start of a drag until it has
		// told itself it is open, which a busy app does a while after it looks open (the system
		// file picker's drawer of places, on a busy emulator: the tap on Downloads left it open on
		// Recent). The screen as it was before the tap says whether the tap showed anything.
		var before image.Image
		if came {
			if shot, err := maskedShot(ctx, d); err == nil {
				before = shot
			}
		}
		// A control the driver says it cannot hit is tapped where it is: the driver's own tap of
		// it taps nothing, and says it tapped.
		if hitTests(sc, app.Name) {
			tapped, err := tapWhereItIs(ctx, sc, d, el, quoted(name)+" "+k.noun)
			if err != nil {
				return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
			}
			if tapped {
				if before != nil {
					return tapAgain(ctx, sc, d, app.Name, k, name, before)
				}
				return nil
			}
		}
		err = el.Click(ctx)
		// A control the app drew again between finding it and tapping it (a dialog's button as the
		// dialog comes in, on a slow machine) is gone from the driver's cache: find it again, and tap
		// that, as a person taps what is there now. It is found as it first was, scrolled to when
		// it is out of view: a page that drew it again may have moved, and one found without that
		// is tapped where it is, off the screen.
		for range 2 {
			if err == nil || !appium.IsNoSuchElement(err) && !appium.IsStale(err) {
				break
			}
			if el, _, _, err = reached(sc, d, app.Name, k, name, actionTimeout); err != nil {
				return err
			}
			if el, err = landed(ctx, sc, d, app.Name, k, name, el); err != nil {
				return err
			}
			err = el.Click(ctx)
		}
		if err != nil {
			return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
		}
		if before != nil {
			return tapAgain(ctx, sc, d, app.Name, k, name, before)
		}
		return nil
	})
}

// tapAgain taps a control once more when its tap showed nothing that lasted: the screen is, two
// seconds on, the screen before it, pixel for pixel, the control where it was. A person whose tap
// did nothing taps again. A tap that was taken shows, as a drawer closes, a dialog goes, a row
// lights up and stays lit or a spinner turns, and is not repeated, nor is one on a control no
// longer there: a tap taken twice would do twice what it does (a delivery confirmed twice). Two
// seconds leave a tap whose app waits for an answer, on a busy emulator, the time to show it.
//
// A change that does not last is no sign of a tap taken: a row lights up under a finger and goes
// dark again whether or not the app acts on the tap (the system file picker's drawer, on CI's
// emulator: "Downloads" lit up for a moment, the picker stayed on Recent, and the one look at
// the screen that found the row lit took the tap for taken). So the screen is read until it is at
// rest: changed and still so at the next look, the tap stands; back to the screen before the tap,
// it is tapped again. One that is still changing five seconds on is left alone.
func tapAgain(ctx context.Context, sc *core.Scenario, d Device, app string, k kind, name string, before image.Image) error {
	var last image.Image
	asBefore := 0 // looks in a row that found the screen as it was before the tap
	for start := time.Now(); ; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tapShows / 4):
		}
		after, err := maskedShot(ctx, d)
		if err != nil {
			return nil //nolint:nilerr // the screen cannot say: the tap stands
		}
		if same(before, after) {
			if asBefore++; asBefore >= 2 && time.Since(start) >= tapShows {
				break
			}
		} else {
			if last != nil && same(last, after) || time.Since(start) >= tapShows*5/2 {
				return nil // the tap showed, and what it showed stayed; or the screen keeps changing
			}
			asBefore = 0
		}
		last = after
	}
	el, _, _, err := target(sc, d, app, k, name, 0)
	if err != nil || el == nil {
		return nil //nolint:nilerr // no longer there: the tap stands
	}
	sc.Log("the tap on the %s %s showed nothing on the screen: it is tapped again, once", quoted(name), k.noun)
	if err := el.Click(ctx); err != nil {
		return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
	}
	return nil
}

// tapShows is how long a tap has to show on the screen before it is taken for one the app took no
// notice of.
const tapShows = 2 * time.Second

func (family) Fill(sc *core.Scenario, app *appcore.App, field, text string) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		el, _, _, err := reached(sc, d, app.Name, kinds["field"], field, actionTimeout)
		if err != nil {
			return err
		}
		if err := el.Clear(ctx); err != nil && !appium.IsNoTextField(err) {
			return fmt.Errorf("cannot empty the %s field: %w", quoted(field), err)
		}
		err = el.Type(ctx, text)
		if appium.IsNoTextField(err) {
			// A field the driver takes for a slider (a web page's number field on Android): a
			// person taps it and types. What it held is selected and typed over.
			err = typeOver(ctx, d, el, text)
		}
		if err != nil {
			return fmt.Errorf("cannot type into the %s field: %w", quoted(field), err)
		}
		return nil
	})
}

// typeOver taps a field and types text over what it holds, a key at a time: the end of the
// line, back over what is there (such a field holds a number, a few keys long), then the text.
func typeOver(ctx context.Context, d Device, el *appium.Element, text string) error {
	if err := el.Click(ctx); err != nil {
		return err
	}
	const end, backspace = "\uE010", "\uE003"
	return d.Session().Keys(ctx, end+strings.Repeat(backspace, 32)+text)
}

func (family) ScrollIntoView(sc *core.Scenario, app *appcore.App, ak appcore.Kind, name string) error {
	k, err := kindFor(ak)
	if err != nil {
		return err
	}
	return onDevice(sc, app.Name, false, func(_ context.Context, d Device) error { return scrollTo(sc, d, app.Name, k, name) })
}

func (family) Shows(sc *core.Scenario, app *appcore.App, text string, wait time.Duration) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		var last *Screen
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			s, err := d.Screen(ctx)
			last = s
			return err == nil && s.Shows(text), err
		})
		if err != nil || ok {
			return err
		}
		return core.Fail(fmt.Sprintf("The %s app does not show %q", app.Name, text), text, shownTexts(last))
	})
}

func (family) DoesNotShow(sc *core.Scenario, app *appcore.App, text string, wait time.Duration) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		var last *Screen
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			s, err := d.Screen(ctx)
			last = s
			return err == nil && !s.Shows(text), err
		})
		if err != nil || ok {
			return err
		}
		return core.Fail(fmt.Sprintf("The %s app shows %q", app.Name, text), fmt.Sprintf("no %q", text), shownTexts(last))
	})
}

func (family) Shown(sc *core.Scenario, app *appcore.App, ak appcore.Kind, name string, wait time.Duration) error {
	k, err := kindFor(ak)
	if err != nil {
		return err
	}
	return onDevice(sc, app.Name, false, func(_ context.Context, d Device) error {
		_, err := element(sc, d, app.Name, k, name, wait)
		return err
	})
}

func (family) Enabled(sc *core.Scenario, app *appcore.App, ak appcore.Kind, name string, enabled bool, wait time.Duration) error {
	k, err := kindFor(ak)
	if err != nil {
		return err
	}
	state := map[bool]string{true: "enabled", false: "disabled"}
	return onDevice(sc, app.Name, false, func(_ context.Context, d Device) error {
		var last *Node
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			n, err := find(sc, d, app.Name, k, name)
			if err != nil {
				return false, nil //nolint:nilerr // not shown yet: wait
			}
			last = n
			return n.Enabled == enabled, nil
		})
		if err != nil || ok {
			return err
		}
		if last == nil {
			_, err := find(sc, d, app.Name, k, name)
			return err
		}
		return core.Fail(fmt.Sprintf("The %q %s in the %s app is not %s", name, k.noun, app.Name, state[enabled]), state[enabled], state[last.Enabled])
	})
}

func (family) Value(sc *core.Scenario, app *appcore.App, field, want string, wait time.Duration) error {
	return onDevice(sc, app.Name, false, func(_ context.Context, d Device) error {
		var got string
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			n, err := find(sc, d, app.Name, kinds["field"], field)
			if err != nil {
				return false, nil //nolint:nilerr // not shown yet: wait
			}
			got = value(n)
			return got == clean(want), nil
		})
		if err != nil || ok {
			return err
		}
		if _, err := find(sc, d, app.Name, kinds["field"], field); err != nil {
			return err
		}
		return core.Fail(fmt.Sprintf("The %q field in the %s app does not have the value", field, app.Name), want, got)
	})
}

func (family) LooksLike(sc *core.Scenario, app *appcore.App, name string, wait time.Duration) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		return looksLike(sc, ctx, d, app.Name, name, wait)
	})
}

func (family) Files(sc *core.Scenario, app *appcore.App, path string) (core.Files, error) {
	return appFiles(sc, app, path)
}
