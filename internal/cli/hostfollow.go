package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/host"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/version"
)

const (
	// envLauncherPath tells a project's version or build of axx where the
	// installed axx that started it is: the axx host restarts on it when it
	// was upgraded.
	envLauncherPath = "AXX_LAUNCHER_PATH"
	// envHostMoved names the project whose host this process restarts, on
	// another version of axx: an axx that fails before it serves takes the
	// host's announcement back (host.Abandon).
	envHostMoved = "AXX_HOST_MOVED"
	// envHostMovedTo is the version it restarts on: a host that restarted
	// on a version and is told to again does not, whatever it runs on.
	envHostMovedTo = "AXX_HOST_MOVED_TO"
)

// launcherPath is where the installed axx is, for the axx this one starts:
// what it was told, or its own path when it is the installed axx itself.
func launcherPath(info version.Info) string {
	if p := os.Getenv(envLauncherPath); p != "" || info.Launcher != "" {
		return p // told, or started by an axx from before it told
	}
	return ownPath()
}

// ownPath is the path to run this axx by, which still works after an
// upgrade replaced it.
func ownPath() string {
	invoked, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err := filepath.EvalSymlinks(invoked)
	if err != nil {
		return invoked
	}
	return stableAxxPath(invoked, exe)
}

// hostFollower keeps an axx host on the axx its project runs on
// (host.Follower): the version axx.yaml names, else the installed axx.
type hostFollower struct {
	app  *App
	cf   *configFlags
	info version.Info
	// installed is the installed axx's path ("" when the axx that started
	// this one did not say), and packed whether this axx is a build of it
	// or of a release, with the project's packs.
	installed string
	// The installed axx's file as last read, and the version it gave.
	seen        os.FileInfo
	seenVersion string
	// bins are the axx to run for each version made ready.
	bins map[string]string
	// movedTo is the version this host restarted on, when it is a host
	// that did.
	movedTo string
}

func (a *App) newHostFollower(cf *configFlags) *hostFollower {
	info := version.Get()
	f := &hostFollower{app: a, cf: cf, info: info, bins: map[string]string{}, movedTo: os.Getenv(envHostMovedTo)}
	if packBuilt || info.Launcher != "" {
		f.installed = os.Getenv(envLauncherPath)
	} else {
		f.installed = ownPath()
	}
	return f
}

// Next is the axx a start here would run on now, when it is not this one:
// the version the project names, as the installed axx reads it before it
// hands over; without one, the installed axx.
func (f *hostFollower) Next(ctx context.Context) (*host.Next, error) {
	next := func(v, on, from string) (*host.Next, error) {
		if v == on {
			return nil, nil
		}
		if v == f.movedTo {
			// It restarted on that version, and here it is: not again.
			return nil, fmt.Errorf("it restarted on axx %s, and runs on axx %s; restart it yourself if that is wrong", v, on)
		}
		return &host.Next{
			Version: v, From: from,
			Older:  selfupdate.Compare(v, on) < 0,
			ByHand: runtime.GOOS == "windows",
		}, nil
	}
	if f.info.Channel != "dev" { // a development build ignores the project's version
		want, err := f.app.projectVersion(f.app.Config)
		if err != nil {
			return nil, err
		}
		if want != "" {
			from := "axx.yaml"
			if f.app.Env(envUseVersion) != "" {
				from = envUseVersion
			}
			return next(want, f.info.Version, from)
		}
	}
	// Without a version of the project's, the installed axx: its version
	// now against the one this host runs on. A build from source has no
	// version of its own to compare, so the installed axx's as it started.
	on := f.info.Version
	if f.info.Channel == "dev" && f.info.Launcher != "" {
		on = f.info.Launcher
	}
	if f.installed == "" {
		if f.info.Launcher != "" && f.info.Launcher != f.info.Version {
			return nil, errors.New("the project no longer names a version of axx; restart the host to run on the installed axx")
		}
		return nil, nil
	}
	st, err := os.Stat(f.installed)
	if err != nil {
		return nil, nil //nolint:nilerr // being replaced: read the next time
	}
	if f.seen == nil || !os.SameFile(st, f.seen) || !st.ModTime().Equal(f.seen.ModTime()) || st.Size() != f.seen.Size() {
		v, err := installedVersion(ctx, f.installed)
		if err != nil {
			return nil, nil //nolint:nilerr // half written: read the next time
		}
		f.seen, f.seenVersion = st, v
	}
	return next(f.seenVersion, on, host.Installed)
}

// Ready gets the release the project names onto this machine, as its first
// command does; the installed axx is there.
func (f *hostFollower) Ready(ctx context.Context, n *host.Next) error {
	if n.From == host.Installed {
		f.bins[n.Version] = f.installed
		return nil
	}
	bin, err := f.app.releaseBinary(ctx, n.Version)
	if err != nil {
		return err
	}
	f.bins[n.Version] = bin
	return nil
}

// Starts is what a start of the host runs of the project's own besides its
// services: its packs, and axx.yaml's commands around `axx host`.
func (f *hostFollower) Starts() (string, error) {
	props, err := f.cf.properties()
	if err != nil {
		return "", err
	}
	cfg, err := config.Load(config.LoadOptions{Path: f.app.Config, Profile: f.cf.profile, Properties: props, Settings: f.cf.settings})
	if err != nil {
		return "", err
	}
	var hooks []config.CommandHook
	if f.app.root != nil {
		own, err := configHooks(f.app.root, cfg, "host", false)
		if err != nil {
			return "", err
		}
		for _, h := range own {
			hooks = append(hooks, h.CommandHook)
		}
	}
	return host.Starts(cfg.Dir, hooks)
}

// restart replaces this process with the axx made ready for n, started as
// this host was: on the release the project names, which prepares the
// project's packs and serves, or on the installed axx, which does all a
// start does. It returns only when that failed.
func (f *hostFollower) restart(dir string, n *host.Next) error {
	bin := f.bins[n.Version]
	if bin == "" {
		return errors.New("axx " + n.Version + " is not ready")
	}
	env := withEnv(withEnv(os.Environ(), envHostMoved, dir), envHostMovedTo, n.Version)
	if n.From == host.Installed {
		env = withoutEnv(withoutEnv(env, version.EnvLauncher), envLauncherPath)
	} else {
		launcher := f.info.Launcher
		if launcher == "" {
			launcher = f.info.Version
		}
		env = withEnv(env, version.EnvLauncher, launcher)
		if f.installed != "" {
			env = withEnv(env, envLauncherPath, f.installed)
		}
	}
	return hostExec(bin, env)
}

// hostExec replaces this process with an axx. It is a variable so that
// tests can see what a restart runs.
var hostExec = execBinary

// withoutEnv is env without key.
func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if len(kv) <= len(key) || kv[:len(key)+1] != key+"=" {
			out = append(out, kv)
		}
	}
	return out
}
