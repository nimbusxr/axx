package modelswindows

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/pe"
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

//go:embed ai/*.h ai/*.cpp ai/exports.def
var aiSource embed.FS

const (
	// The C++/WinRT axx builds its library with, pinned by its package's SHA-256.
	cppwinrtVersion = "2.0.250303.1"
	cppwinrtSum     = "955e3051b35db1c00177ed14ab6b7b995c3b32a53fdb6c184ea53b1dba66e439"
	// aiMarker is the library's own export (ai/exports.def), by which axx knows its file.
	aiMarker = "AxxModelsWindows"
)

// aiFiles are the app's files of the APIs the library answers, the Windows App SDK's, which the
// app's manifest maps their classes to.
var aiFiles = map[string]string{
	"windows.ai.text-recognizer":   "Microsoft.Windows.AI.Imaging.dll",
	"windows.ai.image-description": "Microsoft.Windows.AI.Imaging.dll",
	"windows.ai.object-extractor":  "Microsoft.Windows.AI.Imaging.dll",
	"windows.ai.language-model":    "Microsoft.Windows.AI.Text.dll",
	"windows.ai.text-summarizer":   "Microsoft.Windows.AI.Text.dll",
	"windows.ai.text-rewriter":     "Microsoft.Windows.AI.Text.dll",
	"windows.ai.text-to-table":     "Microsoft.Windows.AI.Text.dll",
}

var (
	// aiComponent are the .winmd files of the classes the library is (aiClasses), and aiProjected
	// those of the other classes it calls.
	aiComponent = []string{"Microsoft.Windows.AI.Imaging.winmd", "Microsoft.Windows.AI.Text.winmd", "Microsoft.Windows.AI.winmd"}
	aiClasses   = []string{
		"Microsoft.Windows.AI.AIFeatureReadyResult",
		"Microsoft.Windows.AI.Imaging.TextRecognizer", "Microsoft.Windows.AI.Imaging.RecognizedText",
		"Microsoft.Windows.AI.Imaging.RecognizedLine", "Microsoft.Windows.AI.Imaging.RecognizedWord",
		"Microsoft.Windows.AI.Imaging.ImageDescriptionGenerator", "Microsoft.Windows.AI.Imaging.ImageDescriptionResult",
		"Microsoft.Windows.AI.Imaging.ImageObjectExtractor", "Microsoft.Windows.AI.Imaging.ImageObjectExtractorHint",
		"Microsoft.Windows.AI.Text.ConversationItem", "Microsoft.Windows.AI.Text.ConversationSummaryOptions",
		"Microsoft.Windows.AI.Text.LanguageModel", "Microsoft.Windows.AI.Text.LanguageModelContext",
		"Microsoft.Windows.AI.Text.LanguageModelEmbeddingVectorResult", "Microsoft.Windows.AI.Text.LanguageModelOptions",
		"Microsoft.Windows.AI.Text.LanguageModelResponseResult", "Microsoft.Windows.AI.Text.TextRewriter",
		"Microsoft.Windows.AI.Text.TextSummarizer", "Microsoft.Windows.AI.Text.TextToTableConverter",
		"Microsoft.Windows.AI.Text.TextToTableResponseResult", "Microsoft.Windows.AI.Text.TextToTableRow",
	}
	aiProjected = []string{"Microsoft.Graphics.Imaging.winmd", "Microsoft.Windows.AI.ContentSafety.winmd", "Microsoft.Windows.AI.Foundation.winmd"}
)

// A swap is an app's file the library took the place of, and where the app's own is meanwhile.
type swap struct{ app, original string }

func (s swap) putBack() error { return replace(s.app, s.original) }

// answerAI puts axx's library in place of the app's files of the APIs its axx-models folder has
// stubs of, and first puts back what a run that ended before it could left in the app's folder.
func answerAI(sc *core.Scenario, c *changes, l *desktopcore.Launch, stubs []*platform.Artifact) error {
	dir := filepath.Dir(l.App.App)
	originals := filepath.Join(npm.CacheDir("models"), "windows-ai-originals", fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(dir))))[:16])
	if err := putBackLeft(dir, originals); err != nil {
		return err
	}
	if len(stubs) == 0 {
		return nil
	}
	need := map[string]bool{}
	for _, a := range stubs {
		f := aiFiles[a.API]
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return unsupportedAI("the %s app has no %s of its own, which its calls of %s reach: models-windows answers a self-contained Windows App SDK build's", l.App.Name, f, a.API)
		}
		need[f] = true
	}
	lib, err := aiLibrary(sc.Context(), sc.Suite(), l.App.App)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(originals, 0o755); err != nil {
		return err
	}
	for _, f := range slices.Sorted(maps.Keys(need)) {
		s := swap{app: filepath.Join(dir, f), original: filepath.Join(originals, f)}
		if err := replace(s.original, s.app); err != nil {
			return err
		}
		if err := replace(s.app, lib); err != nil {
			return err
		}
		c.swaps = append(c.swaps, s)
	}
	l.Environment["AXX_MODELS_DIR"] = filepath.Join(l.Files, platform.Folder)
	l.Environment["AXX_WINDOWS_AI_ORIGINALS"] = originals
	return nil
}

