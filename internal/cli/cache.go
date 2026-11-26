// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/cachedir"
	"github.com/nimbusxr/axx/internal/version"
)

// envCacheKeepDays is for how many days axx keeps a binary of itself that
// nothing uses (30 without it); 0 keeps axx from removing any by itself.
const envCacheKeepDays = "AXX_CACHE_KEEP_DAYS"

// cacheKeep is how long a binary of axx that nothing uses stays in the cache.
func (a *App) cacheKeep() (time.Duration, error) {
	v := strings.TrimSpace(a.Env(envCacheKeepDays))
	if v == "" {
		return cachedir.Keep, nil
	}
	days, err := strconv.Atoi(v)
	if err != nil || days < 0 {
		return cachedir.Keep, usageError{fmt.Errorf("%s=%s is not a number of days: 30 is what axx keeps an unused binary for without it, and 0 keeps axx from removing any by itself", envCacheKeepDays, v)}
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// binariesInUse are the folders of the cache that this process runs from:
// its own, and that of the axx that handed over to it.
func binariesInUse() []string {
	var out []string
	for _, p := range []string{ownPath(), launcherPath(version.Get())} {
		if p != "" {
			out = append(out, filepath.Dir(p))
		}
	}
	return out
}

// tidyCache removes the binaries of axx that nothing used for long, at most
// once a day, before this axx hands over to one in its cache: an axx built
// with the project's packs, or the release the project names. It says a
// line only when that takes a second.
func (a *App) tidyCache() {
	keep, err := a.cacheKeep()
	root, rootErr := cachedir.Root()
	if err != nil || rootErr != nil || keep <= 0 {
		return
	}
	inUse := binariesInUse()
	// A release that runs from the cache marks its own use: the axx that
	// handed over to it may be from before uses were marked.
	for _, dir := range inUse {
		if filepath.Dir(dir) == cachedir.Versions(root) {
			cachedir.Use(dir)
		}
	}
	step := a.stepIfSlow("axx: removing the binaries of axx that nothing used for "+days(keep), "removing unused binaries of axx")
	defer step.Done()
	cachedir.Tidy(root, keep, a.downloadsReleases(), inUse...)
}

// downloadsReleases is whether axx may download the release a project
// names: where it may not, the releases were put there by hand, and stay.
func (a *App) downloadsReleases() bool {
	switch a.Env(envAutoDownload) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func days(d time.Duration) string { return plural(int(d/(24*time.Hour)), "day") }

func binaries(n int) string {
	if n == 1 {
		return "1 binary"
	}
	return strconv.Itoa(n) + " binaries"
}

// unusedAfter is after how long `axx cache` and `axx cache clean` call a
// binary unused: what axx keeps one for, or 30 days where it removes none
// by itself.
func unusedAfter(keep time.Duration) time.Duration {
	if keep <= 0 {
		return cachedir.Keep
	}
	return keep
}

// CacheResult is what `axx cache` says.
type CacheResult struct {
	Path   string           `json:"path"`
	Bytes  int64            `json:"bytes"`
	Groups []cachedir.Group `json:"groups"`
	// KeepDays is for how many days a binary of axx that nothing uses stays
	// (0: axx removes none by itself), and Unused the ones past it.
	KeepDays int         `json:"keepDays"`
	Unused   CacheUnused `json:"unused"`
}

// CacheUnused counts the binaries of axx that nothing used for long.
type CacheUnused struct {
	Binaries int   `json:"binaries"`
	Bytes    int64 `json:"bytes"`
}

// CacheCleanResult is what `axx cache clean` did.
type CacheCleanResult struct {
	DryRun  bool           `json:"dryRun"`
	Bytes   int64          `json:"bytes"`
	Removed []CacheRemoved `json:"removed"`
	// Kept is what was asked for and stays: in use, or not removable.
	Kept []CacheKept `json:"kept"`
}

// CacheRemoved is one thing `axx cache clean` removed.
type CacheRemoved struct {
	// Group is its group in the cache, as `axx cache` lists them.
	Group string `json:"group"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// CacheKept is one thing `axx cache clean` left.
type CacheKept struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

func newCacheCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Show what axx keeps on this machine, and how big it is",
		Long: `Show axx's cache on this machine: what is in it, and how big each part is.

axx keeps there what it would otherwise make or download again: itself as
built with each project's packs, the releases projects name, the Go that
builds packs, and the packs' tools (browsers' drivers, phone drivers). All of
it comes back when a run needs it.

The binaries of axx are the part that grows: a new one with every release of
axx and every change to a project's packs. So axx removes the ones nothing
used for 30 days by itself, at most once a day. ` + envCacheKeepDays + ` sets
another number of days, and 0 keeps axx from removing any. The rest stays
until you remove it: ` + "`axx cache clean`" + `.

What runs leave in a project (logs, videos, traces, reports) is not here:
` + "`axx clean`" + ` removes that.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			keep, err := app.cacheKeep()
			if err != nil {
				return err
			}
			root, err := cachedir.Root()
			if err != nil {
				return err
			}
			step := app.stepIfSlow("axx: measuring "+root, "measuring the cache")
			groups, err := cachedir.Groups(root)
			var unused []cachedir.Binary
			if err == nil {
				unused = cachedir.Stale(cachedir.Binaries(root), time.Now(), unusedAfter(keep), binariesInUse()...)
			}
			step.Done()
			if err != nil {
				return err
			}
			res := CacheResult{Path: root, Groups: groups, KeepDays: int(keep / (24 * time.Hour)), Unused: CacheUnused{Binaries: len(unused)}}
			if res.Groups == nil {
				res.Groups = []cachedir.Group{}
			}
			for _, g := range groups {
				res.Bytes += g.Bytes
			}
			for _, b := range unused {
				res.Unused.Bytes += b.Bytes
			}
			return app.Emit(res, func(w io.Writer) error { return renderCache(w, res, keep) })
		},
	}
	cmd.AddCommand(newCacheCleanCmd(app))
	return cmd
}

