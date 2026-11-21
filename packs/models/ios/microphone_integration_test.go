//go:build integration && darwin

package modelsios

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// probeBundleID is the probe's bundle identifier, where it is installed as an app.
const probeBundleID = "us.nimbusxr.axx.audiodeviceprobe"

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
	cmd := exec.CommandContext(t.Context(), "xcrun", "--sdk", "iphonesimulator", "clang", "-target", arch+"-apple-ios16.0-simulator", "-fobjc-arc", "../internal/apple/runtime/probe.m", "-framework", "Foundation", "-framework", "Vision", "-framework", "Speech", "-framework", "CoreVideo", "-framework", "CoreMedia", "-framework", "AVFoundation", "-framework", "Accelerate", "-framework", "ImageIO", "-framework", "CoreGraphics", "-o", executable)
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
	const bundleID = probeBundleID
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
	// The probe is launched right after it is allowed the microphone, and that is safe. The
	// simulator does end an app that runs as what the app is allowed changes, but it ends the
	// processes it finds then, each by its process id, before it answers the change, and
	// simctl returns after that answer: nothing is left to end an app launched later. On an
	// iOS 27.2 simulator an app launched as simctl returned kept its process, 30 times of 30.
	for _, args := range [][]string{{"install", p.device, bundle}, {"privacy", p.device, "grant", "microphone", bundleID}} {
		if out, err := exec.CommandContext(t.Context(), "xcrun", append([]string{"simctl"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("authorize capture probe: %v: %s", err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("xcrun", "simctl", "uninstall", p.device, bundleID).Run() })
	return bundleID
}

// launch launches the installed probe as an app, waits for its process to end, and returns what
// it said. It does not have simctl's launch --console relay that: simctl looks once a second
// whether the app's process still runs, and exits as soon as it does not, whatever it has not
// read yet of what the app said. The probe says how its session went in its last moments, so on
// a Mac short of processor time those lines can be lost, and a session that went well then
// reads as one that ended without a word, with simctl answering 0. CI's Mac gave two such runs
// in a day (the one that had the probe's lines of its progress lacked its last three, and no
// other); and with simctl starved here, an app that only talks lost its last lines in 2 runs of
// 139 (the simulator's log, read for one of them, has the app exit by itself). The simulator
// writes what the probe says to files instead, which are whole once its process has ended.
func (p *microphoneProbe) launch(t *testing.T, bundleID string, args ...string) (string, error) {
	t.Helper()
	// simctl puts the files in the simulator's own folders: in its temporary folder, then.
	tmp, err := exec.CommandContext(t.Context(), "xcrun", "simctl", "getenv", p.device, "TMPDIR").Output()
	if err != nil {
		return "", fmt.Errorf("the simulator's temporary folder: %w", err)
	}
	dir, err := os.MkdirTemp(strings.TrimSpace(string(tmp)), "axx-probe-")
	if err != nil {
		return "", err
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	stdout, stderr := filepath.Join(dir, "stdout"), filepath.Join(dir, "stderr")
	cmd := exec.CommandContext(t.Context(), "xcrun", append([]string{"simctl", "launch", "--stdout=" + stdout, "--stderr=" + stderr, p.device, bundleID}, args...)...)
	cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+p.lib, "SIMCTL_CHILD_AXX_MODELS_DIR="+p.dir, "SIMCTL_CHILD_AXX_MODELS_MICROPHONE=1")
	launched, err := cmd.CombinedOutput()
	if err != nil {
		return string(launched), err
	}
	// simctl answers with the app's process, us.nimbusxr.axx.audiodeviceprobe: 14365, and a
	// simulator's processes are the Mac's own.
	_, id, _ := strings.Cut(strings.TrimSpace(string(launched)), ": ")
	pid, err := strconv.Atoi(id)
	if err != nil {
		return string(launched), fmt.Errorf("simctl did not say which process it launched: %q", launched)
	}
	said := func() string {
		errs, _ := os.ReadFile(stderr)
		outs, _ := os.ReadFile(stdout)
		return string(errs) + string(outs)
	}
	// The probe hears for 30 seconds at most, and a slow Mac takes long to launch it besides.
	for deadline := time.Now().Add(2 * time.Minute); !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			_ = exec.Command("xcrun", "simctl", "terminate", p.device, bundleID).Run()
			return said(), errors.New("the probe still ran two minutes after it was launched")
		}
	}
	return said(), nil
}

// ended is how the simulator says the app's processes ended, for a failure's report:
// SpringBoard's line of each, which calls an exit of the app's own voluntary, and names what
// ended one that was ended (the privacy service is the tcc domain).
func (p *microphoneProbe) ended(bundleID string) string {
	predicate := `process == "SpringBoard" AND composedMessage CONTAINS "` + bundleID + `" AND composedMessage CONTAINS "Process exited"`
	out, err := exec.Command("xcrun", "simctl", "spawn", p.device, "log", "show", "--last", "3m", "--style", "compact", "--predicate", predicate).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("the simulator's log cannot be read (%v)", err)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "Process exited") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return "the simulator's log has no end of a process of it"
	}
	return strings.Join(lines, "\n")
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
				began := time.Now()
				id := p.install(t)
				installed := time.Since(began)
				out, err = p.launch(t, id, kind, file)
				// The probe says how long its session took; the rest of this is the simulator
				// installing and launching it, 12 to 26 seconds of it on CI's Mac.
				t.Logf("the probe was installed and allowed the microphone in %s, and launched and ended in %s",
					installed.Round(100*time.Millisecond), (time.Since(began) - installed).Round(100*time.Millisecond))
				if strings.Contains(out, "no audio capture device") {
					t.Skip("the simulator exposes no AVCaptureDevice for audio")
				}
				// How long each part of the session took, and why it failed if it did.
				t.Log(strings.TrimSpace(out))
			} else {
				out, err = p.run(t, kind, file, true)
			}
			rate := 48000
			if strings.Contains(kind, "recorder") || strings.HasSuffix(kind, "44100") {
				rate = 44100
			}
			if err != nil || !strings.Contains(out, fmt.Sprintf("captured %d\n", rate)) {
				if kind == "capture-session" {
					t.Fatalf("capture: %v: %s\nthe simulator, of how the probe's process ended:\n%s", err, out, p.ended(probeBundleID))
				}
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
