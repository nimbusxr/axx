package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/filelock"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/version"
)

const (
	codeBadVersion = "AXX-E0020"
	codeNoVersion  = "AXX-E0021"
	// envUseVersion runs on another version than the project names, or names one
	// for a project that does not. (AXX_VERSION is the install scripts'.)
	envUseVersion = "AXX_USE_VERSION"
	// envAutoDownload, set to 0, keeps axx from downloading the project's version
	// by itself: `axx install` does, when asked.
	envAutoDownload = "AXX_AUTO_DOWNLOAD"
	// lastBeforePins is the last release that refuses an `axx:` line in axx.yaml.
	lastBeforePins = "0.4.0"
)

// installedAxxCommands stay on the installed axx: they are about it, or about
// moving the project's version, or are asked for at every Tab of a shell.
var installedAxxCommands = []string{"upgrade", "install", "version", "completion", "__complete", "__completeNoDesc"}

// peekedArgs is what deciding on a version needs from a command line, before
// anything parses it: a newer version can have flags and commands this axx
// does not know.
type peekedArgs struct {
	config  string
	command string
	version bool // axx --version
	project bool // axx lsp --project
}

func peekArgs(args []string) peekedArgs {
	var p peekedArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return p
		case a == "--config" || a == "-c":
			if i+1 < len(args) {
				i++
				p.config = args[i]
			}
		case strings.HasPrefix(a, "--config="):
			p.config = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			p.config = strings.TrimPrefix(a, "-c=")
		case strings.HasPrefix(a, "-c") && !strings.HasPrefix(a, "--"):
			p.config = strings.TrimPrefix(a, "-c")
		case a == "--version" && p.command == "":
			p.version = true
		case a == "--project":
			p.project = true
		case p.command == "" && !strings.HasPrefix(a, "-"):
			p.command = a
		}
	}
	return p
}

// handOver replaces this process with a release of axx. It is a variable so that
// tests can see it asked for.
var handOver = func(bin string) error {
	return execBinary(bin, withEnv(os.Environ(), version.EnvLauncher, version.Get().Version))
}

// useProjectVersion hands over to the version of axx the project names, before
// anything else runs. That version does everything from there, its pack build
// included, with its own list of packs and its own Go: this axx knows only how
// to read the pin and get the release.
func (a *App) useProjectVersion(ctx context.Context, args []string) error {
	if packBuilt {
		return nil
	}
	info := version.Get()
	if info.Channel == "dev" {
		return nil // a build of a commit or of local source: `axx doctor` says it ignores the pin
	}
	peek := peekArgs(args)
	if peek.version || slices.Contains(installedAxxCommands, peek.command) {
		return nil
	}
	if peek.command == "lsp" && peek.config == "" && !peek.project {
		return nil // the router: the server it starts for each project hands over for that project
	}
	want, err := a.projectVersion(peek.config)
	if err != nil {
		return err
	}
	if want == "" || want == info.Version {
		return nil
	}
	bin, err := a.releaseBinary(ctx, want)
	if err != nil {
		if peek.command == "" {
			return nil //nolint:nilerr // help for axx itself: this axx's
		}
		return err
	}
	return handOver(bin)
}

// projectVersion is the version to run on: AXX_USE_VERSION's, or the project's;
// none when neither names one.
func (a *App) projectVersion(configPath string) (string, error) {
	pin, err := config.ReadPin(configPath)
	if err != nil {
		return "", err
	}
	if use := a.Env(envUseVersion); use != "" {
		if !config.ValidPin(use) {
			return "", axxerr.New(codeBadVersion, exitcode.Usage, "%s=%s is not a version of axx", envUseVersion, use).
				WithHint("name one release, like 0.5.0: %s", releasesURL)
		}
		if pin.File != "" {
			return use, checkPinnable(use)
		}
		return use, nil
	}
	if pin.File == "" {
		return "", nil
	}
	if !config.ValidPin(pin.Version) {
		return "", axxerr.New(codeBadVersion, exitcode.Usage, "%s: `axx: %s` is not a version of axx", pin.File, pin.Version).
			WithHint("name one release in full, like 0.5.0: %s", releasesURL)
	}
	return pin.Version, checkPinnable(pin.Version)
}

// checkPinnable refuses a version that would refuse the line naming it.
func checkPinnable(v string) error {
	if selfupdate.Compare(v, lastBeforePins) > 0 {
		return nil
	}
	return axxerr.New(codeBadVersion, exitcode.Usage, "axx %s is from before projects could name their version: it refuses the `axx:` line in axx.yaml", v).
		WithHint("name a version after %s. To run on %s itself, take the line out of axx.yaml and install it: `curl -fsSL https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.sh | AXX_VERSION=%s sh`, or `version: %s` for setup-axx", lastBeforePins, v, v, v)
}

// pinnableVersion is the version of this axx that a new project can name.
func pinnableVersion(info version.Info) string {
	if info.Channel == "dev" || !config.ValidPin(info.Version) || checkPinnable(info.Version) != nil {
		return ""
	}
	return info.Version
}

// versionsDir is where the releases axx downloads for projects are: safe to
// delete, as they come back when a project asks.
func versionsDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "axx", "versions"), nil
}

func isRegularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// releaseBinary is the axx of release v on this machine, downloaded if this is
// the first time and AXX_AUTO_DOWNLOAD allows it.
func (a *App) releaseBinary(ctx context.Context, v string) (string, error) {
	dir, err := versionsDir()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, v, selfupdate.BinaryName(runtime.GOOS))
	if isRegularFile(bin) {
		return bin, nil
	}
	switch a.Env(envAutoDownload) {
	case "0", "false", "no", "off":
		return "", axxerr.New(codeNoVersion, exitcode.Environment, "this project runs on axx %s, which this machine has not got, and %s=%s keeps axx from downloading it", v, envAutoDownload, a.Env(envAutoDownload)).
			WithHint("`axx install` downloads it")
	}
	env, err := newUpgradeEnv()
	if err != nil {
		return "", err
	}
	return a.downloadRelease(ctx, env, v, fmt.Sprintf("axx: this project runs on axx %s: downloading it (once; kept for later runs)", v))
}

// downloadRelease puts release v in the versions directory, checked, and gives its
// axx, saying so when it has to download. One axx downloads a version at a time (a
// run and the language server often start together); the others wait for it and use
// what it got.
func (a *App) downloadRelease(ctx context.Context, env *upgradeEnv, v, announce string) (string, error) {
	dir, err := versionsDir()
	if err != nil {
		return "", err
	}
	versionDir := filepath.Join(dir, v)
	bin := filepath.Join(versionDir, selfupdate.BinaryName(runtime.GOOS))
	unlock, err := filelock.Lock(ctx, versionDir+".lock", func() {
		fmt.Fprintf(a.Stderr, "axx: waiting for another axx downloading %s\n", v)
	})
	if err != nil {
		return "", err
	}
	defer unlock()
	if isRegularFile(bin) {
		return bin, nil
	}
	fmt.Fprintln(a.Stderr, announce)
	tmp, err := os.MkdirTemp(dir, ".download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	fetched, err := env.client.Fetch(ctx, "v"+v, runtime.GOOS, runtime.GOARCH, tmp)
	switch {
	case errors.Is(err, selfupdate.ErrNotFound):
		return "", axxerr.New(codeBadVersion, exitcode.Usage, "axx has no release %s for %s/%s", v, runtime.GOOS, runtime.GOARCH).
			WithHint("its releases are at %s", releasesURL)
	case err != nil:
		return "", axxerr.New(codeNoVersion, exitcode.Environment, "cannot download axx %s: %v", v, err).
			WithHint("axx gets a release from github.com, once. Check that this machine reaches it, name a mirror with %s, or run `axx install` where it can", selfupdate.EnvDownloadURL)
	}
	if err := checkRuns(ctx, fetched.Binary, v); err != nil {
		return "", axxerr.New(codeNoVersion, exitcode.Environment, "the download of axx %s does not run here: %v", v, err).
			WithHint("try again, and if it stays so, tell us: https://github.com/nimbusxr/axx/issues")
	}
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(fetched.Binary, bin); err != nil {
		return "", err
	}
	return bin, nil
}

// pinNote says, for `axx doctor`, when this axx is not the project's version.
func (a *App) pinNote(info version.Info) string {
	pin, err := config.ReadPin(a.Config)
	if err != nil || pin.File == "" || pin.Version == info.Version {
		return ""
	}
	switch {
	case info.Channel == "dev":
		return fmt.Sprintf(", a development build, which ignores this project's axx: %s", pin.Version)
	case a.Env(envUseVersion) != "":
		return fmt.Sprintf(", as %s says, not this project's %s", envUseVersion, pin.Version)
	}
	return ""
}

// InstallResult is what `axx install` did.
type InstallResult struct {
	Version string `json:"version"`
	Path    string `json:"path"`
}

func newInstallCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "install [version]",
		Short: "Download the axx version this project runs on, or the one you name, for later runs",
		Long: `Download a release of axx next to the others axx keeps, checked against the
release's checksums, so that a project on that version finds it: the one this
project's axx.yaml names, or the version you give.

A project does this by itself on its first command. This is for a machine with
AXX_AUTO_DOWNLOAD=0, or for preparing an image or a machine that will be offline.
It does not change which axx is installed: that is ` + "`axx upgrade`" + `.`,
		Args: wrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.install(cmd.Context(), args)
		},
	}
}

func (a *App) install(ctx context.Context, args []string) error {
	var v string
	if len(args) == 1 {
		v = strings.TrimPrefix(args[0], "v")
		if !config.ValidPin(v) {
			return axxerr.New(codeBadVersion, exitcode.Usage, "%s is not a version of axx", args[0]).
				WithHint("name one release, like 0.5.0: %s", releasesURL)
		}
	} else {
		var err error
		if v, err = a.projectVersion(a.Config); err != nil {
			return err
		}
		if v == "" {
			return axxerr.New(codeBadVersion, exitcode.Usage, "this project names no version of axx").
				WithHint("name one: `axx install 0.5.0`, or `axx: 0.5.0` in axx.yaml")
		}
	}
	env, err := newUpgradeEnv()
	if err != nil {
		return err
	}
	bin, err := a.downloadRelease(ctx, env, v, "axx: downloading axx "+v)
	if err != nil {
		return err
	}
	res := InstallResult{Version: v, Path: bin}
	return a.Emit(res, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "axx %s is ready (%s)\n", res.Version, res.Path)
		return err
	})
}
