package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/host"
	"github.com/nimbusxr/axx/internal/version"
)

// follower is the follower of a host that runs on release v in the project
// of the working directory.
func follower(t *testing.T, v string, env map[string]string) *hostFollower {
	t.Helper()
	var out bytes.Buffer
	app := &App{Stdout: &out, Stderr: &out, Env: func(k string) string { return env[k] }}
	newRootCmd(app)
	return &hostFollower{app: app, cf: &configFlags{}, info: version.Info{Version: v, Channel: "stable"}, bins: map[string]string{}}
}

func rewrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHostFollowsTheVersionTheProjectNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a host on Windows is restarted by hand")
	}
	dir, cache := pinnedProject(t, "axx: 0.9.0\n")
	yaml := filepath.Join(dir, "axx.yaml")
	f := follower(t, "0.9.0", nil)
	ctx := context.Background()
	if n, err := f.Next(ctx); n != nil || err != nil {
		t.Fatalf("on the version the project names: %+v, %v", n, err)
	}

	rewrite(t, yaml, "axx: 0.9.1\n")
	n, err := f.Next(ctx)
	if err != nil || n == nil || *n != (host.Next{Version: "0.9.1", From: "axx.yaml"}) {
		t.Fatalf("a later version: %+v, %v", n, err)
	}
	// The release is made ready as a project's first command gets it: here
	// it is on the machine already.
	bin := versionBinary(cache, "0.9.1")
	writeFiles(t, filepath.Dir(bin), map[string]string{filepath.Base(bin): "#!/bin/sh\n"})
	if err := f.Ready(ctx, n); err != nil || f.bins["0.9.1"] != bin {
		t.Fatalf("ready: %q, %v", f.bins, err)
	}
	// The restart is that release, started as this host was, which knows
	// the installed axx that started it.
	f.info.Launcher, f.installed = "0.8.5", "/usr/local/bin/axx"
	var ran string
	var env []string
	old := hostExec
	hostExec = func(bin string, e []string) error {
		ran, env = bin, e
		return nil
	}
	t.Cleanup(func() { hostExec = old })
	t.Setenv(envPackBuild, "") // as Main leaves it
	if err := f.restart(dir, n); err != nil || ran != bin {
		t.Fatalf("restart ran %q: %v", ran, err)
	}
	for _, want := range []string{"AXX_LAUNCHER=0.8.5", "AXX_LAUNCHER_PATH=/usr/local/bin/axx", "AXX_HOST_MOVED=" + dir} {
		if !slices.Contains(env, want) {
			t.Errorf("no %s in the restart's environment", want)
		}
	}

	rewrite(t, yaml, "axx: 0.8.0\n")
	if n, err := f.Next(ctx); err != nil || n == nil || !n.Older || n.Version != "0.8.0" {
		t.Fatalf("an earlier version: %+v, %v", n, err)
	}
	rewrite(t, yaml, "axx: latest\n")
	if n, err := f.Next(ctx); err == nil || n != nil || !strings.Contains(err.Error(), "`axx: latest` is not a version of axx") {
		t.Fatalf("no version: %+v, %v", n, err)
	}
	// AXX_USE_VERSION, which the host started with, stays what it runs on.
	rewrite(t, yaml, "axx: 0.9.5\n")
	if n, err := follower(t, "0.9.0", map[string]string{envUseVersion: "0.9.0"}).Next(ctx); n != nil || err != nil {
		t.Fatalf("with AXX_USE_VERSION: %+v, %v", n, err)
	}
	// A development build ignores the project's version, as its runs do.
	dev := follower(t, "0.9.0-dev", nil)
	dev.info.Channel = "dev"
	if n, err := dev.Next(ctx); n != nil || err != nil {
		t.Fatalf("a development build: %+v, %v", n, err)
	}
}

