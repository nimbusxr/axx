package mobilecore

import (
	"context"
	"time"

	"github.com/nimbusxr/axx/core"
)

// OnDevice runs a platform pack's step on the named app's device, which must
// be running: a failed step attaches a screenshot, and its error masks the
// scenario's secrets.
func OnDevice(sc *core.Scenario, app string, fn func(ctx context.Context, d Device) error) error {
	return onDevice(sc, app, false, fn)
}

// WaitUntil calls check until it reports true or d has passed.
func WaitUntil(sc *core.Scenario, d time.Duration, check func() (bool, error)) (bool, error) {
	return waitUntil(sc, d, check)
}

// SetActionTimeout changes how long actions wait for their controls, for a
// platform pack's tests, and returns what restores it.
func SetActionTimeout(d time.Duration) (restore func()) {
	old := actionTimeout
	actionTimeout = d
	return func() { actionTimeout = old }
}
