//go:build integration

package modelsandroid

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
	mobileandroid "github.com/nimbusxr/axx/packs/mobile/android"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestAndroidAppLaunchHook(t *testing.T) {
	serial := os.Getenv("AXX_MODELS_ANDROID_SERIAL")
	if serial == "" {
		t.Skip("set AXX_MODELS_ANDROID_SERIAL to a disposable emulator")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "../../../examples/parcels/courier/android/gradlew", "-p", "testdata/sdkprobe", "--no-daemon", "--console=plain", "--stacktrace", "assembleDebug")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe build: %v: %s", err, out)
	}
	apk, err := filepath.Abs("testdata/sdkprobe/build/outputs/apk/debug/axx-platform-model-probe-debug.apk")
	if err != nil {
		t.Fatal(err)
	}
	h := cloudtest.New(t, appcore.Pack(), files.Pack(), mobilecore.Pack(), mobileandroid.Pack(), modelscore.Pack(), Pack())
	h.NewScenarioWithin(5 * time.Minute)
	h.Start(&core.Plan{})
	h.OK("the courier android app with the following properties:", [][]string{{"apk", apk}, {"device", serial}})
	c := modelstest.Classifier("")
	c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"android.mlkit.image-labeling"}`)}
	s, err := stub.Resolve(c, []mappings.Mapping{{Name: "default", Default: true, Output: json.RawMessage(`{"label":"carton"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := platform.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	h.File("labels.axxmodel", string(b))
	s, err = stub.Resolve(c, []mappings.Mapping{{Name: "default", Default: true, Output: json.RawMessage(`{"label":"envelope"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	b, err = platform.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	h.File("replacement.axxmodel", string(b))
	var photo bytes.Buffer
	if err = png.Encode(&photo, modelstest.Door.Image()); err != nil {
		t.Fatal(err)
	}
	h.File("photo.png", photo.String())
	h.OK("the courier-models folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "axx-models"}})
	h.OK("the labels.axxmodel file in the courier-models folder is a copy of the labels.axxmodel file")
	h.OK("the courier-files folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "files"}})
	h.OK("the photo.png file in the courier-files folder is a copy of the photo.png file")
	h.OK("the courier app is launched")
	h.OK(`the answer.txt file in the courier-files folder contains "carton:1.0"`)
	h.OK("the labels.axxmodel file in the courier-models folder is a copy of the replacement.axxmodel file")
	h.OK("the courier app is restarted")
	h.OK(`the answer.txt file in the courier-files folder contains "envelope:1.0"`)
}
