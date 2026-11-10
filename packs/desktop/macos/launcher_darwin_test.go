//go:build darwin

package desktopmacos

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/version"
)

// The version the installed axx gives the project's build (AXX_LAUNCHER, ADR
// 0015) says nothing of the macOS launcher: a run handed over finds none, and
// runs as before.
func TestTheInstalledAxxsVersionIsNotALauncher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(launcherEnv, "")
	t.Setenv("AXX_LAUNCHER", "0.6.3")
	app, err := launcherApp()
	if err != nil || app != "" {
		t.Fatalf("launcher %q, %v: want none", app, err)
	}
}

func TestAXXMacOSLauncherOffRunsWithout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(launcherEnv, "off")
	if app, err := launcherApp(); err != nil || app != "" {
		t.Fatalf("launcher %q, %v: want none", app, err)
	}
}

// An app that is not NimbusXR's launcher is refused, by its signature.
func TestALauncherNotSignedByNimbusXRIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := filepath.Join(t.TempDir(), "axx.app")
	if err := os.MkdirAll(filepath.Join(fake, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(launcherEnv, fake)
	if _, err := launcherApp(); err == nil || !strings.Contains(err.Error(), "not axx's launcher signed by NimbusXR") {
		t.Fatalf("got %v, want it refused", err)
	}
}

// Nothing in axx's cache: none, until a release's launcher is downloaded there.
func TestNoLauncherInTheCacheIsNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(launcherEnv, "")
	if app, err := launcherApp(); err != nil || app != "" {
		t.Fatalf("launcher %q, %v: want none", app, err)
	}
}

// releaseServer serves releases' files as GitHub does, through AXX_DOWNLOAD_URL:
// the launcher of the versions in apps, a zip of an axx.app; 404 for others.
func releaseServer(t *testing.T, apps map[string][]byte) *int {
	t.Helper()
	asked := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*asked++
		for v, zip := range apps {
			if r.URL.Path == "/v"+v+"/axx-launcher_"+v+"_darwin.zip" {
				_, _ = w.Write(zip)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AXX_DOWNLOAD_URL", srv.URL)
	return asked
}

// unsignedApp is a zip of an axx.app no one signed.
func unsignedApp(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"axx.app/Contents/Info.plist":         "<plist version=\"1.0\"><dict/></plist>",
		"axx.app/Contents/MacOS/axx-launcher": "#!/bin/sh\n",
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A release from before the launcher has none: axx notes it, and asks no more.
func TestAReleaseWithoutALauncherIsAskedOnce(t *testing.T) {
	asked := releaseServer(t, nil)
	app := filepath.Join(t.TempDir(), "launcher", "axx.app")
	for range 2 {
		if err := downloadLauncher(app, version.Info{Version: "0.6.3", Channel: "beta"}); err != nil {
			t.Fatal(err)
		}
	}
	if exists(app) || *asked != 1 {
		t.Errorf("installed %v, asked %d times: want none, asked once", exists(app), *asked)
	}
}

// What a release serves is installed only when NimbusXR signed it.
func TestADownloadedLauncherNotSignedByNimbusXRIsNotInstalled(t *testing.T) {
	releaseServer(t, map[string][]byte{"0.7.0": unsignedApp(t)})
	app := filepath.Join(t.TempDir(), "launcher", "axx.app")
	err := downloadLauncher(app, version.Info{Version: "0.7.0", Channel: "beta"})
	if err == nil || !strings.Contains(err.Error(), "not axx's launcher signed by NimbusXR") || exists(app) {
		t.Fatalf("got %v, installed %v: want it refused", err, exists(app))
	}
}

// An axx built from source is no release: it downloads nothing.
func TestAnAxxBuiltFromSourceDownloadsNoLauncher(t *testing.T) {
	asked := releaseServer(t, nil)
	app := filepath.Join(t.TempDir(), "launcher", "axx.app")
	if err := downloadLauncher(app, version.Info{Version: "0.0.0-dev", Channel: "dev"}); err != nil || *asked != 0 {
		t.Fatalf("%v, asked %d times: want nothing asked", err, *asked)
	}
}

// A download that fails is tried again an hour later, not on every command.
func TestADownloadThatFailsWaitsAnHour(t *testing.T) {
	t.Setenv("AXX_DOWNLOAD_URL", "http://127.0.0.1:1")
	app := filepath.Join(t.TempDir(), "launcher", "axx.app")
	info := version.Info{Version: "0.7.0", Channel: "beta"}
	if err := downloadLauncher(app, info); err == nil {
		t.Fatal("a download from nowhere succeeded")
	}
	if err := downloadLauncher(app, info); err != nil {
		t.Fatalf("tried again at once: %v", err)
	}
}

// A release's launcher, signed by NimbusXR, is installed where axx runs it
// from. AXX_TEST_LAUNCHER_ZIP names a zip of one (scripts/macos-launcher-app.sh's
// axx.app, as a release publishes it).
func TestADownloadedLauncherSignedByNimbusXRIsInstalled(t *testing.T) {
	path := os.Getenv("AXX_TEST_LAUNCHER_ZIP")
	if path == "" {
		t.Skip("AXX_TEST_LAUNCHER_ZIP names a zip of a signed axx.app")
	}
	signed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	releaseServer(t, map[string][]byte{"0.7.0": signed})
	app := filepath.Join(t.TempDir(), "launcher", "axx.app")
	if err := downloadLauncher(app, version.Info{Version: "0.7.0", Channel: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyLauncher(app); err != nil {
		t.Fatalf("installed, but %v", err)
	}
}
