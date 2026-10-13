package mobileandroid

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sdk is the Android SDK's tools axx runs: adb and the emulator.
type sdk struct {
	adb, emulator string

	versionOnce   sync.Once
	noIncremental bool // whether this adb knows --no-incremental: 30.0.1 and later
}

// findSDK finds the SDK: ANDROID_HOME, ANDROID_SDK_ROOT, or the tools on
// the PATH.
func findSDK() (*sdk, error) {
	exe := func(name string) string {
		if runtime.GOOS == "windows" {
			return name + ".exe"
		}
		return name
	}
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := os.Getenv(env); root != "" {
			s := &sdk{adb: filepath.Join(root, "platform-tools", exe("adb")), emulator: filepath.Join(root, "emulator", exe("emulator"))}
			if _, err := os.Stat(s.adb); err == nil {
				return s, nil
			}
		}
	}
	adb, err := exec.LookPath("adb")
	if err != nil {
		return nil, errors.New("the Android SDK is not found: install it (Android Studio, or its command-line tools) and set ANDROID_HOME to it")
	}
	s := &sdk{adb: adb}
	if em, err := exec.LookPath("emulator"); err == nil {
		s.emulator = em
	} else {
		s.emulator = filepath.Join(filepath.Dir(filepath.Dir(adb)), "emulator", exe("emulator"))
	}
	return s, nil
}

// command is adb with args, run so that an adb server it starts has mDNS discovery off. axx
// needs none: it reaches emulators and USB devices without it. And on some networks adb's mDNS
// bridge aborts the server as it reads a name something else advertises (adb 37 on the Mac mini:
// the server died every few seconds, every install failed, and the device went offline). An
// ADB_MDNS the user sets wins. A server already running is left as it is.
func (s *sdk) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, s.adb, args...) //nolint:gosec // adb, with arguments axx builds
	if _, set := os.LookupEnv("ADB_MDNS"); !set {
		cmd.Env = append(os.Environ(), "ADB_MDNS=0")
	}
	return cmd
}

// run runs adb for a device, and returns what it printed.
func (s *sdk) run(ctx context.Context, serial string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	out, err := s.command(ctx, args...).CombinedOutput()
	text := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if err != nil {
		return text, fmt.Errorf("adb %s: %w: %s%s", strings.Join(args, " "), err, text, serverRestarted(text))
	}
	return text, nil
}

// serverRestarted says why, when adb had to start its server again for a command that failed:
// the server stopped while axx used it.
func serverRestarted(output string) string {
	if !strings.Contains(output, "daemon not running") {
		return ""
	}
	return " (adb's server had stopped and was started again. On some networks its mDNS discovery" +
		" aborts it: ADB_MDNS=0 turns that off, which axx sets for an adb server it starts unless" +
		" ADB_MDNS is set; a server something else started keeps its own setting)"
}

// install installs an APK on a device, replacing what it replaces, never incrementally: an
// incremental install can fail with no reason given (DTCD's emulator on the Mac mini did), and
// Appium's adb installs so too. An adb older than the option, or a device without it, installs
// as it always did.
func (s *sdk) install(ctx context.Context, serial, apk string, args ...string) (string, error) {
	s.versionOnce.Do(func() {
		out, _ := s.command(ctx, "version").CombinedOutput()
		s.noIncremental = knowsNoIncremental(string(out))
	})
	cmd := append([]string{"install", "-r"}, args...)
	if s.noIncremental {
		if features, err := s.run(ctx, serial, "features"); err == nil && strings.Contains(features, "abb_exec") {
			cmd = append(cmd, "--no-incremental")
		}
	}
	return s.run(ctx, serial, append(cmd, apk)...)
}

// adbVersion reads the platform tools' version adb version prints, like "Version 37.0.1-15733141".
var adbVersion = regexp.MustCompile(`(?m)^Version (\d+)\.(\d+)\.(\d+)`)

// knowsNoIncremental reports whether an adb, by what adb version prints, takes --no-incremental:
// from 30.0.1, though its help does not say so.
func knowsNoIncremental(version string) bool {
	m := adbVersion.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v[0] > 30 || (v[0] == 30 && (v[1] > 0 || v[2] >= 1))
}

// shell runs a command on a device.
func (s *sdk) shell(ctx context.Context, serial string, args ...string) (string, error) {
	return s.run(ctx, serial, append([]string{"shell"}, args...)...)
}

// waitOnline waits, up to d, for adb to have the device again: it can drop one for a moment.
func (s *sdk) waitOnline(ctx context.Context, serial string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		out, err := s.run(ctx, serial, "get-state")
		if err == nil && strings.TrimSpace(out) == "device" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("adb has not had %s for %s: %s", serial, d, strings.TrimSpace(out))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// devices is the serials adb lists as ready.
func (s *sdk) devices(ctx context.Context) ([]string, error) {
	out, err := s.run(ctx, "", "devices")
	if err != nil {
		return nil, err
	}
	var serials []string
	for _, line := range strings.Split(out, "\n")[1:] {
		if f := strings.Fields(line); len(f) == 2 && f[1] == "device" {
			serials = append(serials, f[0])
		}
	}
	return serials, nil
}

// avds is the device definitions (AVDs) the emulator knows.
func (s *sdk) avds(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, s.emulator, "-list-avds").Output() //nolint:gosec // the SDK's emulator
	if err != nil {
		return nil, fmt.Errorf("the Android emulator (%s) cannot list its devices: %w", s.emulator, err)
	}
	var names []string
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "INFO") {
			names = append(names, l)
		}
	}
	return names, nil
}

// online reports whether the device has its network: it comes up seconds
// after the device has booted, and an app that calls its service before
// finds none. An Android that does not say is taken as online.
func (s *sdk) online(ctx context.Context, serial string) bool {
	out, err := s.shell(ctx, serial, "dumpsys", "connectivity")
	if err != nil {
		return false
	}
	_, rest, ok := strings.Cut(out, "Active default network:")
	if !ok {
		return true
	}
	rest = strings.TrimSpace(rest)
	return rest != "" && rest[0] >= '0' && rest[0] <= '9'
}

// waitBooted waits until a device has finished booting, and has its network.
func (s *sdk) waitBooted(ctx context.Context, serial string, within time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	for {
		out, err := s.shell(ctx, serial, "getprop", "sys.boot_completed")
		if err == nil && out == "1" {
			if _, err := s.shell(ctx, serial, "pm", "path", "android"); err == nil && s.online(ctx, serial) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the %s device did not boot within %s", serial, within)
		case <-time.After(time.Second):
		}
	}
}
