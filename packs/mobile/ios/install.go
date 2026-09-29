package mobileios

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// The Appium the pack runs, with its XCUITest driver: pinned, down to every
// package, and downloaded the first time a run needs it.
var (
	//go:embed appium/package.json
	appiumPackage []byte
	//go:embed appium/package-lock.json
	appiumLock []byte
)

// appiumName names the install in axx's cache; the lockfile's hash follows.
const appiumName = "appium-3.8.0-xcuitest-12.13.3"

// installFor is the run's Appium: downloaded once into axx's cache, and run
// by the Node.js axx downloads for it.
func installFor(ctx context.Context, s *core.Suite) (appium.Install, error) {
	return core.Cached(s, Name+"/appium", func() (appium.Install, error) {
		cache := npm.CacheDir("mobile")
		node, err := npm.EnsureNode(ctx, npm.Options{CacheDir: cache})
		if err != nil {
			return appium.Install{}, fmt.Errorf("cannot prepare Node.js, which runs Appium: %w\n  NODE_MIRROR points the download at a mirror of nodejs.org", err)
		}
		dir, err := npm.Install(ctx, npm.InstallOptions{
			Lock: appiumLock, CacheDir: cache, Name: appiumName,
			Downloading: func() { s.Logger().Warn("downloading Appium and its XCUITest driver, once") },
		})
		if err != nil {
			return appium.Install{}, fmt.Errorf("cannot download Appium: %w\n  npm_config_registry points the download at a mirror of npm's registry", err)
		}
		// Appium finds its drivers in its home's package.json.
		if err := npm.WriteOnce(filepath.Join(dir, "package.json"), appiumPackage); err != nil {
			return appium.Install{}, fmt.Errorf("cannot prepare Appium: %w", err)
		}
		return appium.Install{Node: node, Main: filepath.Join(dir, "node_modules", "appium", "index.js"), Home: dir}, nil
	})
}

// WebDriverAgent is the app on the simulator that the XCUITest driver talks
// to, and that drives the app under test. The driver builds it with Xcode,
// which takes minutes and breaks with some Xcode and macOS versions; axx
// installs Appium's own build of it instead, the version the driver pins.
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
