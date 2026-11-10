//go:build darwin

package desktopmacos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/nimbusxr/axx/internal/launcher"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/version"
)

// axx's launcher (ADR 0016): the app macOS asks for the permissions axx needs
// to drive Mac apps, whatever terminal, IDE or CI agent started axx. A person
// allows it once.
const (
	// launcherEnv is "off", to run without the launcher, or the path of its
	// axx.app.
	launcherEnv = "AXX_MACOS_LAUNCHER"
	// underLauncherEnv is set, to the launcher's app, for the command axx
	// runs under it, which does not run itself again.
	underLauncherEnv = "AXX_UNDER_MACOS_LAUNCHER"
	// launcherRequirement is the launcher's: its identifier, signed with
	// NimbusXR's Developer ID. macOS keeps a person's grants by it.
	launcherRequirement = `identifier "us.nimbusxr.axx.launcher" and anchor apple generic and ` +
		`certificate 1[field.1.2.840.113635.100.6.2.6] and certificate leaf[field.1.2.840.113635.100.6.1.13] and ` +
		`certificate leaf[subject.OU] = "2SAF8A2PAY"`
)

// relaunched are the commands that drive Mac apps: a run, the MCP server
// (an agent's steps_try runs steps in its process) and doctor, which checks
// the launcher's permissions.
var relaunched = []string{"run", "mcp", "doctor"}

// Relaunch runs the command again under axx's launcher, when there is one
// and the command drives Mac apps.
func (pack) Relaunch(_ context.Context, command string, args []string) (int, bool, error) {
	if !slices.Contains(relaunched, command) || os.Getenv(underLauncherEnv) != "" {
		return 0, false, nil
	}
	app, err := launcherApp()
	if err != nil || app == "" {
		return 0, false, err
	}
	self, err := os.Executable()
	if err != nil {
		return 0, false, err
	}
	exe := filepath.Join(app, "Contents", "MacOS", "axx-launcher")
	env := append(os.Environ(), underLauncherEnv+"="+app)
	pid, err := launcher.Start(exe, append([]string{exe, self}, args...), env)
	if err != nil {
		// A macOS without the function the handoff uses runs axx as it did
		// before the launcher; `axx doctor` says so.
		fmt.Fprintf(os.Stderr, "axx: running without axx's launcher: %v\n", err)
		return 0, false, nil
	}
	// A stop signal for axx (an IDE's stop button) is for the command run
	// again; a terminal's Ctrl-C reaches it on its own as well.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case s := <-stop:
				if sig, ok := s.(syscall.Signal); ok {
					_ = syscall.Kill(pid, sig)
				}
			case <-done:
				return
			}
		}
	}()
	code, err := launcher.Wait(pid)
	return code, true, err
}

// launcherApp is the launcher's axx.app: the one AXX_MACOS_LAUNCHER names, or the
// one in axx's cache, downloaded from the running axx's release the first
// time, or "" for none (AXX_MACOS_LAUNCHER=off, or no release's).
func launcherApp() (string, error) {
	switch v := os.Getenv(launcherEnv); {
	case v == "off":
		return "", nil
	case v != "":
		app, err := filepath.Abs(v)
		if err != nil {
			return "", err
		}
		if err := verifyLauncher(app); err != nil {
			return "", fmt.Errorf("%s=%s: %w", launcherEnv, v, err)
		}
		return app, nil
	}
	app := cachedLauncher()
	if app == "" {
		return "", nil
	}
	if !exists(app) {
		if err := downloadLauncher(app, version.Get()); err != nil {
			fmt.Fprintf(os.Stderr, "axx: running without axx's launcher: %v\n", err)
		}
		if !exists(app) {
			return "", nil
		}
	}
	if err := verifyLauncher(app); err != nil {
		return "", fmt.Errorf("axx's cache has %s: %w", app, err)
	}
	return app, nil
}

// cachedLauncher is where axx keeps the launcher it downloads.
func cachedLauncher() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "axx", "launcher", "axx.app")
}

// launcherURL is where a release's launcher is, beside its archives: under
// base, GitHub's releases or the mirror AXX_DOWNLOAD_URL names.
func launcherURL(base, v string) string {
	return fmt.Sprintf("%s/v%s/axx-launcher_%s_darwin.zip", base, v, v)
}

// downloadLauncher puts the launcher of the running axx's release where app
// is, in axx's cache, once it has checked NimbusXR's signature. An axx that is
// no release (built from source) has none, nor does a release from before the
// launcher, which it notes so as not to ask again; a download that fails is
// tried again an hour later.
func downloadLauncher(app string, info version.Info) error {
	if info.Channel == "dev" {
		return nil
	}
	dir := filepath.Dir(app)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	none, tried := filepath.Join(dir, "none-"+info.Version), filepath.Join(dir, "tried")
	if exists(none) {
		return nil
	}
	if st, err := os.Stat(tried); err == nil && time.Since(st.ModTime()) < time.Hour {
		return nil
	}
	note := func(path string) { _ = os.WriteFile(path, nil, 0o644) }
	url := launcherURL(selfupdate.New().Downloads, info.Version)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		note(tried)
		return fmt.Errorf("cannot download it from %s: %w", url, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		note(none)
		return nil
	default:
		note(tried)
		return fmt.Errorf("cannot download it from %s: %s", url, resp.Status)
	}
	work, err := os.MkdirTemp(dir, ".download-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	zip := filepath.Join(work, "axx.zip")
	f, err := os.Create(zip)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 64<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		note(tried)
		return fmt.Errorf("cannot download it from %s: %w", url, err)
	}
	if out, err := exec.CommandContext(ctx, "ditto", "-x", "-k", zip, work).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot unpack %s: %w %s", url, err, strings.TrimSpace(string(out)))
	}
	got := filepath.Join(work, "axx.app")
	if err := verifyLauncher(got); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	// Another axx that downloaded it as well has put it there first.
	if err := os.Rename(got, app); err != nil && !exists(app) {
		return err
	}
	return nil
}

// verifyLauncher checks that app is axx's launcher, signed by NimbusXR.
func verifyLauncher(app string) error {
	out, err := exec.CommandContext(context.Background(), "codesign", "--verify", "--strict", "-R="+launcherRequirement, app).CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("not axx's launcher signed by NimbusXR: %s", strings.TrimSpace(string(out)))
		}
		return err
	}
	return nil
}

// underLauncher is the launcher's app when axx runs under it, and macOS
// holds it responsible for axx; "" otherwise.
func underLauncher() string {
	app := os.Getenv(underLauncherEnv)
	if app == "" {
		return ""
	}
	if r, err := launcher.ResponsibleFor(os.Getpid()); err != nil || r == os.Getpid() {
		return ""
	}
	return app
}
