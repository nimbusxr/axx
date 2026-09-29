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
