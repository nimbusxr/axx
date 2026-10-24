//go:build integration

package modelsandroid

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// probe is the probe app, installed on the test's emulator with the
// adapter in its code cache, as a launch puts it there.
type probe struct {
	t           *testing.T
	ctx         context.Context
	adb, serial string
	root        string
}

const probePackage = "us.nimbusxr.axx.modelsprobe"

func newProbe(t *testing.T) *probe {
	serial, sdk := os.Getenv("AXX_MODELS_ANDROID_SERIAL"), os.Getenv("ANDROID_HOME")
	if serial == "" {
		t.Skip("set AXX_MODELS_ANDROID_SERIAL to a disposable emulator")
	}
	if sdk == "" {
		t.Fatal("set ANDROID_HOME")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	p := &probe{t: t, ctx: ctx, adb: filepath.Join(sdk, "platform-tools", "adb"), serial: serial}
	abi := strings.TrimSpace(string(p.device("shell", "getprop", "ro.product.cpu.abi")))
	dir, err := build(ctx, sdk, abi)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("adapter built at %s", dir)
	work := t.TempDir()
	tools, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*"))
	slices.Sort(tools)
	tool := tools[len(tools)-1]
	platforms, _ := filepath.Glob(filepath.Join(sdk, "platforms", "android-*", "android.jar"))
	slices.Sort(platforms)
	android := platforms[len(platforms)-1]
	classes := filepath.Join(work, "classes")
	if err = os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	p.run("javac", "--release", "8", "-classpath", android, "-d", classes, "testdata/Probe.java")
	p.run(filepath.Join(tool, "d8"), "--min-api", "26", "--lib", android, "--output", work, filepath.Join(classes, "us/nimbusxr/axx/modelsprobe/Probe.class"))
	unsigned := filepath.Join(work, "unsigned.apk")
	p.run(filepath.Join(tool, "aapt2"), "link", "-I", android, "--manifest", "testdata/AndroidManifest.xml", "-o", unsigned)
	zipBytes, err := os.ReadFile(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatal(err)
	}
	var apk bytes.Buffer
	writer := zip.NewWriter(&apk)
	for _, f := range reader.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		w, err := writer.CreateHeader(&f.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.Copy(w, r); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
	}
	dex, err := os.ReadFile(filepath.Join(work, "classes.dex"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := writer.Create("classes.dex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(dex); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(unsigned, apk.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	keystore := filepath.Join(work, "debug.jks")
	p.run("keytool", "-genkeypair", "-keystore", keystore, "-storepass", "android", "-keypass", "android", "-alias", "debug", "-dname", "CN=axx model probe", "-keyalg", "RSA", "-validity", "3650")
	signed := filepath.Join(work, "probe.apk")
	p.run(filepath.Join(tool, "apksigner"), "sign", "--ks", keystore, "--ks-pass", "pass:android", "--out", signed, unsigned)
	// This package is solely the test's probe, never a user's application.
	_ = exec.CommandContext(ctx, p.adb, "-s", serial, "uninstall", probePackage).Run()
	p.device("install", signed)
	t.Cleanup(func() { _ = exec.Command(p.adb, "-s", serial, "uninstall", probePackage).Run() })
	p.device("shell", "pm", "grant", probePackage, "android.permission.RECORD_AUDIO")
	for _, name := range []string{"libaxx-models.so", "classes.dex"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		p.put("code_cache/axx-models/"+name, b)
	}
	p.root = strings.TrimSpace(string(p.device("shell", "run-as", probePackage, "pwd"))) + "/code_cache/axx-models"
	return p
}

func (p *probe) run(exe string, args ...string) []byte {
	p.t.Helper()
	out, err := exec.CommandContext(p.ctx, exe, args...).CombinedOutput()
	if err != nil {
		p.t.Fatalf("%s %v: %v: %s", exe, args, err, out)
	}
	return out
}

func (p *probe) device(args ...string) []byte {
	p.t.Helper()
	return p.run(p.adb, append([]string{"-s", p.serial}, args...)...)
}

// put writes a file of the probe's, in its data folder.
func (p *probe) put(name string, b []byte) {
	p.t.Helper()
	cmd := exec.CommandContext(p.ctx, p.adb, "-s", p.serial, "shell", "run-as "+probePackage+" sh -c 'mkdir -p "+filepath.ToSlash(filepath.Dir(name))+" && cat > "+name+"'")
	cmd.Stdin = bytes.NewReader(b)
	if out, err := cmd.CombinedOutput(); err != nil {
		p.t.Fatalf("write %s: %v: %s", name, err, out)
	}
}

func (p *probe) read(name string) ([]byte, error) {
	// Shell v2 without a PTY preserves binary data and the remote exit code;
	// exec-out otherwise reports success even when cat found no such file.
	return exec.CommandContext(p.ctx, p.adb, "-s", p.serial, "shell", "-T", "run-as", probePackage, "cat", name).Output()
}

// queue queues a recording, as the microphone step does.
func (p *probe) queue(n int, r modelscore.Recording) {
	p.t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		p.t.Fatal(err)
	}
	p.put(fmt.Sprintf("axx-models/.microphone/%08d.json", n), b)
}

// start starts the probe afresh with the adapter, and waits for what it
// writes to files/transcript.txt.
func (p *probe) start(extras ...string) string {
	p.t.Helper()
	p.device("shell", "am", "force-stop", probePackage)
	p.put("code_cache/axx-models/status", nil)
	p.device("shell", "run-as", probePackage, "rm", "-f", "files/transcript.txt")
	p.device(append([]string{"shell", "am", "start", "-W", "--attach-agent", p.root + "/libaxx-models.so=" + p.root, "-n", probePackage + "/.Probe"}, extras...)...)
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		out, err := exec.CommandContext(p.ctx, p.adb, "-s", p.serial, "shell", "run-as", probePackage, "cat", "files/transcript.txt").CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		if status, err := p.read("code_cache/axx-models/status"); err == nil && bytes.HasPrefix(status, []byte("error: ")) {
			return strings.TrimSpace(string(status))
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, _ := exec.CommandContext(p.ctx, p.adb, "-s", p.serial, "logcat", "-d", "-s", "AXXModels:V", "AndroidRuntime:E", "*:S").CombinedOutput()
	p.t.Fatalf("the probe did not answer: %s", out)
	return ""
}

func (p *probe) artifact(name string, s *stub.Stub) *platform.Artifact {
	p.t.Helper()
	b, err := platform.Write(s)
	if err != nil {
		p.t.Fatal(err)
	}
	p.put("axx-models/"+name, b)
	var a platform.Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		p.t.Fatal(err)
	}
	return &a
}

func TestAndroidSpeechUsesPrebuiltFixture(t *testing.T) {
	p := newProbe(t)
	c := modelstest.SoundClassifier()
	c.Task = "speech-to-text"
	c.Outputs = nil
	c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"android.speech"}`)}
	wav := filepath.Join(t.TempDir(), "bell.wav")
	samples := modelstest.Doorbell.Samples(16000, 0.975, 0, 1, 0)
	modelstest.WriteWAV(t, wav, 16000, samples)
	s, err := stub.Resolve(c, []mappings.Mapping{{Name: "bell", Input: mappings.Input{"audio": json.RawMessage(`"` + filepath.ToSlash(wav) + `"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"text":"Try again."}`)}})
	if err != nil {
		t.Fatal(err)
	}
	p.artifact("speech.axxmodel", s)
	p.queue(1, modelscore.Recording{Rate: 16000, Samples: samples})
	for _, ondevice := range []string{"false", "true"} {
		t.Run("ondevice="+ondevice, func(t *testing.T) {
			p.device("shell", "run-as", probePackage, "rm", "-f", "axx-models/.microphone/00000001.json.taken")
			if got := p.start("--ez", "ondevice", ondevice); got != "Ring twice, please." {
				t.Fatalf("recognizer returned %s", got)
			}
		})
	}
}

// One speaker's phrases are told apart by a stub of log-mel features: a
// queued recording answers the mapping axx matched it with, and audio the
// app gives the recognizer itself is matched on the phone as axx matches.
func TestAndroidSpeechTellsOneSpeakersPhrasesApart(t *testing.T) {
	p := newProbe(t)
	dir := t.TempDir()
	utterances := modelstest.Speech(t, dir, 16000)
	c, err := platform.SpeechFeatures("android.speech")(nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"android.speech"}`)}
	if err := c.Save(filepath.Join(dir, contract.FileName)); err != nil {
		t.Fatal(err)
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := p.artifact("speech.axxmodel", s)
	for i, u := range utterances {
		want := a.Generated[u.Mapping]
		t.Run("queued "+u.Name, func(t *testing.T) {
			item, err := stub.SoundItem(a.Contract.MatchedInput(), u.Samples)
			if err != nil {
				t.Fatal(err)
			}
			at, err := a.Index(item)
			if err != nil {
				t.Fatal(err)
			}
			p.queue(i+1, modelscore.Recording{Rate: 16000, Samples: u.Samples, Matches: map[string]int{"android.speech": at}})
			if got := p.start(); got != want {
				t.Fatalf("recognizer returned %q, want %q", got, want)
			}
		})
		t.Run("the app's "+u.Name, func(t *testing.T) {
			pcm := make([]byte, 2*len(u.Samples))
			for j, v := range u.Samples {
				binary.LittleEndian.PutUint16(pcm[2*j:], uint16(int16(math.Round(max(-1, min(1, v))*32767))))
			}
			p.put("files/source.pcm", pcm)
			if got := p.start("--es", "kind", "source"); got != want {
				t.Fatalf("recognizer returned %q, want %q", got, want)
			}
		})
	}
}

