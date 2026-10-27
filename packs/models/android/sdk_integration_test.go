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
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// The probe depends on the vendor SDKs and calls their public factories and
// result getters. It must never link to the adapter or choose a test provider.
func TestAndroidVendorSDKCalls(t *testing.T) {
	serial, sdk := os.Getenv("AXX_MODELS_ANDROID_SERIAL"), os.Getenv("ANDROID_HOME")
	if serial == "" {
		t.Skip("set AXX_MODELS_ANDROID_SERIAL to a disposable emulator")
	}
	if sdk == "" {
		t.Fatal("set ANDROID_HOME")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	run := func(exe string, args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, exe, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", exe, args, err, out)
		}
		return out
	}
	adb := filepath.Join(sdk, "platform-tools", "adb")
	device := func(args ...string) []byte { return run(adb, append([]string{"-s", serial}, args...)...) }
	dir, err := build(ctx, sdk, strings.TrimSpace(string(device("shell", "getprop", "ro.product.cpu.abi"))))
	if err != nil {
		t.Fatal(err)
	}
	run("../../../examples/parcels/courier/android/gradlew", "-p", "testdata/sdkprobe", "--no-daemon", "--console=plain", "assembleDebug")
	const pkg = "us.nimbusxr.axx.sdkprobe"
	_ = exec.CommandContext(ctx, adb, "-s", serial, "uninstall", pkg).Run()
	device("install", "testdata/sdkprobe/build/outputs/apk/debug/axx-platform-model-probe-debug.apk")
	t.Cleanup(func() { _ = exec.Command(adb, "-s", serial, "uninstall", pkg).Run() })
	put := func(name string, b []byte) {
		t.Helper()
		cmd := exec.CommandContext(ctx, adb, "-s", serial, "shell", "run-as "+pkg+" sh -c 'mkdir -p "+filepath.ToSlash(filepath.Dir(name))+" && cat > "+name+"'")
		cmd.Stdin = bytes.NewReader(b)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("write %s: %v: %s", name, err, out)
		}
	}
	for _, name := range []string{"libaxx-models.so", "classes.dex"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		put("code_cache/axx-models/"+name, b)
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
		put("axx-models/"+api+platform.Extension, b)
	}
	var photo bytes.Buffer
	if err = png.Encode(&photo, modelstest.Door.Image()); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "door.png")
	if err = os.WriteFile(local, photo.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	put("files/photo.png", photo.Bytes())
	image := mappings.Input{"image": json.RawMessage(`"` + filepath.ToSlash(local) + `"`)}
	write("android.mlkit.image-labeling", modelstest.Classifier(""), []mappings.Mapping{{Name: "door", Input: image, Output: json.RawMessage(`{"label":"carton"}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"label":"envelope"}`)}})
	write("android.mlkit.text-recognition", modelstest.TextRecognizer(), []mappings.Mapping{{Name: "door", Input: image, Output: json.RawMessage(`{"text":"PX-4111"}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"text":""}`)}})
	write("android.mlkit.object-detection", modelstest.Detector(false), []mappings.Mapping{{Name: "objects", Default: true, Output: json.RawMessage(`{"tensors":{"boxes":[0.1,0.2,0.5,0.6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"classes":[0,0,0,0,0,0,0,0,0,0],"scores":[1,0,0,0,0,0,0,0,0,0],"count":[1]}}`)}})
	mask := modelstest.Classifier("")
	mask.Task = "segmentation"
	mask.Outputs = []contract.Tensor{{Name: "mask", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 2}}}}
	write("android.mlkit.segmentation", mask, []mappings.Mapping{{Name: "mask", Default: true, Output: json.RawMessage(`{"tensors":{"mask":[1,0,0,1]}}`)}})
	write("android.gemini-nano", &contract.Contract{Task: "text-generation"}, []mappings.Mapping{{Name: "parcel", Input: mappings.Input{"text": json.RawMessage(`"parcel"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"text":"Try again."}`)}})
	root := strings.TrimSpace(string(device("shell", "run-as", pkg, "pwd"))) + "/code_cache/axx-models"
	for _, tc := range []struct{ kind, want string }{{"labels", "carton:1.0:"}, {"text", "PX-4111|PX-4111"}, {"objects", ":carton:null"}, {"mask", "2x2:1.0"}, {"mask-loop", "loop:8000000"}, {"nano", "Ring twice, please."}, {"nano-stream", "Ring twice, please."}} {
		t.Run(tc.kind, func(t *testing.T) {
			device("shell", "am", "force-stop", pkg)
			device("shell", "run-as", pkg, "rm", "-f", "files/answer.txt")
			device("shell", "am", "start", "-W", "--attach-agent", root+"/libaxx-models.so="+root, "-n", pkg+"/.Probe", "--es", "kind", tc.kind)
			for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
				out, err := exec.CommandContext(ctx, adb, "-s", serial, "shell", "run-as", pkg, "cat", "files/answer.txt").CombinedOutput()
				if err == nil {
					if !strings.Contains(string(out), tc.want) {
						t.Fatalf("want %q, got %s", tc.want, out)
					}
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			out, _ := exec.CommandContext(ctx, adb, "-s", serial, "logcat", "-d", "-s", "AXXModels:V", "AndroidRuntime:E", "*:S").CombinedOutput()
			t.Fatalf("SDK did not answer: %s", out)
		})
	}
}
