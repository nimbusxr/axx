// Package modelslinux answers Linux apps' system models from the stubs in the app's axx-models
// folder, in the desktops desktop-linux runs: Tesseract's language data, through its documented
// TESSDATA_PREFIX, and speech-dispatcher's voice, through its user configuration, which speaks
// with the Piper voice stub.
package modelslinux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	desktoplinux "github.com/nimbusxr/axx/packs/desktop/linux"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

// Name is the pack's name.
const Name = "models-linux"

// Pack returns the models-linux pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

const packDoc = "Answer the models Linux apps take from the system, in the desktops desktop-linux runs, from stubs in the app's axx-models folder (in its `./` files, its home's .local/share): " +
	"Tesseract's language data (a stub of the tesseract format, like `eng.traineddata`), which Tesseract reads from the folder its documented TESSDATA_PREFIX names, and the voice of the desktop's speech service, speech-dispatcher (a Piper voice stub: its `.onnx` and `.onnx.json`), which speaks the app's text with Piper (its `piper` program, on PATH) into the folder the speaker step hears. " +
	"axx gives the app's speech-dispatcher its user configuration, in the app's home (`.config/speech-dispatcher`): the voice is its only one, and no sound plays (its audio is ALSA's null device, which needs speech-dispatcher's audio plugins). Copy stubs before launching the app; restarting reloads them."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		Requires: []string{modelscore.Name, desktoplinux.Name, appcore.Name},
		Steps:    []core.StepDef{},
		Hooks:    []core.Hook{{ID: Name + ".prepare", Phase: core.BeforeStep, Run: prepare}},
	}
}

var started = core.NewStateKey(Name+"/started", func(*core.Scenario) *sync.Once { return &sync.Once{} }, nil)

func prepare(sc *core.Scenario) error {
	started.Of(sc).Do(func() { desktopcore.OnLaunch(sc, func(l *desktopcore.Launch) error { return configure(sc, l) }) })
	return nil
}

func unsupported(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, f, args...).WithHint("copy a stub of the system's model to the app's axx-models folder: a Tesseract language's .traineddata, or a Piper voice's .onnx and .onnx.json")
}

// configure points a Linux app's system models at the stubs in its axx-models folder.
func configure(sc *core.Scenario, l *desktopcore.Launch) error {
	if l.App.Driver == nil || l.App.Driver.Platform() != "linux" {
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
	stubs := l.Files + "/" + platform.Folder
	languages, voices := stubsOf(names)
	if len(languages) > 0 {
		l.Environment["TESSDATA_PREFIX"] = stubs
	}
	switch len(voices) {
	case 0:
		return nil
	case 1:
	default:
		return unsupported("the %s app's axx-models folder has %d Piper voices (%s): speech-dispatcher speaks with one", l.App.Name, len(voices), strings.Join(voices, ", "))
	}
	config, ok, err := files.Read(ctx, voices[0]+".json")
	if err != nil {
		return err
	}
	var voice struct {
		Audio struct {
			SampleRate int `json:"sample_rate"`
		} `json:"audio"`
	}
	if !ok || json.Unmarshal(config, &voice) != nil || voice.Audio.SampleRate == 0 {
		return unsupported("%s in the %s app's axx-models folder has no Piper voice configuration beside it (%s.json, of its sample rate)", voices[0], l.App.Name, voices[0])
	}
	piper, err := exec.LookPath("piper")
	if err != nil {
		return unsupported("speech-dispatcher speaks the %s app's Piper voice stub with Piper, whose piper program is not on PATH (its releases: https://github.com/rhasspy/piper/releases)", l.App.Name)
	}
	speaker := l.Files + "/" + modelscore.SpeakerFolder
	conf, module := speechConfig(piper, stubs+"/"+voices[0], speaker)
	dir := filepath.Join(l.HostHome, ".config", "speech-dispatcher")
	for name, body := range map[string]string{"speechd.conf": conf, filepath.Join("modules", "axx-piper.conf"): module} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(l.HostFiles, filepath.FromSlash(modelscore.SpeakerFolder)), 0o755)
}

// stubsOf are the Tesseract languages and the Piper voices of an axx-models folder's files.
func stubsOf(names []string) (languages, voices []string) {
	for _, n := range names {
		switch {
		case strings.Contains(n, "/"):
		case path.Ext(n) == ".traineddata":
			languages = append(languages, n)
		case path.Ext(n) == ".onnx" && slices.Contains(names, n+".json"):
			voices = append(voices, n)
		}
	}
	slices.Sort(languages)
	slices.Sort(voices)
	return languages, voices
}

// speechConfig is speech-dispatcher's user configuration, and its module's, that speak with a
// Piper voice: each text an utterance, a WAV file in speaker, named in the order spoken.
func speechConfig(piper, voice, speaker string) (conf, module string) {
	conf = `# axx's (models-linux): the app's speech is spoken with the Piper voice stub in its
# axx-models folder, into the folder the speaker step hears; no sound plays.
AddModule "axx-piper" "sd_generic" "axx-piper.conf"
DefaultModule axx-piper
AudioOutputMethod "alsa"
AudioALSADevice "null"
`
	module = fmt.Sprintf(`# axx's (models-linux): Piper speaks each text with the voice stub, into the speaker's folder.
GenericExecuteSynth "printf %%s \'$DATA\' | '%s' --model '%s' --output_file \"$(mktemp -p '%s' \$(date +%%s%%N)-XXXX.wav)\""
GenericDelimiters ""
AddVoice "en" "MALE1" "axx"
DefaultVoice "axx"
`, piper, voice, speaker)
	return conf, module
}
