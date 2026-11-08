package mobilecore

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// A notification that shows only once the wait is over is said to have come late, with what
// showed as the wait ended: iOS's notification system, on a busy Mac, took 35 seconds to show one
// the app had posted at once, and the failure said it did not show, listing it.
func TestANotificationThatCameLateIsSaidToHave(t *testing.T) {
	shown := []string{"PARCELS COURIER, now, PX-IOS-9511 delivered"}
	for late, want := range map[bool]string{
		true:  `The device of the courier app showed the notification "2 deliveries left today" only after the 30s wait`,
		false: `The device of the courier app shows no notification "2 deliveries left today"`,
	} {
		err := missed("courier", "2 deliveries left today", 30*time.Second, shown, late)
		var a *core.AssertionError
		if !errors.As(err, &a) || a.Message != want || !strings.Contains(a.Actual.(string), "PX-IOS-9511 delivered") {
			t.Errorf("late %v: %v", late, err)
		}
	}
}
