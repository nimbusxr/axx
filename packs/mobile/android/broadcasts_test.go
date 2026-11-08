package mobileandroid

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// Before the app starts, the scenario waits for Android to deliver the broadcasts its reset sent:
// a force-stop's that reached the job scheduler late cancelled the job WorkManager had just handed
// it. A wait that takes a while is said; an Android without the command goes on at once.
func TestTheResetsBroadcastsAreDeliveredBeforeTheAppStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	for _, tc := range []struct {
		name, answer string
		said         bool
	}{
		{"Android 14 and later", `sleep 2.2; echo "Test barrier passed"`, true},
		{"an older Android", `echo "Error: Unknown command: wait-for-broadcast-barrier" >&2; exit 255`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			adb := filepath.Join(dir, "adb")
			calls := filepath.Join(dir, "calls")
			script := "#!/bin/sh\necho \"$*\" >> " + calls + "\ncase \"$*\" in\n*wait-for-broadcast-barrier*) " + tc.answer + " ;;\nesac\n"
			if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			h := cloudtest.New(t)
			d := &running{app: &app{name: "courier"}, sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}, scenario: h.SC}
			d.awaitBroadcasts(t.Context())
			b, _ := os.ReadFile(calls)
			if !strings.Contains(string(b), "-s emulator-5554 shell am wait-for-broadcast-barrier") {
				t.Errorf("adb calls: %s", b)
			}
			said := strings.Contains(strings.Join(h.Sink.Logs, "\n"), "Android delivered the broadcasts of the courier app's reset in")
			if said != tc.said {
				t.Errorf("logs: %q", h.Sink.Logs)
			}
		})
	}
}
