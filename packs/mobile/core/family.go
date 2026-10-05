package mobilecore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
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
	return kind{}, fmt.Errorf("a phone's app has no %s: the kinds of control on a phone are %s", k.Plural, strings.Join(have, ", "))
}

func (family) Launch(sc *core.Scenario, app *appcore.App) error {
	return onDevice(sc, app.Name, true, func(ctx context.Context, d Device) error { return d.Launch(ctx) })
}

func (family) Restart(sc *core.Scenario, app *appcore.App) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error { return d.Restart(ctx) })
}

func (family) Press(sc *core.Scenario, app *appcore.App, ak appcore.Kind, name string) error {
	k, err := kindFor(ak)
	if err != nil {
		return err
	}
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		el, err := element(sc, d, app.Name, k, name, actionTimeout)
		if err != nil {
			return err
		}
		if err := el.Click(ctx); err != nil {
			return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
		}
		return nil
	})
}

func (family) Fill(sc *core.Scenario, app *appcore.App, field, text string) error {
	return onDevice(sc, app.Name, false, func(ctx context.Context, d Device) error {
		el, err := element(sc, d, app.Name, kinds["field"], field, actionTimeout)
		if err != nil {
			return err
		}
		if err := el.Clear(ctx); err != nil {
			return fmt.Errorf("cannot empty the %s field: %w", quoted(field), err)
		}
		if err := el.Type(ctx, text); err != nil {
			return fmt.Errorf("cannot type into the %s field: %w", quoted(field), err)
		}
		return nil
	})
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
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			s, err := d.Screen(ctx)
			return err == nil && !s.Shows(text), err
		})
		if err != nil || ok {
			return err
		}
		return core.Failf("The %s app shows %q", app.Name, text)
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
			n, err := find(sc, d, app.Name, k, name, 0)
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
			_, err := find(sc, d, app.Name, k, name, 0)
			return err
		}
		return core.Fail(fmt.Sprintf("The %q %s in the %s app is not %s", name, k.noun, app.Name, state[enabled]), state[enabled], state[last.Enabled])
	})
}

func (family) Value(sc *core.Scenario, app *appcore.App, field, want string, wait time.Duration) error {
	return onDevice(sc, app.Name, false, func(_ context.Context, d Device) error {
		var got string
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			n, err := find(sc, d, app.Name, kinds["field"], field, 0)
			if err != nil {
				return false, nil //nolint:nilerr // not shown yet: wait
			}
			got = value(n)
			return got == clean(want), nil
		})
		if err != nil || ok {
			return err
		}
		if _, err := find(sc, d, app.Name, kinds["field"], field, 0); err != nil {
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
