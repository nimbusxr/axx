package mobileandroid

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// The Appium the pack runs, with its UiAutomator2 driver: pinned, down to
// every package, and downloaded the first time a run needs it.
var (
	//go:embed appium/package.json
	appiumPackage []byte
	//go:embed appium/package-lock.json
	appiumLock []byte
)

// appiumName names the install in axx's cache; the lockfile's hash follows.
const appiumName = "appium-3.8.0-uiautomator2-8.7.0"

// appiumUnbundled are packages the UiAutomator2 driver bundles with a flaw,
// removed from it once it is unpacked, so that it runs the fixed version
// Appium itself installs. The lockfile does not list them.
//   - morgan 1.11.0 forges log lines (GHSA-jxfw-x594-9x9m, GHSA-9f6g-j8ch-79g4),
//     fixed in 1.12.1.
//   - brace-expansion 5.0.9 recurses and expands without bounds on crafted
//     patterns (three denials of service), fixed in 5.0.12.
//
// Take them off when a driver bundles fixed versions: the install fails
// until they are.
var appiumUnbundled = []string{
	"node_modules/appium-uiautomator2-driver/node_modules/morgan",
	"node_modules/appium-uiautomator2-driver/node_modules/brace-expansion",
}

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
			Lock: appiumLock, CacheDir: cache, Name: appiumName, Unbundle: appiumUnbundled,
			Downloading: func() { s.Logger().Warn("downloading Appium and its UiAutomator2 driver, once") },
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
