package mobileandroid

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// Files are the app's files at a path in its data folder: on an emulator or
// a device adb reaches, read and written as the app (run-as), which Android
// allows for a debuggable build only; on a device an Appium server of the
// project's own or a device farm's runs, through Appium.
func (d *running) Files(_ context.Context, rel string) (core.Files, error) {
	where := "the " + d.app.name + " app's files"
	if rel != "" {
		where += ", at ./" + rel
	}
	if d.dev == nil {
		return mobilecore.RemoteFiles(d.session, "@"+d.pkg+"/"+rel, where), nil
	}
	return adbFiles{sdk: d.sdk, serial: d.dev.serial, pkg: d.pkg, dir: rel, app: d.app.name, where: where}, nil
}

// adbFiles are an app's files on a device adb reaches, read as the app.
type adbFiles struct {
	sdk                   *sdk
	serial, pkg, dir, app string
	where                 string
}

func (f adbFiles) Where() string { return f.where }

// q quotes a word for the device's shell.
func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (f adbFiles) path(name string) string {
	if f.dir == "" {
		return name
	}
	return path.Join(f.dir, name)
}

// asApp runs a command on the device as the app, in its data folder.
func (f adbFiles) asApp(ctx context.Context, script string) (string, error) {
	out, err := f.sdk.shell(ctx, f.serial, "run-as "+q(f.pkg)+" sh -c "+q(script))
	if strings.Contains(out, "not debuggable") {
		return "", fmt.Errorf("the %s app's files can be read only in a debuggable build, and Android refuses this one (run-as: package not debuggable)", f.app)
	}
	return out, err
}

func (f adbFiles) Read(ctx context.Context, name string) ([]byte, bool, error) {
	p := f.path(name)
	out, err := f.asApp(ctx, "if [ -f "+q(p)+" ]; then echo yes; else echo no; fi")
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(out) != "yes" {
		return nil, false, nil
	}
	cmd := exec.CommandContext(ctx, f.sdk.adb, "-s", f.serial, "exec-out", "run-as "+q(f.pkg)+" cat "+q(p)) //nolint:gosec // adb, with arguments axx builds
	body, err := cmd.Output()
	if err != nil {
		return nil, false, fmt.Errorf("cannot read %s of the %s app: %w", p, f.app, err)
	}
	return body, true, nil
}

func (f adbFiles) Write(ctx context.Context, name string, body []byte) error {
	p := f.path(name)
	script := "mkdir -p " + q(path.Dir(p)) + " && cat > " + q(p)
	cmd := exec.CommandContext(ctx, f.sdk.adb, "-s", f.serial, "exec-in", "run-as "+q(f.pkg)+" sh -c "+q(script)) //nolint:gosec // adb, with arguments axx builds
	cmd.Stdin = bytes.NewReader(body)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot write %s of the %s app: %w: %s", p, f.app, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (f adbFiles) Empty(ctx context.Context) error {
	dir := f.dir
	if dir == "" {
		dir = "."
	}
	_, err := f.asApp(ctx, "[ ! -d "+q(dir)+" ] || find "+q(dir)+" -mindepth 1 -maxdepth 1 -exec rm -rf {} +")
	if err != nil {
		return fmt.Errorf("cannot empty %s of the %s app: %w", dir, f.app, err)
	}
	return nil
}

func (f adbFiles) List(ctx context.Context, max int) ([]string, error) {
	dir := f.dir
	if dir == "" {
		dir = "."
	}
	out, err := f.asApp(ctx, "[ -d "+q(dir)+" ] && find "+q(dir)+" -type f")
	if err != nil {
		if strings.Contains(err.Error(), "debuggable") {
			return nil, err
		}
		return nil, nil // not there yet: no files
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		rel := strings.TrimPrefix(strings.TrimPrefix(line, dir), "/")
		if rel == "" || strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.") {
			continue
		}
		if len(names) == max {
			break
		}
		names = append(names, rel)
	}
	return names, nil
}
