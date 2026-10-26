//go:build integration && darwin

package modelsios

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

type microphoneProbe struct {
	device, lib, executable, dir string
	sequence                     int
}

func newMicrophoneProbe(t *testing.T) *microphoneProbe {
	t.Helper()
	device := os.Getenv("AXX_IOS_MODELS_SIMULATOR")
	if device == "" {
		t.Skip("set AXX_IOS_MODELS_SIMULATOR to a disposable simulator")
	}
	bootProbeSimulator(t, t.Context(), device)
	lib, err := library(t.Context(), core.NewSuite(core.SuiteOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "probe")
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	cmd := exec.CommandContext(t.Context(), "xcrun", "--sdk", "iphonesimulator", "clang", "-target", arch+"-apple-ios16.0-simulator", "-fobjc-arc", "../internal/apple/runtime/probe.m", "-framework", "Foundation", "-framework", "Vision", "-framework", "Speech", "-framework", "CoreVideo", "-framework", "CoreMedia", "-framework", "AVFoundation", "-o", executable)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v: %s", err, out)
	}
	return &microphoneProbe{device: device, lib: lib, executable: executable, dir: filepath.Join(dir, "axx-models")}
}

func (p *microphoneProbe) queue(t *testing.T, recording modelscore.Recording) {
	t.Helper()
	p.sequence++
	writeMicrophoneFile(t, filepath.Join(p.dir, ".microphone", fmt.Sprintf("%08d.json", p.sequence)), recording)
}

func writeMicrophoneFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p *microphoneProbe) run(t *testing.T, kind, file string, microphone bool) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "xcrun", "simctl", "spawn", p.device, p.executable, kind, file)
	cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+p.lib, "SIMCTL_CHILD_AXX_MODELS_DIR="+p.dir)
	if microphone {
		cmd.Env = append(cmd.Env, "SIMCTL_CHILD_AXX_MODELS_MICROPHONE=1")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// AVCaptureDeviceInput needs an installed app's microphone authorization.
// A bare simctl spawn process cannot establish whether capture is supported.
func (p *microphoneProbe) install(t *testing.T) string {
	t.Helper()
	const bundleID = "us.nimbusxr.axx.audiodeviceprobe"
	bundle := filepath.Join(t.TempDir(), "Probe.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p.executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "probe"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + bundleID + `</string>
<key>CFBundleExecutable</key><string>probe</string><key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string><key>CFBundleShortVersionString</key><string>1.0</string>
<key>MinimumOSVersion</key><string>16.0</string><key>CFBundleSupportedPlatforms</key><array><string>iPhoneSimulator</string></array>
<key>NSMicrophoneUsageDescription</key><string>Test the simulator audio input.</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"install", p.device, bundle}, {"privacy", p.device, "grant", "microphone", bundleID}} {
		if out, err := exec.CommandContext(t.Context(), "xcrun", append([]string{"simctl"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("authorize capture probe: %v: %s", err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("xcrun", "simctl", "uninstall", p.device, bundleID).Run() })
	return bundleID
}

func TestSimulatorSpeechTellsOneSpeakersPhrasesApart(t *testing.T) {
	p := newMicrophoneProbe(t)
	dir := t.TempDir()
	utterances := modelstest.Speech(t, dir, 16000)
	c, err := platform.SpeechFeatures("ios.speech")(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(filepath.Join(dir, contract.FileName)); err != nil {
		t.Fatal(err)
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := platform.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.dir, "speech.axxmodel"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := platform.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range utterances {
		for _, kind := range []string{"speech", "speech-buffer", "speech-queued", "speech-engine"} {
			t.Run(kind+"/"+u.Name, func(t *testing.T) {
				wav := filepath.Join(dir, "input.wav")
				// App-supplied audio deliberately differs from the contract's rate.
				modelstest.WriteWAV(t, wav, 44100, match.Resample(u.Samples, 16000, 44100))
				if kind == "speech-queued" || kind == "speech-engine" {
					item, err := stub.SoundItem(a.Contract.MatchedInput(), u.Samples)
					if err != nil {
						t.Fatal(err)
					}
					at, err := a.Index(item)
					if err != nil {
						t.Fatal(err)
					}
					p.queue(t, modelscore.Recording{Rate: 16000, Samples: u.Samples, Matches: map[string]int{"ios.speech": at}})
				}
				out, err := p.run(t, kind, wav, kind == "speech-engine")
				if err != nil || !strings.Contains(out, a.Generated[u.Mapping]+"\n") {
					t.Fatalf("want %q: %v: %s", a.Generated[u.Mapping], err, out)
				}
			})
		}
	}
	t.Run("queued match is authoritative across rates", func(t *testing.T) {
		p.queue(t, modelscore.Recording{Rate: 48000, Samples: []float64{0}, Matches: map[string]int{"ios.speech": 1}})
		out, err := p.run(t, "speech-queued", "unused", false)
		if err != nil || !strings.Contains(out, a.Generated[1]+"\n") {
			t.Fatalf("precomputed match: %v: %s", err, out)
		}
	})
	t.Run("endAudio finishes the capture's current recording", func(t *testing.T) {
		p.queue(t, modelscore.Recording{Rate: 16000, Samples: utterances[0].Samples, Matches: map[string]int{"ios.speech": 0}})
		out, err := p.run(t, "speech-engine-ended", "unused", true)
		if err != nil || !strings.Contains(out, a.Generated[0]+"\n") {
			t.Fatalf("endAudio during capture: %v: %s", err, out)
		}
	})
	t.Run("queued features need their match", func(t *testing.T) {
		p.queue(t, modelscore.Recording{Rate: 16000, Samples: utterances[0].Samples})
		out, err := p.run(t, "speech-queued", "unused", false)
		if err == nil || !strings.Contains(out, "The recording was not matched to this speech stub; rebuild the stub and queue the recording again") {
			t.Fatalf("missing match: %v: %s", err, out)
		}
	})
}

func TestSimulatorCaptureHearsTheRecording(t *testing.T) {
	p := newMicrophoneProbe(t)
	// A missing axx-models directory is valid in a capture-only scenario.
	out, err := p.run(t, "capture-devices", "unused", true)
	if err != nil {
		t.Fatalf("capture device discovery without artifacts: %v: %s", err, out)
	}
	t.Log(strings.TrimSpace(out))
	phrase := modelstest.Speech(t, t.TempDir(), 16000)[0].Samples
	for _, kind := range []string{"capture-engine", "capture-engine-44100", "capture-graph", "capture-recorder", "capture-recorder-duration", "capture-session"} {
		t.Run(kind, func(t *testing.T) {
			p.queue(t, modelscore.Recording{Rate: 16000, Samples: phrase})
			file := filepath.Join(t.TempDir(), "capture.pcm")
			var out string
			var err error
			if kind == "capture-session" {
				id := p.install(t)
				cmd := exec.CommandContext(t.Context(), "xcrun", "simctl", "launch", "--console", p.device, id, kind, file)
				cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+p.lib, "SIMCTL_CHILD_AXX_MODELS_DIR="+p.dir, "SIMCTL_CHILD_AXX_MODELS_MICROPHONE=1")
				var data []byte
				data, err = cmd.CombinedOutput()
				out = string(data)
				if strings.Contains(out, "no audio capture device") {
					t.Skip("the simulator exposes no AVCaptureDevice for audio")
				}
			} else {
				out, err = p.run(t, kind, file, true)
			}
			rate := 48000
			if strings.Contains(kind, "recorder") || strings.HasSuffix(kind, "44100") {
				rate = 44100
			}
			if err != nil || !strings.Contains(out, fmt.Sprintf("captured %d\n", rate)) {
				t.Fatalf("capture: %v: %s", err, out)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want := match.Resample(phrase, 16000, rate)
			if len(data)/4 < 4*rate {
				t.Fatalf("captured %d frames, want at least %d", len(data)/4, 4*rate)
			}
			var worst float64
			for i := 0; i < len(data)/4; i++ {
				v := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:])))
				expected := 0.0
				if i < len(want) {
					expected = want[i]
				}
				worst = max(worst, math.Abs(v-expected))
			}
			if worst > 2e-4 {
				t.Fatalf("capture differs from reference resampling followed by silence by %g, want <= 2e-4", worst)
			}
		})
	}
}

func TestSimulatorCaptureConsumesRecordingsInOrder(t *testing.T) {
	p := newMicrophoneProbe(t)
	first := modelstest.Doorbell.Samples(16000, 0.5, 0, 1, 0)
	second := modelstest.Knock.Samples(16000, 0.75, 0, 1, 0)
	p.queue(t, modelscore.Recording{Rate: 16000, Samples: first})
	p.queue(t, modelscore.Recording{Rate: 16000, Samples: second})
	file := filepath.Join(t.TempDir(), "capture.pcm")
	if out, err := p.run(t, "capture-engine", file, true); err != nil {
		t.Fatalf("capture: %v: %s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := append(match.Resample(first, 16000, 48000), match.Resample(second, 16000, 48000)...)
	if len(data)/4 < len(want)+48000 {
		t.Fatal("capture omitted the second recording or following silence")
	}
	for i := 0; i < len(data)/4; i++ {
		got := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:])))
		expected := 0.0
		if i < len(want) {
			expected = want[i]
		}
		if math.Abs(got-expected) > 2e-4 {
			t.Fatalf("frame %d: got %g, want %g", i, got, expected)
		}
	}
	for i := 1; i <= 2; i++ {
		if _, err := os.Stat(filepath.Join(p.dir, ".microphone", fmt.Sprintf("%08d.json.taken", i))); err != nil {
			t.Fatal(err)
		}
	}
}
