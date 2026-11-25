// SPDX-License-Identifier: Apache-2.0

// Package cachedir is axx's cache on a machine: what is in it, how big it
// is, when the binaries of axx in it were last used, and removing the ones
// nothing used for long.
//
// axx keeps two kinds of binaries of itself there: the ones it builds with
// a project's packs (builds, a folder each, named by what it was built
// from), and the releases projects name (versions). A new one arrives with
// every release of axx and every change to a project's packs, and the
// earlier ones are never asked for again. So each use of one is marked
// (Use), and what was not used for Keep is removed: by axx itself, at most
// once a day (Tidy), and by `axx cache clean`.
//
// Everything else in the cache (the Go that builds packs, a pack's tools)
// is removed only when a person asks: getting it again takes minutes.
package cachedir

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/filelock"
)

// Keep is how long a binary of axx that nothing uses stays in the cache.
const Keep = 30 * 24 * time.Hour

const (
	// usedName is the file in a binary's folder whose time is when the
	// binary was last used.
	usedName = "used"
	// tidiedName is the file in the cache whose time is when axx last looked
	// for binaries to remove.
	tidiedName = ".tidied"
	// axxModule is axx's module path, as a build's go.mod requires it.
	axxModule = "github.com/nimbusxr/axx"
)

// Root is axx's cache on this machine: <user cache>/axx.
func Root() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "axx"), nil
}

// Builds is where the binaries axx builds with projects' packs are.
func Builds(root string) string { return filepath.Join(root, "builds") }

// Versions is where the releases of axx that projects name are.
func Versions(root string) string { return filepath.Join(root, "versions") }

// Use marks the binary in dir as used now. It tries, and no more: a cache
// this process cannot write (a sandbox's) keeps the time it had. An hour is
// near enough, so most uses write nothing.
func Use(dir string) {
	if UsedLately(dir) {
		return
	}
	p, now := filepath.Join(dir, usedName), time.Now()
	if os.Chtimes(p, now, now) != nil {
		_ = os.WriteFile(p, nil, 0o644)
	}
}

// UsedLately reports whether the binary in dir was marked as used within
// the hour.
func UsedLately(dir string) bool {
	at, ok := usedAt(dir)
	return ok && time.Since(at) < time.Hour
}

func usedAt(dir string) (time.Time, bool) {
	st, err := os.Stat(filepath.Join(dir, usedName))
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}

// Binary is an axx in the cache.
type Binary struct {
	// Kind is "build" (axx built with a project's packs) or "version" (a
	// release a project names).
	Kind string `json:"kind"`
	// Name is the build's key, or the version.
	Name string `json:"name"`
	Path string `json:"path"`
	// Version is the axx a build is of, when its folder says.
	Version string    `json:"version,omitempty"`
	Used    time.Time `json:"used"`
	Bytes   int64     `json:"bytes"`
}

// Binaries lists the binaries of axx in the cache at root, with when each
// was last used.
//
// A release of axx from before uses were marked marks none: its own folder
// is marked by the axx that hands over to it, and a build it made counts as
// used when its release was. A release's folder that has no mark yet gets
// one now: it is in use from when axx first saw it.
func Binaries(root string) []Binary {
	var out []Binary
	releases := map[string]time.Time{}
	for _, dir := range folders(Versions(root)) {
		used, ok := usedAt(dir)
		if !ok {
			Use(dir)
			used = time.Now()
		}
		name := filepath.Base(dir)
		releases[name] = used
		out = append(out, Binary{Kind: "version", Name: name, Path: dir, Version: name, Used: used, Bytes: size(dir)})
	}
	for _, dir := range folders(Builds(root)) {
		b := Binary{Kind: "build", Name: filepath.Base(dir), Path: dir, Version: buildVersion(dir), Bytes: size(dir)}
		if used, ok := usedAt(dir); ok {
			b.Used = used
		} else {
			b.Used = built(dir)
			if at := releases[b.Version]; at.After(b.Used) {
				b.Used = at
			}
		}
		out = append(out, b)
	}
	return out
}

// folders are the folders in dir that are a binary's: none whose name
// starts with a dot (a download that has not finished).
func folders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// built is when a build without a mark was made: the time of its newest
// file, of the few at its top.
func built(dir string) time.Time {
	var at time.Time
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().After(at) {
			at = info.ModTime()
		}
	}
	if at.IsZero() {
		if st, err := os.Stat(dir); err == nil {
			at = st.ModTime()
		}
	}
	return at
}

// buildVersion is the release of axx a build is of, from the go.mod it was
// built with: none for a build of local source.
func buildVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "src", "go.mod"))
	if err != nil || bytes.Contains(b, []byte("replace "+axxModule+" ")) {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "require "+axxModule+" "); ok {
			return strings.TrimPrefix(strings.TrimSpace(rest), "v")
		}
	}
	return ""
}

