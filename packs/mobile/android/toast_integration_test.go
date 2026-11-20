//go:build integration

package mobileandroid

import (
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// A courier confirms a delivery, and the app says so in a toast. For 3.5 seconds after a toast
// the UiAutomator2 server adds it to every screen it reads, through a field of Android's that
// Android 9 and later refuse an app: a server refused it fails every one of those reads, and so
// every step ("Cannot set AccessibilityNodeInfo's field 'mSealed'"). The steps right after the
// toast find a control, tap it and read the screen, all inside those seconds, on a device whose
// own settings allow no app the hidden APIs: the server is allowed them as it starts.
func TestACourierConfirmsADeliveryUnderAToastOnAnEmulator(t *testing.T) {
	device := avd(t)
	app := testAPK(t, "toast", filepath.Join("example", "parcels", "receipts", "Receipts"), nil)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	logsOnFailure(t, h)
	h.Start(&core.Plan{})
	h.OK("the receipts android app with the following properties:", [][]string{{"apk", app}, {"device", device}})
	h.OK("the receipts app is launched")
	h.OK(`within 30s the receipts app shows "Parcel 4471 is at the door"`)
	h.OK(`the "Confirm delivery" button is tapped in the receipts app`)
	// The toast is on the screen the server reads: the steps after it run under it.
	h.OK(`within 3s the receipts app shows "Delivery confirmed"`)
	h.OK(`the "Next parcel" button is tapped in the receipts app`)
	h.OK(`the receipts app shows "Parcel 4472 is next"`)
	h.OK(`the receipts app shows "Delivery confirmed"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