func TestHostFollowsTheInstalledAxx(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the installed axx here is a shell script")
	}
	dir, _ := pinnedProject(t, "services: {}\n")
	installed := filepath.Join(t.TempDir(), "axx")
	script := func(v string) string {
		return "#!/bin/sh\necho '{\"ok\":true,\"data\":{\"version\":\"" + v + "\"}}'\n"
	}
	rewrite(t, installed, script("0.9.0"))
	f := follower(t, "0.9.0", nil)
	f.installed = installed
	ctx := context.Background()
	if n, err := f.Next(ctx); n != nil || err != nil {
		t.Fatalf("on the installed axx: %+v, %v", n, err)
	}
	// An upgrade replaces the file: the host reads its version again.
	next := installed + ".new"
	rewrite(t, next, script("0.10.0"))
	if err := os.Rename(next, installed); err != nil {
		t.Fatal(err)
	}
	n, err := f.Next(ctx)
	if err != nil || n == nil || *n != (host.Next{Version: "0.10.0", From: host.Installed}) {
		t.Fatalf("after an upgrade: %+v, %v", n, err)
	}
	if err := f.Ready(ctx, n); err != nil || f.bins["0.10.0"] != installed {
		t.Fatalf("ready: %q, %v", f.bins, err)
	}
	// The restart is the installed axx, which does all a start does: it is
	// told nothing of the axx that ran before.
	t.Setenv(version.EnvLauncher, "0.9.0")
	t.Setenv(envLauncherPath, installed)
	var env []string
	old := hostExec
	hostExec = func(_ string, e []string) error {
		env = e
		return nil
	}
	t.Cleanup(func() { hostExec = old })
	if err := f.restart(dir, n); err != nil {
		t.Fatal(err)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, version.EnvLauncher+"=") || strings.HasPrefix(kv, envLauncherPath+"=") {
			t.Errorf("the installed axx is told %s", kv)
		}
	}

	// A host that restarted on that version and still is not on it (a build
	// that reports another) does not restart again and again: it says so.
	f.movedTo = "0.10.0"
	if n, err := f.Next(ctx); n != nil || err == nil || !strings.Contains(err.Error(), "it restarted on axx 0.10.0, and runs on axx 0.9.0") {
		t.Fatalf("restarted on it already: %+v, %v", n, err)
	}
	f.movedTo = ""
	// A build from source has no version of its own: it compares the
	// installed axx with what that was when it started the host.
	src := follower(t, "0.0.0-dev", nil)
	src.info.Channel, src.info.Launcher, src.installed = "dev", "0.10.0", installed
	if n, err := src.Next(ctx); n != nil || err != nil {
		t.Fatalf("a build from source, started by the installed axx: %+v, %v", n, err)
	}

	// A project that names a version follows that, whatever is installed.
	rewrite(t, filepath.Join(dir, "axx.yaml"), "axx: 0.9.0\n")
	if n, err := f.Next(ctx); n != nil || err != nil {
		t.Fatalf("a project that names the host's version: %+v, %v", n, err)
	}
	// A host started through an axx from before it said where it is cannot
	// go back to it when the project stops naming a version: it says so.
	rewrite(t, filepath.Join(dir, "axx.yaml"), "services: {}\n")
	f.installed, f.info.Launcher = "", "0.8.0"
	if n, err := f.Next(ctx); n != nil || err == nil || !strings.Contains(err.Error(), "restart the host to run on the installed axx") {
		t.Fatalf("no version and no installed axx known: %+v, %v", n, err)
	}
}

func TestHostStartsNamesTheProjectsPacksAndHooks(t *testing.T) {
	dir, _ := pinnedProject(t, `hooks:
  commands:
    - before: host
      command: ./scripts/tunnel.sh up
    - before: run
      command: ./scripts/other.sh
`)
	writeFiles(t, dir, map[string]string{
		"axx-packs.yaml":    "packs:\n  - rest\n",
		"scripts/tunnel.sh": "#!/bin/sh\necho tunnel\n",
		"scripts/other.sh":  "#!/bin/sh\necho other\n",
	})
	f := follower(t, "0.9.0", nil)
	got, err := f.Starts()
	if err != nil {
		t.Fatal(err)
	}
	want := "# axx-packs.yaml: the packs the host's axx is built with\npacks:\n  - rest\n" +
		"\n# axx.yaml runs before `axx host`\n./scripts/tunnel.sh up\n" +
		"\n# scripts/tunnel.sh\n#!/bin/sh\necho tunnel\n"
	if got != want {
		t.Errorf("starts:\n%s\nwant:\n%s", got, want)
	}
}

func TestLauncherPath(t *testing.T) {
	// The installed axx tells where it is; an axx it started passes that
	// on; one started by an axx from before it told has nothing to pass.
	t.Setenv(envLauncherPath, "")
	if got := launcherPath(version.Info{Version: "0.9.0"}); got == "" || got != ownPath() {
		t.Errorf("the installed axx: %q", got)
	}
	if got := launcherPath(version.Info{Version: "0.9.0", Launcher: "0.8.0"}); got != "" {
		t.Errorf("started by an axx that did not say: %q", got)
	}
	t.Setenv(envLauncherPath, "/opt/axx/bin/axx")
	if got := launcherPath(version.Info{Version: "0.9.0", Launcher: "0.8.0"}); got != "/opt/axx/bin/axx" {
		t.Errorf("told: %q", got)
	}
	if got := withoutEnv([]string{"A=1", "AB=2", "B=3"}, "A"); !slices.Equal(got, []string{"AB=2", "B=3"}) {
		t.Errorf("withoutEnv: %q", got)
	}
}

func TestAfterCommandEndsTheCommand(t *testing.T) {
	// What a command leaves to do after its hooks runs with the error it
	// ended with, and gives the one axx ends with: the host's restart.
	var out bytes.Buffer
	var got []error
	app := &App{Stdout: &out, Stderr: &out, Env: func(string) string { return "" }}
	app.afterCommand = func(err error) error {
		got = append(got, err)
		return err
	}
	if code := Run(context.Background(), app, []string{"version"}); code != 0 || len(got) != 1 || got[0] != nil {
		t.Errorf("after a command that succeeded: code %d, %v", code, got)
	}
	if code := Run(context.Background(), app, []string{"no-such-command"}); code == 0 || len(got) != 2 || got[1] == nil {
		t.Errorf("after a command that failed: code %d, %v", code, got)
	}
}
