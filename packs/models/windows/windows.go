// Package modelswindows answers the voice Windows apps speak with, SAPI's (System.Speech's and
// native SAPI's), from the Piper voice stub in the app's axx-models folder, in the desktops
// desktop-windows runs: axx's voice engine, registered as a SAPI voice and made the user's default
// for the scenario, speaks with Piper into the folder the speaker step hears.
package modelswindows

import (
	"path"
	"slices"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	desktopwindows "github.com/nimbusxr/axx/packs/desktop/windows"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

// Name is the pack's name.
const Name = "models-windows"

// Pack returns the models-windows pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = "Answer the voice Windows apps speak with, SAPI's (System.Speech's, and native SAPI's), from a Piper voice stub (its `.onnx` and `.onnx.json`) in the app's axx-models folder (in its `./` files, its home's AppData\\Roaming), in the desktops desktop-windows runs. " +
	"As the app starts, axx adds its voice engine (built with the .NET SDK, 8 or later, into axx's cache) to SAPI's voices, as `axx voice`, makes it the user's default voice, and gives the app the stub: the voice speaks each text with Piper (its Windows release, which axx downloads, pinned) into SAPI's output and into the folder the speaker step hears. " +
	"SAPI lists the voices in HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Speech\\Voices\\Tokens only, so axx runs as an administrator, as GitHub's Windows runners do, or as a user who may create keys there; the scenario's end takes the voice out again and puts the user's default voice back. The engine is 64-bit .NET: it speaks in 64-bit apps."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		Requires: []string{modelscore.Name, desktopwindows.Name, appcore.Name},
		Steps:    []core.StepDef{},
		Hooks: []core.Hook{
			{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare},
			{ID: Name + ".restore", Phase: core.AfterScenario, Run: restore},
		},
	}
}

var started = core.NewStateKey(Name+"/started", func(*core.Scenario) *sync.Once { return &sync.Once{} }, nil)

func prepare(sc *core.Scenario) error {
	started.Of(sc).Do(func() { desktopcore.OnLaunch(sc, func(l *desktopcore.Launch) error { return configure(sc, l) }) })
	return nil
}

func unsupported(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, f, args...).WithHint("copy one Piper voice stub (its .onnx and .onnx.json) to the app's axx-models folder, on Windows, with the .NET SDK 8 or later")
}

// configure gives a Windows app whose axx-models folder has a Piper voice stub axx's voice.
func configure(sc *core.Scenario, l *desktopcore.Launch) error {
	if l.App.Driver == nil || l.App.Driver.Platform() != "windows" {
		return nil
	}
	files, err := appcore.Files(sc, l.App.Name, platform.Folder)
	if err != nil {
		return err
	}
	names, err := files.List(sc.Context(), 1000)
	if err != nil {
		return err
	}
	voices := voicesOf(names)
	switch len(voices) {
	case 0:
		return nil
	case 1:
	default:
		return unsupported("the %s app's axx-models folder has %d Piper voices: SAPI's default voice is one", l.App.Name, len(voices))
	}
	return speakWith(sc, l)
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
