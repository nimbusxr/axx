package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/skills"
)

// fakeAxx is a release's axx: it says its version, and notes how it was called
// in the file that AXX_FAKE_LOG names.
func fakeAxx(v string) string {
	return "#!/bin/sh\n" +
		"[ -n \"$AXX_FAKE_LOG\" ] && echo \"$*\" >> \"$AXX_FAKE_LOG\"\n" +
		"[ \"$1\" = version ] && echo '{\"ok\":true,\"data\":{\"version\":\"" + v + "\"}}'\n" +
		"[ \"$1\" = \"$AXX_FAKE_FAIL\" ] && { echo \"problem found by $1 on " + v + "\"; exit 1; }\n" +
		"exit 0\n"
}

// releaseArchive is the archive of a release this OS gets, holding binary as its axx.
func releaseArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var b bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&b)
		f, err := zw.Create(selfupdate.BinaryName(runtime.GOOS))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(binary)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "axx", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(binary)); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}

// fakeRelease is what GitHub serves for a release of axx.
func fakeRelease(t *testing.T, mux *http.ServeMux, v string, binary string, corrupt bool) string {
	t.Helper()
	archive := releaseArchive(t, binary)
	name := selfupdate.ArchiveName(v, runtime.GOOS, runtime.GOARCH)
	s := sha256.Sum256(archive)
	sum := hex.EncodeToString(s[:])
	if corrupt {
		sum = strings.Repeat("0", 64)
	}
	mux.HandleFunc("/dl/v"+v+"/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/dl/v"+v+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(sum + "  " + name + "\n")) })
	return name
}

type fakeNotes struct{ version, name, body string }

// fakeGitHub serves the releases list, and returns the installed axx it upgrades.
func fakeGitHub(t *testing.T, current string, method selfupdate.Method, notes []fakeNotes, corrupt bool) (exe string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the release's axx here is a shell script")
	}
	mux := http.NewServeMux()
	var list []map[string]any
	for _, n := range notes {
		assets := []map[string]string{}
		if n.name != "" {
			name := fakeRelease(t, mux, n.version, fakeAxx(n.version), corrupt)
			assets = append(assets, map[string]string{"name": "checksums.txt"}, map[string]string{"name": name})
		}
		list = append(list, map[string]any{"tag_name": "v" + n.version, "published_at": "2026-11-02T00:00:00Z", "body": n.body, "assets": assets})
	}
	mux.HandleFunc("/api/releases", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(list) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	exe = filepath.Join(dir, "axx")
	if err := os.WriteFile(exe, []byte(fakeAxx(current)), 0o755); err != nil {
		t.Fatal(err)
	}
	old := newUpgradeEnv
	newUpgradeEnv = func() (*upgradeEnv, error) {
		return &upgradeEnv{
			client: &selfupdate.Client{API: srv.URL + "/api", Downloads: srv.URL + "/dl", HTTP: srv.Client()},
			exe:    exe, invoked: exe, current: current, method: method,
		}, nil
	}
	t.Cleanup(func() { newUpgradeEnv = old })
	return exe
}

func exeVersion(t *testing.T, exe string) string {
	t.Helper()
	v, err := installedVersion(context.Background(), exe)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// leftovers are the files an upgrade left next to axx.
func leftovers(t *testing.T, exe string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Name() != filepath.Base(exe) {
			out = append(out, e.Name())
		}
	}
	return out
}

var twoReleases = []fakeNotes{
	{version: "0.4.0", name: "yes", body: "## [0.4.0](u) (2026-11-02)\n\n### ⚠ BREAKING CHANGES\n\n* **sql:** the clock in seeds ([#292](u))\n\n### Features\n\n* **sql:** triggers ([#293](u)) ([042359d](u))\n"},
	{version: "0.3.1", name: "yes", body: "### Features\n\n* connections that fail ([#291](u))\n"},
	{version: "0.3.0", name: "yes", body: "### Features\n\n* what the person already has ([#1](u))\n"},
}

func TestUpgradeReplacesItselfAndSaysWhatChanged(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.3.0", selfupdate.Archive, twoReleases, false)
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.4.0" {
		t.Errorf("axx is %s", v)
	}
	if l := leftovers(t, exe); len(l) != 0 {
		t.Errorf("left next to axx: %v", l)
	}
	for _, want := range []string{"Breaking changes", "sql: the clock in seeds", "sql: triggers", "connections that fail", "upgraded axx from 0.3.0 to 0.4.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "already has") || strings.Contains(out, "[#") {
		t.Errorf("output has a release it had, or links:\n%s", out)
	}
	if !strings.Contains(stderr, "checksum verified") {
		t.Errorf("stderr: %s", stderr)
	}
}

