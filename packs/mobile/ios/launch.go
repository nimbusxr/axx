package mobileios

import (
	"context"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
)

// Launch describes an app immediately before a process is started, after its
// files have been prepared. Packs may add environment entries and libraries.
// A restart gets a new Launch and runs the hooks again.
type Launch struct {
	App, BundleID, BundlePath, Version string
	// SourcePath is the registered build product, before any zip extraction.
	SourcePath  string
	Simulator   bool
	Environment map[string]string
	Files       func(context.Context, string) (core.Files, error)
}

// InsertLibrary loads a library at initial process creation, composing with
// other packs' libraries and the simulator's own support library.
func (l *Launch) InsertLibrary(path string) {
	for _, p := range strings.Split(l.Environment["DYLD_INSERT_LIBRARIES"], ":") {
		if p == path {
			return
		}
	}
	if old := l.Environment["DYLD_INSERT_LIBRARIES"]; old != "" {
		path = old + ":" + path
	}
	l.Environment["DYLD_INSERT_LIBRARIES"] = path
}

type launchHooks struct {
	mu    sync.Mutex
	hooks []func(*Launch) error
}

var beforeLaunch = core.NewStateKey("mobile-ios/launch-hooks", func(*core.Scenario) *launchHooks { return &launchHooks{} }, nil)

// OnLaunch installs a scenario-local hook for future launches and restarts.
func OnLaunch(sc *core.Scenario, fn func(*Launch) error) {
	h := beforeLaunch.Of(sc)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hooks = append(h.hooks, fn)
}

func prepareLaunch(sc *core.Scenario, l *Launch) error {
	if sc == nil {
		return nil
	}
	h := beforeLaunch.Of(sc)
	h.mu.Lock()
	fns := append([]func(*Launch) error(nil), h.hooks...)
	h.mu.Unlock()
	for _, fn := range fns {
		if err := fn(l); err != nil {
			return err
		}
	}
	return nil
}
