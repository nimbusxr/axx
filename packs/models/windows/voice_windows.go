package modelswindows

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/npm"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	modelscore "github.com/nimbusxr/axx/packs/models/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

//go:embed engine/AxxVoice.csproj engine/AxxVoice.cs engine/Sapi.cs
var engineSource embed.FS

const (
	// engineCLSID is the voice engine's COM class (engine/AxxVoice.cs).
	engineCLSID = "{3C5E8D2A-7B41-4E9F-A6C3-9D0B1F2E4A57}"
	classKey    = `SOFTWARE\Classes\CLSID\` + engineCLSID
	voicesKey   = `SOFTWARE\Microsoft\Speech\Voices`
	tokensKey   = voicesKey + `\Tokens`
	tokenKey    = tokensKey + `\axx-voice`
	tokenID     = `HKEY_LOCAL_MACHINE\` + tokenKey
	// Piper's Windows release, pinned by its SHA-256.
	piperURL = "https://github.com/rhasspy/piper/releases/download/2023.11.14-2/piper_windows_amd64.zip"
	piperSum = "f3c58906402b24f3a96d92145f58acba6d86c9b5db896d207f78dc80811efcea"
)

// voiceChanges are what a scenario changed of SAPI's voices.
type voiceChanges struct {
	// registered is whether axx's voice is SAPI's, its COM class in classes (classesFor);
	// previous the user's default voice before, and had whether there was one.
	registered bool
	classes    registry.Key
	previous   string
	had        bool
}

// speakWith registers axx's voice, makes it the user's default, and gives the app the stub.
func speakWith(sc *core.Scenario, c *changes, l *desktopcore.Launch) error {
	ctx := sc.Context()
	comhost, err := engine(ctx, sc.Suite())
	if err != nil {
		return err
	}
	piper, err := piperProgram(ctx, sc.Suite())
	if err != nil {
		return err
	}
	st := &c.voice
	if !st.registered {
		classes := classesFor()
		if err := register(classes, comhost); err != nil {
			_ = unregister(classes)
			return err
		}
		// A default of axx's own voice is a run's that ended before it could put the user's back.
		st.previous, st.had = defaultVoice()
		if st.previous == tokenID {
			st.previous, st.had = "", false
		}
		if err := setDefaultVoice(tokenID, true); err != nil {
			_ = unregister(classes)
			return err
		}
		st.registered, st.classes = true, classes
	}
	l.Environment["AXX_MODELS_DIR"] = filepath.Join(l.Files, platform.Folder)
	l.Environment["AXX_PIPER"] = piper
	return os.MkdirAll(filepath.Join(l.HostFiles, filepath.FromSlash(modelscore.SpeakerFolder), ".partial"), 0o755)
}

// restore puts the user's default voice back, and takes axx's out of SAPI's voices.
func (st *voiceChanges) restore() error {
	if !st.registered {
		return nil
	}
	st.registered = false
	err := setDefaultVoice(st.previous, st.had)
	return errors.Join(err, unregister(st.classes))
}

// classesFor is where the engine's COM class goes: the user's own classes, which COM gives the
// apps axx starts; or, when axx is elevated, as the apps it starts then are, the machine's: COM
// leaves a user's own classes out of an elevated process.
func classesFor() registry.Key {
	if windows.GetCurrentProcessToken().IsElevated() {
		return registry.LOCAL_MACHINE
	}
	return registry.CURRENT_USER
}

// register makes the engine a SAPI voice: its COM class, and its token, which names the class.
// The token is the machine's: SAPI lists voices from HKEY_LOCAL_MACHINE's tokens only.
func register(classes registry.Key, comhost string) error {
	set := func(root registry.Key, key, name, value string) error {
		k, _, err := registry.CreateKey(root, key, registry.SET_VALUE)
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) && root == registry.LOCAL_MACHINE {
			return axxerr.New(platform.CodeArtifact, exitcode.Usage, "axx cannot add its voice to SAPI's (HKEY_LOCAL_MACHINE\\%s): access is denied", key).
				WithHint("SAPI lists the voices in HKEY_LOCAL_MACHINE\\%s only, where axx adds its own for the scenario: run axx as an administrator, or as a user who may create keys there", tokensKey)
		}
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(name, value)
	}
	for _, v := range []struct {
		root             registry.Key
		key, name, value string
	}{
		{classes, classKey, "", "axx voice (models-windows)"},
		{classes, classKey + `\InprocServer32`, "", comhost},
		{classes, classKey + `\InprocServer32`, "ThreadingModel", "Both"},
		{registry.LOCAL_MACHINE, tokenKey, "", "axx voice"},
		{registry.LOCAL_MACHINE, tokenKey, "CLSID", engineCLSID},
		{registry.LOCAL_MACHINE, tokenKey + `\Attributes`, "Name", "axx voice"},
		{registry.LOCAL_MACHINE, tokenKey + `\Attributes`, "Language", "409"},
		{registry.LOCAL_MACHINE, tokenKey + `\Attributes`, "Gender", "Female"},
		{registry.LOCAL_MACHINE, tokenKey + `\Attributes`, "Age", "Adult"},
		{registry.LOCAL_MACHINE, tokenKey + `\Attributes`, "Vendor", "axx"},
	} {
		if err := set(v.root, v.key, v.name, v.value); err != nil {
			return err
		}
	}
	return nil
}

// unregister takes axx's voice out of SAPI's voices: its token and its class, deepest keys first.
func unregister(classes registry.Key) error {
	var errs []error
	for _, k := range []struct {
		root registry.Key
		key  string
	}{
		{registry.LOCAL_MACHINE, tokenKey + `\Attributes`},
		{registry.LOCAL_MACHINE, tokenKey},
		{classes, classKey + `\InprocServer32`},
		{classes, classKey},
	} {
		if err := registry.DeleteKey(k.root, k.key); err != nil && !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, fmt.Errorf("cannot take axx's voice out of SAPI's (%s): %w", k.key, err))
		}
	}
	return errors.Join(errs...)
}

// defaultVoice is the user's default voice, the token SAPI speaks with when an app names none.
func defaultVoice() (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, voicesKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("DefaultTokenId")
	return v, err == nil
}

// setDefaultVoice sets the user's default voice to a token, or, with none, takes it out.
func setDefaultVoice(token string, set bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, voicesKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !set {
		if err := k.DeleteValue("DefaultTokenId"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	return k.SetStringValue("DefaultTokenId", token)
}

// engine is the voice engine's COM host, built with the .NET SDK, once, into axx's cache.
func engine(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/engine", func() (string, error) {
		h := sha256.New()
		var files []string
		if err := fs.WalkDir(engineSource, "engine", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, p)
			}
			return err
		}); err != nil {
			return "", err
		}
		for _, f := range files {
			b, _ := engineSource.ReadFile(f)
			h.Write([]byte(f))
			h.Write(b)
		}
		cache := npm.CacheDir("models")
		dir := filepath.Join(cache, fmt.Sprintf("windows-voice-%x", h.Sum(nil)[:12]))
		comhost := filepath.Join(dir, "AxxVoice.comhost.dll")
		if _, err := os.Stat(comhost); err == nil {
			return comhost, nil
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		for _, f := range files {
			b, _ := engineSource.ReadFile(f)
			if err := os.WriteFile(filepath.Join(tmp, filepath.Base(f)), b, 0o644); err != nil {
				return "", err
			}
		}
		out := filepath.Join(tmp, "out")
		cmd := exec.CommandContext(ctx, "dotnet", "build", filepath.Join(tmp, "AxxVoice.csproj"), "-c", "Release", "-o", out, "--nologo")
		if b, err := cmd.CombinedOutput(); err != nil {
			return "", unsupported("cannot build axx's voice with the .NET SDK: %v: %s", err, lastLines(b, 20))
		}
		if err := os.Rename(out, dir); err != nil {
			if _, other := os.Stat(comhost); other != nil {
				return "", err
			}
		}
		return comhost, nil
	})
}

// piperProgram is Piper's Windows release, downloaded, once, into axx's cache.
func piperProgram(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/piper", func() (string, error) {
		dir := filepath.Join(npm.CacheDir("models"), "piper-"+piperSum[:12])
		exe := filepath.Join(dir, "piper", "piper.exe")
		if _, err := os.Stat(exe); err == nil {
			return exe, nil
		}
		if err := fetchZip(ctx, "Piper", piperURL, piperSum, dir, nil); err != nil {
			return "", err
		}
		return exe, nil
	})
}

// lastLines are the last n lines of a command's output.
func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
