//go:build integration

package mobileandroid_test

import (
	"os"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
	mobileandroid "github.com/nimbusxr/axx/packs/mobile/android"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/courierapi"
	modelsandroid "github.com/nimbusxr/axx/packs/models/android"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
)

// A courier scans a label whose code is torn off: the camera sees a video of the parcel lifted
// into view (H.264, re-encoded for the emulator's camera), and the app reads the reference printed
// under the code in its frames, with ML Kit's text recognition, and opens the parcel. What ML Kit
// reads is a stub's (models-android), which reads the label only once it is held still in view, as
// the emulator's camera frames it: the frames reach the app whole, and in order.
func TestACourierReadsATornLabelOnAnEmulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), mobileandroid.Pack(), files.Pack(), modelscore.Pack(), modelsandroid.Pack())
	mobileandroid.LogsOnFailure(t, h)
	// The packs start, as a run starts them: app-core makes the app's files a folder's owner.
	h.Start(&core.Plan{})
	h.OK("the courier android app with the following properties:", [][]string{
		{"apk", mobileandroid.APK(t)}, {"device", mobileandroid.AVD(t)}, {"permissions", "POST_NOTIFICATIONS, CAMERA"}, {"host ports", "8400"},
	})
	h.OK("the courier app's camera sees " + courierapi.TornLabel(t))
	stub, err := os.ReadFile(courierapi.TornLabelTextAndroid(t))
	if err != nil {
		t.Fatal(err)
	}
	h.File("torn-label.axxmodel", string(stub))
	h.OK("the courier-models folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "axx-models"}})
	h.OK("the torn-label.axxmodel file in the courier-models folder is a copy of the torn-label.axxmodel file")
	mobileandroid.SignIn(h)
	h.OK(`the "Scan label" button is tapped in the courier app`)
	// An emulator's camera, rendered without a GPU (CI's), takes seconds to show its first frame.
	h.OK(`within 30s the courier app shows "Jonas Weber"`)
}
