//go:build integration && darwin

package modelsmacos

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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
	lib, executable, dir string
	sequence             int
}

func newMicrophoneProbe(t *testing.T) *microphoneProbe {
	t.Helper()
	lib, err := mac.Library(t.Context(), core.NewSuite(core.SuiteOptions{}), arch())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	return &microphoneProbe{lib: lib, executable: probe(t, dir), dir: filepath.Join(dir, "axx-models")}
}

// queue queues a recording, as the microphone step does.
func (p *microphoneProbe) queue(t *testing.T, recording modelscore.Recording) {
	t.Helper()
	p.sequence++
	data, err := json.Marshal(recording)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.dir, ".microphone", fmt.Sprintf("%08d.json", p.sequence))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p *microphoneProbe) run(t *testing.T, kind, file string, microphone bool) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), p.executable, kind, file)
	cmd.Env = append(os.Environ(), "DYLD_INSERT_LIBRARIES="+p.lib, "AXX_MODELS_DIR="+p.dir)
	if microphone {
		cmd.Env = append(cmd.Env, "AXX_MODELS_MICROPHONE=1")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestMacSpeechTellsOneSpeakersPhrasesApart(t *testing.T) {
	p := newMicrophoneProbe(t)
	dir := t.TempDir()
	utterances := modelstest.Speech(t, dir, 16000)
	c, err := platform.SpeechFeatures("macos.speech")(nil)
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
					p.queue(t, modelscore.Recording{Rate: 16000, Samples: u.Samples, Matches: map[string]int{"macos.speech": at}})
				}
				out, err := p.run(t, kind, wav, kind == "speech-engine")
				if err != nil || !strings.Contains(out, a.Generated[u.Mapping]+"\n") {
					t.Fatalf("want %q: %v: %s", a.Generated[u.Mapping], err, out)
				}
			})
		}
	}
	t.Run("endAudio finishes the capture's current recording", func(t *testing.T) {
		p.queue(t, modelscore.Recording{Rate: 16000, Samples: utterances[0].Samples, Matches: map[string]int{"macos.speech": 0}})
		out, err := p.run(t, "speech-engine-ended", "unused", true)
		if err != nil || !strings.Contains(out, a.Generated[0]+"\n") {
			t.Fatalf("endAudio during capture: %v: %s", err, out)
		}
	})
}

// A Mac program's audio capture hears the queued recording, then silence. An AVCaptureSession
// takes a real capture device, which macOS lets a program use only once its user allows it (a
// Mac mini has none): that capture runs where it can.
func TestMacCaptureHearsTheRecording(t *testing.T) {
	p := newMicrophoneProbe(t)
	phrase := modelstest.Speech(t, t.TempDir(), 16000)[0].Samples
	for _, kind := range []string{"capture-engine", "capture-engine-44100", "capture-graph", "capture-recorder", "capture-recorder-duration", "capture-session"} {
		t.Run(kind, func(t *testing.T) {
			p.queue(t, modelscore.Recording{Rate: 16000, Samples: phrase})
			file := filepath.Join(t.TempDir(), "capture.pcm")
			out, err := p.run(t, kind, file, true)
			if kind == "capture-session" && (strings.Contains(out, "no audio capture device") || strings.Contains(out, "not authorized")) {
				t.Skipf("no audio capture device this program may use: %s", strings.TrimSpace(out))
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