func renderCache(w io.Writer, res CacheResult, keep time.Duration) error {
	if len(res.Groups) == 0 {
		_, err := fmt.Fprintf(w, "axx's cache is empty (%s)\n", res.Path)
		return err
	}
	fmt.Fprintf(w, "axx's cache: %s, %s\n\n", res.Path, cachedir.Size(res.Bytes))
	width := 0
	for _, g := range res.Groups {
		width = max(width, len(g.Name))
	}
	for _, g := range res.Groups {
		fmt.Fprintf(w, "  %-*s  %7s  %s\n", width, g.Name, cachedir.Size(g.Bytes), g.What)
	}
	fmt.Fprintln(w)
	if res.Unused.Binaries > 0 {
		by := "axx does so by itself too, at most once a day"
		if keep <= 0 {
			by = envCacheKeepDays + "=0 keeps axx from doing so by itself"
		}
		fmt.Fprintf(w, "%s of axx (%s) went unused for %s: `axx cache clean` removes them (%s).\n",
			binaries(res.Unused.Binaries), cachedir.Size(res.Unused.Bytes), days(unusedAfter(keep)), by)
	}
	_, err := fmt.Fprintln(w, "`axx cache clean <name>` removes all of one of these, and `axx cache clean --all` all of them: axx gets again what a run needs.")
	return err
}

func newCacheCleanCmd(app *App) *cobra.Command {
	var all, dryRun bool
	cmd := &cobra.Command{
		Use:   "clean [name...]",
		Short: "Remove the binaries of axx nothing uses, or the parts of the cache you name",
		Long: `Remove from axx's cache on this machine.

  axx cache clean              the binaries of axx that nothing used for 30 days
  axx cache clean models web   all of the parts you name, as ` + "`axx cache`" + ` lists them
  axx cache clean --all        all of it

Whatever a run needs comes back: axx builds or downloads it again, which
takes from seconds (a binary) to minutes (a simulator, a model runtime). A
binary of axx that is running, or being built, stays.

--dry-run says what would go, and removes nothing.`,
		Args: wrapArgs(cobra.ArbitraryArgs),
		RunE: func(_ *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return usageError{fmt.Errorf("--all removes every part of the cache: name parts, or give --all, not both")}
			}
			keep, err := app.cacheKeep()
			if err != nil {
				return err
			}
			root, err := cachedir.Root()
			if err != nil {
				return err
			}
			res := CacheCleanResult{DryRun: dryRun, Removed: []CacheRemoved{}, Kept: []CacheKept{}}
			step := app.stepIfSlow("axx: cleaning "+root, "cleaning the cache")
			if all || len(args) > 0 {
				err = cleanGroups(root, args, dryRun, &res)
			} else {
				cleanUnused(root, unusedAfter(keep), dryRun, &res)
			}
			step.Done()
			if err != nil {
				return err
			}
			for _, r := range res.Removed {
				res.Bytes += r.Bytes
			}
			return app.Emit(res, func(w io.Writer) error { return renderCacheClean(w, res, all || len(args) > 0, unusedAfter(keep)) })
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "remove every part of the cache")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would be removed without removing it")
	return cmd
}