// putBackLeft puts the app's own files back where axx's library still is.
func putBackLeft(dir, originals string) error {
	for _, f := range slices.Sorted(maps.Values(aiFiles)) {
		app := filepath.Join(dir, f)
		b, err := os.ReadFile(app)
		if err != nil || !bytes.Contains(b, []byte(aiMarker)) {
			continue
		}
		original := filepath.Join(originals, f)
		if _, err := os.Stat(original); err != nil {
			return unsupportedAI("%s is axx's library of the Windows AI APIs, which a run that ended before it could put the app's own back left, and axx no longer has the app's: build the app again", app)
		}
		if err := replace(app, original); err != nil {
			return err
		}
	}
	return nil
}

// replace makes dst a copy of src: a file written beside it, which takes its place. A file in use,
// as a running app's library is, is moved aside first, and goes once it can.
func replace(dst, src string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".axx-new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil { //nolint:gosec // a library
		return err
	}
	if err := os.Rename(tmp, dst); err == nil {
		return nil
	}
	old := dst + ".axx-old"
	_ = os.Remove(old)
	if err := os.Rename(dst, old); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot replace %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	_ = os.Remove(old)
	return nil
}

// aiLibrary is axx's library of the Windows AI APIs for an app: built with MSVC and C++/WinRT,
// against the app's own .winmd files, once, into axx's cache.
func aiLibrary(ctx context.Context, s *core.Suite, exe string) (string, error) {
	return core.Cached(s, Name+"/ai/"+strings.ToLower(exe), func() (string, error) {
		target, err := peArch(exe)
		if err != nil {
			return "", err
		}
		dir := filepath.Dir(exe)
		winmds, err := filepath.Glob(filepath.Join(dir, "*.winmd"))
		if err != nil {
			return "", err
		}
		h := sha256.New()
		fmt.Fprintf(h, "cppwinrt %s, %s\n", cppwinrtVersion, target)
		var sources []string
		if err := fs.WalkDir(aiSource, "ai", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				sources = append(sources, p)
			}
			return err
		}); err != nil {
			return "", err
		}
		for _, f := range sources {
			b, _ := aiSource.ReadFile(f)
			fmt.Fprintf(h, "%s %d\n", f, len(b))
			h.Write(b)
		}
		slices.Sort(winmds)
		for _, f := range winmds {
			b, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "%s %d\n", filepath.Base(f), len(b))
			h.Write(b)
		}
		cache := npm.CacheDir("models")
		out := filepath.Join(cache, fmt.Sprintf("windows-ai-%x", h.Sum(nil)[:12]))
		lib := filepath.Join(out, "axx-windows-ai.dll")
		if _, err := os.Stat(lib); err == nil {
			return lib, nil
		}
		vcvars, arg, err := msvc(ctx, target)
		if err != nil {
			return "", err
		}
		cppwinrt, err := cppwinrtProgram(ctx, cache)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		for _, d := range []string{"src", "winmd", "obj"} {
			if err := os.Mkdir(filepath.Join(tmp, d), 0o755); err != nil {
				return "", err
			}
		}
		for _, f := range sources {
			b, _ := aiSource.ReadFile(f)
			if err := os.WriteFile(filepath.Join(tmp, "src", filepath.Base(f)), b, 0o644); err != nil {
				return "", err
			}
		}
		for _, f := range winmds {
			b, _ := os.ReadFile(f)
			if err := os.WriteFile(filepath.Join(tmp, "winmd", filepath.Base(f)), b, 0o644); err != nil {
				return "", err
			}
		}
		if err := os.WriteFile(filepath.Join(tmp, "build.cmd"), []byte(buildScript(vcvars, arg, cppwinrt)), 0o644); err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, "cmd", "/d", "/c", "build.cmd")
		cmd.Dir = tmp
		if b, err := cmd.CombinedOutput(); err != nil {
			return "", unsupportedAI("cannot build axx's library of the Windows AI APIs with MSVC and C++/WinRT: %v: %s", err, lastLines(b, 30))
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(filepath.Join(tmp, "axx-windows-ai.dll"), lib); err != nil {
			if _, other := os.Stat(lib); other != nil {
				return "", err
			}
		}
		return lib, nil
	})
}

