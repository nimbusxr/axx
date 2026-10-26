//go:build integration && darwin

package modelsmacos

import (
	"context"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// arch is this Mac's architecture, as clang names it.
func arch() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return runtime.GOARCH
}

// probe is the probe of the models-ios pack's, built for this Mac: public framework calls, with
// no provider selection or test adapter code linked into it.
func probe(t *testing.T, dir string) string {
	t.Helper()
	exe := filepath.Join(dir, "probe")
	cmd := exec.Command("xcrun", "--sdk", "macosx", "clang", "-target", arch()+"-apple-macos15.0", "-fobjc-arc", "-Wno-deprecated-declarations", "../internal/apple/runtime/probe.m", "-framework", "Foundation", "-framework", "Vision", "-framework", "Speech", "-framework", "CoreVideo", "-framework", "CoreMedia", "-framework", "AVFoundation", "-o", exe)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v: %s", err, out)
	}
	return exe
}

// write writes a stub of an API in dir.
func write(t *testing.T, dir, api string, c *contract.Contract, ms []mappings.Mapping) {
	t.Helper()
	c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"` + api + `"}`)}
	s, err := stub.Resolve(c, ms)
	if err != nil {
		t.Fatal(err)
	}
	b, err := platform.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, api+platform.Extension), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A Mac program's calls to Vision and Speech get the stubs' answers, with axx's library in its
// environment, as desktop-macos starts an app.
func TestMacPlatformCalls(t *testing.T) {
	ctx := context.Background()
	lib, err := mac.Library(ctx, core.NewSuite(core.SuiteOptions{}), runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := probe(t, dir)
	photo := filepath.Join(dir, "door.png")
	f, err := os.Create(photo)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, modelstest.Door.Image()); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	noisy, empty := filepath.Join(dir, "noisy.png"), filepath.Join(dir, "empty.png")
	modelstest.WritePNG(t, noisy, modelstest.Noisy(modelstest.Door))
	modelstest.WritePNG(t, empty, modelstest.Empty.Image())
	write(t, dir, "macos.vision.classify", modelstest.Classifier(""), []mappings.Mapping{
		{Name: "door", Input: mappings.Input{"image": json.RawMessage(`"` + photo + `"`)}, Output: json.RawMessage(`{"label":"carton"}`)},
		{Name: "other", Default: true, Output: json.RawMessage(`{"label":"envelope"}`)},
	})
	write(t, dir, "macos.vision.text", modelstest.TextRecognizer(), []mappings.Mapping{
		{Name: "door", Input: mappings.Input{"image": json.RawMessage(`"` + photo + `"`)}, Output: json.RawMessage(`{"text":"PX-4111"}`)},
		{Name: "other", Default: true, Output: json.RawMessage(`{"text":""}`)},
	})
	for _, api := range []string{"macos.vision.humans", "macos.vision.animals"} {
		write(t, dir, api, modelstest.Detector(false), []mappings.Mapping{{Name: "objects", Default: true, Output: json.RawMessage(`{"tensors":{"boxes":[0.1,0.2,0.5,0.6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"classes":[0,0,0,0,0,0,0,0,0,0],"scores":[1,0,0,0,0,0,0,0,0,0],"count":[1]}}`)}})
	}
	mask := modelstest.Classifier("")
	mask.Task = "segmentation"
	mask.Outputs = []contract.Tensor{{Name: "mask", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 2}}}}
	write(t, dir, "macos.vision.person-segmentation", mask, []mappings.Mapping{{Name: "mask", Default: true, Output: json.RawMessage(`{"tensors":{"mask":[1,0,0,1]}}`)}})
	rate := 16000
	bell, knock, hum := filepath.Join(dir, "bell.wav"), filepath.Join(dir, "knock.wav"), filepath.Join(dir, "hum.wav")
	for file, sound := range map[string]modelstest.Sound{bell: modelstest.Doorbell, knock: modelstest.Knock, hum: modelstest.Hum} {
		modelstest.WriteWAV(t, file, rate, sound.Samples(rate, 0.975, 0, 1, 0))
	}
	c := modelstest.SoundClassifier()
	c.Task = "speech-to-text"
	c.Outputs = nil
	write(t, dir, "macos.speech", c, []mappings.Mapping{
		{Name: "bell", Input: mappings.Input{"audio": json.RawMessage(`"` + bell + `"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)},
		{Name: "knock", Input: mappings.Input{"audio": json.RawMessage(`"` + knock + `"`)}, Output: json.RawMessage(`{"text":"Leave it by the door."}`)},
		{Name: "other", Default: true, Output: json.RawMessage(`{"text":"Please try again."}`)},
	})
	h := cloudtest.New(t, modelscore.Pack())
	h.Start(&core.Plan{})
	h.Suite.SetFileOwner("app", func(_ *core.Scenario, _, path string) (core.Files, error) {
		if path == platform.Folder {
			return core.LocalFiles(dir), nil
		}
		return core.LocalFiles(filepath.Join(dir, strings.TrimPrefix(path, platform.Folder+"/"))), nil
	})
	b, err := os.ReadFile(bell)
	if err != nil {
		t.Fatal(err)
	}
	h.File("bell.wav", string(b))
	h.OK(`the depot app's microphone hears the "bell.wav" file`)
	for _, tc := range []struct{ kind, file, want string }{{"vision", photo, "carton"}, {"vision", noisy, "carton"}, {"vision", empty, "envelope"}, {"sequence", photo, "carton"}, {"text", photo, "PX-4111"}, {"humans", photo, "box 0.20 0.50 0.40 0.40"}, {"animals", photo, "box 0.20 0.50 0.40 0.40"}, {"mask", photo, "mask 2 2 255"}, {"speech", bell, "Ring twice, please."}, {"speech", knock, "Leave it by the door."}, {"speech", hum, "Please try again."}, {"speech-buffer", bell, "Ring twice, please."}, {"speech-cancel", bell, "cancelled"}, {"speech-queued", bell, "Ring twice, please."}} {
		t.Run(tc.kind+filepath.Base(tc.file), func(t *testing.T) {
			cmd := exec.CommandContext(ctx, exe, tc.kind, tc.file)
			cmd.Env = append(os.Environ(), "DYLD_INSERT_LIBRARIES="+lib, "AXX_MODELS_DIR="+dir)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), tc.want+"\n") {
				t.Fatalf("want %q: %v: %s", tc.want, err, out)
			}
		})
	}
}
