//go:build integration

package mobileandroid

import (
	"bytes"
	"image/jpeg"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/courierapi"
)

// parcelScene is the parcels example's scene of a parcel on a porch, 60 cm long, its front corners
// in guards a tap on which lands on the corner.
var parcelScene = filepath.Join("..", "..", "..", "examples", "parcels", "acceptance", "scenes", "parcel-on-porch.glb")

func porch(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(parcelScene)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func cameraApp(t *testing.T) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	logsOnFailure(t, h)
	h.OK("the courier android app with the following properties:", [][]string{
		{"apk", apk(t)}, {"device", avd(t)}, {"permissions", "POST_NOTIFICATIONS, CAMERA"}, {"host ports", "8400"},
	})
	return h
}

// A courier scans a parcel's label with the app's camera, which sees a photo of it: the app reads
// its QR code (ML Kit's barcode scanning, on the emulator's camera's frames) and opens the parcel.
// The emulator shows a picture off its middle: axx lays it out so that the camera sees all of it.
func TestACourierScansALabelOnAnEmulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cameraApp(t)
	h.OK("the courier app's camera sees " + courierapi.Label(t))
	signIn(h)
	h.OK(`the "Scan label" button is tapped in the courier app`)
	// An emulator's camera, rendered without a GPU (CI's), takes seconds to show its first frame.
	h.OK(`within 30s the courier app shows "Lena Vogel"`)
	_ = h.Fails(`the "label" in the courier app's camera view is tapped`, "which has no objects to tap: a scene names them")
}

// A courier photographs the parcel where they left it: the camera sees the porch scene, and the
// photo (CameraX's) reaches the service as a JPEG, upright as the phone was held.
func TestACourierPhotographsAParcelAtTheDoorOnAnEmulator(t *testing.T) {
	api := courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cameraApp(t)
	h.OK("the courier app's camera sees " + porch(t))
	signIn(h)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Photo at the door" button is tapped in the courier app`)
	h.OK(`the "Take photo" button is tapped in the courier app`)
	h.OK(`the courier app shows "photo-at-the-door.jpg attached"`)
	docs := api.Documents()
	if len(docs) != 1 || docs[0].Name != "photo-at-the-door.jpg" || docs[0].ContentType != "image/jpeg" {
		t.Fatalf("documents: %+v", docs)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(docs[0].Content))
	if err != nil || cfg.Width >= cfg.Height {
		t.Errorf("the photo is not upright: %+v, %v", cfg, err)
	}
}

// A courier measures a parcel with the app's AR (ARCore), whose camera sees a scene: a tap on
// each of the parcel's front corners (on the guard over it, which lands on the corner), and the
// app measures them exactly 60 cm apart. A scene object the scene has not got is named with those
// it has.
func TestACourierMeasuresAParcelOnAnEmulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cameraApp(t)
	h.OK("the courier app's camera sees " + porch(t))
	signIn(h)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Measure parcel" button is tapped in the courier app`)
	h.OK(`the courier app shows "Tap one end of the parcel, then the other"`)
	_ = h.Fails(`the "parcel" in the courier app's camera view is tapped`, `its objects are "front left corner", "front right corner"`)
	h.OK(`the "front left corner" in the courier app's camera view is tapped`)
	h.OK(`the "front right corner" in the courier app's camera view is tapped`)
	h.OK(`the courier app shows "60 cm"`)
}

// A courier gets out of the van, walks up the garden path, steps onto the porch and points the
// phone down at the parcel to measure it: the emulator's camera, and the app's AR, see the scene
// from wherever the phone is. From the van, the parcel is out of the camera's view.
func TestACourierWalksUpToAParcelAndMeasuresItOnAnEmulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cameraApp(t)
	h.OK("the courier app's camera sees " + porch(t))
	h.OK(`the courier app's phone moves to the scene's "by the van" camera`)
	signIn(h)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Measure parcel" button is tapped in the courier app`)
	_ = h.Fails(`the "front left corner" in the courier app's camera view is tapped`, `front left corner is out of the camera view`)
	h.OK(`the courier app's phone travels the scene's "up the garden path" path`)
	h.OK("the courier app's phone moves 1 m forward")
	h.OK("the courier app's phone turns 40 degrees down")
	h.OK(`the "front left corner" in the courier app's camera view is tapped`)
	h.OK(`the "front right corner" in the courier app's camera view is tapped`)
	h.OK(`the courier app shows "60 cm"`)
}
