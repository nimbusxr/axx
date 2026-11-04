package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/skills"
	"github.com/nimbusxr/axx/internal/version"
)

const (
	codeUpgradeFailed = "AXX-E0018"
	codeCannotUpgrade = "AXX-E0019"
	changelogURL      = "https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md"
	releasesURL       = "https://github.com/nimbusxr/axx/releases"
)

// upgradeEnv is what `axx upgrade` works with: where releases come from, and the
// axx it replaces.
type upgradeEnv struct {
	client  *selfupdate.Client
	exe     string // the axx that runs, its links followed
	invoked string // the path to run it by, which still works after Homebrew moves it
	current string
	method  selfupdate.Method
	// attest checks a release archive's build provenance, when it can: whether
	// it could, and whether the archive passed.
	attest func(ctx context.Context, archive string) (bool, bool)
}

// newUpgradeEnv is a function so that tests can stand in for GitHub and for the
// installed axx.
var newUpgradeEnv = func() (*upgradeEnv, error) {
	invoked, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exe, err := filepath.EvalSymlinks(invoked)
	if err != nil {
		return nil, err
	}
	current := version.Get().Version
	return &upgradeEnv{
		client: selfupdate.New(), exe: exe, invoked: stableAxxPath(invoked, exe), current: current,
		method: selfupdate.Detect(exe, version.Version, current), attest: ghAttest,
	}, nil
}

// stableAxxPath is the path to run axx by after an upgrade. On Linux, the path a
// program finds for itself has its links followed, so for Homebrew's axx it is a
// version's directory, which `brew upgrade` removes: Homebrew's own link stays.
func stableAxxPath(invoked, exe string) string {
	link := selfupdate.HomebrewLink(exe)
	if link == "" || !isRegularFile(link) {
		return invoked
	}
	return link
}

// ghAttest verifies an archive's build provenance with gh, as the install
// scripts do; without gh, or for gh's own reasons, it cannot say.
func ghAttest(ctx context.Context, archive string) (bool, bool) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return true, exec.CommandContext(ctx, gh, "attestation", "verify", archive, "--repo", selfupdate.Repo).Run() == nil
}

// UpgradeChange is what one release changed.
type UpgradeChange struct {
	Version  string               `json:"version"`
	Date     string               `json:"date,omitempty"`
	Sections []selfupdate.Section `json:"sections,omitempty"`
}

// UpgradeResult is what `axx upgrade` did.
type UpgradeResult struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Path     string `json:"path"`
	Method   string `json:"method"`
	UpToDate bool   `json:"upToDate,omitempty"`
	// Available: --check, which changes nothing, found a newer release.
	Available bool            `json:"available,omitempty"`
	Upgraded  bool            `json:"upgraded,omitempty"`
	Changes   []UpgradeChange `json:"changes,omitempty"`
	// Skills are the skills it updated, by scope; SkillsProblem says why it could not.
	Skills        []string `json:"skills,omitempty"`
	SkillsProblem string   `json:"skillsProblem,omitempty"`
	// Project: From and To are the versions this project's axx.yaml names, and Path
	// is that file.
	Project bool `json:"project,omitempty"`
	// Checks are `axx validate` and `axx lint` on the version the project moved to.
	Checks []UpgradeCheck `json:"checks,omitempty"`
	// Installed is what became of the installed axx, which a project's move takes
	// along when it is older; InstalledProblem says why it could not.
	Installed        *UpgradeResult `json:"installed,omitempty"`
	InstalledProblem string         `json:"installedProblem,omitempty"`
}

// UpgradeCheck is one check of a project on the version it moved to.
type UpgradeCheck struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Output  string `json:"output,omitempty"`
}

