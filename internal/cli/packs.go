package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/packbuild"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/version"
)

// envPackBuild marks a process started from a build with the project's
// packs, so it never tries to build again.
const envPackBuild = "AXX_PACK_BUILD"

// commandsNeedingPacks load the project's steps, or start what does: up's
// supervisor is this same executable, and its packs prepare the apps.
var commandsNeedingPacks = []string{"run", "up", "env", "config", "validate", "lint", "steps", "explain", "mcp", "lsp", "doctor", "skills", "docs"}

// ensurePacks makes sure the running axx has the project's packs compiled
// in. If it has not, it builds (or reuses) such an axx and replaces this
// process with it.
func (a *App) ensurePacks(ctx context.Context, cmd *cobra.Command) error {
	top := cmd
	for top.HasParent() && top.Parent().HasParent() {
		top = top.Parent()
	}
	needs := slices.Contains(commandsNeedingPacks, top.Name()) || cmd.CommandPath() == "axx pack list"
	if cmd.CommandPath() == "axx mcp install" {
		needs = false // writes agents' configuration, no steps
	}
	if !needs || os.Getenv(envPackBuild) != "" {
		return nil
	}
	if fl := cmd.Flags().Lookup("debug-steps"); fl != nil && fl.Changed {
		return nil // --debug-steps builds its own axx, with debug information
	}
	if fl := cmd.Flags().Lookup("scope"); fl != nil && fl.Value.String() == "user" {
		return nil // a home directory's skills serve every project, whatever its packs
	}
	if fl := cmd.Flags().Lookup("project"); top.Name() == "lsp" && a.Config == "" && (fl == nil || fl.Value.String() != "true") {
		return nil // the router: the server it starts for each project prepares that project's packs
	}
	dir, err := config.ProjectDir(a.Config)
	if err != nil {
		return nil //nolint:nilerr // the command reports configuration errors itself
	}
	entries, missing := missingPacks(dir)
	if !missing {
		return nil
	}
	info := version.Get()
	source := axxSource(info)
	// A build of a commit fetches the packs at its own pseudo-version, which
	// the module proxy serves; a build of local source needs that source.
	if source == "" && info.Channel == "dev" && !version.FromModuleProxy(info.Version) {
		return axxerr.New(packbuild.CodeBuild, exitcode.Usage, "this development build of axx cannot prepare packs").
			WithHint("use a released axx, or set AXX_SOURCE_DIR to your axx checkout")
	}
	lock, err := packset.LoadLock(dir)
	if err != nil {
		return err
	}
	res, err := packbuild.Ensure(ctx, packbuild.Options{
		ProjectDir: dir, Entries: entries, Lock: lock, AxxVersion: info.Version, AxxSource: source, Log: a.Stderr,
	})
	if err != nil {
		return err
	}
	if len(res.Lock.Modules) > 0 && !slices.Equal(res.Lock.Modules, lock.Modules) {
		res.Lock.Axx = info.Version
		if err := res.Lock.Save(dir); err != nil {
			return err
		}
	}
	return reexec(res.Binary)
}

// missingPacks returns the packs the project in dir lists, and whether this
// axx lacks any of them. A missing or broken axx-packs.yaml lacks nothing:
// the engine reports a broken one with its code.
func missingPacks(dir string) ([]packset.Entry, bool) {
	f, found, err := packset.Load(dir)
	if err != nil || !found {
		return nil, false
	}
	entries, _ := f.Entries()
	compiled := engine.Compiled()
	for _, e := range packset.WithRequired(entries) {
		if _, ok := compiled[e.Key()]; !ok {
			return entries, true
		}
	}
	return entries, false
}

// reexec replaces this process with bin, keeping the arguments. On Windows
// it runs bin as a child and exits with its code.
func reexec(bin string) error {
	env := append(os.Environ(), envPackBuild+"=1", version.EnvLauncher+"="+version.Get().Version)
	if runtime.GOOS != "windows" {
		return syscall.Exec(bin, append([]string{bin}, os.Args[1:]...), env)
	}
	cmd := exec.Command(bin, os.Args[1:]...) //nolint:noctx // the child owns the rest of the run
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdin, os.Stdout, os.Stderr, env
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
