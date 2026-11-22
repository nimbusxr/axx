// SPDX-License-Identifier: Apache-2.0

package mobilecore

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// Keys a field did not take. iOS's driver types into a field as a keyboard does, a key at a
// time, and the keys that come before the keyboard is ready are dropped: the driver has typed,
// and the field holds nothing, or only the end of the text. A simulator's keyboard takes seconds
// to come up the first time it is used. On CI's Mac the first field of the couriers' sign-in was
// typed into a third of a second after it was tapped, two seconds before the keyboard was there;
// the field stayed empty, the "Sign in" button disabled, and the tap on it did nothing. A person
// looks at what they typed, and types it again.

// typesWithKeys reports whether the app's platform has a driver that types into a field with a
// keyboard's keys, and says what a field holds: its text, or its placeholder when it has none.
func typesWithKeys(sc *core.Scenario, app string) bool {
	a, err := mobileApp(sc, app)
	return err == nil && a.Platform.Kind() == "ios"
}

// typedAgain types text into the field again where the field took none of it, or only its end,
// four times at most. Keys that are on their way get a second to arrive first each time, so a
// keyboard has some six seconds to come up. What the field holds after that is the app's doing
// (a field that takes four digits of six, or shows a text its own way), and the step leaves it.
func typedAgain(ctx context.Context, sc *core.Scenario, el *appium.Element, field, text string) error {
	for range 4 {
		none, lost := false, true
		for arrived := time.Now().Add(time.Second); lost; {
			if none, lost = lostKeys(ctx, el, text); !lost || !time.Now().Before(arrived) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		if !lost {
			return nil
		}
		if none {
			sc.Log("the %s field took nothing of what was typed: it is typed again", quoted(field))
		} else {
			sc.Log("the %s field took only the end of what was typed: it is emptied and typed again", quoted(field))
			if err := el.Clear(ctx); err != nil {
				return nil //nolint:nilerr // a field that cannot be emptied is left as it is
			}
		}
		if err := el.Type(ctx, text); err != nil {
			return fmt.Errorf("cannot type into the %s field: %w", quoted(field), err)
		}
	}
	return nil
}

// lostKeys reports whether a field holds less than the text typed into it because keys were
// dropped: none of it (it is empty, or shows its placeholder), or only its end. A field for a
// password shows a dot for each character, and is held to their number.
func lostKeys(ctx context.Context, el *appium.Element, text string) (none, lost bool) {
	value, err := el.Attribute(ctx, "value")
	if err != nil {
		return false, false
	}
	if value == "" {
		return true, true
	}
	if placeholder, _ := el.Attribute(ctx, "placeholderValue"); value == placeholder && value != text {
		return true, true
	}
	if kind, _ := el.Attribute(ctx, "type"); kind == "XCUIElementTypeSecureTextField" {
		return false, strings.Trim(value, "•") == "" && utf8.RuneCountInString(value) < utf8.RuneCountInString(text)
	}
	return false, value != text && strings.HasSuffix(text, value)
}
