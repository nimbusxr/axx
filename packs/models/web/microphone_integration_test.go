//go:build integration

package modelsweb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// listening is a page that records with its microphone once its button is
// clicked, and says what it heard: the recording, then silence; with a
// recognizer, what it recognized of the same track.
const listening = `<body><button id=listen>Listen</button><script>
document.getElementById('listen').onclick = () => listen().catch(e => document.body.append(' error: ' + e));
async function listen() {
  const stream = await navigator.mediaDevices.getUserMedia({audio: true});
  const [track] = stream.getAudioTracks();
  if (globalThis.SpeechRecognition) {
    const r = new SpeechRecognition();
    r.onresult = e => document.body.append(' recognized: ' + e.results[0][0].transcript);
    r.onerror = e => document.body.append(' recognition error: ' + e.message);
    r.start(track);
  }
  const ctx = new AudioContext(); await ctx.resume();
  const node = ctx.createScriptProcessor(4096, 1, 1);
  let loud = 0, quiet = 0, heard = false;
  node.onaudioprocess = e => {
    const d = e.inputBuffer.getChannelData(0); let sum = 0;
    for (const x of d) sum += x * x;
    const rms = Math.sqrt(sum / d.length);
    if (rms > 0.1) loud++; else if (loud) quiet++;
    if (!heard && loud >= 2 && quiet >= 4) { heard = true; document.body.append(' heard the recording, then silence'); }
  };
  ctx.createMediaStreamSource(stream).connect(node); node.connect(ctx.destination);
}
</script>`

func microphoneHarness(t *testing.T, engine string) *cloudtest.Harness {
	t.Helper()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, listening)
	}))
	t.Cleanup(site.Close)
	h := cloudtest.New(t, appcore.Pack(), files.Pack(), modelscore.Pack(), webcore.Pack(), Pack())
	h.Start(&core.Plan{})
	// The scenario's steps include the microphone step, as its plan says.
	plan := &core.Plan{Scenarios: []core.PlannedScenario{{ScenarioInfo: core.ScenarioInfo{ID: h.SC.ID}, Steps: []core.PlannedStep{{Definition: modelscore.Name + ".microphone"}}}}}
	if err := modelscore.Pack().(core.Preparer).Prepare(context.Background(), h.Suite, plan); err != nil {
		t.Fatal(err)
	}
	h.OK("the courier web app with the following properties:", [][]string{{"url", site.URL}, {"engine", engine}})
	wav := filepath.Join(t.TempDir(), "bell.wav")
	modelstest.WriteWAV(t, wav, 16000, modelstest.Doorbell.Samples(16000, 1, 0, 1, 0))
	b, err := os.ReadFile(wav)
	if err != nil {
		t.Fatal(err)
	}
	h.File("recording.wav", string(b))
	return h
}

// A page that records with its own microphone hears the recording, with no
// speech stub in its folder.
func TestThePagesMicrophoneHearsTheRecording(t *testing.T) {
	for _, engine := range []string{"chromium", "firefox", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			h := microphoneHarness(t, engine)
			h.OK(`the "/" page is opened`)
			h.OK(`the courier app's microphone hears the "recording.wav" file`)
			h.OK(`the "Listen" button is clicked`)
			h.OK(`the page shows "heard the recording, then silence"`)
		})
	}
}

// A recognizer started on a track of the page's microphone answers the
// recording the track plays.
func TestARecognizerOfThePagesMicrophoneAnswersItsRecording(t *testing.T) {
	for _, engine := range []string{"chromium", "firefox", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			h := microphoneHarness(t, engine)
			c, err := platform.SpeechFeatures("web.speech")(nil)
			if err != nil {
				t.Fatal(err)
			}
			wav := filepath.Join(t.TempDir(), "bell.wav")
			modelstest.WriteWAV(t, wav, 16000, modelstest.Doorbell.Samples(16000, 1, 0, 1, 0))
			s, err := stub.Resolve(c, []mappings.Mapping{
				{Name: "bell", Input: mappings.Input{"audio": json.RawMessage(`"` + filepath.ToSlash(wav) + `"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)},
				{Name: "default", Default: true, Output: json.RawMessage(`{"text":"Please try again."}`)},
			})
			if err != nil {
				t.Fatal(err)
			}
			b, err := platform.Write(s)
			if err != nil {
				t.Fatal(err)
			}
			h.File("speech.axxmodel", string(b))
			h.OK("the courier-models folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "axx-models"}})
			h.OK("the speech.axxmodel file in the courier-models folder is a copy of the speech.axxmodel file")
			h.OK(`the "/" page is opened`)
			h.OK(`the courier app's microphone hears the "recording.wav" file`)
			h.OK(`the "Listen" button is clicked`)
			h.OK(`the page shows "recognized: Ring twice, please."`)
			h.OK(`the page shows "heard the recording, then silence"`)
		})
	}
}
