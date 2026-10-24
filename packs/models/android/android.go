// Package modelsandroid answers platform APIs inside debuggable emulator apps
// through an ART TI agent attached before application binding.
package modelsandroid

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/npm"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobileandroid "github.com/nimbusxr/axx/packs/mobile/android"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

const Name = "models-android"

//go:embed runtime/agent.cc runtime/Bridge.java
var sources embed.FS

func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Requires: []string{modelscore.Name, mobileandroid.Name, appcore.Name}, Steps: []core.StepDef{},
		Doc:   "Answer SpeechRecognizer, ML Kit built-in image labeling, text recognition, object detection and selfie segmentation, and ML Kit Gemini Nano text generation in debuggable emulator apps from prebuilt .axxmodel files in axx-models. Speech uses log-mel matching for app-provided audio and the microphone step's precomputed matches for queued recordings. Microphone scenarios play recordings into AudioRecord, converted to its rate, channels and PCM format, then silence. MediaRecorder audio capture uses documented JNI registration for its public native controls and public codecs to write the requested file on stop. It supports pause/resume, amplitude, reset/release, paths, File and FileDescriptor outputs, preserving the app's descriptor ownership and microphone permission. It requires the public MediaRecorder(Context) constructor and Java reset/release methods; video, automatic duration/file-size limits and output rotation fail explicitly. The ART agent attaches before application binding. Vision inputs use InputImage.fromBitmap; custom ML Kit model clients retain their native loader. Text generation includes single-chunk streaming through the Prompt API.",
		Hooks: []core.Hook{{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare}},
	}
}

var started = core.NewStateKey(Name+"/started", func(*core.Scenario) *sync.Once { return &sync.Once{} }, nil)

func prepare(sc *core.Scenario) error {
	started.Of(sc).Do(func() { mobileandroid.OnLaunch(sc, func(l *mobileandroid.Launch) error { return configure(sc, l) }) })
	return nil
}

func problem(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, "Android model adapter: "+f, args...).WithHint("use a debuggable emulator app, a supported API artifact, and an Android SDK with an NDK, build-tools and a JDK")
}

func configure(sc *core.Scenario, l *mobileandroid.Launch) error {
	f, err := appcore.Files(sc, l.App, platform.Folder)
	if err != nil {
		return err
	}
	artifacts, err := platform.Load(sc.Context(), f)
	if err != nil {
		return err
	}
	// The microphone step's recordings play into the app's own audio capture.
	listens := modelscore.UsesMicrophone(sc)
	if listens {
		modelscore.FeedsMicrophone(sc, l.App)
	}
	if len(artifacts) == 0 && !listens {
		return nil
	}
	if !l.Emulator {
		return problem("%s must run on a local emulator", l.App)
	}
	for _, a := range artifacts {
		if err := supported(a); err != nil {
			return err
		}
	}
	if l.Prepare != nil {
		if err := l.Prepare(); err != nil {
			return err
		}
	}
	if l.Agent != "" {
		return problem("another launch hook already supplied an agent")
	}
	dir, err := build(sc.Context(), l.SDKDir, l.ABI)
	if err != nil {
		return err
	}
	cache, err := l.Files(sc.Context(), "code_cache/axx-models")
	if err != nil {
		return err
	}
	for _, name := range []string{"libaxx-models.so", "classes.dex"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if err := cache.Write(sc.Context(), name, b); err != nil {
			return err
		}
	}
	if err := cache.Write(sc.Context(), "status", nil); err != nil {
		return err
	}
	// Whether the app's own audio capture hears the microphone step.
	if err := cache.Write(sc.Context(), "microphone", []byte(strconv.FormatBool(listens))); err != nil {
		return err
	}
	remote := l.DataDir + "/code_cache/axx-models"
	l.Agent = remote + "/libaxx-models.so=" + remote
	l.After = append(l.After, func() error {
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			b, _, err := cache.Read(sc.Context(), "status")
			if err != nil {
				return err
			}
			if string(b) == "ready" {
				return nil
			}
			if strings.HasPrefix(string(b), "error:") {
				return problem("%s", b)
			}
			select {
			case <-sc.Context().Done():
				return sc.Context().Err()
			case <-deadline.C:
				return problem("the debug agent did not report readiness; Android refused or failed to load it")
			case <-tick.C:
			}
		}
	})
	return nil
}

func supported(a *platform.Artifact) error {
	want := apiTask(a.API)
	if want == "" {
		return problem("%s is not supported", a.API)
	}
	if a.Contract.Task != want {
		return problem("%s needs task %s", a.API, want)
	}
	if want == "text-generation" {
		if a.Matcher != stub.TextMatcher {
			return problem("Gemini Nano requires text mappings")
		}
		return nil
	}
	if a.Matcher != stub.NearestMatcher {
		return problem("%s needs nearest image or audio mappings", a.API)
	}
	in := a.Contract.MatchedInput()
	if want == "speech-to-text" {
		if in == nil || in.Audio == nil || in.Audio.Features != "" && in.Audio.Features != "log-mel" {
			return problem("speech needs an audio input: samples, or log-mel features")
		}
		return nil
	}
	if in == nil || in.Image == nil {
		return problem("%s needs an image input", a.API)
	}
	for _, out := range a.Contract.Outputs {
		if out.Quant != nil {
			return problem("ML Kit API outputs must be unquantized")
		}
	}
	if want == "detection" {
		roles := map[string]bool{}
		for _, out := range a.Contract.Outputs {
			roles[out.Detection] = true
			if out.Box != nil && (out.Box.Pixels || out.Box.Order != "" && out.Box.Order != "ymin,xmin,ymax,xmax") {
				return problem("detections need normalized ymin,xmin,ymax,xmax boxes")
			}
		}
		if !roles["boxes"] || !roles["classes"] || !roles["scores"] || !roles["count"] {
			return problem("detections need boxes, classes, scores and count outputs")
		}
		return nil
	}
	if len(a.Contract.Outputs) != 1 || a.Contract.Outputs[0].Activation == "none" {
		return problem("%s needs one probability output", a.API)
	}
	out := a.Contract.Outputs[0]
	shape := out.Shape
	if out.Batched() {
		shape = shape[1:]
	}
	if want == "text-recognition" && (len(shape) != 2 || int(shape[1].Size) != len(out.Labels)) {
		return problem("text recognition needs [steps, characters] CTC probabilities")
	}
	if want == "segmentation" && (len(shape) != 2 || len(out.Labels) > 1) {
		return problem("segmentation needs [height,width] foreground probabilities")
	}
	return nil
}