func TestUpgradeLeavesAxxAloneWhenTheChecksumFails(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.3.0", selfupdate.Archive, twoReleases, true)
	_, stderr, code := run(t, "upgrade")
	if code != int(exitcode.Environment) || !strings.Contains(stderr, "AXX-E0018") || !strings.Contains(stderr, "checksum mismatch") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.3.0" {
		t.Errorf("axx is %s", v)
	}
	if l := leftovers(t, exe); len(l) != 0 {
		t.Errorf("left next to axx: %v", l)
	}
}

func TestUpgradeLeavesAxxAloneWhenTheDownloadIsAnotherVersion(t *testing.T) {
	agentProject(t, nil)
	notes := []fakeNotes{{version: "0.4.0", name: "yes"}}
	exe := fakeGitHub(t, "0.3.0", selfupdate.Archive, notes, false)
	// A checksummed archive that says it is something else.
	old := newUpgradeEnv
	mux := http.NewServeMux()
	fakeRelease(t, mux, "0.4.0", fakeAxx("0.2.0"), false)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	newUpgradeEnv = func() (*upgradeEnv, error) {
		env, err := old()
		if err == nil {
			env.client.Downloads = srv.URL + "/dl"
		}
		return env, err
	}
	t.Cleanup(func() { newUpgradeEnv = old })
	_, stderr, code := run(t, "upgrade")
	if code != int(exitcode.Environment) || !strings.Contains(stderr, "does not run here") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.3.0" {
		t.Errorf("axx is %s", v)
	}
}

func TestUpgradeWithNothingNewer(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.4.0", selfupdate.Archive, twoReleases, false)
	before, _ := os.ReadFile(exe)
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) || !strings.Contains(out, "axx 0.4.0 is the newest release") {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	if after, _ := os.ReadFile(exe); !bytes.Equal(before, after) {
		t.Error("it replaced axx")
	}
}

// A release can be out before its files are, and an axx can be newer than any
// release with files.
func TestUpgradeNeverGoesBackWithoutBeingAsked(t *testing.T) {
	agentProject(t, nil)
	notes := append([]fakeNotes{{version: "0.5.0"}}, twoReleases...)
	exe := fakeGitHub(t, "0.4.1", selfupdate.Archive, notes, false)
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) || !strings.Contains(out, "is the newest release") {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	if v := exeVersion(t, exe); v != "0.4.1" {
		t.Errorf("axx is %s", v)
	}
}

func TestUpgradeCheckChangesNothing(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.3.1", selfupdate.Archive, twoReleases, false)
	out, stderr, code := run(t, "upgrade", "--check")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"sql: the clock in seeds", "axx 0.3.1 is installed and 0.4.0 is available"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if v := exeVersion(t, exe); v != "0.3.1" {
		t.Errorf("axx is %s", v)
	}
}

func TestUpgradeGoesBackToAVersionItIsGiven(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.4.0", selfupdate.Archive, twoReleases, false)
	out, stderr, code := run(t, "upgrade", "--version", "v0.3.1")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.3.1" {
		t.Errorf("axx is %s", v)
	}
	if !strings.Contains(out, "went back from axx 0.4.0 to 0.3.1") {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(out, "what changed") {
		t.Errorf("going back lists no release as new:\n%s", out)
	}
}

func TestUpgradeToAVersionThatDoesNotExist(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.4.0", selfupdate.Archive, twoReleases, false)
	_, stderr, code := run(t, "upgrade", "--version", "9.9.9")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0019") || !strings.Contains(stderr, "9.9.9") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.4.0" {
		t.Errorf("axx is %s", v)
	}
	if l := leftovers(t, exe); len(l) != 0 {
		t.Errorf("left next to axx: %v", l)
	}
}

