package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// sandboxPath is a path in an app's file context, relative to its sandbox:
// "./" and "~/" both start there, as a phone's app keeps its data in it.
func sandboxPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	switch {
	case p == "." || p == "~" || p == "./" || p == "~/":
		return "", nil
	case strings.HasPrefix(p, "./"), strings.HasPrefix(p, "~/"):
		p = p[2:]
	case strings.HasPrefix(p, "/"):
		return "", fmt.Errorf("%s is no path in the app's files: start it with ./ (the app's sandbox), as in ./Documents/exports", p)
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("%s leaves the app's files", p)
	}
	if p == "." {
		return "", nil
	}
	return p, nil
}

// appFiles are an app's files at a path in its sandbox, on the device its
// scenario leased. The device is asked when a check reads them: a folder of
// the files pack is registered before the app starts.
func appFiles(sc *core.Scenario, app *appcore.App, p string) (core.Files, error) {
	rel, err := sandboxPath(p)
	if err != nil {
		return nil, err
	}
	return &deviceFiles{sc: sc, app: app.Name, path: rel}, nil
}

type deviceFiles struct {
	sc   *core.Scenario
	app  string
	path string
}

func (f *deviceFiles) Where() string {
	if f.path == "" {
		return "the " + f.app + " app's files"
	}
	return "the " + f.app + " app's files, at ./" + f.path
}

func (f *deviceFiles) files(ctx context.Context) (core.Files, error) {
	d, err := device(f.sc, f.app, false)
	if err != nil {
		return nil, fmt.Errorf("the %s app's files are on its device: %w", f.app, err)
	}
	return d.Files(ctx, f.path)
}

func (f *deviceFiles) Read(ctx context.Context, name string) ([]byte, bool, error) {
	fs, err := f.files(ctx)
	if err != nil {
		return nil, false, err
	}
	return fs.Read(ctx, name)
}

func (f *deviceFiles) Write(ctx context.Context, name string, body []byte) error {
	fs, err := f.files(ctx)
	if err != nil {
		return err
	}
	return fs.Write(ctx, name, body)
}

func (f *deviceFiles) Empty(ctx context.Context) error {
	fs, err := f.files(ctx)
	if err != nil {
		return err
	}
	return fs.Empty(ctx)
}

func (f *deviceFiles) List(ctx context.Context, max int) ([]string, error) {
	fs, err := f.files(ctx)
	if err != nil {
		return nil, err
	}
	return fs.List(ctx, max)
}

// RemoteFiles are an app's files on a device an Appium server of the
// project's own or a device farm's runs, read and written through it: prefix
// names the folder as Appium does ("@<bundle id>:data/Documents"). Appium
// cannot list a folder, so a failure names no files.
func RemoteFiles(s *appium.Session, prefix, where string) core.Files {
	return remoteFiles{s: s, prefix: strings.TrimSuffix(prefix, "/"), where: where}
}

type remoteFiles struct {
	s      *appium.Session
	prefix string
	where  string
}

func (r remoteFiles) Where() string { return r.where }

func (r remoteFiles) Read(ctx context.Context, name string) ([]byte, bool, error) {
	b, err := r.s.PullFile(ctx, r.prefix+"/"+name)
	var ae *appium.Error
	if errors.As(err, &ae) && strings.Contains(strings.ToLower(ae.Message), "not exist") {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (r remoteFiles) Write(ctx context.Context, name string, body []byte) error {
	return r.s.PushFile(ctx, r.prefix+"/"+name, body)
}

func (r remoteFiles) List(context.Context, int) ([]string, error) { return nil, nil }

// Empty cannot: Appium neither lists nor removes an app's files.
func (r remoteFiles) Empty(context.Context) error {
	return fmt.Errorf("%s cannot be emptied: Appium neither lists nor removes an app's files on its device", r.where)
}