// The app's own AudioRecord hears the queued recording at its rate, then
// silence, however it reads; a MediaRecorder's file has it.
func TestAndroidCaptureHearsTheRecording(t *testing.T) {
	p := newProbe(t)
	p.put("code_cache/axx-models/microphone", []byte("true"))
	all, err := match.ReadSound(modelstest.Fetch(t, "https://raw.githubusercontent.com/ggml-org/whisper.cpp/231638a0717b7822784297e5980965ffcec05810/samples/jfk.wav", "59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e"), 16000)
	if err != nil {
		t.Fatal(err)
	}
	phrase := all[int(0.3*16000):int(3.0*16000)]
	for i, kind := range []string{"record-short", "record-float", "record-buffer"} {
		t.Run(kind, func(t *testing.T) {
			p.queue(10+i, modelscore.Recording{Rate: 16000, Samples: phrase})
			got := p.start("--es", "kind", kind)
			var rate int
			if _, err := fmt.Sscanf(got, "recorded %d", &rate); err != nil {
				t.Fatalf("the probe says %s", got)
			}
			heard := floats(t, p)
			want := match.Resample(phrase, 16000, rate)
			if len(heard) != 4*rate {
				t.Fatalf("heard %d frames, want %d", len(heard), 4*rate)
			}
			var worst float64
			for j, v := range heard {
				w := 0.0
				if j < len(want) {
					w = want[j]
				}
				worst = max(worst, math.Abs(v-w))
			}
			if worst > 2e-4 {
				t.Fatalf("heard the recording, then silence, within %g; want 2e-4", worst)
			}
		})
	}
	for i, kind := range []string{"recorder", "recorder-paused-file", "recorder-fd", "recorder-fd-closed", "recorder-reset", "recorder-release"} {
		t.Run(kind, func(t *testing.T) {
			local := *p
			local.t = t
			p := &local
			if kind == "recorder-release" || kind == "recorder-reset" {
				p.queue(20+2*i, modelscore.Recording{Rate: 16000, Samples: phrase[:4800]})
			}
			p.queue(21+2*i, modelscore.Recording{Rate: 16000, Samples: phrase})
			got := p.start("--es", "kind", kind)
			var rate, loud int
			if _, err := fmt.Sscanf(got, "recorded %d loud %d", &rate, &loud); err != nil || rate != 16000 {
				t.Fatalf("the probe says %s", got)
			}
			if loud < 3000 {
				t.Errorf("the recorder's loudest while it played is %d", loud)
			}
			heard := floats(t, p)
			if len(heard) < 4*rate || len(heard) > 43*rate/10 {
				t.Fatalf("recorded %d frames, want four seconds plus codec padding", len(heard))
			}
			// AAC is lossy and starts late: the sound's loudness, a tenth of a
			// second at a time, follows the recording's, then silence.
			window := rate / 10
			level := func(s []float64, at int) float64 {
				var sum float64
				for j := at; j < at+window && j < len(s); j++ {
					sum += s[j] * s[j]
				}
				return math.Sqrt(sum / float64(window))
			}
			var a, b []float64
			for at := 0; at+window <= len(phrase); at += window {
				a = append(a, level(phrase, at))
				b = append(b, level(heard, at+2048))
			}
			r := correlation(a, b)
			t.Logf("amplitude %d; loudness correlation %.4f; %d decoded frames", loud, r, len(heard))
			if math.IsNaN(r) || r < 0.9 {
				t.Errorf("the file's loudness follows the recording's by %.2f", r)
			}
			for at := len(phrase) + rate/4; at+window <= min(len(heard), 4*rate-window); at += window {
				if l := level(heard, at); l > 0.01 {
					t.Fatalf("the file is not silent after the recording, at %d: %g", at, l)
				}
			}
		})
	}
}

