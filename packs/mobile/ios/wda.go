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
// it when it does not. One that ends as it starts is launched again, twice at
// most, and warn is told what became of it: on CI's Mac one aborted 11 seconds
// after it was launched, and axx waited its 90 seconds for a process that had
// gone, where the job run again had it answer in a few.
func (d *device) ensureWDA(ctx context.Context, warn func(msg string, args ...any)) error {
	if wdaUp(ctx, d.wdaURL(), time.Second) {
		return nil
	}
	launched, err := d.launchWDA(ctx)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(wdaAnswers)
	again, gone, looked := 0, 0, time.Now()
	for !wdaUp(ctx, d.wdaURL(), 2*time.Second) {
		if time.Now().After(deadline) {
			return fmt.Errorf("WebDriverAgent did not answer on the %s simulator within %s: %s", d.name, wdaAnswers, d.wdaUnanswered(ctx, launched))
		}
		// Whether its process still runs, asked now and then once it has had the time to
		// start: gone twice in a row, it has ended, and is not waited for.
		if time.Since(launched) >= wdaStarts && time.Since(looked) >= wdaLooks {
			looked = time.Now()
			if d.wdaGone(ctx) {
				gone++
			} else {
				gone = 0
			}
			if gone >= 2 {
				what := d.wdaUnanswered(ctx, launched)
				if again == 2 {
					return fmt.Errorf("WebDriverAgent ended as it started on the %s simulator, three times: %s", d.name, what)
				}
				again++
				if warn != nil {
					warn("WebDriverAgent ended as it started on the iOS simulator: it is launched again", "simulator", d.name, "after", time.Since(launched).Round(time.Second), "what became of it", what)
				}
				if launched, err = d.launchWDA(ctx); err != nil {
					return err
				}
				gone = 0
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

// launchWDA launches WebDriverAgent on the simulator, in place of one that runs, and says when.
func (d *device) launchWDA(ctx context.Context) (time.Time, error) {
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
		return launched, fmt.Errorf("cannot launch WebDriverAgent on the %s simulator: %w: %s", d.name, err, strings.TrimSpace(string(out)))
	}
	return launched, nil
}

// wdaGone is whether the simulator's launchd has no running process of WebDriverAgent. A launchd
// that does not say is not taken for one that has none.
func (d *device) wdaGone(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	services, err := d.set.simctl(ctx, "spawn", d.udid, "launchctl", "list")
	if err != nil {
		return false
	}
	pid, _, listed := launchdJob(services, wdaBundleID)
	return !listed || pid == "-"
}

// wdaStarts is how long a launched WebDriverAgent is left before its process is looked for, and
// wdaLooks how long between two looks; variables, for tests.
var (
	wdaStarts = 5 * time.Second
	wdaLooks  = 3 * time.Second
)

// wdaAnswers is how long WebDriverAgent has to answer once it is launched; a variable, for tests.
var wdaAnswers = 90 * time.Second

// wdaUnanswered tells, for the error of a WebDriverAgent that did not answer, what became of it
// since it was launched: whether its process still runs, as the simulator's launchd lists it (one
// that runs and does not answer hangs, one that ended crashed or was ended), the last of what it
// logged itself, and the last of what other processes logged that names it (SpringBoard and
// RunningBoard say there why they ended an app, and with which signal it ended).
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
	runner := strings.TrimSuffix(wdaApp, ".app")
	predicate := fmt.Sprintf(`process == %q OR composedMessage CONTAINS %q`, runner, wdaBundleID)
	out, err := d.set.simctl(ctx, "spawn", d.udid, "log", "show", "--start", since.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate", predicate)
	if err != nil {
		fmt.Fprintf(&b, "; the simulator's log cannot be read (%v)", err)
		return b.String()
	}
	// Its own lines apart from the others': when it ends, SpringBoard, RunningBoard and a
	// dozen more log its end in forty lines, and the reason it gave before is what is asked.
	var own, others []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, " "+runner+"[") {
			own = append(own, l)
		} else {
			others = append(others, l)
		}
	}
	ownLog, othersLog := lastLines(strings.Join(own, "\n"), 25, 300), lastLines(strings.Join(others, "\n"), 15, 300)
	switch {
	case ownLog == "" && othersLog == "":
		b.WriteString("; the simulator's log has nothing of it since it was launched")
	case ownLog == "":
		fmt.Fprintf(&b, "; it logged nothing itself since it was launched, and the last of what the simulator logged of it:\n%s", othersLog)
	case othersLog == "":
		fmt.Fprintf(&b, "; the last of what it logged since it was launched:\n%s", ownLog)
	default:
		fmt.Fprintf(&b, "; the last of what it logged since it was launched:\n%s\nand the last of what the simulator logged of it:\n%s", ownLog, othersLog)
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
