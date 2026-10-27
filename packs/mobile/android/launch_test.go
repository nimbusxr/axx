package mobileandroid

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

func TestDeepLinkDoesNotRequireDebugAccessWithoutWork(t *testing.T) {
	d, log := launchTestDevice(t, false)
	OnLaunch(d.scenario, func(*Launch) error { return nil })
	if err := d.OpenLink(t.Context(), "parcels-courier://deliveries/PX-1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "run-as") {
		t.Fatalf("a no-op launch hook required debug access:\n%s", b)
	}
}

func TestDeepLinkPreparesWhenPidofFails(t *testing.T) {
	d, log := launchTestDevice(t, true)
	after := 0
	OnLaunch(d.scenario, func(l *Launch) error {
		l.Agent = "marker"
		l.After = append(l.After, func() error { after++; return nil })
		return nil
	})
	if err := d.OpenLink(t.Context(), "parcels-courier://deliveries/PX-1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "run-as") || !strings.Contains(string(b), "--attach-agent|marker") {
		t.Fatalf("deep link skipped cold-start preparation:\n%s", b)
	}
	// am ignores options after the intent's last argument, the app's package.
	if !strings.Contains(string(b), "|am|start|--attach-agent|marker|-W|") {
		t.Fatalf("the agent is not among am's options, before the intent:\n%s", b)
	}
	if after != 1 {
		t.Fatalf("%d readiness checks ran, want 1", after)
	}
}

func launchTestDevice(t *testing.T, pidofFails bool) (*running, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	adb := filepath.Join(dir, "adb")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s|' \"$a\"; done >> " + log + "\necho >> " + log + "\n"
	if pidofFails {
		script += "case \"$*\" in *pidof*) printf 'pidof unavailable\\n'; exit 1;; esac\n"
	}
	script += "case \"$*\" in *getprop*) printf 'arm64\\n';; *run-as*) printf '/data/user/0/pkg\\n';; esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{}, nil, nil)
	d := &running{app: &app{name: "courier"}, pkg: "pkg", sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}, scenario: sc}
	return d, log
}
