//go:build darwin

package desktopmacos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
