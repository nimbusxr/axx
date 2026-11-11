//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// permissions are what an app under the launcher may ask macOS for: each
// entitlement the hardened runtime needs for some of them, with the
// Info.plist messages macOS shows for it, and ("") the messages of those that
// need no entitlement.
var permissions = map[string][]string{
	"com.apple.security.automation.apple-events":          {"NSAppleEventsUsageDescription"},
	"com.apple.security.device.camera":                    {"NSCameraUsageDescription"},
	"com.apple.security.device.audio-input":               {"NSMicrophoneUsageDescription"},
	"com.apple.security.personal-information.addressbook": {"NSContactsUsageDescription"},
	"com.apple.security.personal-information.calendars": {
		"NSCalendarsUsageDescription", "NSCalendarsFullAccessUsageDescription",
		"NSCalendarsWriteOnlyAccessUsageDescription", "NSRemindersUsageDescription",
		"NSRemindersFullAccessUsageDescription",
	},
	"com.apple.security.personal-information.location": {"NSLocationUsageDescription", "NSLocationWhenInUseUsageDescription"},
	"com.apple.security.personal-information.photos-library": {
		"NSPhotoLibraryUsageDescription", "NSPhotoLibraryAddUsageDescription",
	},
	"": {
		"NSSpeechRecognitionUsageDescription", "NSBluetoothAlwaysUsageDescription",
		"NSLocalNetworkUsageDescription", "NSDesktopFolderUsageDescription",
		"NSDocumentsFolderUsageDescription", "NSDownloadsFolderUsageDescription",
		"NSNetworkVolumesUsageDescription", "NSRemovableVolumesUsageDescription",
	},
}

// The apps axx tests run under the launcher, so macOS asks for their
// permissions on its behalf. It refuses one, without asking anyone, that the
// launcher's signature has no entitlement for or its Info.plist no message
// for: axx 0.6.4's launcher had neither, and macOS refused an app under it the
// camera and Apple Events (ADR 0016). The app is built as a release builds
// it, by scripts/macos-launcher-app.sh, signed ad hoc.
func TestTheLauncherMayAskForWhatAppsUnderItAskFor(t *testing.T) {
	dir := t.TempDir()
	build := exec.Command("bash", filepath.Join("..", "..", "..", "scripts", "macos-launcher-app.sh"), dir, "0.0.0-test")
	build.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "APPLE_API_KEY_PATH=") || strings.HasPrefix(kv, "AXX_SIGN_IDENTITY=")
	}), "AXX_SIGN_IDENTITY=-")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("scripts/macos-launcher-app.sh: %v\n%s", err, out)
	}
	app := filepath.Join(dir, "axx.app")

	signature, err := exec.Command("codesign", "-dv", app).CombinedOutput()
	if err != nil || !regexp.MustCompile(`flags=0x[0-9a-f]+\([^)]*\bruntime\b`).Match(signature) {
		t.Errorf("not signed under the hardened runtime (%v):\n%s", err, signature)
	}

	xml, err := exec.Command("codesign", "-d", "--entitlements", "-", "--xml", app).Output()
	if err != nil {
		t.Fatalf("its entitlements: %v", err)
	}
	var entitlements map[string]any
	if len(xml) > 0 {
		entitlements = plist(t, xml)
	}
	info := plist(t, read(t, filepath.Join(app, "Contents", "Info.plist")))

	for entitlement, messages := range permissions {
		if entitlement != "" && entitlements[entitlement] != true {
			t.Errorf("signed without %s", entitlement)
		}
		for _, key := range messages {
			if s, _ := info[key].(string); !strings.Contains(s, "axx") {
				t.Errorf("Info.plist's %s is %q: macOS shows it when an app under axx asks", key, s)
			}
		}
	}
	// And nothing else: the table above is the list of them.
	for entitlement := range entitlements {
		if _, ok := permissions[entitlement]; !ok {
			t.Errorf("signed with %s, which the hardened runtime's resources do not include: add it here with its messages, or take it out", entitlement)
		}
	}
	listed := map[string]bool{}
	for _, messages := range permissions {
		for _, key := range messages {
			listed[key] = true
		}
	}
	for key := range info {
		if strings.HasSuffix(key, "UsageDescription") && !listed[key] {
			t.Errorf("Info.plist has %s, which is not listed here", key)
		}
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// plist reads a property list, as plutil does.
func plist(t *testing.T, b []byte) map[string]any {
	t.Helper()
	cmd := exec.Command("plutil", "-convert", "json", "-o", "-", "-")
	cmd.Stdin = strings.NewReader(string(b))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("plutil: %v\n%s", err, b)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
