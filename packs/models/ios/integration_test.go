//go:build integration && darwin

package modelsios

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

// This probe uses public framework calls in a separate simulator executable.
// It has no provider selection or test adapter code linked into it.
func TestSimulatorPlatformCalls(t *testing.T) {
	device := os.Getenv("AXX_IOS_MODELS_SIMULATOR")
	if device == "" {
		t.Skip("set AXX_IOS_MODELS_SIMULATOR to a booted simulator UDID")
	}
	ctx := context.Background()
	bootProbeSimulator(t, ctx, device)
	lib, err := library(ctx, core.NewSuite(core.SuiteOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe")
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	cmd := exec.CommandContext(ctx, "xcrun", "--sdk", "iphonesimulator", "clang", "-target", arch+"-apple-ios16.0-simulator", "-fobjc-arc", "runtime/probe.m", "-framework", "Foundation", "-framework", "Vision", "-framework", "Speech", "-framework", "CoreVideo", "-framework", "CoreMedia", "-framework", "AVFoundation", "-o", probe)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v: %s", err, out)
	}
	write := func(api string, c *contract.Contract, ms []mappings.Mapping) {
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
	write("ios.vision.classify", modelstest.Classifier(""), []mappings.Mapping{
		{Name: "door", Input: mappings.Input{"image": json.RawMessage(`"` + photo + `"`)}, Output: json.RawMessage(`{"label":"carton"}`)},
		{Name: "other", Default: true, Output: json.RawMessage(`{"label":"envelope"}`)},
	})
	rate := 16000
	write("ios.vision.text", modelstest.TextRecognizer(), []mappings.Mapping{
		{Name: "door", Input: mappings.Input{"image": json.RawMessage(`"` + photo + `"`)}, Output: json.RawMessage(`{"text":"PX-4111"}`)},
		{Name: "other", Default: true, Output: json.RawMessage(`{"text":""}`)},
	})
	for _, api := range []string{"ios.vision.humans", "ios.vision.animals"} {
		write(api, modelstest.Detector(false), []mappings.Mapping{{Name: "objects", Default: true, Output: json.RawMessage(`{"tensors":{"boxes":[0.1,0.2,0.5,0.6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"classes":[0,0,0,0,0,0,0,0,0,0],"scores":[1,0,0,0,0,0,0,0,0,0],"count":[1]}}`)}})
	}
	mask := modelstest.Classifier("")
	mask.Task = "segmentation"
	mask.Outputs = []contract.Tensor{{Name: "mask", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 2}}}}
	write("ios.vision.person-segmentation", mask, []mappings.Mapping{{Name: "mask", Default: true, Output: json.RawMessage(`{"tensors":{"mask":[1,0,0,1]}}`)}})
	bell, knock, hum := filepath.Join(dir, "bell.wav"), filepath.Join(dir, "knock.wav"), filepath.Join(dir, "hum.wav")
	for file, sound := range map[string]modelstest.Sound{bell: modelstest.Doorbell, knock: modelstest.Knock, hum: modelstest.Hum} {
		modelstest.WriteWAV(t, file, rate, sound.Samples(rate, 0.975, 0, 1, 0))
	}
	c := modelstest.SoundClassifier()
	c.Task = "speech-to-text"
	c.Outputs = nil
	write("ios.speech", c, []mappings.Mapping{
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
	h.OK(`the courier app's microphone hears the "bell.wav" file`)
	for _, tc := range []struct{ kind, file, want string }{{"vision", photo, "carton"}, {"vision", noisy, "carton"}, {"vision", empty, "envelope"}, {"sequence", photo, "carton"}, {"text", photo, "PX-4111"}, {"humans", photo, "box 0.20 0.50 0.40 0.40"}, {"animals", photo, "box 0.20 0.50 0.40 0.40"}, {"mask", photo, "mask 2 2 255"}, {"speech", bell, "Ring twice, please."}, {"speech", knock, "Leave it by the door."}, {"speech", hum, "Please try again."}, {"speech-buffer", bell, "Ring twice, please."}, {"speech-cancel", bell, "cancelled"}, {"speech-queued", bell, "Ring twice, please."}} {
		t.Run(tc.kind+filepath.Base(tc.file), func(t *testing.T) {
			cmd := exec.CommandContext(ctx, "xcrun", "simctl", "spawn", device, probe, tc.kind, tc.file)
			// Use the same initial library list as application launches.
			cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+lib, "SIMCTL_CHILD_AXX_MODELS_DIR="+dir)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), tc.want+"\n") {
				t.Fatalf("want %q: %v: %s", tc.want, err, out)
			}
		})
	}
}

func bootProbeSimulator(t *testing.T, ctx context.Context, device string) {
	t.Helper()
	// The app integration test releases its simulator at cleanup. Each probe
	// must also work on its own, regardless of which test ran before it.
	if out, err := exec.CommandContext(ctx, "xcrun", "simctl", "bootstatus", device, "-b").CombinedOutput(); err != nil {
		t.Fatalf("boot probe simulator: %v: %s", err, out)
	}
}
