//go:build integration

package modelsweb

import (
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
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

func TestBrowserAPIsReadAppOwnedPrebuiltStubs(t *testing.T) {
	for _, engine := range []string{"chromium", "firefox", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if r.URL.Path == "/child" {
					_, _ = fmt.Fprint(w, `<script>(async()=>{const m=await LanguageModel.create();opener.postMessage(await m.prompt('fragile'),'*');window.close();})()</script>`)
					return
				}
				_, _ = fmt.Fprint(w, `<body><script>
(async () => {
  const model = await LanguageModel.create();
  const a = await model.prompt('The box is fragile');
  const clone = await model.clone();
  const history = await clone.prompt('Thank you');
  let stream = ''; for await (const chunk of model.promptStreaming('again')) stream += chunk;
  model.destroy(); let disposed = false;
  try { await model.prompt('anything'); } catch (e) { disposed = e.name === 'InvalidStateError'; }
  const other = await LanguageModel.create();
  const fallback = await other.prompt('A normal parcel');
  const ac = new AbortController(); ac.abort(); let aborted = false;
  try { await other.prompt('fragile', {signal: ac.signal}); } catch(e) { aborted = e.name === 'AbortError'; }
  document.body.append(a === 'Handle with care.' && history === a && stream === a && disposed && aborted && fallback === 'No instructions.' ? 'models passed' : 'models failed');
  for(const [C,method] of [[Summarizer,'summarize'],[Writer,'write'],[Rewriter,'rewrite']]) {
    const task=await C.create();const one=await task[method]('fragile');const two=await task[method]('ordinary');
    document.body.append(one===a&&two===fallback ? ' '+C.name+' passed' : ' task failed');
  }
  const tr=await Translator.create({sourceLanguage:'de',targetLanguage:'en'});
  const t1=await tr.translate('Bitte beim Nachbarn abgeben');let ts='';for await (const c of tr.translateStreaming('Bitte beim Nachbarn abgeben')) ts+=c;
  document.body.append(t1==='Please leave it with the neighbours.'&&ts===t1&&tr.sourceLanguage==='de'&&tr.targetLanguage==='en' ? ' Translator passed' : ' Translator failed: '+t1);
  const det=await LanguageDetector.create();const d1=await det.detect('Bitte beim Nachbarn abgeben');const d2=await det.detect('Ring twice');
  document.body.append(d1[0].detectedLanguage==='de'&&d1[0].confidence===0.97&&d1[1].detectedLanguage==='nl'&&d2.length===1&&d2[0].detectedLanguage==='en'&&d2[0].confidence===1&&!('detectStreaming' in det) ? ' LanguageDetector passed' : ' LanguageDetector failed: '+JSON.stringify([d1,d2]));
  const pr=await Proofreader.create({expectedInputLanguages:['en']});const pf=await pr.proofread('Pleese ring twice');
  document.body.append(pf.correctedInput==='Please ring twice'&&pf.corrections.length===1&&pf.corrections[0].startIndex===3&&pf.corrections[0].endIndex===4&&pf.corrections[0].correction==='a'&&pr.expectedInputLanguages[0]==='en' ? ' Proofreader passed' : ' Proofreader failed: '+JSON.stringify(pf));
  const frame=document.createElement('iframe');
  frame.srcdoc='<script>LanguageModel.create().then(m=>m.prompt("fragile")).then(text=>parent.postMessage("frame: "+text,"*"))<\/script>';
  window.addEventListener('message',e=>{if(e.origin===location.origin)document.body.append(' child: '+e.data);});
  document.body.append(frame);
  window.open('/child');
  const speech = new SpeechRecognition();
  speech.onresult = e => {
    document.body.append(' heard: ' + e.results.item(0).item(0).transcript);
    const listener=document.createElement('iframe');
    listener.srcdoc='<script>const s=new SpeechRecognition();s.onresult=e=>parent.postMessage("frame heard: "+e.results[0][0].transcript,"*");s.onerror=e=>parent.postMessage("frame error: "+e.message,"*");s.start();<\/script>';
    document.body.append(listener);
  };
  speech.onerror = e => document.body.append(' speech error: '+e.message);
  speech.start();
})().catch(e => document.body.append(e.stack));
</script>`)
			}))
			t.Cleanup(site.Close)
			h := cloudtest.New(t, appcore.Pack(), files.Pack(), modelscore.Pack(), webcore.Pack(), Pack())
			h.Start(&core.Plan{})
			h.OK("the courier web app with the following properties:", [][]string{{"url", site.URL}, {"engine", engine}})
			write := func(name, api string, c *contract.Contract, ms []mappings.Mapping) {
				c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"` + api + `"}`)}
				s, err := stub.Resolve(c, ms)
				if err != nil {
					t.Fatal(err)
				}
				b, err := platform.Write(s)
				if err != nil {
					t.Fatal(err)
				}
				h.File(name, string(b))
			}
			write("language.axxmodel", "web.language-model", &contract.Contract{Task: "text-generation"}, []mappings.Mapping{
				{Name: "fragile", Input: mappings.Input{"text": json.RawMessage(`"fragile"`)}, Output: json.RawMessage(`{"text":"Handle with care."}`)},
				{Name: "default", Default: true, Output: json.RawMessage(`{"text":"No instructions."}`)},
			})
			write("translator.axxmodel", "web.translator", &contract.Contract{Task: "text-generation"}, []mappings.Mapping{
				{Name: "neighbour", Input: mappings.Input{"text": json.RawMessage(`"Nachbarn"`)}, Output: json.RawMessage(`{"text":"Please leave it with the neighbours."}`)},
				{Name: "default", Default: true, Output: json.RawMessage(`{"text":""}`)},
			})
			write("language-detector.axxmodel", "web.language-detector", &contract.Contract{Task: "text-generation"}, []mappings.Mapping{
				{Name: "German", Input: mappings.Input{"text": json.RawMessage(`"Nachbarn"`)}, Output: json.RawMessage(`{"json":[{"detectedLanguage":"de","confidence":0.97},{"detectedLanguage":"nl","confidence":0.02}]}`)},
				{Name: "default", Default: true, Output: json.RawMessage(`{"text":"en"}`)},
			})
			write("proofreader.axxmodel", "web.proofreader", &contract.Contract{Task: "text-generation"}, []mappings.Mapping{
				{Name: "ring", Input: mappings.Input{"text": json.RawMessage(`"ring twice"`)}, Output: json.RawMessage(`{"text":"Please ring twice"}`)},
				{Name: "default", Default: true, Output: json.RawMessage(`{"text":""}`)},
			})
			for _, api := range []string{"summarizer", "writer", "rewriter"} {
				write(api+".axxmodel", "web."+api, &contract.Contract{Task: "text-generation"}, []mappings.Mapping{{Name: "fragile", Input: mappings.Input{"text": json.RawMessage(`"fragile"`)}, Output: json.RawMessage(`{"text":"Handle with care."}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"text":"No instructions."}`)}})
			}
			// Phrases of one speaker: the first is a mapping's key, the
			// third no mapping's, which gets the default.
			sounds := t.TempDir()
			utterances := modelstest.Speech(t, sounds, 16000)
			c, err := platform.SpeechFeatures("web.speech")(nil)
			if err != nil {
				t.Fatal(err)
			}
			write("speech.axxmodel", "web.speech", c, []mappings.Mapping{
				{Name: "first", Input: mappings.Input{"audio": json.RawMessage(`"` + filepath.ToSlash(filepath.Join(sounds, "sounds", "first.wav")) + `"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)},
				{Name: "second", Input: mappings.Input{"audio": json.RawMessage(`"` + filepath.ToSlash(filepath.Join(sounds, "sounds", "second.wav")) + `"`)}, Output: json.RawMessage(`{"text":"Leave it by the door."}`)},
				{Name: "default", Default: true, Output: json.RawMessage(`{"text":"Please try again."}`)},
			})
			for name, samples := range map[string][]float64{"recording.wav": utterances[0].Samples, "other.wav": utterances[3].Samples} {
				wav := filepath.Join(t.TempDir(), name)
				modelstest.WriteWAV(t, wav, 16000, samples)
				b, err := os.ReadFile(wav)
				if err != nil {
					t.Fatal(err)
				}
				h.File(name, string(b))
			}
			h.OK("the courier-models folder with the following properties:", [][]string{{"owner", "app:courier"}, {"path", "axx-models"}})
			h.OK("the language.axxmodel file in the courier-models folder is a copy of the language.axxmodel file")
			h.OK("the speech.axxmodel file in the courier-models folder is a copy of the speech.axxmodel file")
			for _, api := range []string{"summarizer", "writer", "rewriter", "translator", "language-detector", "proofreader"} {
				h.OK("the " + api + ".axxmodel file in the courier-models folder is a copy of the " + api + ".axxmodel file")
			}
			h.OK(`the courier app's microphone hears the "recording.wav" file`)
			h.OK(`the "/" page is opened`)
			h.OK(`the page shows "models passed"`)
			h.OK(`the page shows "heard: Ring twice, please."`)
			h.OK(`the courier app's microphone hears the "other.wav" file`)
			h.OK(`the page shows "frame heard: Please try again."`)
			for _, name := range []string{"Summarizer", "Writer", "Rewriter", "Translator", "LanguageDetector", "Proofreader"} {
				h.OK(`the page shows "` + name + ` passed"`)
			}
			h.OK(`the page shows "child: Handle with care."`)
			h.OK(`the page shows "child: frame: Handle with care."`)
		})
	}
}