func newUpgradeCmd(app *App) *cobra.Command {
	var target string
	var check bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade axx to its newest release, say what changed, and update the skills",
		Long: `Upgrade axx to its newest release, or to the version --version names, older ones
too. It says what changed between the versions, breaking changes first, and then
updates the agent skills of this project and your home directory, where they are
installed.

In a project whose axx.yaml names its version (` + "`axx: 0.5.0`" + `), it moves that line,
checks the project on the new version with ` + "`axx validate`" + ` and ` + "`axx lint`" + `, and leaves
the line for you to commit. An installed axx older than the newest release comes
along; --version moves only the project. Outside a project, it upgrades only the
installed axx.

An axx from the install scripts or a release archive replaces itself, after
checking the download against the release's checksums. Homebrew's is upgraded with
` + "`brew`" + `, and ` + "`go install`" + `'s with ` + "`go install`" + `. A project's version comes from GitHub
however axx was installed, as its Homebrew tap keeps only the newest. GITHUB_TOKEN, if it is
set, avoids GitHub's rate limit.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.upgrade(cmd.Context(), target, check)
		},
	}
	cmd.Flags().StringVar(&target, "version", "", "the version to go to, like 0.4.0 or nightly, instead of the newest")
	cmd.Flags().BoolVar(&check, "check", false, "say what a newer release would change, without installing it")
	return cmd
}

// upgradeUnreachable is a failure to read from GitHub.
func upgradeUnreachable(format string, args ...any) *axxerr.Error {
	return axxerr.New(codeUpgradeFailed, exitcode.Environment, format, args...).
		WithHint("axx talks to api.github.com and github.com: check that this machine reaches them. A version you know goes without the list of releases: `axx upgrade --version <version>`; GITHUB_TOKEN avoids GitHub's rate limit")
}

// upgradeFailed is any other failure, which leaves axx as it was.
func upgradeFailed(hint, format string, args ...any) *axxerr.Error {
	return axxerr.New(codeUpgradeFailed, exitcode.Environment, format, args...).WithHint("%s", hint)
}

const upgradeReportHint = "axx is as it was. Try again, and if it stays so, tell us: https://github.com/nimbusxr/axx/issues"

// upgrade moves the project's version when it names one, and the installed axx
// when it does not.
func (a *App) upgrade(ctx context.Context, target string, check bool) error {
	if packBuilt {
		return axxerr.New(codeCannotUpgrade, exitcode.Usage, "this is a project's build of axx, with its packs, which axx makes for itself").
			WithHint("run the axx you installed: `axx upgrade`")
	}
	env, err := newUpgradeEnv()
	if err != nil {
		return err
	}
	pin, err := config.ReadPin(a.Config)
	if err != nil {
		return err
	}
	if pin.File != "" {
		return a.upgradeProject(ctx, env, pin, target, check)
	}
	res, err := a.upgradeInstalled(ctx, env, target, check)
	if err != nil {
		return err
	}
	if res.Upgraded {
		a.upgradeSkills(ctx, env.invoked, res, true, true)
	}
	return a.Emit(res, func(w io.Writer) error { return renderUpgrade(w, res) })
}

// upgradeInstalled finds the version to go to, says what it changes and takes the
// installed axx there.
func (a *App) upgradeInstalled(ctx context.Context, env *upgradeEnv, target string, check bool) (*UpgradeResult, error) {
	switch env.method {
	case selfupdate.Source:
		return nil, axxerr.New(codeCannotUpgrade, exitcode.Usage, "this axx (%s) was built from source: it upgrades with its checkout", env.current).
			WithHint("build it again, or install a release: %s", "https://github.com/nimbusxr/axx/blob/main/docs/src/content/docs/guides/install.md")
	case selfupdate.Prerelease:
		if target == "" {
			return nil, axxerr.New(codeCannotUpgrade, exitcode.Usage, "this axx (%s) is not a release, so no release is newer than it", env.current).
				WithHint("say which you want: `axx upgrade --version nightly`, or `axx upgrade --version <version>`, from %s", releasesURL)
		}
	case selfupdate.Homebrew:
		if target != "" {
			return nil, axxerr.New(codeCannotUpgrade, exitcode.Usage, "Homebrew installed this axx, and axx's tap keeps only the newest version").
				WithHint("for another version, install it next to Homebrew's, first on your PATH: `curl -fsSL https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.sh | AXX_VERSION=%s AXX_INSTALL_DIR=~/.local/bin sh`", strings.TrimPrefix(target, "v"))
		}
	}

	res := &UpgradeResult{From: env.current, Path: env.exe, Method: string(env.method)}
	releases, listErr := env.client.Releases(ctx)
	tag, to, err := upgradeTarget(releases, listErr, target)
	if err != nil {
		return nil, err
	}
	res.To = to
	if tag != selfupdate.Nightly {
		if to == env.current || (target == "" && selfupdate.Compare(to, env.current) < 0) {
			res.To, res.UpToDate = env.current, true
			return res, nil
		}
		for _, r := range selfupdate.Between(releases, env.current, to) {
			res.Changes = append(res.Changes, upgradeChange(r))
		}
	}
	if check {
		res.Available = true
		return res, nil
	}

	switch env.method {
	case selfupdate.Homebrew:
		err = a.upgradeWithBrew(ctx, env, res)
	case selfupdate.GoInstall:
		err = a.upgradeWithGo(ctx, tag)
		res.Upgraded = err == nil
	default:
		err = a.replaceSelf(ctx, env, tag, res)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// upgradeProject moves the version the project's axx.yaml names: it gets the
// release first, so that a version that cannot be had leaves the line as it was.
func (a *App) upgradeProject(ctx context.Context, env *upgradeEnv, pin config.Pin, target string, check bool) error {
	res := &UpgradeResult{From: pin.Version, Path: pin.File, Method: "project", Project: true}
	emit := func() error { return a.Emit(res, func(w io.Writer) error { return renderUpgrade(w, res) }) }
	if target == selfupdate.Nightly {
		return axxerr.New(codeBadVersion, exitcode.Usage, "a project cannot run on the nightly: it changes every night").
			WithHint("name a release, or take the `axx:` line out of %s and install the nightly with `axx upgrade --version nightly`", pin.File)
	}
	releases, listErr := env.client.Releases(ctx)
	_, to, err := upgradeTarget(releases, listErr, target)
	if err != nil {
		return err
	}
	if !config.ValidPin(to) {
		return axxerr.New(codeBadVersion, exitcode.Usage, "%s is not a version of axx", to).WithHint("name one release, like 0.5.0: %s", releasesURL)
	}
	res.To = to
	if to == pin.Version || (target == "" && config.ValidPin(pin.Version) && selfupdate.Compare(to, pin.Version) < 0) {
		res.To, res.UpToDate = pin.Version, true
		if !check {
			a.bringInstalledAlong(ctx, env, target, to, res)
		}
		return emit()
	}
	if err := checkPinnable(to); err != nil {
		return err
	}
	if config.ValidPin(pin.Version) {
		for _, r := range selfupdate.Between(releases, pin.Version, to) {
			res.Changes = append(res.Changes, upgradeChange(r))
		}
	}
	if check {
		res.Available = true
		return emit()
	}

	bin, err := a.downloadRelease(ctx, env, to, "downloading axx "+to)
	if err != nil {
		return err
	}
	if err := config.SetPin(pin.File, to); err != nil {
		return upgradeFailed("change the `axx:` line yourself", "cannot move the version in %s: %v", pin.File, err)
	}
	res.Upgraded = true
	a.checkOnNewVersion(ctx, bin, to, res)
	a.upgradeSkills(ctx, bin, res, true, false)
	a.bringInstalledAlong(ctx, env, target, to, res)
	return emit()
}

// checkOnNewVersion runs what tells a person at once what the new version minds in
// their project. The project has moved whatever it finds: `axx upgrade --version`
// goes back.
func (a *App) checkOnNewVersion(ctx context.Context, bin, to string, res *UpgradeResult) {
	fmt.Fprintf(a.Stderr, "checking this project on axx %s\n", to)
	for _, name := range []string{"validate", "lint"} {
		args := []string{name, "--compact"}
		if a.Config != "" {
			args = append(args, "--config", a.Config)
		}
		out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
		check := UpgradeCheck{Command: "axx " + name, OK: err == nil}
		if err != nil {
			check.Output = strings.TrimSpace(string(out))
		}
		res.Checks = append(res.Checks, check)
	}
}

// bringInstalledAlong upgrades an installed axx older than the project now is. A
// failure here takes nothing back: an older axx runs the project all the same.
func (a *App) bringInstalledAlong(ctx context.Context, env *upgradeEnv, target, to string, res *UpgradeResult) {
	if target != "" || selfupdate.Compare(env.current, to) >= 0 {
		return
	}
	switch env.method {
	case selfupdate.Archive, selfupdate.Homebrew, selfupdate.GoInstall:
	default:
		return
	}
	installed, err := a.upgradeInstalled(ctx, env, "", false)
	if err != nil {
		res.InstalledProblem = fmt.Sprintf("the installed axx stays %s, which runs this project all the same: %v", env.current, err)
		return
	}
	installed.Changes = nil
	res.Installed = installed
	if installed.Upgraded {
		a.upgradeSkills(ctx, env.invoked, res, false, true)
	}
}

// upgradeTarget is the tag and the version to go to.
func upgradeTarget(releases []selfupdate.Release, listErr error, target string) (string, string, error) {
	switch {
	case target == selfupdate.Nightly:
		return selfupdate.Nightly, selfupdate.Nightly, nil
	case target != "":
		v := strings.TrimPrefix(target, "v")
		return "v" + v, v, nil
	case listErr != nil:
		return "", "", upgradeUnreachable("cannot read axx's releases on GitHub: %v", listErr)
	}
	newest := selfupdate.Newest(releases, runtime.GOOS, runtime.GOARCH)
	if newest == nil {
		return "", "", upgradeUnreachable("no release of axx has a build for %s/%s yet", runtime.GOOS, runtime.GOARCH)
	}
	return newest.Tag, newest.Version(), nil
}

func upgradeChange(r selfupdate.Release) UpgradeChange {
	c := UpgradeChange{Version: r.Version(), Sections: selfupdate.Notes(r.Body)}
	if !r.Published.IsZero() {
		c.Date = r.Published.Format(time.DateOnly)
	}
	return c
}

// replaceSelf downloads and checks a release's axx, makes sure it runs and puts
// it in place of this one.
func (a *App) replaceSelf(ctx context.Context, env *upgradeEnv, tag string, res *UpgradeResult) error {
	dir := filepath.Dir(env.exe)
	tmp, err := os.MkdirTemp(dir, ".axx-upgrade-")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return notWritable(env.exe, err)
		}
		return upgradeFailed(upgradeReportHint, "cannot make a temporary directory next to %s: %v", env.exe, err)
	}
	defer os.RemoveAll(tmp)
	fmt.Fprintf(a.Stderr, "downloading axx %s\n", res.To)
	fetched, err := env.client.Fetch(ctx, tag, runtime.GOOS, runtime.GOARCH, tmp)
	switch {
	case errors.Is(err, selfupdate.ErrNotFound):
		return axxerr.New(codeCannotUpgrade, exitcode.Usage, "axx has no release %s for %s/%s", res.To, runtime.GOOS, runtime.GOARCH).
			WithHint("its releases are at %s", releasesURL)
	case errors.Is(err, selfupdate.ErrChecksum):
		return upgradeFailed(upgradeReportHint, "%v", err)
	case err != nil:
		return upgradeUnreachable("%v", err)
	}
	fmt.Fprintln(a.Stderr, "checksum verified")
	if tag != selfupdate.Nightly && env.attest != nil {
		if could, ok := env.attest(ctx, fetched.Archive); could && ok {
			fmt.Fprintln(a.Stderr, "build provenance verified (gh attestation)")
		} else if could {
			fmt.Fprintln(a.Stderr, "note: could not verify build provenance with gh (continuing; checksum matched)")
		}
	}
	if err := checkRuns(ctx, fetched.Binary, res.To); err != nil {
		return upgradeFailed(upgradeReportHint, "the download does not run here: %v", err)
	}
	// Into the same directory, so that the rename is one step.
	fresh := filepath.Join(dir, ".axx-upgrade-new-"+filepath.Base(tmp))
	if err := os.Rename(fetched.Binary, fresh); err != nil {
		return upgradeFailed(upgradeReportHint, "%v", err)
	}
	defer os.Remove(fresh)
	if err := selfupdate.Replace(env.exe, fresh); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return notWritable(env.exe, err)
		}
		return upgradeFailed(upgradeReportHint, "cannot replace %s: %v", env.exe, err)
	}
	res.Upgraded = true
	return nil
}

func notWritable(exe string, err error) *axxerr.Error {
	return axxerr.New(codeUpgradeFailed, exitcode.Environment, "cannot replace %s: %v", exe, err).
		WithHint("run `axx upgrade` as the user who can write there, or install axx in a directory of yours with the install script's AXX_INSTALL_DIR. In a container, use a newer image")
}

// checkRuns makes sure a downloaded axx starts, and is the version it should be.
func checkRuns(ctx context.Context, binary, want string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version", "--json")
	cmd.Dir = filepath.Dir(binary)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	var env struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("its version: %w", err)
	}
	if want != selfupdate.Nightly && env.Data.Version != want {
		return fmt.Errorf("it says it is %q, not %q", env.Data.Version, want)
	}
	return nil
}

// upgradeWithBrew has Homebrew upgrade its axx. Its tap can lag behind a release
// for a while, which is no failure: the result says what axx is now.
func (a *App) upgradeWithBrew(ctx context.Context, env *upgradeEnv, res *UpgradeResult) error {
	// Quietly: after a while without one, an update lists every new formula there is.
	for _, args := range [][]string{{"update", "--quiet"}, {"upgrade", "--cask", "axx"}} {
		cmd := exec.CommandContext(ctx, "brew", args...)
		cmd.Stdout, cmd.Stderr = a.Stderr, a.Stderr
		if err := cmd.Run(); err != nil {
			return upgradeFailed("run it yourself to see why: `brew "+strings.Join(args, " ")+"`", "`brew %s`: %v", strings.Join(args, " "), err)
		}
	}
	now, err := installedVersion(ctx, env.invoked)
	if err != nil {
		return upgradeFailed("see what Homebrew says of it: `brew info --cask axx`", "cannot run axx after `brew upgrade`: %v", err)
	}
	res.Upgraded = now != env.current
	res.To = now
	res.Path = env.invoked
	return nil
}

func installedVersion(ctx context.Context, binary string) (string, error) {
	out, err := exec.CommandContext(ctx, binary, "version", "--json").Output()
	if err != nil {
		return "", err
	}
	var env struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return "", err
	}
	return env.Data.Version, nil
}

func (a *App) upgradeWithGo(ctx context.Context, tag string) error {
	if tag == selfupdate.Nightly {
		return axxerr.New(codeCannotUpgrade, exitcode.Usage, "the nightly is a release archive, which `go install` does not take").
			WithHint("use a commit: `go install %s/cmd/axx@main`", version.Module)
	}
	cmd := exec.CommandContext(ctx, "go", "install", version.Module+"/cmd/axx@"+tag)
	cmd.Stdout, cmd.Stderr = a.Stderr, a.Stderr
	if err := cmd.Run(); err != nil {
		return upgradeFailed("run it yourself to see why: `go install "+version.Module+"/cmd/axx@"+tag+"`", "`go install %s/cmd/axx@%s`: %v", version.Module, tag, err)
	}
	return nil
}

// upgradeSkills has the axx at binary update the skills, where they are installed.
// It runs `skills install`, which every version has, as a version gone back to may
// have no `skills update`. A failure here takes nothing back: axx has upgraded.
func (a *App) upgradeSkills(ctx context.Context, binary string, res *UpgradeResult, project, home bool) {
	type scope struct {
		name string
		dir  string
		args []string
	}
	var scopes []scope
	if project {
		if dir, err := config.ProjectDir(a.Config); err == nil {
			if _, ok := skills.InstalledVersion(dir); ok {
				args := []string{"skills", "install", "--json"}
				if a.Config != "" {
					args = append(args, "--config", a.Config)
				}
				scopes = append(scopes, scope{"this project", "", args})
			}
		}
	}
	if home {
		if dir, err := os.UserHomeDir(); err == nil {
			if _, ok := skills.InstalledVersion(dir); ok {
				// From the home directory: a project's version is not what serves every project.
				scopes = append(scopes, scope{"your home directory", dir, []string{"skills", "install", "--scope", "user", "--json"}})
			}
		}
	}
	for _, s := range scopes {
		cmd := exec.CommandContext(ctx, binary, s.args...)
		cmd.Dir = s.dir
		cmd.Stderr = a.Stderr
		if err := cmd.Run(); err != nil {
			res.SkillsProblem = fmt.Sprintf("cannot update the skills of %s: %v; `axx skills update` tries again", s.name, err)
			continue
		}
		res.Skills = append(res.Skills, s.name)
	}
}

// renderUpgrade says what changed, then where axx is now.
func renderUpgrade(w io.Writer, res *UpgradeResult) error {
	if res.UpToDate && res.Project {
		fmt.Fprintf(w, "this project is on axx %s, the newest release\n", res.From)
		renderInstalledAxx(w, res)
		renderSkills(w, res)
		return nil
	}
	if res.UpToDate {
		_, err := fmt.Fprintf(w, "axx %s is the newest release\n", res.From)
		return err
	}
	if len(res.Changes) > 0 {
		fmt.Fprintf(w, "what changed since %s:\n", res.From)
		for _, c := range res.Changes {
			fmt.Fprintf(w, "\n%s", c.Version)
			if c.Date != "" {
				fmt.Fprintf(w, " (%s)", c.Date)
			}
			fmt.Fprintln(w)
			for _, s := range c.Sections {
				fmt.Fprintf(w, "  %s\n", s.Title)
				for _, item := range s.Items {
					fmt.Fprintf(w, "    - %s\n", item)
				}
			}
		}
		fmt.Fprintf(w, "\nthe whole changelog: %s\n", changelogURL)
	}
	if res.Project {
		renderProjectMove(w, res)
	}
	switch {
	case res.Project:
	case res.Available:
		fmt.Fprintf(w, "axx %s is installed and %s is available: `axx upgrade` installs it\n", res.From, res.To)
	case res.Upgraded && res.To == selfupdate.Nightly:
		fmt.Fprintf(w, "installed axx's nightly build in place of %s (%s)\n", res.From, res.Path)
	case res.Upgraded && selfupdate.Compare(res.To, res.From) < 0:
		fmt.Fprintf(w, "went back from axx %s to %s (%s)\n", res.From, res.To, res.Path)
	case res.Upgraded:
		fmt.Fprintf(w, "upgraded axx from %s to %s (%s)\n", res.From, res.To, res.Path)
	default:
		fmt.Fprintf(w, "axx is still %s: Homebrew's tap has no newer version yet, a release takes it a few minutes to reach\n", res.From)
	}
	renderSkills(w, res)
	return nil
}

func renderSkills(w io.Writer, res *UpgradeResult) {
	if len(res.Skills) > 0 {
		fmt.Fprintf(w, "updated the agent skills of %s\n", strings.Join(res.Skills, " and "))
	}
	if res.SkillsProblem != "" {
		fmt.Fprintln(w, res.SkillsProblem)
	}
}

// renderInstalledAxx says what became of the installed axx, which a project's move
// takes along.
func renderInstalledAxx(w io.Writer, res *UpgradeResult) {
	if i := res.Installed; i != nil && i.Upgraded {
		fmt.Fprintf(w, "upgraded the installed axx from %s to %s (%s)\n", i.From, i.To, i.Path)
	}
	if res.InstalledProblem != "" {
		fmt.Fprintln(w, res.InstalledProblem)
	}
}

// renderProjectMove says where the project went, and what its version thinks of it.
func renderProjectMove(w io.Writer, res *UpgradeResult) {
	if res.Available {
		fmt.Fprintf(w, "this project is on axx %s and %s is available: `axx upgrade` moves it\n", res.From, res.To)
		return
	}
	verb := "moved"
	if config.ValidPin(res.From) && selfupdate.Compare(res.To, res.From) < 0 {
		verb = "took back"
	}
	fmt.Fprintf(w, "%s this project from axx %s to %s (%s): commit it, so that everyone and CI run the same axx\n", verb, res.From, res.To, res.Path)
	failed := false
	for _, c := range res.Checks {
		if c.OK {
			fmt.Fprintf(w, "%s passes on %s\n", c.Command, res.To)
			continue
		}
		failed = true
		fmt.Fprintf(w, "%s finds problems on %s:\n%s\n", c.Command, res.To, c.Output)
	}
	if failed {
		fmt.Fprintf(w, "fix them, or go back: `axx upgrade --version %s`\n", res.From)
	}
	renderInstalledAxx(w, res)
}
