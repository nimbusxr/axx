// Package apple builds the library axx loads into an app on an Apple platform (the iOS
// Simulator's, a Mac's), which answers the app's calls to Apple's models (Vision, Speech,
// Foundation Models) and its microphone from the stubs in its axx-models folder, and lists those
// APIs. models-ios and models-macos share it: the library's source is one, built for each.
package apple

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

//go:embed runtime/models.m
var source []byte

//go:embed runtime/audio.inc
var audioSource []byte

//go:embed runtime/microphone.inc
var microphoneSource []byte

//go:embed runtime/foundation.swift
var foundationSource []byte

//go:embed runtime/foundation-references.swift
var foundationReferences []byte

// A Platform is where the library runs.
type Platform struct {
	// Name prefixes its APIs (ios.vision.text, macos.vision.text), and Title names it in
	// messages (iOS, macOS).
	Name, Title string
	// SDK is Xcode's SDK of it (iphonesimulator, macosx).
	SDK string
	// Target is clang's target for the library on an architecture (arm64, x86_64), and
	// FoundationTarget the Foundation Models adapter's, of the first version with its public
	// provider API.
	Target, FoundationTarget func(arch string) string
	// Hint says what an app needs for the library to answer its calls.
	Hint string
}

// Unsupported is the error of what the platform's library cannot answer.
func (p Platform) Unsupported(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, f, args...).WithHint("%s", p.Hint)
}

// APIs are the APIs the library answers on the platform, and their contracts, which
// `axx models contract <api>` writes. Vision names its animals Cat and Dog.
func (p Platform) APIs() []platform.API {
	n := func(api string) string { return p.Name + "." + api }
	return []platform.API{
		{Name: n("speech"), Calls: "SFSpeechRecognizer", Task: "speech-to-text", Contract: platform.SpeechFeatures(n("speech"))},
		{Name: n("vision.classify"), Calls: "VNClassifyImageRequest", Task: "classification", Contract: platform.Classifier(n("vision.classify"))},
		{Name: n("vision.text"), Calls: "VNRecognizeTextRequest", Task: "text-recognition", Contract: platform.TextRecognizer(n("vision.text"))},
		{Name: n("vision.humans"), Calls: "VNDetectHumanRectanglesRequest", Task: "detection", Contract: platform.Detector(n("vision.humans"), "person")},
		{Name: n("vision.animals"), Calls: "VNRecognizeAnimalsRequest", Task: "detection", Contract: platform.Detector(n("vision.animals"), "Cat", "Dog")},
		{Name: n("vision.person-segmentation"), Calls: "VNGeneratePersonSegmentationRequest", Task: "segmentation", Contract: platform.Segmenter(n("vision.person-segmentation"))},
		{
			Name: n("foundation-models"), Calls: "LanguageModelSession, of SystemLanguageModel", Task: "text-generation", Contract: platform.TextGenerator(n("foundation-models")),
			Unavailable: []string{"deviceNotEligible", "appleIntelligenceNotEnabled", "modelNotReady"},
		},
		{
			Name: n("private-cloud-compute"), Calls: "LanguageModelSession, of PrivateCloudComputeLanguageModel (Apple's server model)", Task: "text-generation", Contract: platform.TextGenerator(n("private-cloud-compute")),
			Unavailable: []string{"deviceNotEligible", "systemNotReady"},
		},
	}
}

// FoundationModels tells whether a stub is of Foundation Models: the system model's, or Apple's
// server model's (Private Cloud Compute).
func (p Platform) FoundationModels(a *platform.Artifact) bool {
	return a.API == p.Name+".foundation-models" || a.API == p.Name+".private-cloud-compute"
}

// task is the task of the platform's API, or "" for an API the library does not answer.
func (p Platform) task(api string) string {
	for _, a := range p.APIs() {
		if a.Name == api {
			return a.Task
		}
	}
	return ""
}