func TestUpgradeUpdatesTheSkillsWhereTheyAreInstalled(t *testing.T) {
	dir, home := agentProject(t, map[string]string{"axx.yaml": "version: 1\n"})
	for _, root := range []string{dir, home} {
		writeFiles(t, root, map[string]string{".agents/skills/" + skills.LockFile: `{"axxVersion":"0.3.0"}`})
	}
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("AXX_FAKE_LOG", log)
	fakeGitHub(t, "0.3.0", selfupdate.Archive, twoReleases, false)
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"skills install --json\n", "skills install --scope user --json\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the new axx was not run with %q:\n%s", want, b)
		}
	}
	if !strings.Contains(out, "updated the agent skills of this project and your home directory") {
		t.Errorf("output:\n%s", out)
	}
}

func TestUpgradeLeavesSkillsThatAreNotInstalled(t *testing.T) {
	agentProject(t, map[string]string{"axx.yaml": "version: 1\n"})
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("AXX_FAKE_LOG", log)
	fakeGitHub(t, "0.3.0", selfupdate.Archive, twoReleases, false)
	if _, stderr, code := run(t, "upgrade"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "skills") {
		t.Errorf("it ran the skills:\n%s", b)
	}
}

// fakeTool puts a program on the PATH that notes how it was called.
func fakeTool(t *testing.T, name, script string) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	body := "#!/bin/sh\necho \"$*\" >> \"" + log + "\"\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// Homebrew's axx is Homebrew's to replace: replacing the file would leave it
// believing it has the old one.
func TestUpgradeHasHomebrewDoIt(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.3.1", selfupdate.Homebrew, twoReleases, false)
	upgraded := filepath.Join(t.TempDir(), "axx")
	if err := os.WriteFile(upgraded, []byte(fakeAxx("0.4.0")), 0o755); err != nil {
		t.Fatal(err)
	}
	log := fakeTool(t, "brew", `[ "$1" = upgrade ] && cp "`+upgraded+`" "`+exe+`"; exit 0`)
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	b, _ := os.ReadFile(log)
	if string(b) != "update --quiet\nupgrade --cask axx\n" {
		t.Errorf("brew calls:\n%s", b)
	}
	if !strings.Contains(out, "upgraded axx from 0.3.1 to 0.4.0") {
		t.Errorf("output:\n%s", out)
	}
}

// Homebrew's tap reaches a release some minutes after it.
func TestUpgradeSaysWhenHomebrewHasNoNewerVersionYet(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.3.1", selfupdate.Homebrew, twoReleases, false)
	fakeTool(t, "brew", "exit 0")
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(out, "Homebrew's tap has no newer version yet") {
		t.Errorf("output:\n%s", out)
	}
	if v := exeVersion(t, exe); v != "0.3.1" {
		t.Errorf("axx is %s", v)
	}
}

func TestUpgradeTakesNoVersionFromHomebrew(t *testing.T) {
	agentProject(t, nil)
	fakeGitHub(t, "0.4.0", selfupdate.Homebrew, twoReleases, false)
	_, stderr, code := run(t, "upgrade", "--version", "0.3.1")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX_VERSION=0.3.1") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestUpgradeHasGoInstallIt(t *testing.T) {
	agentProject(t, nil)
	fakeGitHub(t, "0.3.1", selfupdate.GoInstall, twoReleases, false)
	log := fakeTool(t, "go", "exit 0")
	if _, stderr, code := run(t, "upgrade"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	b, _ := os.ReadFile(log)
	if string(b) != "install github.com/nimbusxr/axx/cmd/axx@v0.4.0\n" {
		t.Errorf("go calls:\n%s", b)
	}
}

func TestUpgradeRefusesABuildOfACheckout(t *testing.T) {
	agentProject(t, nil)
	fakeGitHub(t, "0.0.0-dev", selfupdate.Source, twoReleases, false)
	_, stderr, code := run(t, "upgrade")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0019") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestUpgradeAsksANightlyWhichVersion(t *testing.T) {
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.1.0-SNAPSHOT-abc", selfupdate.Prerelease, twoReleases, false)
	_, stderr, code := run(t, "upgrade")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "--version nightly") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, stderr, code := run(t, "upgrade", "--version", "0.4.0"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.4.0" {
		t.Errorf("axx is %s", v)
	}
}

func TestUpgradeSaysWhenItCannotWriteThere(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes anywhere")
	}
	agentProject(t, nil)
	exe := fakeGitHub(t, "0.3.1", selfupdate.Archive, twoReleases, false)
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, stderr, code := run(t, "upgrade")
	if code != int(exitcode.Environment) || !strings.Contains(stderr, "AXX_INSTALL_DIR") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
