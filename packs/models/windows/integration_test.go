//go:build integration && windows

package modelswindows

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/piper"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// workDir is a folder of the test's outside C:\Users, which a self-contained Windows App SDK app
// of the Windows AI APIs does not run from: the runner's temporary folder, or the system drive's.
func workDir(t *testing.T) string {
	t.Helper()
	base := os.Getenv("RUNNER_TEMP")
	if base == "" {
		base = filepath.Join(os.Getenv("SystemDrive")+`\`, "axx-test")
	}
	dir := filepath.Join(base, "models-windows-"+t.Name())
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// build builds a probe program of its sources with the .NET SDK, into its folder's out.
func build(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "out")
	cmd := exec.Command("dotnet", "build", "-c", "Release", "-o", out, "--nologo")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet build: %v: %s", err, lastLines(b, 30))
	}
	return out
}

// run runs a probe with the launch's environment, and gives its lines, "what: answer", by what.
func run(t *testing.T, exe string, l *desktopcore.Launch, args ...string) map[string]string {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	for k, v := range l.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v: %s", exe, err, b)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if what, answer, ok := strings.Cut(line, ": "); ok {
			out[what] = answer
		}
	}
	return out
}

// samples are a WAV's samples, its data chunk's.
func samples(t *testing.T, path string) []int16 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for at := 12; at+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[at+4:]))
		if string(b[at:at+4]) == "data" {
			out := make([]int16, min(size, len(b)-at-8)/2)
			for i := range out {
				out[i] = int16(binary.LittleEndian.Uint16(b[at+8+2*i:]))
			}
			return out
		}
		at += 8 + size + size&1
	}
	t.Fatalf("%s has no data chunk", path)
	return nil
}

const speakProject = `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0-windows</TargetFramework>
    <PlatformTarget>x64</PlatformTarget>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="System.Speech" Version="8.0.0" />
  </ItemGroup>
</Project>
`

// The voice probe speaks with the user's default voice into a WAV file of the voice's format, as an
// app with no sound device does.
const speakProgram = `using System.Speech.AudioFormat;
using System.Speech.Synthesis;
using var voice = new SpeechSynthesizer();
Console.WriteLine("voice: " + voice.Voice.Name);
voice.SetOutputToWaveFile(args[0], new SpeechAudioFormatInfo(16000, AudioBitsPerSample.Sixteen, AudioChannel.Mono));
voice.Speak(args[1]);
voice.SetOutputToNull();
Console.WriteLine("spoke: yes");
`

// A System.Speech program speaks with axx's voice, the user's default while the scenario lasts:
// Piper's answer to the voice stub's mapping, into SAPI's output and into the speaker's folder;
// the scenario's end takes the voice out again.
func TestSAPIVoice(t *testing.T) {
	dir := workDir(t)
	c := &contract.Contract{
		Format: contract.Format{Type: "piper", IRVersion: 8, Opsets: map[string]int64{"": 13}, Details: json.RawMessage(`{"sampleRate":16000}`)},
		Task:   "text-to-speech",
		Inputs: []contract.Tensor{
			{Name: "input", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}},
			{Name: "input_lengths", Type: "int64", Shape: []contract.Dim{{Size: 1}}},
			{Name: "scales", Type: "float32", Shape: []contract.Dim{{Size: 3}}},
		},
		Outputs: []contract.Tensor{{Name: "output", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 1}, {Size: 64}}}},
		Text:    &contract.Text{IDs: "input", Tokenizer: "piper", Vocabulary: "vocabulary"},
	}
	c.Carry("vocabulary", "vocab.json", []byte(`{"_":[0],"^":[1],"$":[2]," ":[3],"a":[14],"b":[15],"c":[16],"d":[17],"e":[18],"f":[19],"g":[20],"h":[21],"i":[22],"l":[23],"n":[24],"o":[25],"p":[26],"r":[27],"s":[28],"t":[29],"u":[30],"v":[31],"w":[32],"y":[33]}`))
	c.Carry("config", "voice.json", []byte(`{"audio":{"sample_rate":16000},"phoneme_id_map":{"_":[0],"^":[1],"$":[2]}}`))
	stubs := modelstest.Dir(t, c,
		map[string]any{"name": "Ring", "input": map[string]any{"text": "ring twice"}, "output": map[string]any{"audio": "../inputs/ring.wav"}},
		modelstest.Mapping("Anything else", "", map[string]any{"audio": "../inputs/default.wav"}),
	)
	for name, value := range map[string]float64{"ring": 0.25, "default": 0} {
		modelstest.WriteWAV(t, filepath.Join(stubs, "inputs", name+".wav"), 16000, slicesOf(64, value))
	}
	s, err := stub.Load(stubs)
	if err != nil {
		t.Fatal(err)
	}
	m, err := piper.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	models := filepath.Join(dir, platform.Folder)
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(models, "voice.onnx"), m.File, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(models, "voice.onnx.json"), m.Beside[".onnx.json"], 0o644); err != nil {
		t.Fatal(err)
	}

	probe := filepath.Join(build(t, filepath.Join(dir, "probe"), map[string]string{"Probe.csproj": speakProject, "Program.cs": speakProgram}), "Probe.exe")
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "a voice"}, core.NewSuite(core.SuiteOptions{}), nil)
	before, had := defaultVoice()
	changed := &changes{}
	l := &desktopcore.Launch{App: &desktopcore.App{Name: "probe", App: probe}, Files: dir, HostFiles: dir, Environment: map[string]string{}}
	if err := speakWith(sc, changed, l); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = changed.restore() })
	out := filepath.Join(dir, "spoken.wav")
	got := run(t, probe, l, out, "Please ring twice at the door.")
	if got["voice"] != "axx voice" {
		t.Errorf("the default voice is %q, want axx voice", got["voice"])
	}
	utterances, _ := filepath.Glob(filepath.Join(models, ".speaker", "*.wav"))
	if len(utterances) != 1 {
		t.Fatalf("the speaker's folder has %d utterances, want 1", len(utterances))
	}
	// The mapping's 64 samples (Piper normalizes them to 32767), in silence: Piper's after its
	// sentence, and SAPI's.
	for _, path := range []string{utterances[0], out} {
		heard := samples(t, path)
		first := slices.Index(heard, 32767)
		if first < 0 || first+64 > len(heard) || slices.ContainsFunc(heard[first:first+64], func(v int16) bool { return v != 32767 }) {
			t.Errorf("%s has not the mapping's 64 samples (%d samples)", path, len(heard))
			continue
		}
		if slices.ContainsFunc(heard[:first], func(v int16) bool { return v != 0 }) || slices.ContainsFunc(heard[first+64:], func(v int16) bool { return v != 0 }) {
			t.Errorf("%s has more than the mapping's samples and silence", path)
		}
	}
	if err := changed.restore(); err != nil {
		t.Fatal(err)
	}
	if now, nowHad := defaultVoice(); now != before || nowHad != had {
		t.Errorf("the default voice is %q (%v) after the scenario, want %q (%v)", now, nowHad, before, had)
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, tokenKey, registry.QUERY_VALUE); err == nil {
		k.Close()
		t.Errorf("axx's voice is still SAPI's")
	}
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func slicesOf(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

const aiProject = `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0-windows10.0.19041.0</TargetFramework>
    <TargetPlatformMinVersion>10.0.17763.0</TargetPlatformMinVersion>
    <Platforms>x64</Platforms>
    <RuntimeIdentifier>win-x64</RuntimeIdentifier>
    <WindowsPackageType>None</WindowsPackageType>
    <WindowsAppSDKSelfContained>true</WindowsAppSDKSelfContained>
    <ImplicitUsings>enable</ImplicitUsings>
    <ApplicationManifest>app.manifest</ApplicationManifest>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Microsoft.WindowsAppSDK" Version="1.8.260921001" />
  </ItemGroup>
</Project>
`

const aiManifest = `<?xml version="1.0" encoding="utf-8"?>
<assembly manifestVersion="1.0" xmlns="urn:schemas-microsoft-com:asm.v1">
  <msix xmlns="urn:schemas-microsoft-com:msix.v1" publisher="CN=axx test" packageName="Axx.ModelsWindows.Probe" applicationId="Probe" />
</assembly>
`

const aiPackage = `<?xml version="1.0" encoding="utf-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"
         xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"
         xmlns:uap10="http://schemas.microsoft.com/appx/manifest/uap/windows10/10"
         xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"
         xmlns:systemai="http://schemas.microsoft.com/appx/manifest/systemai/windows10"
         IgnorableNamespaces="uap uap10 rescap systemai">
  <Identity Name="Axx.ModelsWindows.Probe" Publisher="CN=axx test" Version="1.0.0.0" ProcessorArchitecture="neutral" />
  <Properties>
    <DisplayName>Probe</DisplayName>
    <PublisherDisplayName>axx</PublisherDisplayName>
    <Logo>logo.png</Logo>
    <uap10:AllowExternalContent>true</uap10:AllowExternalContent>
  </Properties>
  <Resources><Resource Language="en-us" /></Resources>
  <Dependencies><TargetDeviceFamily Name="Windows.Desktop" MinVersion="10.0.17763.0" MaxVersionTested="10.0.26226.0" /></Dependencies>
  <Capabilities>
    <rescap:Capability Name="runFullTrust" />
    <systemai:Capability Name="systemAIModels" />
  </Capabilities>
  <Applications>
    <Application Id="Probe" Executable="Probe.exe" uap10:TrustLevel="mediumIL" uap10:RuntimeBehavior="win32App">
      <uap:VisualElements AppListEntry="none" DisplayName="Probe" Description="Probe" BackgroundColor="transparent" Square150x150Logo="logo.png" Square44x44Logo="logo.png" />
    </Application>
  </Applications>
</Package>
`

// The AI probe calls each API as an app does, a line of each answer.
const aiProgram = `using System.Runtime.InteropServices.WindowsRuntime;
using Microsoft.Graphics.Imaging;
using Microsoft.Windows.AI;
using Microsoft.Windows.AI.ContentSafety;
using Microsoft.Windows.AI.Imaging;
using Microsoft.Windows.AI.Text;
using Windows.Graphics;
using Windows.Graphics.Imaging;
using Windows.Storage;

static async Task Say(string what, Func<Task<string>> f)
{
    try { Console.WriteLine($"{what}: {await f()}"); }
    catch (Exception e) { Console.WriteLine($"{what}: threw 0x{e.HResult:X8}"); }
}
static async Task<SoftwareBitmap> Bitmap(string path)
{
    var file = await StorageFile.GetFileFromPathAsync(path);
    using var stream = await file.OpenReadAsync();
    var decoder = await BitmapDecoder.CreateAsync(stream);
    return await decoder.GetSoftwareBitmapAsync(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Premultiplied);
}

var (door, empty) = (args[0], args[1]);
await Say("identity", () => Task.FromResult(Windows.ApplicationModel.Package.Current.Id.FullName));
await Say("capability", () => Task.FromResult(Windows.Security.Authorization.AppCapabilityAccess.AppCapability.Create("systemAIModels").CheckAccess().ToString()));
await Say("ready", () => Task.FromResult(TextRecognizer.GetReadyState().ToString()));
await Say("ensure", async () => (await TextRecognizer.EnsureReadyAsync()).Status.ToString());
await Say("text door", async () => { using var r = await TextRecognizer.CreateAsync(); return string.Join("|", r.RecognizeTextFromImage(ImageBuffer.CreateForSoftwareBitmap(await Bitmap(door))).Lines.Select(l => l.Text)); });
await Say("text empty", async () => { using var r = await TextRecognizer.CreateAsync(); return r.RecognizeTextFromImage(ImageBuffer.CreateForSoftwareBitmap(await Bitmap(empty))).Lines.Length.ToString(); });
await Say("describe door", async () => { using var g = await ImageDescriptionGenerator.CreateAsync(); return (await g.DescribeAsync(ImageBuffer.CreateForSoftwareBitmap(await Bitmap(door)), ImageDescriptionKind.BriefDescription, new ContentFilterOptions())).Description; });
await Say("mask door", async () =>
{
    var bitmap = await Bitmap(door);
    using var x = await ImageObjectExtractor.CreateWithSoftwareBitmapAsync(bitmap);
    var mask = x.GetSoftwareBitmapObjectMask(new ImageObjectExtractorHint([new RectInt32(0, 0, 8, 8)], [], []));
    var bytes = new byte[mask.PixelWidth * mask.PixelHeight];
    mask.CopyToBuffer(bytes.AsBuffer());
    return $"{mask.PixelWidth}x{mask.PixelHeight} {bytes.Count(b => b >= 128) * 1000 / bytes.Length}";
});
await Say("model", async () =>
{
    using var model = await LanguageModel.CreateAsync();
    var op = model.GenerateResponseAsync(model.CreateContext("You sum up a parcel depot's day."), "Today: PX-DSK-4311, Express, fragile.", new LanguageModelOptions());
    var deltas = 0;
    op.Progress = (_, _) => deltas++;
    var r = await op;
    return $"{r.Text} ({(deltas > 1 ? "streamed" : "whole")})";
});
await Say("summary", async () => { using var model = await LanguageModel.CreateAsync(); return (await new TextSummarizer(model).SummarizeParagraphAsync("The handover rules, for the courier.")).Text; });
await Say("rewrite", async () => { using var model = await LanguageModel.CreateAsync(); return (await new TextRewriter(model).RewriteAsync("Handover at 18:00.", TextRewriteTone.Casual)).Text; });
await Say("table", async () => { using var model = await LanguageModel.CreateAsync(); return string.Join("|", (await new TextToTableConverter(model).ConvertAsync("PX-DSK-4311 came in express.")).GetRows().Select(r => string.Join(",", r.GetColumns()))); });
`

// A Windows App SDK program's calls of the Windows AI APIs get the stubs' answers, with axx's
// library in place of its own files, as it starts: with package identity, as Windows answers it;
// without, refused as Windows refuses it; and the stub of a PC without the model gives Windows'
// reason. The scenario's end puts the program's own files back.
func TestWindowsAIAPIs(t *testing.T) {
	dir := workDir(t)
	door, empty := filepath.Join(dir, "door.png"), filepath.Join(dir, "empty.png")
	modelstest.Door.Write(t, door)
	modelstest.Empty.Write(t, empty)
	mask := image.NewGray(image.Rect(0, 0, modelstest.Door.W, modelstest.Door.H))
	for y := range modelstest.Door.H {
		for x := range modelstest.Door.W {
			if x < modelstest.Door.W/2 {
				mask.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	modelstest.WritePNG(t, filepath.Join(dir, "half-mask.png"), mask)
	nobody := image.NewGray(mask.Rect)
	modelstest.WritePNG(t, filepath.Join(dir, "no-mask.png"), nobody)

	write := func(models, api, availability string, mappings ...any) *platform.Artifact {
		t.Helper()
		a, _ := platform.APIByName(api)
		c, err := a.Contract(nil)
		if err != nil {
			t.Fatal(err)
		}
		details, _ := json.Marshal(map[string]string{"api": api, "availability": availability})
		if availability == "" {
			details, _ = json.Marshal(map[string]string{"api": api})
		}
		c.Format.Details = details
		s, err := stub.Load(modelstest.Dir(t, c, mappings...))
		if err != nil {
			t.Fatal(err)
		}
		b, err := platform.Write(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(models, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(models, strings.TrimPrefix(api, "windows.ai.")+platform.Extension), b, 0o644); err != nil {
			t.Fatal(err)
		}
		art, err := platform.Read(b)
		if err != nil {
			t.Fatal(err)
		}
		return art
	}
	text := func(name, key, answer string) map[string]any {
		return map[string]any{"name": name, "input": map[string]any{"text": key}, "output": map[string]any{"text": answer}}
	}
	other := func(output map[string]any) map[string]any { return modelstest.Mapping("Anything else", "", output) }
	models := filepath.Join(dir, "files", platform.Folder)
	stubs := []*platform.Artifact{
		write(models, "windows.ai.text-recognizer", "", modelstest.Mapping("The door", door, map[string]any{"text": "PX-5101 LEIPZIG"}), other(map[string]any{"text": ""})),
		write(models, "windows.ai.image-description", "", modelstest.Mapping("The door", door, map[string]any{"text": "A parcel at a door."}), other(map[string]any{"text": "A photo."})),
		write(models, "windows.ai.object-extractor", "", modelstest.Mapping("The door", door, map[string]any{"mask": filepath.Join(dir, "half-mask.png")}), other(map[string]any{"mask": filepath.Join(dir, "no-mask.png")})),
		write(models, "windows.ai.language-model", "", text("A busy day", "PX-DSK-4311, Express, fragile", "Mind PX-DSK-4311: express and fragile."), other(map[string]any{"text": "A quiet day."})),
		write(models, "windows.ai.text-summarizer", "", text("The rules", "handover", "Handover at 18:00."), other(map[string]any{"text": "Nothing."})),
		write(models, "windows.ai.text-rewriter", "", text("The time", "18:00", "The courier comes at six."), other(map[string]any{"text": "Nothing."})),
		// The table's answer is its rows, in json.
		write(models, "windows.ai.text-to-table", "", map[string]any{"name": "Arrivals", "input": map[string]any{"text": "PX-DSK"}, "output": map[string]any{"json": [][]string{{"Reference", "Level"}, {"PX-DSK-4311", "Express"}}}}, other(map[string]any{"json": [][]string{}})),
	}
	for _, a := range stubs {
		if err := supported(a); err != nil {
			t.Fatal(err)
		}
	}

	out := build(t, filepath.Join(dir, "probe"), map[string]string{"Probe.csproj": aiProject, "Program.cs": aiProgram, "app.manifest": aiManifest})
	exe := filepath.Join(out, "Probe.exe")
	if err := os.WriteFile(filepath.Join(out, "AppxManifest.xml"), []byte(aiPackage), 0o644); err != nil {
		t.Fatal(err)
	}
	modelstest.WritePNG(t, filepath.Join(out, "logo.png"), modelstest.Plain(44, 44, modelstest.Brown))
	identity := func(add bool) {
		t.Helper()
		script := "Get-AppxPackage Axx.ModelsWindows.Probe | Remove-AppxPackage"
		if add {
			script = "Add-AppxPackage -Register '" + filepath.Join(out, "AppxManifest.xml") + "' -ExternalLocation '" + out + "'"
		}
		if b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s (Windows registers a package of no signature in Developer Mode)", script, err, b)
		}
	}
	identity(true)
	t.Cleanup(func() { identity(false) })
	files := map[string][]byte{}
	for _, f := range []string{"Microsoft.Windows.AI.Imaging.dll", "Microsoft.Windows.AI.Text.dll"} {
		b, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatal(err)
		}
		files[f] = b
	}

	// Windows' own answers on this machine, before axx's library is the program's: its identity, the
	// capability Windows gives it, and the real APIs'.
	own := run(t, exe, &desktopcore.Launch{Environment: map[string]string{}}, door, empty)
	t.Logf("Windows' own answers: identity %q, systemAIModels %q, TextRecognizer %q, LanguageModel %q", own["identity"], own["capability"], own["ready"], own["model"])
	if strings.HasPrefix(own["identity"], "threw") || own["capability"] != "Allowed" {
		t.Fatalf("the program has no package identity with the systemAIModels capability here (identity %q, capability %q): Windows answers no app the Windows AI APIs, and axx's library answers as Windows does", own["identity"], own["capability"])
	}

	suite := core.NewSuite(core.SuiteOptions{})
	launch := func(models string, stubs []*platform.Artifact) (*changes, *desktopcore.Launch) {
		t.Helper()
		sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "the AI APIs"}, suite, nil)
		c := &changes{}
		l := &desktopcore.Launch{App: &desktopcore.App{Name: "probe", App: exe}, Files: filepath.Dir(models), HostFiles: filepath.Dir(models), Environment: map[string]string{}}
		if err := answerAI(sc, c, l, stubs); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.restore() })
		return c, l
	}
	c, l := launch(models, stubs)
	got := run(t, exe, l, door, empty)
	for what, want := range map[string]string{
		"ready":         "Ready",
		"ensure":        "Success",
		"text door":     "PX-5101 LEIPZIG",
		"text empty":    "0",
		"describe door": "A parcel at a door.",
		"model":         "Mind PX-DSK-4311: express and fragile. (streamed)",
		"summary":       "Handover at 18:00.",
		"rewrite":       "The courier comes at six.",
		"table":         "Reference,Level|PX-DSK-4311,Express",
	} {
		if got[what] != want {
			t.Errorf("%s: %q, want %q", what, got[what], want)
		}
	}
	size, perMille, _ := strings.Cut(got["mask door"], " ")
	if want, share := fmt.Sprintf("%dx%d", modelstest.Door.W, modelstest.Door.H), atoi(perMille); size != want || share < 490 || share > 510 {
		t.Errorf("mask door: %q, want the door's size (%s) and half of it the object's (500 in 1000)", got["mask door"], want)
	}

	// Without identity, Windows refuses the program, and so does the library.
	identity(false)
	got = run(t, exe, l, door, empty)
	for _, what := range []string{"ready", "text door", "model"} {
		if got[what] != "threw 0x80070005" {
			t.Errorf("without identity, %s: %q, want access denied (0x80070005)", what, got[what])
		}
	}
	identity(true)
	if err := c.restore(); err != nil {
		t.Fatal(err)
	}

	// A PC without the model: GetReadyState's reason, and CreateAsync's error, Windows' own.
	unavailable := filepath.Join(dir, "unavailable", platform.Folder)
	c, l = launch(unavailable, []*platform.Artifact{write(unavailable, "windows.ai.text-recognizer", "NotSupportedOnCurrentSystem", other(map[string]any{"text": ""}))})
	got = run(t, exe, l, door, empty)
	for what, want := range map[string]string{"ready": "NotSupportedOnCurrentSystem", "ensure": "Failure", "text door": "threw 0x80070490"} {
		if got[what] != want {
			t.Errorf("a PC without the model, %s: %q, want %q", what, got[what], want)
		}
	}
	if err := c.restore(); err != nil {
		t.Fatal(err)
	}
	for f, b := range files {
		now, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(now, b) {
			t.Errorf("%s is not the program's own after the scenarios", f)
		}
	}
}
