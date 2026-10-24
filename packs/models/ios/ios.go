// Package modelsios answers platform model requests in debug simulator apps
// from prebuilt files in the app's axx-models folder.
package modelsios

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/npm"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobileios "github.com/nimbusxr/axx/packs/mobile/ios"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

const Name = "models-ios"

//go:embed runtime/models.m
var source []byte

//go:embed runtime/audio.inc
var audioSource []byte

//go:embed runtime/microphone.inc
var microphoneSource []byte

func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Requires: []string{modelscore.Name, mobileios.Name, appcore.Name}, Steps: []core.StepDef{},
		Doc:   "Answer supported Speech and Vision calls in debug iOS Simulator apps from prebuilt .axxmodel files in the app's axx-models folder. Speech uses log-mel matching for app-provided audio and the microphone step's precomputed matches for queued recordings. Microphone scenarios feed AVAudioEngine input taps and connections, AVAudioRecorder files (including duration, pause and metering), and AVCaptureSession audio data outputs, then silence. A tap-fed recognition shares its capture's recording and finishes when that recording ends or the app ends audio. Input nodes use 48 kHz mono float PCM independently of the host microphone; taps receive their requested PCM format. Audio data output requires a simulator audio capture device and microphone permission. Foundation Models text sessions, of the system model and of Apple's server model (PrivateCloudComputeLanguageModel; a session of a model of the app's own is left as it is), use the public provider API on iOS 27 or later; a stub whose contract gives format.details.availability (deviceNotEligible, appleIntelligenceNotEnabled or modelNotReady) is an iPhone without the system model, which reports it unavailable. Copy stubs before launching the app; restarting reloads them.",
		Hooks: []core.Hook{{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare}},
	}
}

var started = core.NewStateKey(Name+"/started", func(*core.Scenario) *sync.Once { return &sync.Once{} }, nil)

func prepare(sc *core.Scenario) error {
	started.Of(sc).Do(func() { mobileios.OnLaunch(sc, func(l *mobileios.Launch) error { return configure(sc, l) }) })
	return nil
}

func unsupported(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, f, args...).WithHint("use a debug iOS Simulator build and a supported platform API contract; embedded model stubs still belong in the app's test build")
}

func configure(sc *core.Scenario, l *mobileios.Launch) error {
	files, err := appcore.Files(sc, l.App, platform.Folder)
	if err != nil {
		return err
	}
	artifacts, err := platform.Load(sc.Context(), files)
	microphone := modelscore.UsesMicrophone(sc)
	if err != nil || len(artifacts) == 0 && !microphone {
		return err
	}
	if !l.Simulator {
		return unsupported("%s's platform model stubs require an iOS Simulator", l.App)
	}
	for _, a := range artifacts {
		if err := supported(a); err != nil {
			return err
		}
	}
	if err := debugBuild(sc.Context(), l.BundlePath, l.SourcePath); err != nil {
		return err
	}
	lib, err := library(sc.Context(), sc.Suite())
	if err != nil {
		return err
	}
	l.InsertLibrary(lib)
	pathFiles, err := l.Files(sc.Context(), platform.Folder)
	if err != nil {
		return err
	}
	l.Environment["AXX_MODELS_DIR"] = pathFiles.Where()
	if microphone {
		l.Environment["AXX_MODELS_MICROPHONE"] = "1"
		modelscore.FeedsMicrophone(sc, l.App)
	}
	// The Foundation Models adapter, once, for a stub of the system model or
	// of the server model (Private Cloud Compute).
	if slices.ContainsFunc(artifacts, func(a *platform.Artifact) bool {
		return a.API == "ios.foundation-models" || a.API == "ios.private-cloud-compute"
	}) {
		major, _ := strconv.Atoi(strings.Split(l.Version, ".")[0])
		if major < 27 {
			return unsupported("Foundation Models stubs require an iOS 27 or later simulator (found %s)", l.Version)
		}
		foundation, err := foundationLibrary(sc.Context(), sc.Suite())
		if err != nil {
			return err
		}
		// Swift interposition requires initial dyld loading. Re-executing
		// the app would invalidate the process identity FrontBoard tracks.
		l.InsertLibrary(foundation)
	}
	return nil
}

