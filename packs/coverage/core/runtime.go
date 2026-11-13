package coveragecore

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// Runtime measures the coverage of the services of one runtime: the pack of
// that runtime (coverage-jvm, coverage-go...) registers one, as
// models-core's formats register.
type Runtime interface {
	// Name is the runtime a service's coverage section names: jvm, go.
	Name() string
	// Measured are the metrics it can measure besides lines.
	Measured() istanbul.Measured
	// Launch readies the service's coverage folder (an agent, a preload) and
	// returns the variables the service gets, NAME=value.
	Launch(ctx context.Context, call LaunchCall) ([]string, error)
	// Convert reads what a stopped service wrote into its folder: the
	// coverage of its files, named by their paths on this machine, and the
	// metrics it has. None is no error: the run says why the service wrote
	// nothing.
	Convert(ctx context.Context, call ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error)
}

// Keeper is a runtime whose services can be read while they run: those axx
// up keeps are measured per run, reset as a run starts and taken as it
// ends.
type Keeper interface {
	// Reset starts the service's counts afresh.
	Reset(ctx context.Context, call LaunchCall) error
	// Take writes what ran since the service was reset into the folder
	// into, as its folder would have it once stopped: what Convert reads.
	Take(ctx context.Context, call LaunchCall, into string) error
}

// LaunchCall is a service a run with coverage starts.
type LaunchCall struct {
	Service core.CoverageService
	// Settings decodes a pack's section of axx.yaml (packs.<name>).
	Settings func(pack string, v any) error
}

// ConvertCall is a service a run with coverage stopped.
type ConvertCall struct {
	End core.CoverageEnd
	// Settings decodes a pack's section of axx.yaml (packs.<name>).
	Settings func(pack string, v any) error
	// Path is where a file the service named, as it ran (in its container,
	// say), is on this machine: through coverage-core's paths for it, and
	// relative to its working directory.
	Path func(string) string
	// Project is the project's folder, which settings' folders are
	// relative to.
	Project string
	Logger  *slog.Logger
}

var (
	registryMu sync.Mutex
	registry   = map[string]Runtime{}
)

// Register adds a runtime the services' coverage sections can name.
func Register(r Runtime) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[r.Name()] = r
}

func runtimeOf(name string) (Runtime, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	r, ok := registry[name]
	return r, ok
}

// runtimes are the registered runtimes' names, in order.
func runtimes() []string {
	registryMu.Lock()
	defer registryMu.Unlock()
	return slices.Sorted(maps.Keys(registry))
}