// Stale are the binaries of bins that nothing used for keep, as of asOf,
// the largest first. A binary under one of inUse (the running axx's own
// folder) is never one.
func Stale(bins []Binary, asOf time.Time, keep time.Duration, inUse ...string) []Binary {
	var out []Binary
	for _, b := range bins {
		if b.Used.Before(asOf.Add(-keep)) && !InUse(b.Path, inUse...) {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// InUse reports whether one of dirs, the folders an axx runs from, is path
// or inside it.
func InUse(path string, dirs ...string) bool {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if rel, err := filepath.Rel(path, d); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Remove removes the binary b, unless still says no, and reports whether it
// did. It holds the lock an axx holds while it builds or downloads the
// binary and while it marks a use, so it removes none that is being made,
// and still (when not nil) sees a use marked a moment before.
func Remove(b Binary, still func() bool) bool {
	lock := b.Path + ".lock"
	unlock, ok, err := filelock.TryLock(lock)
	if err != nil || !ok {
		return false
	}
	if still != nil && !still() {
		unlock()
		return false
	}
	err = os.RemoveAll(b.Path)
	unlock()
	if err != nil {
		return false // Windows keeps a binary that runs
	}
	_ = os.Remove(lock)
	return true
}

// Tidy removes the binaries nothing used for keep, at most once a day, and
// returns the ones it removed. It is what axx does by itself, so it is
// careful with a cache that was put there whole: one a CI job restored, or
// an image has. Its marks are as old as the day it was saved, though the
// job uses what is in it. So a binary is judged as of the last time axx
// looked (the time of a file in the cache), not as of now: a cache that was
// just restored loses nothing, and one on a machine that keeps it loses a
// day later what a check now would take. The first look removes nothing.
//
// A release is removed only with releases set: a machine that keeps axx
// from downloading one (AXX_AUTO_DOWNLOAD=0) got its releases by hand.
func Tidy(root string, keep time.Duration, releases bool, inUse ...string) []Binary {
	if keep <= 0 {
		return nil
	}
	mark, now := filepath.Join(root, tidiedName), time.Now()
	st, err := os.Stat(mark)
	if err != nil {
		if os.MkdirAll(root, 0o755) == nil {
			_ = os.WriteFile(mark, nil, 0o644)
		}
		return nil
	}
	last := st.ModTime()
	if now.Sub(last) < 24*time.Hour {
		return nil
	}
	if os.Chtimes(mark, now, now) != nil {
		return nil // a cache this process cannot write: nothing to remove from it either
	}
	var removed []Binary
	for _, b := range Stale(Binaries(root), last, keep, inUse...) {
		if b.Kind == "version" && !releases {
			continue
		}
		dir := b.Path
		if Remove(b, func() bool {
			used, ok := usedAt(dir)
			return !ok || used.Before(last.Add(-keep))
		}) {
			removed = append(removed, b)
		}
	}
	Sweep(root, now.Add(-keep))
	return removed
}

// Sweep removes what axx left beside its binaries before the time: the lock
// of a binary that is gone, and a download that never finished.
func Sweep(root string, before time.Time) {
	sweep(Builds(root), before)
	sweep(Versions(root), before)
}

func sweep(dir string, before time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(before) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir() && strings.HasPrefix(e.Name(), ".download-"):
			_ = os.RemoveAll(p)
		case !e.IsDir() && strings.HasSuffix(e.Name(), ".lock"):
			if _, err := os.Stat(strings.TrimSuffix(p, ".lock")); err == nil {
				continue
			}
			if unlock, ok, err := filelock.TryLock(p); err == nil && ok {
				unlock()
				_ = os.Remove(p)
			}
		}
	}
}

// Group is the folders of the cache that hold one kind of thing.
type Group struct {
	// Name is what `axx cache clean` takes: the folders' name, up to its
	// first dash (models-<hash> is of models).
	Name string `json:"name"`
	// What says what is in it, for the ones axx's own code makes.
	What  string `json:"what,omitempty"`
	Bytes int64  `json:"bytes"`
	// Paths are its folders.
	Paths []string `json:"-"`
}

// what says what axx keeps in a group.
var what = map[string]string{
	"builds":   "axx as built with each project's packs",
	"versions": "the releases of axx that projects name",
	"go":       "the Go that builds packs, and the modules it downloaded",
	"node":     "the Node.js that runs Playwright and other tools",
	"web":      "the web packs' tools: Playwright, Lighthouse, axe-core",
	"mobile":   "the mobile packs' drivers, simulators and emulators",
	"desktop":  "the desktop packs' tools",
	"launcher": "axx's launcher on macOS",
	"models":   "the models packs' runtimes and tools",
	"parakeet": "the models packs' runtimes and tools",
	"piper":    "the models packs' runtimes and tools",
	"coverage": "the coverage packs' tools",
	"demo":     "the demo pack's tools",
	"openh264": "the encoder of the videos axx makes",
	"delve":    "the debugger of step code",
	"docs":     "the docs site, for `axx docs serve`",
}

// Groups lists what is in the cache at root, the largest first. Measuring
// it reads every folder, which takes a while for a cache of many gigabytes.
func Groups(root string) ([]Group, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	byName := map[string]*Group{}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		slot = make(chan struct{}, 8)
	)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name, _, _ := strings.Cut(e.Name(), "-")
		g := byName[name]
		if g == nil {
			g = &Group{Name: name, What: what[name]}
			byName[name] = g
		}
		p := filepath.Join(root, e.Name())
		g.Paths = append(g.Paths, p)
		wg.Add(1)
		go func() {
			defer wg.Done()
			slot <- struct{}{}
			n := size(p)
			<-slot
			mu.Lock()
			g.Bytes += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	out := make([]Group, 0, len(byName))
	for _, g := range byName {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Bytes is the bytes of the files in dir.
func Bytes(dir string) int64 { return size(dir) }

func size(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // what cannot be read counts as nothing
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// Size says n bytes as a person reads them: 73 MB, 1.2 GB.
func Size(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	digits := 0
	if i > 0 && f < 10 {
		digits = 1
	}
	return strconv.FormatFloat(f, 'f', digits, 64) + " " + units[i]
}
