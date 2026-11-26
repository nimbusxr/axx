// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/cachedir"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/filelock"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// codeProjectInUse: `axx clean` in a project whose services are up, or
// that a run is going on in.
const codeProjectInUse = "AXX-E0419"

// keptByClean is what `axx clean` leaves in a project's .axx: it says what
// is running. run has the services `axx up` keeps, what an interrupted run
// left for `axx down` to stop, and the locks runs take turns by; host.json
// is where the axx host serving the project announces itself.
var keptByClean = []string{"run", "host.json"}

// CleanResult is what `axx clean` did.
type CleanResult struct {
	DryRun  bool           `json:"dryRun"`
	Bytes   int64          `json:"bytes"`
	Removed []CleanRemoved `json:"removed"`
}

// CleanRemoved is a file or a folder `axx clean` removed.
type CleanRemoved struct {
	// Path is relative to the project's folder.
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

func newCleanCmd(app *App) *cobra.Command {
	var cf configFlags
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove what runs left in this project: logs, videos, traces, reports",
		Long: `Remove what runs left in this project:

  - everything axx and its packs wrote into the project's .axx folder: the
    services' logs, the scenarios' videos, screenshots and traces, coverage,
    what the services told axx, demo videos and their takes
  - the report files that axx.yaml's reporters name, where they are in the
    project

Two things in .axx stay, as they say what is running: .axx/run (the services
` + "`axx up`" + ` keeps, and what an interrupted run left for ` + "`axx down`" + ` to stop)
and .axx/host.json (the axx host that serves the project).

It refuses while the project is in use, as what is running writes there:
while ` + "`axx up`" + ` keeps services (` + "`axx down`" + ` first), or a run is going on.

--dry-run says what would go, and removes nothing.

To remove what your own tools leave beside it, give the project a hook in
axx.yaml (hooks.commands, with ` + "`after: clean`" + `). What axx keeps on the machine
for every project (itself as built with packs, tools) is its cache:
` + "`axx cache`" + `.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			if !dryRun {
				release, err := holdForCleaning(cfg)
				if err != nil {
					return err
				}
				defer release()
			}
			res := CleanResult{DryRun: dryRun, Removed: []CleanRemoved{}}
			for _, p := range leftByRuns(cfg) {
				rel, err := filepath.Rel(cfg.Dir, p)
				if err != nil {
					return err
				}
				r := CleanRemoved{Path: filepath.ToSlash(rel), Bytes: cachedir.Bytes(p)}
				if !dryRun {
					if err := os.RemoveAll(p); err != nil {
						return err
					}
				}
				res.Removed = append(res.Removed, r)
				res.Bytes += r.Bytes
			}
			sort.SliceStable(res.Removed, func(i, j int) bool { return res.Removed[i].Bytes > res.Removed[j].Bytes })
			return app.Emit(res, func(w io.Writer) error { return renderClean(w, res, cfg.File != "") })
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would be removed without removing it")
	cf.register(cmd)
	return cmd
}

// holdForCleaning refuses a project that is in use, and otherwise holds the
// turn a run takes with the project's services until release: a run that
// starts while axx cleans waits for it.
func holdForCleaning(cfg *config.Config) (release func(), err error) {
	if _, err := os.Stat(filepath.Join(cfg.Dir, ".axx")); err != nil {
		return func() {}, nil //nolint:nilerr // no .axx: no run was here
	}
	if st, ok := liveUp(cfg); ok {
		return nil, axxerr.New(codeProjectInUse, exitcode.Environment, "`axx up` keeps this project's services running (%s), and their logs and what they told axx are in .axx: nothing was removed", strings.Join(st.Services, ", ")).
			WithHint("stop them with `axx down`, then run `axx clean` again")
	}
	release, ok, err := filelock.TryLock(turnLock(cfg))
	if err != nil {
		return nil, axxerr.Wrap(err, lifecycle.CodeStateFile, exitcode.Environment, "cannot tell whether a run of this project is going on: %s cannot be locked", relPath(turnLock(cfg)))
	}
	if !ok {
		return nil, axxerr.New(codeProjectInUse, exitcode.Environment, "a run of this project is going on, and writes into .axx: nothing was removed").
			WithHint("run `axx clean` again once it has ended")
	}
	return release, nil
}

// leftByRuns are the files and folders runs left in the project: what is in
// its .axx but for keptByClean, and the reporters' files inside the project.
func leftByRuns(cfg *config.Config) []string {
	var out []string
	state := filepath.Join(cfg.Dir, ".axx")
	entries, _ := os.ReadDir(state)
	for _, e := range entries {
		kept := false
		for _, k := range keptByClean {
			kept = kept || e.Name() == k
		}
		if !kept {
			out = append(out, filepath.Join(state, e.Name()))
		}
	}
	for _, r := range cfg.Run.Reporters {
		if r.Path == "" {
			continue
		}
		p := r.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(cfg.Dir, p)
		}
		p = filepath.Clean(p)
		// A report outside the project is not this project's alone to remove,
		// and one in .axx went with it.
		if inside(cfg.Dir, p) && !inside(state, p) && isRegularFile(p) {
			out = append(out, p)
		}
	}
	return out
}

// inside reports whether p is in dir.
func inside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func renderClean(w io.Writer, res CleanResult, project bool) error {
	if len(res.Removed) == 0 {
		said := "nothing to remove: runs left nothing in this project"
		if !project {
			said = "nothing to remove: this is not an axx project (no axx.yaml here or above), and no run left anything in this folder"
		}
		_, err := fmt.Fprintln(w, said)
		return err
	}
	verb := "removed"
	if res.DryRun {
		verb = "would remove"
	}
	fmt.Fprintf(w, "%s %s that runs left in this project:\n", verb, cachedir.Size(res.Bytes))
	width := 0
	for _, r := range res.Removed {
		width = max(width, len(r.Path))
	}
	for _, r := range res.Removed {
		fmt.Fprintf(w, "  %-*s  %7s\n", width, r.Path, cachedir.Size(r.Bytes))
	}
	return nil
}