// cleanUnused removes the binaries of axx that nothing used for keep.
func cleanUnused(root string, keep time.Duration, dryRun bool, res *CacheCleanResult) {
	now := time.Now()
	for _, b := range cachedir.Stale(cachedir.Binaries(root), now, keep, binariesInUse()...) {
		removeBinary(b, dryRun, res)
	}
	if !dryRun {
		cachedir.Sweep(root, now.Add(-keep))
	}
}

// cleanGroups removes the groups of the cache that names name, or all of
// them when it names none.
func cleanGroups(root string, names []string, dryRun bool, res *CacheCleanResult) error {
	groups, err := cachedir.Groups(root)
	if err != nil {
		return err
	}
	var known []string
	for _, g := range groups {
		known = append(known, g.Name)
	}
	slices.Sort(known)
	for _, n := range names {
		if !slices.Contains(known, n) {
			if len(known) == 0 {
				return usageError{fmt.Errorf("the cache has no part %q: it is empty", n)}
			}
			return usageError{fmt.Errorf("the cache has no part %q: it has %s", n, strings.Join(known, ", "))}
		}
	}
	inUse := binariesInUse()
	for _, g := range groups {
		if len(names) > 0 && !slices.Contains(names, g.Name) {
			continue
		}
		if g.Name == "builds" || g.Name == "versions" {
			// A binary at a time, by its lock: none that runs here, or is
			// being built.
			for _, b := range cachedir.Binaries(root) {
				if (b.Kind == "build") != (g.Name == "builds") {
					continue
				}
				if cachedir.InUse(b.Path, inUse...) {
					res.Kept = append(res.Kept, CacheKept{Path: b.Path, Why: "this axx runs from it"})
					continue
				}
				removeBinary(b, dryRun, res)
			}
			if !dryRun {
				cachedir.Sweep(root, time.Now())
				_ = os.Remove(g.Paths[0]) // when nothing is left in it
			}
			continue
		}
		for _, p := range g.Paths {
			r := CacheRemoved{Group: g.Name, Path: p, Bytes: g.Bytes}
			if len(g.Paths) > 1 {
				r.Bytes = cachedir.Bytes(p)
			}
			if !dryRun {
				if err := os.RemoveAll(p); err != nil {
					res.Kept = append(res.Kept, CacheKept{Path: p, Why: err.Error()})
					continue
				}
			}
			res.Removed = append(res.Removed, r)
		}
	}
	return nil
}

func removeBinary(b cachedir.Binary, dryRun bool, res *CacheCleanResult) {
	if !dryRun && !cachedir.Remove(b, nil) {
		res.Kept = append(res.Kept, CacheKept{Path: b.Path, Why: "an axx runs from it, or is making it"})
		return
	}
	group := "builds"
	if b.Kind == "version" {
		group = "versions"
	}
	res.Removed = append(res.Removed, CacheRemoved{Group: group, Path: b.Path, Bytes: b.Bytes})
}

func renderCacheClean(w io.Writer, res CacheCleanResult, named bool, keep time.Duration) error {
	verb := "removed"
	if res.DryRun {
		verb = "would remove"
	}
	switch {
	case len(res.Removed) == 0 && !named:
		fmt.Fprintf(w, "no binary of axx has gone without use for %s: nothing to remove (`axx cache` lists what is kept)\n", days(keep))
	case len(res.Removed) == 0:
		fmt.Fprintln(w, "nothing to remove")
	case !named:
		fmt.Fprintf(w, "%s %s of axx that nothing used for %s (%s)\n", verb, binaries(len(res.Removed)), days(keep), cachedir.Size(res.Bytes))
	default:
		// A line for each group, in the order they went.
		var order []string
		bytes := map[string]int64{}
		for _, r := range res.Removed {
			if _, ok := bytes[r.Group]; !ok {
				order = append(order, r.Group)
			}
			bytes[r.Group] += r.Bytes
		}
		fmt.Fprintf(w, "%s %s:\n", verb, cachedir.Size(res.Bytes))
		for _, g := range order {
			fmt.Fprintf(w, "  %-10s %7s\n", g, cachedir.Size(bytes[g]))
		}
	}
	for _, k := range res.Kept {
		fmt.Fprintf(w, "kept %s: %s\n", k.Path, k.Why)
	}
	return nil
}