// Supported is nil for a stub the library answers as its API's calls expect.
func (p Platform) Supported(a *platform.Artifact) error {
	want := p.task(a.API)
	if want == "" {
		return p.Unsupported("the %s model adapter does not support %s", p.Title, a.API)
	}
	if a.Contract.Task != want {
		return p.Unsupported("%s needs task %s", a.API, want)
	}
	if want == "text-generation" {
		if a.Matcher != stub.TextMatcher {
			return p.Unsupported("Foundation Models needs text mappings")
		}
		return nil
	}
	if a.Matcher != stub.NearestMatcher {
		return p.Unsupported("%s needs nearest image or audio mappings", a.API)
	}
	in := a.Contract.MatchedInput()
	if a.API == p.Name+".speech" {
		if in == nil || in.Audio == nil || (in.Audio.Features != "" && in.Audio.Features != "log-mel") {
			return p.Unsupported("%s needs audio samples or log-mel features, with its sample rate", a.API)
		}
		return nil
	}
	if in == nil || in.Image == nil {
		return p.Unsupported("%s needs an image input", a.API)
	}
	if want == "classification" && (len(a.Contract.Outputs) != 1 || a.Contract.Outputs[0].Quant != nil || a.Contract.Outputs[0].Activation == "none") {
		return p.Unsupported("%s needs one unquantized output of label probabilities", a.API)
	}
	for _, out := range a.Contract.Outputs {
		if out.Quant != nil {
			return p.Unsupported("Vision API outputs must be unquantized")
		}
	}
	if want == "text-recognition" {
		if len(a.Contract.Outputs) != 1 {
			return p.Unsupported("%s needs one CTC output", a.API)
		}
		out := a.Contract.Outputs[0]
		shape := out.Shape
		if out.Batched() {
			shape = shape[1:]
		}
		if len(shape) != 2 || int(shape[1].Size) != len(out.Labels) || out.Activation == "none" {
			return p.Unsupported("%s needs [steps, characters] probabilities, with a blank label", a.API)
		}
	}
	if want == "detection" {
		roles := map[string]bool{}
		for _, out := range a.Contract.Outputs {
			roles[out.Detection] = true
			if out.Box != nil && (out.Box.Pixels || out.Box.Order != "" && out.Box.Order != "ymin,xmin,ymax,xmax") {
				return p.Unsupported("Vision detections need normalized ymin,xmin,ymax,xmax boxes")
			}
		}
		if !roles["boxes"] || !roles["classes"] || !roles["scores"] || !roles["count"] {
			return p.Unsupported("Vision detections need boxes, classes, scores and count outputs")
		}
	}
	if want == "segmentation" {
		if len(a.Contract.Outputs) != 1 {
			return p.Unsupported("person segmentation needs one confidence mask")
		}
		out := a.Contract.Outputs[0]
		shape := out.Shape
		if out.Batched() {
			shape = shape[1:]
		}
		if len(shape) != 2 || len(out.Labels) > 1 || out.Activation == "none" {
			return p.Unsupported("person segmentation needs a [height,width] output of foreground probabilities")
		}
	}
	return nil
}

