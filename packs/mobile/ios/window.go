// SPDX-License-Identifier: Apache-2.0

package mobileios

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The apps that show simulators on the Mac: Device Hub from Xcode 27, the
// Simulator app before it.
var windowBundleIDs = []string{"com.apple.dt.Devices", "com.apple.iphonesimulator"}

// windowApp is an app that shows simulators, found in Xcode.
type windowApp struct {
	path, bundleID string
}

var windowApps = sync.OnceValue(func() []windowApp {
	ctx := context.Background()
	return findWindowApps(ctx, developerDir(ctx), bundleID)
})

// deviceWindowOpen reports whether the app that shows simulators on the Mac
// (Device Hub, or Simulator) is running.
func deviceWindowOpen(ctx context.Context) bool {
	for _, app := range windowApps() {
		// A process started from the app.
		if exec.CommandContext(ctx, "pgrep", "-f", app.path).Run() == nil {
			return true
		}
	}
	return false
}

// developerDir is Xcode's Developer folder: DEVELOPER_DIR, or the one
// xcode-select chose.
func developerDir(ctx context.Context) string {
	if d := os.Getenv("DEVELOPER_DIR"); d != "" {
		return d
	}
	out, err := exec.CommandContext(ctx, "xcode-select", "-p").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// findWindowApps returns the apps in Xcode (next to its Developer folder
// from Xcode 27, inside it before) whose bundle id is one of the window apps'.
func findWindowApps(ctx context.Context, devDir string, idOf func(ctx context.Context, app string) string) []windowApp {
	if devDir == "" {
		return nil
	}
	var out []windowApp
	for _, dir := range []string{filepath.Join(devDir, "..", "Applications"), filepath.Join(devDir, "Applications")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			app := filepath.Join(dir, e.Name())
			id := idOf(ctx, app)
			for _, want := range windowBundleIDs {
				if id == want {
					out = append(out, windowApp{filepath.Clean(app), id})
				}
			}
		}
	}
	return out
}

// bundleID reads an app's bundle id from its Info.plist.
func bundleID(ctx context.Context, app string) string {
	out, err := exec.CommandContext(ctx, "plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// showDevice opens the simulator in the app that shows simulators on the
// Mac (Device Hub from Xcode 27, Simulator before), and waits for the app to
// run.
func showDevice(ctx context.Context, udid string) error {
	apps := windowApps()
	if len(apps) == 0 {
		return errors.New("no app of Xcode's shows simulators (Device Hub, or Simulator)")
	}
	app := apps[0]
	cmd := exec.CommandContext(ctx, "open", "-a", app.path, "--args", "-CurrentDeviceUDID", udid)
	if app.bundleID == "com.apple.dt.Devices" {
		cmd = exec.CommandContext(ctx, "open", "devices://device/open?id="+udid)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot show the simulator in %s: %w: %s", filepath.Base(app.path), err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(15 * time.Second)
	for !deviceWindowOpen(ctx) {
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not open", filepath.Base(app.path))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return nil
}
