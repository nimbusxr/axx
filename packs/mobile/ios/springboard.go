package mobileios

import (
	"context"
	"crypto/sha256"
	_ "embed" // the SpringBoard library's source
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
)

// springboardSource is a library axx loads into the simulators it boots. In SpringBoard, it
// gives Notification Center the proximity sensor an iPhone has: without one, iOS takes a
// hold on a notification there for a tap, and never shows its actions. It does nothing in
// any other process.
//
//go:embed springboard/springboard.m
var springboardSource []byte

// springboardLibrary is the library, built once for the Mac's simulators with Xcode's
// compiler into axx's cache.
func springboardLibrary(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/springboard", func() (string, error) {
		arch := simulatorArch(ctx)
		sum := sha256.Sum256(springboardSource)
		cache := npm.CacheDir("mobile")
		if err := os.MkdirAll(springboardStatuses(), 0o755); err != nil {
			return "", err
		}
		dir := filepath.Join(cache, "springboard-"+hex.EncodeToString(sum[:6])+"-"+arch)
		lib := filepath.Join(dir, "axx-springboard.dylib")
		if _, err := os.Stat(lib); err == nil {
			return lib, nil
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		src := filepath.Join(tmp, "springboard.m")
		if err := os.WriteFile(src, springboardSource, 0o644); err != nil {
			return "", err
		}
		built := filepath.Join(tmp, "axx-springboard.dylib")
		for _, args := range [][]string{
			{"-sdk", "iphonesimulator", "clang", "-arch", arch, "-mios-simulator-version-min=15.0", "-dynamiclib", "-fobjc-arc", "-framework", "Foundation", "-o", built, src},
			{"codesign", "--sign", "-", "--force", built},
		} {
			if out, err := exec.CommandContext(ctx, "xcrun", args...).CombinedOutput(); err != nil { //nolint:gosec // Xcode's tools, with arguments axx builds
				return "", fmt.Errorf("cannot build axx's library for the simulators' SpringBoard (xcrun %s): %w: %s", args[0], err, strings.TrimSpace(string(out)))
			}
		}
		// Another axx may have built it meanwhile: keep the first.
		if err := os.Rename(tmp, dir); err != nil {
			if _, statErr := os.Stat(lib); statErr == nil {
				return lib, nil
			}
			return "", err
		}
		return lib, nil
	})
}

// springboardStatuses is where the library tells, a file for each simulator, how it went in
// SpringBoard.
func springboardStatuses() string { return filepath.Join(npm.CacheDir("mobile"), "springboard") }

// springboardEnv is what a simulator's processes are started with to load the library: simctl
// passes each SIMCTL_CHILD_ variable on to them.
func springboardEnv(lib string) []string {
	return []string{
		"SIMCTL_CHILD_DYLD_INSERT_LIBRARIES=" + lib,
		"SIMCTL_CHILD_AXX_SPRINGBOARD_STATUS=" + springboardStatuses(),
	}
}

// springboardStatus is what the library reported in the simulator's SpringBoard as it runs
// now: "" when it is not loaded there (the simulator was booted without it).
func springboardStatus(udid string) string {
	data, err := os.ReadFile(filepath.Join(springboardStatuses(), udid))
	if err != nil {
		return ""
	}
	pid, status, _ := strings.Cut(strings.TrimSpace(string(data)), " ")
	n, err := strconv.Atoi(pid)
	if err != nil {
		return ""
	}
	if p, err := os.FindProcess(n); err != nil || p.Signal(syscall.Signal(0)) != nil {
		return "" // a SpringBoard of an earlier boot
	}
	return status
}

// loadSpringboard makes sure the library runs in a simulator's SpringBoard: one axx boots
// starts with it, and one booted before (kept from an earlier run, or the project's own)
// gets it with SpringBoard started again, before WebDriverAgent is launched. A library that
// cannot help this iOS only costs the actions of notifications in Notification Center: the
// run goes on.
func (p *pool) loadSpringboard(ctx context.Context, d *device, lib string) {
	started := time.Now()
	if springboardStatus(d.udid) == "" {
		err := errors.Join(
			setenv(ctx, d, "DYLD_INSERT_LIBRARIES", lib),
			setenv(ctx, d, "AXX_SPRINGBOARD_STATUS", springboardStatuses()),
		)
		since := time.Now().Add(-time.Second)
		if err == nil {
			_, err = d.set.simctl(ctx, "spawn", d.udid, "launchctl", "kickstart", "-k", "user/foreground/com.apple.SpringBoard")
		}
		for deadline := time.Now().Add(30 * time.Second); err == nil && springboardStatus(d.udid) == "" && time.Now().Before(deadline); {
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
		if err != nil {
			p.suite.Logger().Warn("cannot load axx's library into the simulator's SpringBoard: Notification Center will not show notifications' actions", "simulator", d.name, "error", err)
			return
		}
		// SpringBoard, started again, serves apps' requests for notifications a while later, as
		// after a boot.
		if !d.set.notificationsServed(ctx, d.udid, since, min(p.boot, notificationsWait)) {
			p.suite.Logger().Warn("the iOS simulator does not serve notifications yet; an app that asks for them may get no dialog", "simulator", d.name, "waited", min(p.boot, notificationsWait))
		}
		p.suite.Logger().Info("started the simulator's SpringBoard again, with axx's library", "simulator", d.name, "took", time.Since(started).Round(time.Second))
	}
	switch status := springboardStatus(d.udid); status {
	case "ok":
	case "":
		p.suite.Logger().Warn("the simulator's SpringBoard runs without axx's library: Notification Center will not show notifications' actions", "simulator", d.name)
	default:
		p.suite.Logger().Warn("Notification Center will not show notifications' actions on this simulator", "simulator", d.name, "springboard", status)
	}
}

// setenv sets a variable for the processes the simulator starts from now on.
func setenv(ctx context.Context, d *device, name, value string) error {
	_, err := d.set.simctl(ctx, "spawn", d.udid, "launchctl", "setenv", name, value)
	return err
}
