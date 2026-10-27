// Package modelswindows answers the voice Windows apps speak with, SAPI's (System.Speech's and
// native SAPI's), from the Piper voice stub in the app's axx-models folder, in the desktops
// desktop-windows runs: axx's voice engine, registered as a SAPI voice and made the user's default
// for the scenario, speaks with Piper into the folder the speaker step hears.
package modelswindows

import (
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	desktopwindows "github.com/nimbusxr/axx/packs/desktop/windows"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Name is the pack's name.
const Name = "models-windows"

// Pack returns the models-windows pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = "Answer the models Windows apps take from Windows, from the stubs in the app's axx-models folder (in its `./` files, its home's AppData\\Roaming), in the desktops desktop-windows runs: the voice they speak with, SAPI's, from a Piper voice stub, and the Windows AI APIs, from stubs of them. " +
	"**The voice.** SAPI's (System.Speech's, and native SAPI's), from a Piper voice stub (its `.onnx` and `.onnx.json`). " +
	"As the app starts, axx adds its voice engine (built with the .NET SDK, 8 or later, into axx's cache) to SAPI's voices, as `axx voice`, makes it the user's default voice, and gives the app the stub: the voice speaks each text with Piper (its Windows release, which axx downloads, pinned) into SAPI's output and into the folder the speaker step hears. " +
	"SAPI lists the voices in HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Speech\\Voices\\Tokens only, so axx runs as an administrator, as GitHub's Windows runners do, or as a user who may create keys there; the scenario's end takes the voice out again and puts the user's default voice back. The engine is 64-bit .NET: it speaks in 64-bit apps. " +
	"**The Windows AI APIs.** The Windows App SDK's: TextRecognizer, ImageDescriptionGenerator, ImageObjectExtractor, and the language model, Phi Silica, with its skills, TextSummarizer, TextRewriter and TextToTableConverter (`windows.ai.*`: `axx models contract` writes their contracts), in a self-contained build (WindowsAppSDKSelfContained), whose own folder has their files, which its manifest maps their classes to. As the app starts, axx puts its library in place of the app's file of each API the folder has a stub of (Microsoft.Windows.AI.Imaging.dll, Microsoft.Windows.AI.Text.dll), and the app's own back as the scenario ends: a class the folder has a stub of is the library's, any other the app's file's; the language model's are the library's together, with a stub of the model or a skill. axx builds the library once for each app, against the app's .winmd files, with C++/WinRT (its release, which axx downloads, pinned) and MSVC (Visual Studio's, or its Build Tools', C++ tools and a Windows 11 SDK). " +
	"The library answers as Windows does: only an app with package identity whose package declares the systemAIModels capability. A stub of a PC without the model gives the reason (format.details.availability) in Windows' words: NotSupportedOnCurrentSystem, or NotReady, which EnsureReadyAsync makes ready."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		Requires: []string{modelscore.Name, desktopwindows.Name, appcore.Name},
		Steps:    []core.StepDef{},
		Hooks:    []core.Hook{{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare}},
	}
}

var started = core.NewStateKey(Name+"/started", func(*core.Scenario) *sync.Once { return &sync.Once{} }, nil)

// changed is what a scenario changes of the machine and its apps (changes, of each OS), which its
// end puts back once its apps are stopped: made as its first step starts, before desktop-core's
// state, whose end, which stops the apps, comes first.
var changed = core.NewStateKey(Name+"/changed", func(*core.Scenario) *changes { return &changes{} }, func(_ *core.Scenario, c *changes) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.restore()
})

func prepare(sc *core.Scenario) error {
	started.Of(sc).Do(func() {
		changed.Of(sc)
		desktopcore.OnLaunch(sc, func(l *desktopcore.Launch) error { return configure(sc, l) })
	})
	return nil
}

func unsupportedAI(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, f, args...).WithHint("build the stub from `axx models contract <api>` (windows.ai.text-recognizer) and its mappings; the app is a self-contained Windows App SDK build (WindowsAppSDKSelfContained), on Windows")
}

func unsupported(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, f, args...).WithHint("copy one Piper voice stub (its .onnx and .onnx.json) to the app's axx-models folder, on Windows, with the .NET SDK 8 or later")
}

// configure gives a Windows app what its axx-models folder has stubs of: a Piper voice stub, SAPI's
// voice, and stubs of the Windows AI APIs, axx's library of them in place of the app's own files.
func configure(sc *core.Scenario, l *desktopcore.Launch) error {
	if l.App.Driver == nil || l.App.Driver.Platform() != "windows" {
		return nil
	}
	ctx := sc.Context()
	files, err := appcore.Files(sc, l.App.Name, platform.Folder)
	if err != nil {
		return err
	}
	names, err := files.List(ctx, 1000)
	if err != nil {
		return err
	}
	voices := voicesOf(names)
	if len(voices) > 1 {
		return unsupported("the %s app's axx-models folder has %d Piper voices: SAPI's default voice is one", l.App.Name, len(voices))
	}
	stubs, err := platform.Load(ctx, files)
	if err != nil {
		return err
	}
	stubs = slices.DeleteFunc(stubs, func(a *platform.Artifact) bool { return !strings.HasPrefix(a.API, "windows.") })
	for _, a := range stubs {
		if err := supported(a); err != nil {
			return err
		}
	}
	c := changed.Of(sc)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(voices) == 1 {
		if err := speakWith(sc, c, l); err != nil {
			return err
		}
	}
	return answerAI(sc, c, l, stubs)
}

// supported is nil for a stub of an API models-windows answers, as its calls expect.
func supported(a *platform.Artifact) error {
	for _, api := range apis() {
		if api.Name != a.API {
			continue
		}
		if a.Contract.Task != api.Task {
			return unsupportedAI("%s needs task %s", a.API, api.Task)
		}
		c, _ := api.Contract(nil)
		if len(c.Inputs) == 0 {
			if a.Matcher != stub.TextMatcher {
				return unsupportedAI("%s needs text mappings", a.API)
			}
			return nil
		}
		if a.Matcher != stub.NearestMatcher || a.Contract.MatchedInput() == nil || a.Contract.MatchedInput().Image == nil {
			return unsupportedAI("%s needs image mappings", a.API)
		}
		return nil
	}
	return unsupportedAI("models-windows does not answer %s", a.API)
}

// voicesOf are the Piper voices of an axx-models folder's files: the .onnx files with their
// configuration beside them.
func voicesOf(names []string) []string {
	var voices []string
	for _, n := range names {
		if path.Dir(n) == "." && path.Ext(n) == ".onnx" && slices.Contains(names, n+".json") {
			voices = append(voices, n)
		}
	}
	slices.Sort(voices)
	return voices
}