func TestAndroidRecorderRequiresPermissionAndAudioCapture(t *testing.T) {
	p := newProbe(t)
	p.put("code_cache/axx-models/microphone", []byte("true"))
	p.queue(1, modelscore.Recording{Rate: 16000, Samples: []float64{0.25, 0.5, 0.25}})
	p.device("shell", "pm", "revoke", probePackage, "android.permission.RECORD_AUDIO")
	if got := p.start("--es", "kind", "recorder"); !strings.Contains(got, "SecurityException") || !strings.Contains(got, "RECORD_AUDIO") {
		t.Fatalf("recording without microphone permission: %s", got)
	}
	p.device("shell", "pm", "grant", probePackage, "android.permission.RECORD_AUDIO")
	if got := p.start("--es", "kind", "recorder-video"); !strings.HasPrefix(got, "error:") || !strings.Contains(got, "audio-only recording") {
		t.Fatalf("unsupported video recording did not fail explicitly: %s", got)
	}
	if _, err := p.read("axx-models/.microphone/00000001.json.taken"); err == nil {
		t.Fatal("a denied or unsupported capture consumed the recording")
	}
}

func floats(t *testing.T, p *probe) []float64 {
	t.Helper()
	b, err := p.read("files/capture.pcm")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, len(b)/4)
	for i := range out {
		out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:])))
	}
	return out
}

func correlation(a, b []float64) float64 {
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= float64(len(a))
	mb /= float64(len(b))
	var ab, aa, bb float64
	for i := range a {
		ab += (a[i] - ma) * (b[i] - mb)
		aa += (a[i] - ma) * (a[i] - ma)
		bb += (b[i] - mb) * (b[i] - mb)
	}
	return ab / math.Sqrt(aa*bb)
}
