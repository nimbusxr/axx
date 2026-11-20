package mobileios

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// A run takes up a kept simulator's WebDriverAgent only when the process listening on its port
// is that simulator's: its program is in the simulator's folder, named for its UDID. Here the
// listener is this test's own process, whose program's name stands in for the folder.
func TestTheProcessOnAPortIsTheSimulators(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("lsof lists a Mac's ports here")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !listensFor(context.Background(), port, filepath.Base(exe)) {
		t.Errorf("the process on port %d is not taken for %s's", port, filepath.Base(exe))
	}
	if listensFor(context.Background(), port, "9F3B2D3E-0000-0000-0000-000000000000") {
		t.Errorf("the process on port %d is taken for another simulator's", port)
	}
	if listensFor(context.Background(), port+1, filepath.Base(exe)) {
		t.Errorf("a port nothing listens on is taken for the simulator's")
	}
}

// A WebDriverAgent that never answers is reported with what became of it: whether its process
// still runs, and the last of what the simulator logged of it. Here xcrun is a script that
// launches nothing, lists the simulator's jobs as the test gives them, and prints a log of two
// lines under the header log show prints.
func TestAWebDriverAgentThatDoesNotAnswerIsReportedWithWhatBecameOfIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("iOS simulators run on macOS only")
	}
	tools := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*\"launchctl list\"*) printf '%s\\n' \"$AXX_TEST_LAUNCHD_LIST\" ;;\n" +
		"*\"log show\"*) printf 'Timestamp               Ty Process[PID:TID]\\n%s\\n' \"$AXX_TEST_SIMULATOR_LOG\" ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(tools, "xcrun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	started := "2026-11-20 00:11:36.102 Df WebDriverAgentRunner-Runner[5120:1a2b] Running tests..."
	ended := "2026-11-20 00:11:56.480 Df SpringBoard[4711:3c4d] [com.apple.FrontBoard:Common] Terminating application<" + wdaBundleID + ">"
	t.Setenv("AXX_TEST_SIMULATOR_LOG", started+"\n"+ended)
	within := wdaAnswers
	wdaAnswers = 200 * time.Millisecond
	t.Cleanup(func() { wdaAnswers = within })
	port, err := appium.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d := &device{udid: "9F3B2D3E-0000-0000-0000-000000000000", name: "axx iPhone 16, iOS 26.5 (1)", wdaPort: port, mjpegPort: port}
	for _, tc := range []struct{ jobs, want string }{
		{"-\t9\tUIKitApplication:" + wdaBundleID + "[1a2b][rb-legacy]", "its process has ended (launchd's status of it is 9)"},
		{"5120\t0\tUIKitApplication:" + wdaBundleID + "[1a2b][rb-legacy]", "its process still runs (pid 5120)"},
		{"4711\t0\tcom.apple.SpringBoard", "the simulator's launchd lists no process of it"},
	} {
		t.Setenv("AXX_TEST_LAUNCHD_LIST", "PID\tStatus\tLabel\n"+tc.jobs)
		err := d.ensureWDA(t.Context(), nil)
		if err == nil {
			t.Fatalf("a WebDriverAgent that answers nothing is taken for one that answers (launchd lists %q)", tc.jobs)
		}
		want := "WebDriverAgent did not answer on the axx iPhone 16, iOS 26.5 (1) simulator within 200ms: " + tc.want +
			"; the last of what it logged since it was launched:\n" + started + "\nand the last of what the simulator logged of it:\n" + ended
		if err.Error() != want {
			t.Errorf("launchd lists %q: the error is\n%v\nwant\n%s", tc.jobs, err, want)
		}
	}
}

// A WebDriverAgent whose process ends as it starts is not waited for: it is launched again, and
// the run is told what became of it. Here xcrun is a script that counts its launches and lists
// no process of WebDriverAgent, and the test answers as WebDriverAgent once it was launched a
// second time, or never.
func TestAWebDriverAgentThatEndsAsItStartsIsLaunchedAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("iOS simulators run on macOS only")
	}
	for _, d := range []struct {
		v    *time.Duration
		with time.Duration
	}{{&wdaAnswers, 20 * time.Second}, {&wdaStarts, 0}, {&wdaLooks, 20 * time.Millisecond}} {
		was := *d.v
		*d.v = d.with
		t.Cleanup(func() { *d.v = was })
	}
	for _, tc := range []struct {
		name     string
		answers  int // the launch from which it answers; 0 for never
		launches string
		warned   int
		want     string
	}{
		{"its second start answers", 2, "2", 1, ""},
		{"it ends every time", 0, "3", 2, "WebDriverAgent ended as it started on the axx iPhone 16, iOS 26.5 (1) simulator, three times: the simulator's launchd lists no process of it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools := t.TempDir()
			count := filepath.Join(tools, "launches")
			script := "#!/bin/sh\ncase \"$*\" in\n" +
				"*\" launch \"*) echo x >> '" + count + "' ;;\n" +
				"*\"launchctl list\"*) printf 'PID\\tStatus\\tLabel\\n4711\\t0\\tcom.apple.SpringBoard\\n' ;;\n" +
				"*\"log show\"*) printf 'Timestamp               Ty Process[PID:TID]\\n' ;;\n" +
				"esac\n"
			if err := os.WriteFile(filepath.Join(tools, "xcrun"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			launched := func() int {
				b, _ := os.ReadFile(count)
				return strings.Count(string(b), "x")
			}
			wda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.answers == 0 || launched() < tc.answers {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer wda.Close()
			port := wda.Listener.Addr().(*net.TCPAddr).Port
			d := &device{udid: "9F3B2D3E-0000-0000-0000-000000000000", name: "axx iPhone 16, iOS 26.5 (1)", wdaPort: port, mjpegPort: port}
			var warned []string
			err := d.ensureWDA(t.Context(), func(msg string, _ ...any) { warned = append(warned, msg) })
			if tc.want == "" && err != nil {
				t.Fatalf("a WebDriverAgent that answers at its second start: %v", err)
			}
			if tc.want != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.want)) {
				t.Fatalf("the error is\n%v\nwant one that begins\n%s", err, tc.want)
			}
			if got := strconv.Itoa(launched()); got != tc.launches {
				t.Errorf("it was launched %s times, want %s", got, tc.launches)
			}
			if len(warned) != tc.warned {
				t.Errorf("the run was told %d times that it ended, want %d: %q", len(warned), tc.warned, warned)
			}
		})
	}
}

// An app's job is found in a simulator's list of launchd's jobs by its bundle identifier.
func TestAnAppsJobIsFoundInLaunchdsList(t *testing.T) {
	list := "PID\tStatus\tLabel\n" +
		"4711\t0\tcom.apple.SpringBoard\n" +
		"-\t-9\tUIKitApplication:example.parcels.courier[0f0f][rb-legacy]\n" +
		"5120\t0\tUIKitApplication:" + wdaBundleID + "[1a2b][rb-legacy]\n"
	if pid, status, listed := launchdJob(list, wdaBundleID); pid != "5120" || status != "0" || !listed {
		t.Errorf("WebDriverAgent's job is %q, %q, %v", pid, status, listed)
	}
	if pid, status, listed := launchdJob(list, "example.parcels.courier"); pid != "-" || status != "-9" || !listed {
		t.Errorf("an ended app's job is %q, %q, %v", pid, status, listed)
	}
	if _, _, listed := launchdJob(list, "example.parcels.other"); listed {
		t.Error("an app launchd does not list is found")
	}
}
