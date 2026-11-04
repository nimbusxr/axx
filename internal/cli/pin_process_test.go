package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
)

// The tests of this file run axx as a machine does, on every OS: in processes of
// their own, with a real program for a release, unpacked from the archive that OS
// gets. The other tests of versions and upgrades watch for a hand-over instead of
// making it, and their releases are shell scripts, which Windows cannot run.

// releaseProgram is a release of axx: it says its version as axx does, and otherwise
// says how it was started and exits as releaseExitEnv says.
const releaseProgram = `package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

var version string

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("{\"ok\":true,\"data\":{\"version\":%q}}\n", version)
		return
	}
	stdin, _ := io.ReadAll(os.Stdin)
	fmt.Printf("release %s ran %q for launcher %q with stdin %q\n", version, os.Args[1:], os.Getenv("AXX_LAUNCHER"), stdin)
	code, _ := strconv.Atoi(os.Getenv("AXX_CLI_RELEASE_EXIT"))
	os.Exit(code)
}
`

const releaseExitEnv = "AXX_CLI_RELEASE_EXIT"

// buildRelease compiles releaseProgram as a release of a version. A test does this
// before aMachine, as Go keeps its caches where that moves them from.
func buildRelease(t *testing.T, v string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(releaseProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, selfupdate.BinaryName(runtime.GOOS))
	cmd := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.version="+v, "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building a release: %v\n%s", err, out)
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// aMachine gives the processes a test starts a home and caches of their own, and
// says where axx keeps its versions there.
func aMachine(t *testing.T) (home, cache string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(envNoReminder, "1")
	t.Setenv(nestedEnv, "1")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return home, cache
}

// serveReleases serves releases as GitHub does, and counts the archives it gives.
func serveReleases(t *testing.T, mux *http.ServeMux, releases map[string]string) (*atomic.Int32, *httptest.Server) {
	t.Helper()
	list := []map[string]any{}
	for v, binary := range releases {
		name := fakeRelease(t, mux, v, binary, false)
		list = append(list, map[string]any{
			"tag_name": "v" + v, "published_at": "2026-11-02T00:00:00Z", "body": "### Features\n\n* something new\n",
			"assets": []map[string]string{{"name": selfupdate.ChecksumsFile}, {"name": name}},
		})
	}
	mux.HandleFunc("/api/releases", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(list) })
	var archives atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") || strings.HasSuffix(r.URL.Path, ".tar.gz") {
			archives.Add(1)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(selfupdate.EnvDownloadURL, srv.URL+"/dl")
	t.Setenv(installedAPIEnv, srv.URL+"/api")
	return &archives, srv
}

// installedAxx is a copy of the test binary in a directory of its own, for what
// replaces the running axx.
func installedAxx(t *testing.T, home string) string {
	t.Helper()
	from, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	exe := filepath.Join(home, "bin", selfupdate.BinaryName(runtime.GOOS))
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(exe, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return exe
}

// runInstalled runs exe as the axx 0.9.0 a machine has installed.
func runInstalled(t *testing.T, exe, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), installedEnv+"=0.9.0")
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

// versionOf is the version exe says it is, which is 0.9.0 while it is still the
// installed axx.
func versionOf(t *testing.T, exe string) string {
	t.Helper()
	stdout, stderr, code := runInstalled(t, exe, t.TempDir(), "", "version", "--json")
	var env struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); code != 0 || err != nil {
		t.Fatalf("exit %d, %v\nstdout: %s\nstderr: %s", code, err, stdout, stderr)
	}
	return env.Data.Version
}