// Helpers are compiled during tool setup, separately from the committed model
// artifacts. The host Go binary remains pure Go, including CGO_ENABLED=0.
func build(ctx context.Context, sdk, abi string) (string, error) {
	triple := map[string]string{"arm64-v8a": "aarch64-linux-android26", "x86_64": "x86_64-linux-android26"}[abi]
	if triple == "" {
		return "", problem("unsupported emulator ABI %s", abi)
	}
	host := map[string]string{"darwin": "darwin-x86_64", "linux": "linux-x86_64", "windows": "windows-x86_64"}[runtime.GOOS]
	ndk := os.Getenv("ANDROID_NDK_HOME")
	if ndk == "" {
		dirs, _ := filepath.Glob(filepath.Join(sdk, "ndk", "*"))
		slices.Sort(dirs)
		for i := len(dirs) - 1; i >= 0; i-- {
			compiler := triple + "-clang++"
			if runtime.GOOS == "windows" {
				compiler += ".cmd"
			}
			if _, err := os.Stat(filepath.Join(dirs[i], "toolchains", "llvm", "prebuilt", host, "bin", compiler)); err == nil {
				ndk = dirs[i]
				break
			}
		}
	}
	tools, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*"))
	slices.Sort(tools)
	platforms, _ := filepath.Glob(filepath.Join(sdk, "platforms", "android-*", "android.jar"))
	slices.Sort(platforms)
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome == "" && runtime.GOOS == "darwin" {
		out, err := exec.CommandContext(ctx, "/usr/libexec/java_home").Output()
		if err == nil {
			javaHome = strings.TrimSpace(string(out))
		}
	}
	if javaHome == "" {
		javac, err := exec.LookPath("javac")
		if err == nil {
			real, _ := filepath.EvalSymlinks(javac)
			javaHome = filepath.Dir(filepath.Dir(real))
		}
	}
	if ndk == "" || len(tools) == 0 || len(platforms) == 0 || javaHome == "" {
		return "", problem("the NDK, build-tools, Android platform or JDK is missing")
	}
	header, err := os.ReadFile(filepath.Join(javaHome, "include", "jvmti.h"))
	if err != nil {
		return "", problem("cannot read the JDK's jvmti.h: %v", err)
	}
	agent, _ := sources.ReadFile("runtime/agent.cc")
	bridge, _ := sources.ReadFile("runtime/Bridge.java")
	h := sha256.New()
	for _, data := range [][]byte{agent, bridge, header, []byte(ndk + tools[len(tools)-1] + platforms[len(platforms)-1] + javaHome + abi)} {
		h.Write(data)
	}
	cache := npm.CacheDir("models")
	dir := filepath.Join(cache, fmt.Sprintf("android-%x", h.Sum(nil)[:12]))
	if _, err := os.Stat(filepath.Join(dir, "classes.dex")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cache, "prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for name, data := range map[string][]byte{"agent.cc": agent, "Bridge.java": bridge, "jvmti.h": header} {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return "", err
		}
	}
	classes := filepath.Join(tmp, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return "", err
	}
	compiler := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", host, "bin", triple+"-clang++")
	if runtime.GOOS == "windows" {
		compiler += ".cmd"
	}
	javac := filepath.Join(javaHome, "bin", "javac")
	d8 := filepath.Join(tools[len(tools)-1], "d8")
	if runtime.GOOS == "windows" {
		javac += ".exe"
		d8 += ".bat"
	}
	run := func(exe string, args ...string) error {
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), "JAVA_HOME="+javaHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			return problem("%s: %v: %s", filepath.Base(exe), err, out)
		}
		return nil
	}
	if err := run(compiler, "-shared", "-fPIC", "-static-libstdc++", "-std=c++17", "-O2", "-I"+tmp, filepath.Join(tmp, "agent.cc"), "-llog", "-o", filepath.Join(tmp, "libaxx-models.so")); err != nil {
		return "", err
	}
	if err := run(javac, "--release", "8", "-classpath", platforms[len(platforms)-1], "-d", classes, filepath.Join(tmp, "Bridge.java")); err != nil {
		return "", err
	}
	args := []string{"--min-api", "26", "--lib", platforms[len(platforms)-1], "--output", tmp}
	if err := filepath.WalkDir(classes, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".class") {
			args = append(args, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if err := run(d8, args...); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, other := os.Stat(filepath.Join(dir, "classes.dex")); other != nil {
			return "", err
		}
	}
	return dir, nil
}
