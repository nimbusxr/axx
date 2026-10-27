package appcore

import (
	"context"
	"fmt"
	"slices"

	"github.com/nimbusxr/axx/core"
)

// RegisterFiles gives an app managed by another pack (for example a web app)
// an app-owned file context without requiring the native control interface.
// A name still identifies one app in a scenario.
func RegisterFiles(sc *core.Scenario, name string, files func(string) (core.Files, error)) error {
	name = Named(sc, name)
	r := apps.Of(sc)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.apps[name]; exists {
		return fmt.Errorf("the %s app is already registered", name)
	}
	if _, exists := r.files[name]; exists {
		return fmt.Errorf("the %s app's files are already registered", name)
	}
	r.files[name] = files
	return nil
}

// Files gives the registered app's file context, whether its platform exposes
// native controls or is managed by another pack.
func Files(sc *core.Scenario, name, path string) (core.Files, error) {
	name = Named(sc, name)
	r := apps.Of(sc)
	r.mu.Lock()
	fn := r.files[name]
	r.mu.Unlock()
	if fn != nil {
		files, err := fn(path)
		return indexedFiles{Files: files, registry: r, app: name, path: path}, err
	}
	app, err := Get(sc, name)
	if err != nil {
		return nil, err
	}
	files, err := app.Family.Files(sc, app, path)
	return indexedFiles{Files: files, registry: r, app: name, path: path}, err
}

// indexedFiles adds a scenario-local write index to an app's file context.
// Some remote file providers cannot list a directory; files the scenario
// explicitly wrote still need to be visible to model loading and other steps.
type indexedFiles struct {
	core.Files
	registry  *registry
	app, path string
}

func (f indexedFiles) Write(ctx context.Context, name string, body []byte) error {
	if err := f.Files.Write(ctx, name, body); err != nil {
		return err
	}
	f.registry.mu.Lock()
	defer f.registry.mu.Unlock()
	paths := f.registry.index[f.app]
	if paths == nil {
		paths = map[string]map[string]bool{}
		f.registry.index[f.app] = paths
	}
	names := paths[f.path]
	if names == nil {
		names = map[string]bool{}
		paths[f.path] = names
	}
	names[name] = true
	return nil
}

func (f indexedFiles) List(ctx context.Context, max int) ([]string, error) {
	names, err := f.Files.List(ctx, max)
	if err != nil {
		return nil, err
	}
	f.registry.mu.Lock()
	for name := range f.registry.index[f.app][f.path] {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	f.registry.mu.Unlock()
	slices.Sort(names)
	if len(names) > max {
		names = names[:max]
	}
	return names, nil
}

// Written returns only the files written through this scenario's app-file
// context, without asking the provider to list anything.
func (f indexedFiles) Written() []string {
	f.registry.mu.Lock()
	defer f.registry.mu.Unlock()
	names := make([]string, 0, len(f.registry.index[f.app][f.path]))
	for name := range f.registry.index[f.app][f.path] {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (f indexedFiles) Empty(ctx context.Context) error {
	if err := f.Files.Empty(ctx); err != nil {
		return err
	}
	f.registry.mu.Lock()
	delete(f.registry.index[f.app], f.path)
	f.registry.mu.Unlock()
	return nil
}