// Library is the library, built for the platform on an architecture (arm64, amd64), once per
// suite, and kept in axx's cache for the next.
func (p Platform) Library(ctx context.Context, s *core.Suite, arch string) (string, error) {
	arch = clangArch(arch)
	return core.Cached(s, "models-apple/"+p.Name+"/library/"+arch, func() (string, error) {
		sdk, err := exec.CommandContext(ctx, "xcrun", "--sdk", p.SDK, "--show-sdk-version").Output()
		if err != nil {
			return "", p.Unsupported("%s model stubs need Xcode: %v", p.Title, err)
		}
		target := p.Target(arch)
		h := sha256.New()
		h.Write(source)
		h.Write(audioSource)
		h.Write(microphoneSource)
		h.Write(sdk)
		h.Write([]byte(target))
		cache := npm.CacheDir("models")
		dir := filepath.Join(cache, fmt.Sprintf("%s-%x", p.Name, h.Sum(nil)[:12]))
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
		args := []string{"--sdk", p.SDK, "clang", "-target", target, "-dynamiclib", "-fobjc-arc", "-fblocks", "-ffp-contract=off", "-O2", `-DAXX_PLATFORM="` + p.Name + `"`, file, "-o", built}
		for _, fw := range []string{"Foundation", "Vision", "Speech", "AVFoundation", "CoreImage", "CoreGraphics", "CoreMedia", "CoreVideo"} {
			args = append(args, "-framework", fw)
		}
		if out, err := exec.CommandContext(ctx, "xcrun", args...).CombinedOutput(); err != nil {
			return "", p.Unsupported("cannot build the %s model adapter: %v: %s", p.Title, err, out)
		}
		if out, err := exec.CommandContext(ctx, "codesign", "--sign", "-", "--force", built).CombinedOutput(); err != nil {
			return "", p.Unsupported("cannot sign the %s model adapter: %v: %s", p.Title, err, out)
		}
		if err := os.Rename(tmp, dir); err != nil {
			if _, other := os.Stat(lib); other != nil {
				return "", err
			}
		}
		return lib, nil
	})
}

// FoundationLibrary is the Foundation Models adapter, built for the platform on an
// architecture. Swift's compiler emits the linkage names for our public-API references and
// replacements, and dyld's public DYLD_INTERPOSE facility binds them at initial process loading.
func (p Platform) FoundationLibrary(ctx context.Context, s *core.Suite, arch string) (string, error) {
	arch = clangArch(arch)
	return core.Cached(s, "models-apple/"+p.Name+"/foundation-library/"+arch, func() (string, error) {
		sdk, err := exec.CommandContext(ctx, "xcrun", "--sdk", p.SDK, "--show-sdk-path").Output()
		if err != nil {
			return "", p.Unsupported("Foundation Models needs Xcode 27: %v", err)
		}
		target := p.FoundationTarget(arch)
		sum := sha256.New()
		sum.Write(foundationSource)
		sum.Write(foundationReferences)
		sum.Write(sdk)
		sum.Write([]byte(target))
		cache := npm.CacheDir("models")
		dir := filepath.Join(cache, fmt.Sprintf("%s-foundation-%x", p.Name, sum.Sum(nil)[:12]))
		lib := filepath.Join(dir, "axx-foundation.dylib")
		if _, err := os.Stat(lib); err == nil {
			return lib, nil
		}
		if err = os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		run := func(args ...string) error {
			if out, err := exec.CommandContext(ctx, "xcrun", args...).CombinedOutput(); err != nil {
				return p.Unsupported("cannot prepare the Foundation Models adapter: %v: %s", err, out)
			}
			return nil
		}
		base := []string{"swiftc", "-sdk", strings.TrimSpace(string(sdk)), "-target", target, "-parse-as-library", "-module-cache-path", filepath.Join(tmp, "modules"), "-module-name", "AXXModels"}
		for name, data := range map[string][]byte{"foundation": foundationSource, "references": foundationReferences} {
			path := filepath.Join(tmp, name+".swift")
			if err = os.WriteFile(path, data, 0o644); err != nil {
				return "", err
			}
			if err = run(append(append([]string{}, base...), "-emit-ir", path, "-o", filepath.Join(tmp, name+".ll"))...); err != nil {
				return "", err
			}
		}
		a, err := os.ReadFile(filepath.Join(tmp, "foundation.ll"))
		if err != nil {
			return "", err
		}
		r, err := os.ReadFile(filepath.Join(tmp, "references.ll"))
		if err != nil {
			return "", err
		}
		interpose, err := p.foundationInterpose(string(a), string(r))
		if err != nil {
			return "", err
		}
		c, o := filepath.Join(tmp, "interpose.c"), filepath.Join(tmp, "interpose.o")
		if err = os.WriteFile(c, []byte(interpose), 0o644); err != nil {
			return "", err
		}
		if err = run("--sdk", p.SDK, "clang", "-target", target, "-c", c, "-o", o); err != nil {
			return "", err
		}
		built := filepath.Join(tmp, "axx-foundation.dylib")
		if err = run(append(append([]string{}, base...), "-emit-library", filepath.Join(tmp, "foundation.swift"), o, "-o", built)...); err != nil {
			return "", err
		}
		if out, err := exec.CommandContext(ctx, "codesign", "--sign", "-", "--force", built).CombinedOutput(); err != nil {
			return "", p.Unsupported("cannot sign Foundation Models adapter: %v: %s", err, out)
		}
		if err = os.Rename(tmp, dir); err != nil {
			if _, other := os.Stat(lib); other != nil {
				return "", err
			}
		}
		return lib, nil
	})
}