// buildScript builds the library in its folder: C++/WinRT's projection of the SDK's classes and
// the app's that the library calls, its classes' bases, then MSVC's compile and link, in the
// environment vcvarsall.bat gives.
func buildScript(vcvars, arg, cppwinrt string) string {
	in := func(files []string) string {
		var b strings.Builder
		for _, f := range files {
			fmt.Fprintf(&b, ` -in "winmd\%s"`, f)
		}
		return b.String()
	}
	var include strings.Builder
	for _, c := range aiClasses {
		fmt.Fprintf(&include, " -include %s", c)
	}
	return strings.Join([]string{
		"@echo off",
		fmt.Sprintf(`call "%s" %s > nul || exit /b 1`, vcvars, arg),
		fmt.Sprintf(`"%s" -in sdk%s -ref winmd -out gen || exit /b 1`, cppwinrt, in(aiProjected)),
		fmt.Sprintf(`"%s"%s -ref winmd -ref sdk -component gen -out gen -name Microsoft.Windows.AI -prefix -optimize%s || exit /b 1`, cppwinrt, in(aiComponent), include.String()),
		`cl /nologo /std:c++20 /EHsc /O2 /MT /DNDEBUG /permissive- /bigobj /Zc:__cplusplus /utf-8 /W3 /I gen /I src /Fo:obj\ /c src\*.cpp gen\module.g.cpp || exit /b 1`,
		`link /nologo /DLL /OUT:axx-windows-ai.dll /DEF:src\exports.def obj\*.obj windowsapp.lib kernel32.lib ole32.lib oleaut32.lib || exit /b 1`,
		"",
	}, "\r\n")
}

// peArch is the architecture of an app's executable, as vcvarsall.bat names it.
func peArch(exe string) (string, error) {
	f, err := pe.Open(exe)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", exe, err)
	}
	defer f.Close()
	switch f.Machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "x64", nil
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64", nil
	}
	return "", unsupportedAI("%s is a build of machine %#x: axx's library of the Windows AI APIs is for x64 and ARM64 apps", exe, f.Machine)
}

// msvc is vcvarsall.bat of the Visual Studio installation that has the C++ tools for the target,
// and its argument, of this machine's architecture and the target's.
func msvc(ctx context.Context, target string) (string, string, error) {
	component := map[string]string{"x64": "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "arm64": "Microsoft.VisualStudio.Component.VC.Tools.ARM64"}[target]
	vswhere := filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio", "Installer", "vswhere.exe")
	b, err := exec.CommandContext(ctx, vswhere, "-latest", "-products", "*", "-requires", component, "-property", "installationPath").Output()
	install := strings.TrimSpace(string(b))
	if err != nil || install == "" {
		return "", "", unsupportedAI("no Visual Studio with C++ build tools for %s (%s): models-windows builds its library of the Windows AI APIs with MSVC; install Visual Studio's or its Build Tools' \"Desktop development with C++\", with a Windows 11 SDK", target, component)
	}
	host := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	arg := target
	if host != target {
		arg = host + "_" + target
	}
	return filepath.Join(install, "VC", "Auxiliary", "Build", "vcvarsall.bat"), arg, nil
}

// cppwinrtProgram is C++/WinRT's cppwinrt.exe, from its NuGet package, downloaded, once, into
// axx's cache.
func cppwinrtProgram(ctx context.Context, cache string) (string, error) {
	dir := filepath.Join(cache, "cppwinrt-"+cppwinrtVersion)
	exe := filepath.Join(dir, "bin", "cppwinrt.exe")
	if _, err := os.Stat(exe); err == nil {
		return exe, nil
	}
	url := fmt.Sprintf("https://api.nuget.org/v3-flatcontainer/microsoft.windows.cppwinrt/%[1]s/microsoft.windows.cppwinrt.%[1]s.nupkg", cppwinrtVersion)
	if err := fetchZip(ctx, "C++/WinRT", url, cppwinrtSum, dir, func(name string) bool { return name == "bin/cppwinrt.exe" }); err != nil {
		return "", err
	}
	return exe, nil
}
