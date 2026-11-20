// SPDX-License-Identifier: Apache-2.0

package mobileios

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/npm"
)

// WebDriverAgent runs on a simulator for every scenario that leases it:
// axx launches it once, and each session uses it where it runs. Left to the
// driver, it would be quit at the end of each session and launched again for
// the next, which takes 10 to 30 seconds a scenario; a session given its URL
// leaves it running. It drives the simulator, and holds nothing of the app's:
// each scenario's app is still reset.

// wdaURL is where WebDriverAgent answers: on the Mac's loopback, which the
// simulator shares.
func (d *device) wdaURL() string { return "http://127.0.0.1:" + strconv.Itoa(d.wdaPort) }

// ensureWDA makes sure WebDriverAgent answers on the simulator, and launches
// it when it does not.
func (d *device) ensureWDA(ctx context.Context) error {
	if wdaUp(ctx, d.wdaURL(), time.Second) {
		return nil
	}
	cmd := exec.CommandContext(ctx, "xcrun", append([]string{"simctl"}, d.set.args("launch", "--terminate-running-process", d.udid, wdaBundleID)...)...) //nolint:gosec // simctl, with arguments axx builds
	// What the driver gives the WebDriverAgent it launches: its port, its
	// bundle id, and the port of its stream of the screen.
	cmd.Env = append(os.Environ(),
		"SIMCTL_CHILD_USE_PORT="+strconv.Itoa(d.wdaPort),
		"SIMCTL_CHILD_WDA_PRODUCT_BUNDLE_IDENTIFIER="+wdaBundleID,
		"SIMCTL_CHILD_MJPEG_SERVER_PORT="+strconv.Itoa(d.mjpegPort),
	)
	launched := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot launch WebDriverAgent on the %s simulator: %w: %s", d.name, err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(wdaAnswers)
	for !wdaUp(ctx, d.wdaURL(), 2*time.Second) {
		if time.Now().After(deadline) {
			return fmt.Errorf("WebDriverAgent did not answer on the %s simulator within %s: %s", d.name, wdaAnswers, d.wdaUnanswered(ctx, launched))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

// wdaAnswers is how long WebDriverAgent has to answer once it is launched; a variable, for tests.
var wdaAnswers = 90 * time.Second

// wdaUnanswered tells, for the error of a WebDriverAgent that did not answer, what became of it
// since it was launched: whether its process still runs, as the simulator's launchd lists it (one
// that runs and does not answer hangs, one that ended crashed or was ended), and the last of what
// the simulator logged of it, its own lines and the lines of other processes that name it
// (SpringBoard and RunningBoard say there why they ended an app).
func (d *device) wdaUnanswered(ctx context.Context, since time.Time) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var b strings.Builder
	services, err := d.set.simctl(ctx, "spawn", d.udid, "launchctl", "list")
	switch pid, status, listed := launchdJob(services, wdaBundleID); {
	case err != nil:
		fmt.Fprintf(&b, "the simulator's launchd does not say whether its process runs (%v)", err)
	case !listed:
		b.WriteString("the simulator's launchd lists no process of it")
	case pid == "-":
		fmt.Fprintf(&b, "its process has ended (launchd's status of it is %s)", status)
	default:
		fmt.Fprintf(&b, "its process still runs (pid %s)", pid)
	}
	predicate := fmt.Sprintf(`process == %q OR composedMessage CONTAINS %q`, strings.TrimSuffix(wdaApp, ".app"), wdaBundleID)
	out, err := d.set.simctl(ctx, "spawn", d.udid, "log", "show", "--start", since.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate", predicate)
	switch log := lastLines(out, 40, 300); {
	case err != nil:
		fmt.Fprintf(&b, "; the simulator's log cannot be read (%v)", err)
	case log == "":
		b.WriteString("; the simulator's log has nothing of it since it was launched")
	default:
		fmt.Fprintf(&b, "; the last of what the simulator logged of it since it was launched:\n%s", log)
	}
	return b.String()
}

// launchdJob is the process id and the status of the job whose label names an app, in what a
// simulator's launchctl list prints: a line each of a process id (- for a job that does not run),
// the status its last process ended with, and a label, which for an app holds its bundle
// identifier (UIKitApplication:com.example.app[1a2b][rb-legacy]).
func launchdJob(list, bundleID string) (pid, status string, listed bool) {
	for _, l := range strings.Split(list, "\n") {
		if f := strings.Fields(l); len(f) == 3 && strings.Contains(f[2], bundleID) {
			return f[0], f[1], true
		}
	}
	return "", "", false
}

// wdaUp reports whether WebDriverAgent answers its status at url.
func wdaUp(ctx context.Context, url string, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/status", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// wdaState is where a kept simulator's WebDriverAgent is, for the next run: its port, its screen
// stream's, and the build installed.
func wdaState(name string) string {
	return filepath.Join(npm.CacheDir("mobile"), "kept", lockName(name)+".wda")
}

// keepWDA notes where the simulator's WebDriverAgent is, for the next run to take it up.
func (d *device) keepWDA(wda string) {
	_ = os.WriteFile(wdaState(d.name), []byte(fmt.Sprintf("%d\n%d\n%s\n", d.wdaPort, d.mjpegPort, wda)), 0o644)
}

// reuseWDA takes up the WebDriverAgent the run before left running on a kept simulator, rather
// than installing and launching it again: that took from 4 seconds to over a minute on a small CI
// machine, for every run. It does when WebDriverAgent answers where the run before put it, the
// process listening there is this simulator's, and it is the build this axx installs.
func (d *device) reuseWDA(ctx context.Context, wda string) bool {
	b, err := os.ReadFile(wdaState(d.name))
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 3 || lines[2] != wda {
		return false
	}
	wdaPort, err1 := strconv.Atoi(lines[0])
	mjpegPort, err2 := strconv.Atoi(lines[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if !wdaUp(ctx, "http://127.0.0.1:"+lines[0], time.Second) || !listensFor(ctx, wdaPort, d.udid) {
		return false
	}
	d.wdaPort, d.mjpegPort = wdaPort, mjpegPort
	return true
}

// listensFor reports whether the process listening on a port of the Mac is one of the simulator's:
// its program is in the simulator's own folder, named for its UDID. Another simulator's
// WebDriverAgent can have the port a run before used.
func listensFor(ctx context.Context, port int, udid string) bool {
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp").Output() //nolint:gosec // a port number
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if pid, ok := strings.CutPrefix(line, "p"); ok {
			cmd, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", pid).Output() //nolint:gosec // a process id lsof gave
			if err == nil && strings.Contains(string(cmd), udid) {
				return true
			}
		}
	}
	return false
}
