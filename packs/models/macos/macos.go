// Package modelsmacos answers Mac apps' calls to Apple's models (Vision, Speech, Foundation
// Models) and their microphone from prebuilt files in the app's axx-models folder, through the
// library desktop-macos loads into the app as it starts.
package modelsmacos

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	desktopmacos "github.com/nimbusxr/axx/packs/desktop/macos"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

// Name is the pack's name.
const Name = "models-macos"

// Pack returns the models-macos pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = "Answer Mac apps' calls to Apple's models from prebuilt .axxmodel files in the app's axx-models folder (in its `./` files, its home's Library/Application Support): Vision (image classification, text recognition, human and animal rectangles, person segmentation), Speech (SFSpeechRecognizer) and Foundation Models text sessions, of the system model and of Apple's server model (PrivateCloudComputeLanguageModel; a session of a model of the app's own is left as it is), as models-ios does in the iOS Simulator, with the same contracts (`macos.*` APIs). desktop-macos loads axx's library into the app as it starts, from its environment (DYLD_INSERT_LIBRARIES): macOS lets it into an app signed without the hardened runtime, or with the `com.apple.security.cs.allow-dyld-environment-variables` and `com.apple.security.cs.disable-library-validation` entitlements (the app's test build's), which axx checks before it starts the app. Speech uses log-mel matching for app-provided audio and the microphone step's precomputed matches for queued recordings; microphone scenarios feed AVAudioEngine input taps and connections, AVAudioRecorder files and AVCaptureSession audio data outputs, then silence (a capture session still opens the Mac's audio capture device, which needs one, and the user's permission to use it). A Mac has no test's grant of speech recognition or the microphone: a stubbed recognizer, and the microphone axx feeds, are authorized. A stub whose contract gives format.details.availability (deviceNotEligible, appleIntelligenceNotEnabled or modelNotReady) is a Mac without the system model, which reports it unavailable. Foundation Models stubs need macOS 27 or later; the rest, macOS 15. Copy stubs before launching the app; restarting reloads them."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		Requires: []string{modelscore.Name, desktopmacos.Name, appcore.Name},
		Steps:    []core.StepDef{},
		Hooks:    []core.Hook{{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare}},
	}
}

var started = core.NewStateKey(Name+"/started", func(*core.Scenario) *sync.Once { return &sync.Once{} }, nil)

func prepare(sc *core.Scenario) error {
	started.Of(sc).Do(func() { desktopcore.OnLaunch(sc, func(l *desktopcore.Launch) error { return configure(sc, l) }) })
	return nil
}

// configure loads axx's library into a Mac app whose axx-models folder has stubs, or whose
// microphone a scenario feeds.
func configure(sc *core.Scenario, l *desktopcore.Launch) error {
	if l.App.Driver == nil || l.App.Driver.Platform() != "macos" {
		return nil
	}
	ctx := sc.Context()
	files, err := appcore.Files(sc, l.App.Name, platform.Folder)
	if err != nil {
		return err
	}
	artifacts, err := platform.Load(ctx, files)
	microphone := modelscore.UsesMicrophone(sc)
	if err != nil || len(artifacts) == 0 && !microphone {
		return err
	}
	for _, a := range artifacts {
		if err := mac.Supported(a); err != nil {
			return err
		}
	}
	path, err := desktopmacos.AppPath(ctx, l.App.App)
	if err != nil {
		return err
	}
	exe, err := executable(ctx, path)
	if err != nil {
		return err
	}
	if err := loadable(ctx, l.App.Name, path); err != nil {
		return err
	}
	arch, err := processArch(ctx, exe)
	if err != nil {
		return err
	}
	lib, err := mac.Library(ctx, sc.Suite(), arch)
	if err != nil {
		return err
	}
	l.InsertLibrary(lib)
	l.Environment["AXX_MODELS_DIR"] = filepath.Join(l.Files, platform.Folder)
	if microphone {
		l.Environment["AXX_MODELS_MICROPHONE"] = "1"
		modelscore.FeedsMicrophone(sc, l.App.Name)
	}
	if slices.ContainsFunc(artifacts, mac.FoundationModels) {
		version := macOSVersion(ctx)
		if major, _ := strconv.Atoi(strings.Split(version, ".")[0]); major < 27 {
			return mac.Unsupported("Foundation Models stubs need macOS 27 or later (this Mac has %s)", version)
		}
		foundation, err := mac.FoundationLibrary(ctx, sc.Suite(), arch)
		if err != nil {
			return err
		}
		l.InsertLibrary(foundation)
	}
	return nil
}