func supported(a *platform.Artifact) error {
	want := apiTask(a.API)
	if want == "" {
		return unsupported("the iOS model adapter does not support %s", a.API)
	}
	if a.Contract.Task != want {
		return unsupported("%s needs task %s", a.API, want)
	}
	if want == "text-generation" {
		if a.Matcher != stub.TextMatcher {
			return unsupported("Foundation Models needs text mappings")
		}
		return nil
	}
	if a.Matcher != stub.NearestMatcher {
		return unsupported("%s needs nearest image or audio mappings", a.API)
	}
	in := a.Contract.MatchedInput()
	if a.API == "ios.speech" {
		if in == nil || in.Audio == nil || (in.Audio.Features != "" && in.Audio.Features != "log-mel") {
			return unsupported("ios.speech needs audio samples or log-mel features, with its sample rate")
		}
	} else {
		if in == nil || in.Image == nil {
			return unsupported("ios.vision.classify needs an image input")
		}
		if want == "classification" && (len(a.Contract.Outputs) != 1 || a.Contract.Outputs[0].Quant != nil || a.Contract.Outputs[0].Activation == "none") {
			return unsupported("ios.vision.classify needs one unquantized output of label probabilities")
		}
		for _, out := range a.Contract.Outputs {
			if out.Quant != nil {
				return unsupported("Vision API outputs must be unquantized")
			}
		}
		if want == "text-recognition" {
			if len(a.Contract.Outputs) != 1 {
				return unsupported("ios.vision.text needs one CTC output")
			}
			out := a.Contract.Outputs[0]
			shape := out.Shape
			if out.Batched() {
				shape = shape[1:]
			}
			if len(shape) != 2 || int(shape[1].Size) != len(out.Labels) || out.Activation == "none" {
				return unsupported("ios.vision.text needs [steps, characters] probabilities, with a blank label")
			}
		}
		if want == "detection" {
			roles := map[string]bool{}
			for _, out := range a.Contract.Outputs {
				roles[out.Detection] = true
				if out.Box != nil && (out.Box.Pixels || out.Box.Order != "" && out.Box.Order != "ymin,xmin,ymax,xmax") {
					return unsupported("Vision detections need normalized ymin,xmin,ymax,xmax boxes")
				}
			}
			if !roles["boxes"] || !roles["classes"] || !roles["scores"] || !roles["count"] {
				return unsupported("Vision detections need boxes, classes, scores and count outputs")
			}
		}
		if want == "segmentation" {
			if len(a.Contract.Outputs) != 1 {
				return unsupported("person segmentation needs one confidence mask")
			}
			out := a.Contract.Outputs[0]
			shape := out.Shape
			if out.Batched() {
				shape = shape[1:]
			}
			if len(shape) != 2 || len(out.Labels) > 1 || out.Activation == "none" {
				return unsupported("person segmentation needs a [height,width] output of foreground probabilities")
			}
		}
	}
	return nil
}

func debugBuild(ctx context.Context, bundle, source string) error {
	out, err := exec.CommandContext(ctx, "codesign", "-d", "--entitlements", "-", bundle).Output()
	if err != nil {
		return unsupported("cannot read the debug entitlement of %s: %v", bundle, err)
	}
	for _, key := range []string{"get-task-allow", "com.apple.security.get-task-allow"} {
		cmd := exec.CommandContext(ctx, "plutil", "-extract", key, "raw", "-o", "-", "-")
		cmd.Stdin = bytes.NewReader(out)
		value, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(value)) == "true" {
			return nil
		}
	}
	// Simulator signatures commonly contain no debugger entitlement at all.
	// In that case require Xcode's explicit Debug simulator build product;
	// do not infer Debug from ad-hoc signing, which Release builds use too.
	product := source
	if product == "" {
		product = bundle
	}
	if filepath.Base(filepath.Dir(product)) == "Debug-iphonesimulator" {
		cmd := exec.CommandContext(ctx, "plutil", "-extract", "CFBundleSupportedPlatforms.0", "raw", "-o", "-", filepath.Join(bundle, "Info.plist"))
		platform, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(platform)) == "iPhoneSimulator" {
			return nil
		}
	}
	return unsupported("%s does not identify a Debug simulator build; supply Xcode's Debug-iphonesimulator product or a build signed with its debugger entitlement", bundle)
}

func library(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/library", func() (string, error) {
		sdk, err := exec.CommandContext(ctx, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-version").Output()
		if err != nil {
			return "", unsupported("iOS model stubs need Xcode: %v", err)
		}
		arch := mobileios.SimulatorArch(ctx)
		if arch == "amd64" {
			arch = "x86_64"
		}
		h := sha256.New()
		h.Write(source)
		h.Write(audioSource)
		h.Write(microphoneSource)
		h.Write(sdk)
		h.Write([]byte(arch))
		cache := npm.CacheDir("models")
		dir := filepath.Join(cache, fmt.Sprintf("ios-%x", h.Sum(nil)[:12]))
		lib := filepath.Join(dir, "axx-models.dylib")
		if _, err := os.Stat(lib); err == nil {
			return lib, nil
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		file := filepath.Join(tmp, "models.m")
		if err := os.WriteFile(file, source, 0o644); err != nil {
			return "", err
		}
		for name, data := range map[string][]byte{"audio.inc": audioSource, "microphone.inc": microphoneSource} {
			if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
				return "", err
			}
		}
		built := filepath.Join(tmp, "axx-models.dylib")
		args := []string{"--sdk", "iphonesimulator", "clang", "-target", arch + "-apple-ios16.0-simulator", "-dynamiclib", "-fobjc-arc", "-fblocks", "-ffp-contract=off", "-O2", file, "-o", built}
		for _, fw := range []string{"Foundation", "Vision", "Speech", "AVFoundation", "CoreImage", "CoreGraphics", "CoreMedia", "CoreVideo"} {
			args = append(args, "-framework", fw)
		}
		if out, err := exec.CommandContext(ctx, "xcrun", args...).CombinedOutput(); err != nil {
			return "", unsupported("cannot build the iOS model adapter: %v: %s", err, out)
		}
		if out, err := exec.CommandContext(ctx, "codesign", "--sign", "-", "--force", built).CombinedOutput(); err != nil {
			return "", unsupported("cannot sign the iOS model adapter: %v: %s", err, out)
		}
		if err := os.Rename(tmp, dir); err != nil {
			if _, other := os.Stat(lib); other != nil {
				return "", err
			}
		}
		return lib, nil
	})
}
