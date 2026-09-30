package mobileandroid

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// checks are what `axx doctor` checks for the pack: the Android SDK, the
// emulator's devices, and KVM, which Linux emulators need.
func checks() []core.Check {
	out := []core.Check{{Name: "Android SDK", Run: checkSDK}}
	if runtime.GOOS == "linux" {
		out = append(out, core.Check{Name: "KVM", Run: checkKVM})
	}
	return out
}

func checkSDK(ctx context.Context) core.CheckResult {
	s, err := findSDK()
	if err != nil {
		return core.CheckResult{Status: core.CheckFail, Detail: err.Error(), Hint: "Android Studio installs the SDK, or its command-line tools: set ANDROID_HOME to it"}
	}
	if _, err := os.Stat(s.emulator); err != nil {
		return core.CheckResult{Status: core.CheckWarn, Detail: "adb " + s.adb + "; the SDK has no emulator", Hint: "sdkmanager --install emulator: axx starts emulators with it (devices adb lists need none)"}
	}
	avds, err := s.avds(ctx)
	if err != nil {
		return core.CheckResult{Status: core.CheckWarn, Detail: "the emulator cannot list its devices (AVDs): " + err.Error()}
	}
	if len(avds) == 0 {
		return core.CheckResult{
			Status: core.CheckWarn, Detail: "no emulator devices (AVDs) in " + avdHome(),
			Hint: "make one with Android Studio's Device Manager, or with avdmanager (see Test mobile apps)",
		}
	}
	return core.CheckResult{Status: core.CheckOK, Detail: "adb and the emulator; devices (AVDs): " + strings.Join(avds, ", ")}
}

func checkKVM(context.Context) core.CheckResult {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return core.CheckResult{
			Status: core.CheckWarn, Detail: "/dev/kvm cannot be opened: " + err.Error(),
			Hint: "emulators need KVM on Linux: give your user access to /dev/kvm (the kvm group, or a udev rule)",
		}
	}
	_ = f.Close()
	return core.CheckResult{Status: core.CheckOK, Detail: "/dev/kvm"}
}
