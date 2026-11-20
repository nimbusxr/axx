package mobileios

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// What the library reported counts only while the SpringBoard it reported from runs: a
// simulator booted again (or shut down) has none of it.
func TestTheSpringBoardLibraryReportsForTheSpringBoardThatRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("iOS simulators run on macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(springboardStatuses(), 0o755); err != nil {
		t.Fatal(err)
	}
	report := func(udid string, pid int, status string) {
		t.Helper()
		line := strconv.Itoa(pid) + " " + status + "\n"
		if err := os.WriteFile(filepath.Join(springboardStatuses(), udid), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	report("RUNNING", os.Getpid(), "ok\n/cache/springboard-1/axx-springboard.dylib")
	report("EARLIER", os.Getpid(), "ok")
	report("FAILED", os.Getpid(), "this iOS's Cover Sheet has no -[CSCombinedListViewController _hasProximitySensor] to give a proximity sensor")
	report("REBOOTED", gone.Process.Pid, "ok")
	for udid, want := range map[string]string{
		"RUNNING":  "ok",
		"EARLIER":  "ok",
		"FAILED":   "this iOS's Cover Sheet has no -[CSCombinedListViewController _hasProximitySensor] to give a proximity sensor",
		"REBOOTED": "",
		"NEVER":    "",
	} {
		if got := springboardStatus(udid); got != want {
			t.Errorf("%s: %q, want %q", udid, got, want)
		}
	}
	// The library names itself, but for an earlier axx's.
	if _, lib := springboardReport("RUNNING"); lib != "/cache/springboard-1/axx-springboard.dylib" {
		t.Errorf("the library running is %q", lib)
	}
	if _, lib := springboardReport("EARLIER"); lib != "" {
		t.Errorf("an earlier axx's library is %q", lib)
	}
	if !springboardRuns("RUNNING", "/cache/springboard-1/axx-springboard.dylib") || springboardRuns("EARLIER", "/cache/springboard-1/axx-springboard.dylib") {
		t.Error("springboardRuns does not tell this library from an earlier one")
	}
	// A boot-time library must be migrated even if it is the current build:
	// otherwise it still overrides the app's initial libraries. A launchd
	// default is safe to keep, and a simulator with no library needs no reboot.
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "xcrun"), []byte("#!/bin/sh\nprintf '%s\\n' \"$AXX_TEST_LAUNCHD_LIBRARY\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		udid, defaultLibrary string
		want                 bool
	}{
		{"RUNNING", "", true},
		{"EARLIER", "", true},
		{"RUNNING", "/cache/springboard-1/axx-springboard.dylib", false},
		{"NEVER", "", false},
		{"REBOOTED", "", false},
	} {
		t.Setenv("AXX_TEST_LAUNCHD_LIBRARY", tc.defaultLibrary)
		if got := springboardBootedWithLibrary(t.Context(), &device{udid: tc.udid}); got != tc.want {
			t.Errorf("boot-time library for %s (launchd %q): %v, want %v", tc.udid, tc.defaultLibrary, got, tc.want)
		}
	}
}

