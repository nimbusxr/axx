// SPDX-License-Identifier: Apache-2.0

package mobileios

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