// clangArch is clang's name of an architecture (Go's amd64 is x86_64).
func clangArch(arch string) string {
	if arch == "amd64" {
		return "x86_64"
	}
	return arch
}

func (p Platform) foundationInterpose(adapter, refs string) (string, error) {
	symbol := regexp.MustCompile(`@"([^"\n]+)"`)
	find := func(ir, kind string, contains ...string) (string, error) {
		found := ""
		for _, line := range strings.Split(ir, "\n") {
			if !strings.HasPrefix(line, kind) {
				continue
			}
			m := symbol.FindStringSubmatch(line)
			if len(m) != 2 {
				continue
			}
			ok := true
			for _, part := range contains {
				if !strings.Contains(m[1], part) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if found != "" {
				return "", p.Unsupported("ambiguous public Foundation Models linkage")
			}
			found = m[1]
		}
		if found == "" {
			return "", p.Unsupported("the SDK did not emit the public Foundation Models API %v", contains)
		}
		return found, nil
	}
	pairs := []struct {
		replacement string
		original    []string
	}{
		// Sessions of the system model (its type in their signature)...
		{"axxInstructions", []string{"LanguageModelSessionC5model5tools12instructions", "SystemcD0C_", "InstructionsVSg"}},
		{"axxText", []string{"LanguageModelSessionC5model5tools12instructions", "SystemcD0C_", "SSSg"}},
		{"axxBuilder", []string{"LanguageModelSessionC5model5tools12instructions", "SystemcD0C_", "KXE"}},
		{"axxTranscript", []string{"LanguageModelSessionC5model5tools10transcript", "SystemcD0C_"}},
		// ...and of any model (a generic parameter's: x, or q_ after the
		// builder's Failure).
		{"axxAnyInstructions", []string{"LanguageModelSessionC5model5tools12instructionsACx_", "InstructionsVSg"}},
		{"axxAnyText", []string{"LanguageModelSessionC5model5tools12instructionsACx_", "SSSg"}},
		{"axxAnyBuilder", []string{"LanguageModelSessionC5model5tools12instructionsACq__"}},
		{"axxAnyTranscript", []string{"LanguageModelSessionC5model5tools10transcriptACx_"}},
		{"axxAvailability", []string{"SystemLanguageModelC12availability"}},
		{"axxIsAvailable", []string{"SystemLanguageModelC11isAvailable"}},
		{"axxServerAvailability", []string{"PrivateCloudComputeLanguageModelC12availability"}},
		{"axxServerIsAvailable", []string{"PrivateCloudComputeLanguageModelC11isAvailable"}},
	}
	var out strings.Builder
	out.WriteString("// dyld's public interpose section: replacement, original.\n")
	for i, pair := range pairs {
		replacement, err := find(adapter, "define swiftcc", pair.replacement)
		if err != nil {
			return "", err
		}
		original, err := find(refs, "declare swiftcc", pair.original...)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "extern void original%d(void) __asm__(\"_%s\");\nextern void replacement%d(void) __asm__(\"_%s\");\n__attribute__((used,section(\"__DATA,__interpose\"))) static const struct { const void *replacement; const void *original; } pair%d = { replacement%d, original%d };\n", i, original, i, replacement, i, i, i)
	}
	return out.String(), nil
}
