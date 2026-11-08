package mobileandroid

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/proc"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// A free emulator that went down is not leased to the next scenario: it is stopped, and another
// started in its place. CI's emulator shut itself down now and then; adb said it was offline
// while it stopped, and the scenarios waiting for it each got it and failed at once.
func TestAnEmulatorThatWentDownIsReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	defer func(was time.Duration) { offlineGrace = was }(offlineGrace)
	offlineGrace = 300 * time.Millisecond
	dir := t.TempDir()
	// adb: emulator-5554 is offline, as an emulator shutting itself down is; emu kill stops an
	// emulator.
	adb := filepath.Join(dir, "adb")
	script := `#!/bin/sh
case "$3" in
get-state) if [ "$2" = emulator-5554 ]; then echo "error: device offline" >&2; exit 1; fi; echo device ;;
emu) kill "$(cat "` + dir + `/$2.pid")" ;;
esac
`
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	for _, tc := range []struct {
		name string
		down *device
	}{
		{"its emulator stopped", emulatorProcess(t, dir, "emulator-5556", false)},
		{"adb has it offline", emulatorProcess(t, dir, "emulator-5554", true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := &device{serial: "emulator-5558"}
			p := &pool{suite: h.SC.Suite(), sdk: &sdk{adb: adb}, name: "parcels-pixel", avd: true, max: 1, changed: make(chan struct{})}
			p.all, p.free = []*device{tc.down}, []*device{tc.down}
			p.newDevice = func(context.Context, string) (*device, error) { return fresh, nil }
			got, err := p.lease(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != fresh || !slices.Equal(p.all, []*device{fresh}) {
				t.Fatalf("leased %s; the pool has %v", got.serial, p.all)
			}
			select {
			case <-tc.down.exited:
			case <-time.After(5 * time.Second):
				t.Error("the emulator that went down still runs")
			}
		})
	}

	// A device adb lists, which axx did not start, is leased as it is: it is not axx's to replace.
	listed := &device{serial: "R5CT1234"}
	p := &pool{suite: h.SC.Suite(), sdk: &sdk{adb: adb}, name: "R5CT1234", max: 1, changed: make(chan struct{})}
	p.all, p.free = []*device{listed}, []*device{listed}
	if got, err := p.lease(t.Context(), dir); err != nil || got != listed {
		t.Fatalf("leased %v, %v", got, err)
	}
}

// emulatorProcess is a device whose emulator is a process standing in for one: running, or
// already stopped.
func emulatorProcess(t *testing.T, dir, serial string, running bool) *device {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	proc.Setup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	d := &device{serial: serial, avd: "parcels-pixel", emulator: cmd, exited: make(chan struct{})}
	d.group, _ = proc.NewGroup(cmd)
	go func() {
		_ = cmd.Wait()
		close(d.exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-d.exited
	})
	if err := os.WriteFile(filepath.Join(dir, serial+".pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	if !running {
		_ = cmd.Process.Kill()
		<-d.exited
	}
	return d
}