func projectNaming(t *testing.T, v string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\naxx: "+v+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAProjectsVersionTakesOverTheProcess(t *testing.T) {
	release := buildRelease(t, "0.5.0")
	_, cache := aMachine(t)
	archives, _ := serveReleases(t, http.NewServeMux(), map[string]string{"0.5.0": release})
	dir := projectNaming(t, "0.5.0")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(releaseExitEnv, "7")
	stdout, stderr, code := runInstalled(t, exe, dir, "typed\n", "validate", "--compact")
	if code != 7 {
		t.Fatalf("its exit code is not the release's: %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if want := `release 0.5.0 ran ["validate" "--compact"] for launcher "0.9.0" with stdin "typed\n"`; !strings.Contains(stdout, want) {
		t.Errorf("stdout %q has no %q", stdout, want)
	}
	if !isRegularFile(versionBinary(cache, "0.5.0")) {
		t.Errorf("nothing kept at %s", versionBinary(cache, "0.5.0"))
	}

	t.Setenv(releaseExitEnv, "0")
	stdout, stderr, code = runInstalled(t, exe, dir, "", "run")
	if code != 0 || !strings.Contains(stdout, `release 0.5.0 ran ["run"]`) {
		t.Errorf("the second run: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if n := archives.Load(); n != 1 {
		t.Errorf("downloaded %d times", n)
	}
}

func TestAReleaseThatCannotBeTrustedDoesNotTakeOver(t *testing.T) {
	for _, tc := range []struct {
		name    string
		says    string
		corrupt bool
		want    string
	}{
		{name: "not what its checksums say", says: "0.5.0", corrupt: true, want: "checksum mismatch"},
		{name: "another version than it says", says: "0.5.1", want: "does not run here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := buildRelease(t, tc.says)
			_, cache := aMachine(t)
			mux := http.NewServeMux()
			fakeRelease(t, mux, "0.5.0", release, tc.corrupt)
			serveReleases(t, mux, nil)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := runInstalled(t, exe, projectNaming(t, "0.5.0"), "", "validate")
			if code != int(exitcode.Environment) || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if strings.Contains(stdout, "release ") {
				t.Errorf("it ran: %s", stdout)
			}
			if isRegularFile(versionBinary(cache, "0.5.0")) {
				t.Error("it was kept")
			}
		})
	}
}

// onlyUpgradeLeftovers fails on what an upgrade left next to axx, but for the old axx
// on Windows, which cannot remove a file that is running: the next upgrade does.
func onlyUpgradeLeftovers(t *testing.T, exe string) {
	t.Helper()
	left := leftovers(t, exe)
	if runtime.GOOS == "windows" {
		left = slices.DeleteFunc(left, func(f string) bool { return f == filepath.Base(exe)+".old" })
	}
	if len(left) > 0 {
		t.Errorf("left next to axx: %v", left)
	}
}

func TestAnUpgradeReplacesTheRunningAxx(t *testing.T) {
	release := buildRelease(t, "0.9.1")
	home, _ := aMachine(t)
	serveReleases(t, http.NewServeMux(), map[string]string{"0.9.1": release})
	exe := installedAxx(t, home)

	stdout, stderr, code := runInstalled(t, exe, t.TempDir(), "", "upgrade")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got := versionOf(t, exe); got != "0.9.1" {
		t.Errorf("axx is now %q\nstdout: %s\nstderr: %s", got, stdout, stderr)
	}
	onlyUpgradeLeftovers(t, exe)
}

func TestAnUpgradeInAProjectMovesItAndTheRunningAxx(t *testing.T) {
	release := buildRelease(t, "0.9.1")
	home, cache := aMachine(t)
	serveReleases(t, http.NewServeMux(), map[string]string{"0.9.1": release})
	exe := installedAxx(t, home)
	dir := projectNaming(t, "0.9.0")

	stdout, stderr, code := runInstalled(t, exe, dir, "", "upgrade")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	pin, err := config.ReadPin(filepath.Join(dir, "axx.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if pin.Version != "0.9.1" {
		t.Errorf("the project names %q\nstdout: %s\nstderr: %s", pin.Version, stdout, stderr)
	}
	if !isRegularFile(versionBinary(cache, "0.9.1")) {
		t.Errorf("nothing kept at %s", versionBinary(cache, "0.9.1"))
	}
	if got := versionOf(t, exe); got != "0.9.1" {
		t.Errorf("axx is now %q\nstdout: %s\nstderr: %s", got, stdout, stderr)
	}
	onlyUpgradeLeftovers(t, exe)
}
