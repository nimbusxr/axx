package mobileandroid

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// sdk is the Android SDK's tools axx runs: adb and the emulator.
type sdk struct {
	adb, emulator string
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

// run runs adb for a device, and returns what it printed.
func (s *sdk) run(ctx context.Context, serial string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	out, err := exec.CommandContext(ctx, s.adb, args...).CombinedOutput() //nolint:gosec // adb, with arguments axx builds
	text := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if err != nil {
		return text, fmt.Errorf("adb %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// shell runs a command on a device.
func (s *sdk) shell(ctx context.Context, serial string, args ...string) (string, error) {
	return s.run(ctx, serial, append([]string{"shell"}, args...)...)
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

// waitBooted waits until a device has finished booting.
func (s *sdk) waitBooted(ctx context.Context, serial string, within time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	for {
		out, err := s.shell(ctx, serial, "getprop", "sys.boot_completed")
		if err == nil && out == "1" {
			if _, err := s.shell(ctx, serial, "pm", "path", "android"); err == nil {
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
