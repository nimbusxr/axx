package modelsmacos

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/format/platform/platformtest"
)

// Every API the pack answers takes the contract `axx models contract` writes
// for it, and a stub of it builds.
func TestEveryAPIsContractIsSupported(t *testing.T) {
	for _, api := range macAPIs {
		t.Run(api.Name, func(t *testing.T) {
			if !strings.HasPrefix(api.Name, "macos.") {
				t.Fatalf("%s is not a macos API", api.Name)
			}
			var labels []string
			if api.Task == "classification" {
				labels = []string{"Cardboard", "Dog"}
			}
			if err := mac.Supported(platformtest.Artifact(t, api, labels)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// macOS lets axx's library into an app signed without the hardened runtime, or with its two
// exceptions; an app it hardens without them is refused, naming what it lacks.
func TestLoadable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS signs apps")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "app.c")
	if err := os.WriteFile(src, []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents := filepath.Join(dir, "debug.entitlements")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>com.apple.security.cs.allow-dyld-environment-variables</key><true/>
<key>com.apple.security.cs.disable-library-validation</key><true/>
</dict></plist>
`
	if err := os.WriteFile(ents, []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	build := func(name string, sign ...string) string {
		t.Helper()
		exe := filepath.Join(dir, name)
		if out, err := exec.Command("xcrun", "clang", src, "-o", exe).CombinedOutput(); err != nil {
			t.Fatalf("compile: %v: %s", err, out)
		}
		if out, err := exec.Command("codesign", append(append([]string{"--sign", "-", "--force"}, sign...), exe)...).CombinedOutput(); err != nil {
			t.Fatalf("sign: %v: %s", err, out)
		}
		return exe
	}
	plain := build("plain")
	hardened := build("hardened", "--options", "runtime")
	allowed := build("allowed", "--options", "runtime", "--entitlements", ents)
	if err := loadable(t.Context(), "depot", plain); err != nil {
		t.Errorf("an app without the hardened runtime: %v", err)
	}
	if err := loadable(t.Context(), "depot", allowed); err != nil {
		t.Errorf("an app with the exceptions: %v", err)
	}
	err := loadable(t.Context(), "depot", hardened)
	if err == nil || !strings.Contains(err.Error(), "hardened runtime") || !strings.Contains(err.Error(), exceptions[0]) || !strings.Contains(err.Error(), exceptions[1]) {
		t.Errorf("a hardened app: %v", err)
	}
	host := runtime.GOARCH
	if host == "amd64" {
		host = "x86_64"
	}
	if arch, err := processArch(t.Context(), plain); err != nil || arch != host {
		t.Errorf("arch %q, %v; want %s", arch, err, host)
	}
}
