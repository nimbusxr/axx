//go:build integration && darwin

package modelsios

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	mobileios "github.com/nimbusxr/axx/packs/mobile/ios"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
)

func TestCourierUsesPlatformFixtureWithoutDebugProvider(t *testing.T) {
	device := os.Getenv("AXX_IOS_MODELS_SIMULATOR")
	if device == "" {
		t.Skip("set AXX_IOS_MODELS_SIMULATOR to an iOS 27 simulator")
	}
	app, err := filepath.Abs("../../../examples/parcels/courier/ios/build/Debug-iphonesimulator/Courier.app")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(app, "Info.plist")); err != nil {
		t.Fatal("build the courier Debug app first")
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sign-in") {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "test", "courier": "CR-LEJ-12", "name": "Hanna Wolf", "expiresIn": 43200})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/deliveries") {
			_ = json.NewEncoder(w).Encode(map[string]any{"courier": "CR-LEJ-12", "deliveries": []any{map[string]any{"reference": "PX-MODEL-1", "serviceLevel": "STANDARD", "recipient": map[string]string{"name": "Recipient", "street": "Example 1", "city": "Leipzig", "postcode": "04107", "country": "DE"}, "note": "Please leave the parcel behind the green gate, in the wooden box by the shed. Close the gate because the dog may escape. If the box is full, ask the Meyers at number 14."}}})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(api.Close)
	h := cloudtest.NewWith(t, map[string]any{mobileios.Name: map[string]any{"keep": true}}, appcore.Pack(), files.Pack(), mobilecore.Pack(), mobileios.Pack(), modelscore.Pack(), Pack())
	h.NewScenarioWithin(15 * time.Minute)
	h.Start(&core.Plan{})
	mobileios.OnLaunch(h.SC, func(l *mobileios.Launch) error { l.Environment["API_URL"] = api.URL; return nil })
	h.OK("the courier ios app with the following properties:", [][]string{{"app", app}, {"device", device}, {"locale", "en-GB"}})
	b, err := os.ReadFile("../../../examples/parcels/acceptance/features/courier-app/models/delivery-note.axxmodel")
	if err != nil {
		t.Fatal(err)
	}
	h.File("note.axxmodel", string(b))
	h.OK("the courier-models folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "axx-models"}})
	h.OK("the note.axxmodel file in the courier-models folder is a copy of the note.axxmodel file")
	h.OK("the courier app is launched")
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "4711"`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the "PX-MODEL-1" list item is tapped in the courier app`)
	h.OK(`the courier app shows "Note: Behind the green gate, in the wooden box by the shed. Close the gate: the dog. Box full: the Meyers at 14."`)
	h.OK("the courier app is restarted")
	h.OK(`the "PX-MODEL-1" list item is tapped in the courier app`)
	h.OK(`the courier app shows "Note: Behind the green gate, in the wooden box by the shed. Close the gate: the dog. Box full: the Meyers at 14."`)
}
