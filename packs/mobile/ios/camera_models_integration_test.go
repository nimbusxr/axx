//go:build integration

package mobileios_test

import (
	"os"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/files"
	"github.com/nimbusxr/axx/packs/mobile/internal/courierapi"
	mobileios "github.com/nimbusxr/axx/packs/mobile/ios"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	modelsios "github.com/nimbusxr/axx/packs/models/ios"
)

// A courier scans a label whose code is torn off: the camera sees a video of the parcel lifted
// into view, and the app reads its printed reference in the frames (a video data output, read
// with Vision) and opens the parcel. What Vision reads is a stub's (models-ios): the simulator's
// own model reads differently from one iOS to the next (iOS 26.5's reads PX-MOB-9401 with a
// Cyrillic М), and the stub reads the label only once it is held still in view.
func TestACourierReadsATornLabelOnASimulator(t *testing.T) {
	device := mobileios.Simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := mobileios.HarnessWith(t, nil, files.Pack(), modelscore.Pack(), modelsios.Pack())
	mobileios.Warm(t, h, device)
	// The packs start, as a run starts them: app-core makes the app's files a folder's owner.
	h.Start(&core.Plan{})
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", mobileios.CourierBuild(t)}, {"device", device}, {"permissions", "camera"},
	})
	h.OK("the courier app's camera sees " + courierapi.TornLabel(t))
	stub, err := os.ReadFile(courierapi.TornLabelText(t))
	if err != nil {
		t.Fatal(err)
	}
	h.File("torn-label.axxmodel", string(stub))
	h.OK("the courier-models folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "axx-models"}})
	h.OK("the torn-label.axxmodel file in the courier-models folder is a copy of the torn-label.axxmodel file")
	mobileios.SignIn(h)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the "Scan label" button is tapped in the courier app`)
	h.OK(`the courier app shows "Jonas Weber"`)
}
