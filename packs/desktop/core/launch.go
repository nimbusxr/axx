package desktopcore

import (
	"maps"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
)

// Launch describes a desktop app immediately before its process starts, after its home is
// prepared. Packs may add environment entries and libraries (OnLaunch). A restart gets a new
// Launch and runs the hooks again.
type Launch struct {
	App *App
	// Home is the app's home, and Files the folder its files are in (its "./"), as the app sees
	// them: the paths its environment names. HostHome and HostFiles are where they are on this
	// machine, where axx reads and writes them: another folder only on a Linux desktop of several.
	Home, Files, HostHome, HostFiles string
	// Environment is what the app starts with, beyond what axx sets: the registration's env.*
	// entries, and what the hooks add.
	Environment map[string]string
}

// InsertLibrary loads a library into the app as it starts (macOS's DYLD_INSERT_LIBRARIES),
// after the registration's and other packs' libraries.
func (l *Launch) InsertLibrary(path string) {
	old := l.Environment["DYLD_INSERT_LIBRARIES"]
	for _, p := range strings.Split(old, ":") {
		if p == path {
			return
		}
	}
	if old != "" {
		path = old + ":" + path
	}
	l.Environment["DYLD_INSERT_LIBRARIES"] = path
}

type launchHooks struct {
	mu    sync.Mutex
	hooks []func(*Launch) error
}

var beforeLaunch = core.NewStateKey("desktop-core/launch-hooks", func(*core.Scenario) *launchHooks { return &launchHooks{} }, nil)

// OnLaunch installs a scenario's hook for the launches of its desktop apps, restarts included.
func OnLaunch(sc *core.Scenario, fn func(*Launch) error) {
	h := beforeLaunch.Of(sc)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hooks = append(h.hooks, fn)
}

// launching is the app as it starts: with the environment the scenario's launch hooks give it.
func launching(sc *core.Scenario, app *App, desk Desktop, host string) (*App, error) {
	h := beforeLaunch.Of(sc)
	h.mu.Lock()
	fns := append([]func(*Launch) error(nil), h.hooks...)
	h.mu.Unlock()
	if len(fns) == 0 {
		return app, nil
	}
	home := HomeOf(sc, app)
	data := filepath.FromSlash(desk.DataDir())
	l := &Launch{
		App: app, Home: home, Files: filepath.Join(home, data), HostHome: host, HostFiles: filepath.Join(host, data),
		Environment: maps.Clone(app.Env),
	}
	if l.Environment == nil {
		l.Environment = map[string]string{}
	}
	for _, fn := range fns {
		if err := fn(l); err != nil {
			return nil, err
		}
	}
	started := *app
	started.Env = l.Environment
	return &started, nil
}
