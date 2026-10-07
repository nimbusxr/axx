package desktopcore

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// homePath is a path in a desktop app's file context: "./" (or no prefix)
// is where the OS keeps the app's data in its home, "~/" the home itself.
// It returns whether the path is in the home, and the rest of it.
func homePath(p string) (inHome bool, rel string, err error) {
	p = strings.ReplaceAll(p, "\\", "/")
	switch {
	case p == "~" || p == "~/":
		return true, "", nil
	case p == "." || p == "./":
		return false, "", nil
	case strings.HasPrefix(p, "~/"):
		inHome, p = true, p[2:]
	case strings.HasPrefix(p, "./"):
		p = p[2:]
	case strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "":
		return false, "", fmt.Errorf("%s is no path in the app's files: start it with ./ (where the app keeps its data) or ~/ (its home), as in ./exports", p)
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return false, "", fmt.Errorf("%s leaves the app's files", p)
	}
	if p == "." {
		p = ""
	}
	return inHome, p, nil
}

// appFiles are a desktop app's files at a path in its file context, in its
// home on the scenario's desktop. The home is found, and reset, when a
// check first reads or writes them: a folder of the files pack is
// registered before the app starts, and a scenario may put files in place
// for the app before it does.
func appFiles(sc *core.Scenario, a *appcore.App, p string) (core.Files, error) {
	app, ok := a.Data.(*App)
	if !ok {
		return nil, fmt.Errorf("the %s app is not a desktop app", a.Name)
	}
	inHome, rel, err := homePath(p)
	if err != nil {
		return nil, err
	}
	return &homeFiles{sc: sc, app: app, inHome: inHome, rel: rel}, nil
}

type homeFiles struct {
	sc     *core.Scenario
	app    *App
	inHome bool
	rel    string
}

func (f *homeFiles) Where() string {
	prefix := "./"
	if f.inHome {
		prefix = "~/"
	}
	return "the " + f.app.Name + " app's files, at " + prefix + f.rel
}

// local are the files on this machine, in the app's home.
func (f *homeFiles) local() (core.Files, error) {
	desk, h, err := home(f.sc, f.app)
	if err != nil {
		return nil, err
	}
	dir := h
	if !f.inHome {
		dir = filepath.Join(dir, filepath.FromSlash(desk.DataDir()))
	}
	return core.LocalFiles(filepath.Join(dir, filepath.FromSlash(f.rel))), nil
}

func (f *homeFiles) Read(ctx context.Context, name string) ([]byte, bool, error) {
	l, err := f.local()
	if err != nil {
		return nil, false, err
	}
	return l.Read(ctx, name)
}

func (f *homeFiles) Empty(ctx context.Context) error {
	l, err := f.local()
	if err != nil {
		return err
	}
	return l.Empty(ctx)
}

func (f *homeFiles) Write(ctx context.Context, name string, body []byte) error {
	l, err := f.local()
	if err != nil {
		return err
	}
	return l.Write(ctx, name, body)
}

func (f *homeFiles) List(ctx context.Context, max int) ([]string, error) {
	l, err := f.local()
	if err != nil {
		return nil, err
	}
	return l.List(ctx, max)
}

func (family) Files(sc *core.Scenario, a *appcore.App, p string) (core.Files, error) {
	return appFiles(sc, a, p)
}