// executable is the program of a .app, or the path itself.
func executable(ctx context.Context, path string) (string, error) {
	if !strings.HasSuffix(strings.TrimRight(path, "/"), ".app") {
		return path, nil
	}
	out, err := exec.CommandContext(ctx, "plutil", "-extract", "CFBundleExecutable", "raw", "-o", "-", filepath.Join(path, "Contents", "Info.plist")).Output()
	if err != nil {
		return "", mac.Unsupported("cannot read the executable of %s: %v", path, err)
	}
	return filepath.Join(path, "Contents", "MacOS", strings.TrimSpace(string(out))), nil
}

// processArch is the architecture the app runs as: this Mac's, when its program has it, else
// its program's (an Intel app, under Rosetta).
func processArch(ctx context.Context, exe string) (string, error) {
	out, err := exec.CommandContext(ctx, "lipo", "-archs", exe).Output()
	if err != nil {
		return "", mac.Unsupported("cannot read the architectures of %s: %v", exe, err)
	}
	archs := strings.Fields(string(out))
	host := runtime.GOARCH
	if host == "amd64" {
		host = "x86_64"
	}
	if slices.Contains(archs, host) || len(archs) == 0 {
		return host, nil
	}
	return archs[0], nil
}

var codeFlags = regexp.MustCompile(`flags=0x[0-9a-f]+\(([^)]*)\)`)

// The entitlements that let an app with the hardened runtime load the library its environment
// names, of another signer: Apple's documented exceptions, for a test build.
var exceptions = []string{"com.apple.security.cs.allow-dyld-environment-variables", "com.apple.security.cs.disable-library-validation"}

// loadable is nil when macOS lets the app load axx's library: signed without the hardened
// runtime, or with its exceptions. macOS ignores DYLD_INSERT_LIBRARIES in an app it hardens,
// which would leave the app with Apple's models instead of the stubs, without a word.
func loadable(ctx context.Context, app, path string) error {
	out, err := exec.CommandContext(ctx, "codesign", "-dv", "--verbose=2", path).CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("not signed at all")) {
			return nil // no signature, no hardened runtime (an Intel program's)
		}
		return mac.Unsupported("cannot read the signature of the %s app (%s): %v: %s", app, path, err, bytes.TrimSpace(out))
	}
	m := codeFlags.FindSubmatch(out)
	if m == nil || !slices.Contains(strings.Split(string(m[1]), ","), "runtime") {
		return nil
	}
	ents, err := exec.CommandContext(ctx, "codesign", "-d", "--entitlements", "-", "--xml", path).Output()
	if err != nil {
		return mac.Unsupported("cannot read the entitlements of the %s app (%s): %v", app, path, err)
	}
	// As JSON: plutil's -extract reads the dots in an entitlement's name as a path. No
	// entitlements, no plist.
	granted := map[string]any{}
	if len(bytes.TrimSpace(ents)) > 0 {
		cmd := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", "-")
		cmd.Stdin = bytes.NewReader(ents)
		js, err := cmd.Output()
		if err == nil {
			err = json.Unmarshal(js, &granted)
		}
		if err != nil {
			return mac.Unsupported("cannot read the entitlements of the %s app (%s): %v", app, path, err)
		}
	}
	var missing []string
	for _, key := range exceptions {
		if granted[key] != true {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return mac.Unsupported("the %s app (%s) is signed with the hardened runtime, and without %s: macOS keeps axx's library out of it", app, path, strings.Join(missing, " and "))
	}
	return nil
}

// macOSVersion is this Mac's macOS version, like 27.0.1.
func macOSVersion(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "sw_vers", "-productVersion").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
