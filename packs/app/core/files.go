package appcore

import (
	"context"
	"fmt"
	"slices"
	"strings"

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
		return indexedFiles{Files: files, sc: sc, app: name, path: path}, err
	}
	app, err := Get(sc, name)
	if err != nil {
		return nil, err
	}
	files, err := app.Family.Files(sc, app, path)
	return indexedFiles{Files: files, sc: sc, app: name, path: path}, err
}

// writtenNote, with an app's name, a path in its files and a file's name
// (each ended by a zero byte), notes that the scenario wrote the file:
// written, or emptied once the scenario emptied the folder after.
const (
	writtenNote = Name + "/written/"
	written     = "written"
	emptied     = "emptied"
)

// indexedFiles adds a scenario-local write index to an app's file context.
// Some remote file providers cannot list a directory; files the scenario
// explicitly wrote still need to be visible to model loading and other steps.
// The index is the scenario's notes (core.Scenario.Note): where the axx host
// starts the app, the packs that prepare it there read what the run's steps
// wrote.
type indexedFiles struct {
	core.Files
	sc        *core.Scenario
	app, path string
}

func (f indexedFiles) prefix() string { return writtenNote + f.app + "\x00" + f.path + "\x00" }

func (f indexedFiles) Write(ctx context.Context, name string, body []byte) error {
	if err := f.Files.Write(ctx, name, body); err != nil {
		return err
	}
	f.sc.Note(f.prefix()+name, written)
	return nil
}

func (f indexedFiles) List(ctx context.Context, max int) ([]string, error) {
	names, err := f.Files.List(ctx, max)
	if err != nil {
		return nil, err
	}
	for _, name := range f.Written() {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) > max {
		names = names[:max]
	}
	return names, nil
}

// Written returns only the files written through this scenario's app-file
// context, without asking the provider to list anything.
func (f indexedFiles) Written() []string {
	prefix := f.prefix()
	var names []string
	for note, state := range f.sc.Notes() {
		if name, ok := strings.CutPrefix(note, prefix); ok && state == written {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if names == nil {
		names = []string{}
	}
	return names
}

func (f indexedFiles) Empty(ctx context.Context) error {
	if err := f.Files.Empty(ctx); err != nil {
		return err
	}
	for _, name := range f.Written() {
		f.sc.Note(f.prefix()+name, emptied)
	}
	return nil
}
