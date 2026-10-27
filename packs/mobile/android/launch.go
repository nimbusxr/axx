package mobileandroid

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
)

// Launch is a fresh local app process, before ActivityManager binds it. An
// agent is attached using am's documented --attach-agent option. After runs
// after the launch to verify readiness; an attach failure must not be silent.
type Launch struct {
	App, Package, SDKDir, ABI, DataDir string
	Emulator                           bool
	Agent                              string
	Prepare                            func() error
	Files                              func(context.Context, string) (core.Files, error)
	After                              []func() error
}
type launchHooks struct {
	mu    sync.Mutex
	hooks []func(*Launch) error
}

var beforeLaunch = core.NewStateKey("mobile-android/launch-hooks", func(*core.Scenario) *launchHooks { return &launchHooks{} }, nil)

func OnLaunch(sc *core.Scenario, fn func(*Launch) error) {
	h := beforeLaunch.Of(sc)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hooks = append(h.hooks, fn)
}

func (d *running) prepareLaunch(ctx context.Context) (*Launch, error) {
	l := &Launch{App: d.app.name, Package: d.pkg, Emulator: d.dev != nil && strings.HasPrefix(d.dev.serial, "emulator-"), Files: d.Files}
	if d.scenario == nil {
		return l, nil
	}
	h := beforeLaunch.Of(d.scenario)
	h.mu.Lock()
	fns := append([]func(*Launch) error(nil), h.hooks...)
	h.mu.Unlock()
	if len(fns) == 0 {
		return l, nil
	}
	if d.dev != nil {
		abi, err := d.sdk.shell(ctx, d.dev.serial, "getprop", "ro.product.cpu.abi")
		if err != nil {
			return nil, err
		}
		l.ABI = strings.TrimSpace(abi)
		l.Prepare = func() error {
			root, err := d.sdk.shell(ctx, d.dev.serial, "run-as", d.pkg, "pwd")
			if err != nil {
				return err
			}
			l.DataDir = strings.TrimSpace(root)
			return nil
		}
		adb, err := filepath.EvalSymlinks(d.sdk.adb)
		if err != nil {
			return nil, err
		}
		l.SDKDir = filepath.Dir(filepath.Dir(adb))
	}
	for _, fn := range fns {
		if err := fn(l); err != nil {
			return nil, err
		}
	}
	if l.Agent != "" && l.DataDir == "" && l.Prepare != nil {
		if err := l.Prepare(); err != nil {
			return nil, err
		}
	}
	return l, nil
}
