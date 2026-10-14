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
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot launch WebDriverAgent on the %s simulator: %w: %s", d.name, err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(90 * time.Second)
	for !wdaUp(ctx, d.wdaURL(), 2*time.Second) {
		if time.Now().After(deadline) {
			return fmt.Errorf("WebDriverAgent did not answer on the %s simulator within 90s", d.name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
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
