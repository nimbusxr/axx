// Package modelsios answers platform model requests in debug simulator apps
// from prebuilt files in the app's axx-models folder.
package modelsios

import (
	"bytes"
	"context"
	_ "embed"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobileios "github.com/nimbusxr/axx/packs/mobile/ios"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

const Name = "models-ios"

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
	if slices.ContainsFunc(artifacts, ios.FoundationModels) {
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
