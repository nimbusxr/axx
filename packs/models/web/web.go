// Package modelsweb supplies prebuilt answers to the browser's model APIs,
// before application scripts run.
package modelsweb

import (
	_ "embed"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

const Name = "models-web"

//go:embed runtime.js
var script string

func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Requires: []string{modelscore.Name, webcore.Name, appcore.Name}, Steps: []core.StepDef{},
		Doc:   "Answer a browser's built-in AI (the Prompt API's LanguageModel, Summarizer, Writer, Rewriter, Translator, LanguageDetector, Proofreader) and SpeechRecognition from prebuilt .axxmodel files in the web app's axx-models folder, and play the microphone step's recordings into the page's own microphone (getUserMedia). Copy the files into the app's folder before opening its first page. The adapter runs before the page's scripts, in its frames of its origin and its popups.",
		Hooks: []core.Hook{{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare}},
	}
}

type state struct {
	mu         sync.Mutex
	started    bool
	registered map[string]bool
}

var states = core.NewStateKey(Name+"/state", func(*core.Scenario) *state { return &state{registered: map[string]bool{}} }, nil)

func prepare(sc *core.Scenario) error {
	s := states.Of(sc)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range webcore.Apps(sc) {
		if s.registered[name] {
			continue
		}
		if err := appcore.RegisterFiles(sc, name, func(path string) (core.Files, error) { return webcore.Files(sc, name, path) }); err != nil {
			return err
		}
		// The pages' own microphone (getUserMedia) hears the app's recordings.
		modelscore.FeedsMicrophone(sc, name)
		s.registered[name] = true
	}
	if s.started {
		return nil
	}
	s.started = true
	return webcore.OnContext(sc, func(c webcore.Context) error {
		files, err := appcore.Files(sc, c.App(), platform.Folder)
		if err != nil {
			return err
		}
		artifacts, err := platform.Load(sc.Context(), files)
		if err != nil {
			return err
		}
		if len(artifacts) == 0 && !modelscore.UsesMicrophone(sc) {
			return nil
		}
		for _, a := range artifacts {
			if err := supported(a); err != nil {
				return err
			}
		}
		u, err := url.Parse(c.URL())
		if err != nil {
			return err
		}
		if err := c.ExposeBinding("__axxTakeRecording", func(source *playwright.BindingSource, args ...any) any {
			// srcdoc and about:blank inherit their parent's origin, which is
			// not represented by Frame.URL(). Check the actual frame origin.
			origin, err := source.Frame.Evaluate("globalThis.origin")
			if err != nil || origin != canonicalOrigin(u) {
				return map[string]any{"error": "Recording input belongs to the app's origin"}
			}
			r, err := modelscore.TakeRecording(sc, c.App())
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			if r == nil {
				return nil
			}
			// The binding's value is JSON of any's: Playwright serializes no
			// other map.
			matches := map[string]any{}
			for api, at := range r.Matches {
				matches[api] = at
			}
			return map[string]any{"rate": r.Rate, "samples": r.Samples, "matches": matches}
		}); err != nil {
			return err
		}
		data, err := json.Marshal(artifacts)
		if err != nil {
			return err
		}
		origin, _ := json.Marshal(canonicalOrigin(u))
		return c.AddInitScript(playwright.Script{Content: playwright.String("(" + script + ")(" + string(data) + "," + string(origin) + ");")})
	})
}

func supported(a *platform.Artifact) error {
	switch apiTask(a.API) {
	case "text-generation":
		if a.Contract.Task != "text-generation" || a.Matcher != stub.TextMatcher {
			return unsupported(a.API + " requires text-generation with text mappings")
		}
	case "speech-to-text":
		in := a.Contract.MatchedInput()
		if a.Contract.Task != "speech-to-text" || a.Matcher != stub.NearestMatcher || in == nil || in.Audio == nil || in.Audio.Features != "" && in.Audio.Features != "log-mel" {
			return unsupported(a.API + " requires nearest audio mappings and an audio input: samples, or log-mel features")
		}
	default:
		return unsupported(a.API + " is not supported by the browser adapter")
	}
	return nil
}

func canonicalOrigin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

func unsupported(message string) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, "browser model stub: %s", message).WithHint("use a supported browser API contract and rebuild the artifact before opening the app")
}