// A SpringBoard started again is taken to serve notifications by the log's entry of its
// notification server, read from a second before it was started: the SpringBoard it replaced
// may have logged its own in that second. The pack's log says when the only entry is the
// replaced one's, and nothing more when the one started again has logged its own. Here xcrun is a
// script: starting SpringBoard again has the library report from a process that runs (this
// test's), and the simulator's log is what the test gives.
func TestASpringBoardStartedAgainIsToldFromTheOneItReplacedInTheLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("iOS simulators run on macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(springboardStatuses(), 0o755); err != nil {
		t.Fatal(err)
	}
	const udid, lib = "9F3B2D3E-0000-0000-0000-000000000000", "/cache/library-1/axx-simulator.dylib"
	status := filepath.Join(springboardStatuses(), udid)
	tools := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*kickstart*) printf '%s ok\\n%s\\n' \"$AXX_TEST_SPRINGBOARD\" \"$AXX_TEST_LIBRARY\" > \"$AXX_TEST_STATUS\" ;;\n" +
		"*\"log show\"*) printf '%s\\n' \"$AXX_TEST_SIMULATOR_LOG\" ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(tools, "xcrun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AXX_TEST_SPRINGBOARD", strconv.Itoa(os.Getpid()))
	t.Setenv("AXX_TEST_LIBRARY", lib)
	t.Setenv("AXX_TEST_STATUS", status)
	entry := func(pid int) string {
		return "2026-11-20 00:11:29.412 Df SpringBoard[" + strconv.Itoa(pid) + ":41fb] [com.apple.UserNotifications:ContentProtection] UNSUserNotificationServer: adding content protection observer"
	}
	replaced := os.Getpid() + 1
	for _, tc := range []struct {
		name, log string
		want      string
	}{
		{
			"the replaced SpringBoard's entry alone", entry(replaced),
			"the log has them served by the SpringBoard that was replaced (pid " + strconv.Itoa(replaced) + "), not yet by the one started again (pid " + strconv.Itoa(os.Getpid()) + ")",
		},
		{"the entry of the one started again", entry(os.Getpid()), ""},
		{"both entries", entry(replaced) + "\n" + entry(os.Getpid()), ""},
		{"an entry that does not name its process", "[com.apple.UserNotifications:ContentProtection] <compose failure [UUID]>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Remove(status); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			t.Setenv("AXX_TEST_SIMULATOR_LOG", tc.log)
			var log bytes.Buffer
			p := &pool{suite: core.NewSuite(core.SuiteOptions{Logger: slog.New(slog.NewTextHandler(&log, nil))}), boot: 10 * time.Second}
			p.loadSpringboard(t.Context(), &device{udid: udid, name: "axx iPhone 16, iOS 26.5 (1)"}, lib)
			said := log.String()
			if !strings.Contains(said, "started the simulator's SpringBoard again, with axx's library") || strings.Contains(said, "level=WARN") {
				t.Fatalf("the pack's log is\n%s", said)
			}
			if tc.want == "" && strings.Contains(said, "notifications=") {
				t.Errorf("the pack's log tells of a replaced SpringBoard:\n%s", said)
			}
			if tc.want != "" && !strings.Contains(said, `notifications="`+tc.want+`"`) {
				t.Errorf("the pack's log is\n%s\nwithout notifications=%q", said, tc.want)
			}
		})
	}
}

// The log's entries of a notification server starting are found with the SpringBoards that
// logged them, each once.
func TestTheNotificationServersOfALogAreFoundWithTheirSpringBoards(t *testing.T) {
	log := "Timestamp               Ty Process[PID:TID]\n" +
		"2026-11-20 00:11:27.900 Df SpringBoard[3576:41fb] [com.apple.UserNotifications:ContentProtection] UNSUserNotificationServer: adding content protection observer\n" +
		"2026-11-20 00:11:27.901 Df SpringBoard[3576:41fb] [com.apple.UserNotifications:ContentProtection] another line\n" +
		"2026-11-20 00:11:33.100 Df SpringBoard[4711:5a5a] [com.apple.UserNotifications:ContentProtection] UNSUserNotificationServer: adding content protection observer\n" +
		"2026-11-20 00:11:33.200 Df SpringBoard[4711:5a5b] [com.apple.UserNotifications:ContentProtection] <compose failure [UUID]>\n"
	if by, found := notificationServers(log); !found || !slices.Equal(by, []int{3576, 4711}) {
		t.Errorf("the notification servers are %v, %v", by, found)
	}
	if by, found := notificationServers("Timestamp               Ty Process[PID:TID]\n"); found || by != nil {
		t.Errorf("a log without the entry has %v, %v", by, found)
	}
}

// The library builds with Xcode for the Mac's simulators, once: a second suite finds it built.
func TestTheSpringBoardLibraryBuildsForTheSimulators(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("iOS simulators run on macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	lib, err := simulatorLibrary(context.Background(), core.NewSuite(core.SuiteOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("file", lib).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "dynamically linked shared library") || !strings.Contains(string(out), SimulatorArch(context.Background())) {
		t.Errorf("built %s", out)
	}
	if _, err := os.Stat(springboardStatuses()); err != nil {
		t.Errorf("no folder for its reports: %v", err)
	}
	again, err := simulatorLibrary(context.Background(), core.NewSuite(core.SuiteOptions{}))
	if err != nil || again != lib {
		t.Errorf("built again: %q, %v", again, err)
	}
}
