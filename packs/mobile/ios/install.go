package mobileios

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
)

// The WebDriverAgent the pack runs on its simulators, as Appium's project
// builds it for them: pinned, and downloaded the first time a run needs it.
const (
	wdaVersion  = "16.12.11"
	wdaBundleID = "com.facebook.WebDriverAgentRunner.xctrunner"
	wdaApp      = "WebDriverAgentRunner-Runner.app"
)

// wdaSums are the SHA-256 sums of its builds for simulators, by the Mac's
// processor.
var wdaSums = map[string]string{
	"arm64":  "fc47e5334061040c5c5da660b7d7b68bcc4e7252a06c1857fcd6bc9be18cafa3",
	"x86_64": "33805030743aab14c59d8d5fc6436f6e840a09fa2c5ff697bb2e8f77923f7a33",
}

// wdaFor is WebDriverAgent for the Mac's simulators, downloaded once into
// axx's cache and checked against its pinned sum.
func wdaFor(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/wda", func() (string, error) {
		arch := simulatorArch(ctx)
		cache := npm.CacheDir("mobile")
		dir := filepath.Join(cache, "webdriveragent-"+wdaVersion+"-sim-"+arch)
		app := filepath.Join(dir, wdaApp)
		if _, err := os.Stat(filepath.Join(app, "Info.plist")); err == nil {
			return app, nil
		}
		s.Logger().Warn("downloading WebDriverAgent for simulators, once")
		name := "WebDriverAgentRunner-Build-Sim-" + arch + ".zip"
		url := "https://github.com/appium/WebDriverAgent/releases/download/v" + wdaVersion + "/" + name
		body, err := npm.Fetch(ctx, url)
		if err != nil {
			return "", fmt.Errorf("cannot download WebDriverAgent: %w", err)
		}
		if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != wdaSums[arch] {
			return "", fmt.Errorf("%s: SHA-256 %x does not match the pinned %s", name, sum, wdaSums[arch])
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		if err := npm.Unzip(body, tmp); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		// Another axx may have prepared it meanwhile: keep the first.
		if err := os.Rename(tmp, dir); err != nil {
			if _, statErr := os.Stat(filepath.Join(app, "Info.plist")); statErr == nil {
				return app, nil
			}
			return "", err
		}
		return app, nil
	})
}

// simulatorArch is the processor the Mac's simulators run apps for: arm64 on
// Apple silicon, even for an axx built for Intel Macs.
func simulatorArch(ctx context.Context) string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	out, err := exec.CommandContext(ctx, "sysctl", "-n", "hw.optional.arm64").Output()
	if err == nil && strings.TrimSpace(string(out)) == "1" {
		return "arm64"
	}
	return "x86_64"
}
