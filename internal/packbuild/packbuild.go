// Package packbuild builds a copy of axx with a project's packs compiled
// in, and caches it.
//
// Go cannot load compiled code into a running program reliably, so a
// project runs through an axx built for its packs (axx's own, by name; a Go
// package in the repository; a Go module): a generated main that imports
// each pack and calls app.Main. Builds are keyed on the axx version, the
// pack list, the lock file and the pack sources, so they happen once. They
// use the Go toolchain axx provides (internal/gotool): nobody installs Go.
package packbuild

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/cachedir"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/filelock"
	"github.com/nimbusxr/axx/internal/gotool"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/progress"
)

// Error codes.
const (
	// CodeNoGo: axx cannot get the toolchain it prepares packs with.
	CodeNoGo = "AXX-E0303"
	// CodeBuild: a pack does not build.
	CodeBuild = "AXX-E0304"
)

// offline matches the Go command's errors when it cannot reach the module
// proxy: a module this machine has not downloaded yet.
var offline = regexp.MustCompile(`(?m)(dial tcp|lookup \S+ on|network is unreachable|no such host|i/o timeout|connection refused|TLS handshake timeout).*$`)

// AxxModule is axx's module path.
const AxxModule = "github.com/nimbusxr/axx"

// Options configures a build.
type Options struct {
	ProjectDir string          // directory of axx.yaml / axx-packs.yaml
	Entries    []packset.Entry // the project's packs
	Lock       *packset.Lock
	AxxVersion string // the running axx's module version
	// AxxSource is a local axx checkout to build against instead of the
	// released module (development builds; AXX_SOURCE_DIR).
	AxxSource string
	CacheDir  string // default: <user cache>/axx/builds
	// Toolchain builds; default the one gotool provides.
	Toolchain *gotool.Toolchain
	// Debug builds for a debugger: with DWARF, source paths and
	// optimizations off (-gcflags=all=-N -l), and without -trimpath.
	Debug bool
	Log   io.Writer
}

// Result is a built (or cached) binary.
type Result struct {
	Binary string
	Cached bool
	Lock   *packset.Lock // module versions the build resolved
}

// Ensure returns a binary with the packs compiled in, building it if the
// cache has none for these inputs.
func Ensure(ctx context.Context, o Options) (*Result, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	o.Entries = packset.WithRequired(o.Entries)
	for _, e := range o.Entries {
		if _, ok := packset.Lookup(e.Name); e.Kind == packset.Published && !ok {
			return nil, axxerr.New(CodeBuild, exitcode.Usage, "%s: %q is not a pack axx publishes", packset.FileName, e.Name).
				WithHint("axx's packs: %s; other packs are listed by path (./steps) or Go module path", strings.Join(packset.Names(), ", "))
		}
	}
	if o.Lock == nil {
		o.Lock = &packset.Lock{}
	}
	if o.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		o.CacheDir = filepath.Join(base, "axx", "builds")
	}
	locals, err := resolveLocals(o.ProjectDir, o.Entries)
	if err != nil {
		return nil, err
	}
	key, err := buildKey(o, locals)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(o.CacheDir, key)
	bin := filepath.Join(dir, exeName("axx"))
	lockFile := filepath.Join(dir, "resolved.json")
	cached := func() *Result {
		st, err := os.Stat(bin)
		if err != nil || !st.Mode().IsRegular() {
			return nil
		}
		res := &Result{Binary: bin, Cached: true, Lock: o.Lock}
		if b, err := os.ReadFile(lockFile); err == nil {
			l := &packset.Lock{}
			if json.Unmarshal(b, l) == nil {
				res.Lock = l
			}
		}
		return res
	}
	if res := cached(); res != nil {
		if cachedir.UsedLately(dir) {
			return res, nil
		}
		// The use is marked under the build's lock, which the axx that removes
		// the builds nothing uses holds while it looks at the mark: so it never
		// removes one that is about to run. A cache this process cannot write
		// (a sandbox's) has the build as it is.
		unlock, err := filelock.Lock(ctx, dir+".lock", nil)
		if err != nil {
			return res, nil //nolint:nilerr // the build is there, and runs
		}
		res = cached()
		if res != nil {
			cachedir.Use(dir)
		}
		unlock()
		if res != nil {
			return res, nil
		}
	}
	// One axx prepares a build at a time (a run and the language server often
	// start together); the others wait for it and use what it built.
	unlock, err := filelock.Lock(ctx, dir+".lock", func() {
		fmt.Fprintln(o.Log, "axx: waiting for another axx preparing the same packs")
	})
	if err != nil {
		return nil, err
	}
	defer unlock()
	if res := cached(); res != nil {
		cachedir.Use(dir)
		return res, nil
	}

	tc := o.Toolchain
	if tc == nil {
		if tc, err = gotool.Ensure(ctx, gotool.Options{Log: o.Log}); err != nil {
			return nil, axxerr.Wrap(err, CodeNoGo, exitcode.Environment, "cannot prepare this project's packs").
				WithHint("axx downloads Go once, as the go command does: from the Go module proxy (GOPROXY), checked against Go's checksum database (GOSUMDB). Check the network and those settings, or set AXX_GO to an installed go")
		}
	}
	src := filepath.Join(dir, "src")
	if err := os.RemoveAll(src); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(o.Entries))
	for _, e := range o.Entries {
		names = append(names, e.Raw)
	}
	// Downloading the packs' modules and building them takes a while the
	// first time: the line says so while it does (o.Log may be a Reporter).
	what := "preparing " + strings.Join(names, ", ")
	switch {
	case o.Debug:
		what = "preparing a build for debugging"
	case len(names) == 0:
		what = "preparing"
	}
	step := progress.On(o.Log).Start("axx: "+what+" (once; cached for later runs)", what)
	defer step.Done()
	start := time.Now()

	if err := os.WriteFile(filepath.Join(src, "go.mod"), goMod(o, locals), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), mainGo(o.Entries, locals), 0o644); err != nil {
		return nil, err
	}
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, tc.Go, args...)
		cmd.Dir = src
		cmd.Env = tc.Env
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			if offline.Match(out.Bytes()) {
				return axxerr.New(CodeBuild, exitcode.Environment, "preparing the packs failed: their Go modules could not be downloaded (go %s)\n%s", strings.Join(args, " "), strings.TrimSpace(out.String())).
					WithHint("axx downloads a pack's modules once, from the Go module proxy, and this machine has not got them yet; run again once it can reach the proxy (GOPROXY), after which these packs, for any project, prepare offline")
			}
			return axxerr.New(CodeBuild, exitcode.Usage, "preparing the packs failed (go %s)\n%s", strings.Join(args, " "), strings.TrimSpace(out.String())).
				WithHint("a pack is a Go package that exports `func Pack() core.Pack` (github.com/nimbusxr/axx/core); fix the errors above and run again")
		}
		return nil
	}
	for _, e := range o.Entries {
		if e.Kind != packset.Module {
			continue
		}
		v := e.Version
		if pinned := o.Lock.Version(e.Module); pinned != "" && v == "" {
			v = pinned
		}
		if v == "" {
			v = "latest"
		}
		if err := run("get", e.Module+"@"+v); err != nil {
			return nil, err
		}
	}
	tmp := bin + ".tmp"
	build := []string{"build", "-trimpath", "-ldflags", "-s -w", "-o", tmp, "."}
	if o.Debug {
		build = []string{"build", "-gcflags=all=-N -l", "-o", tmp, "."}
	}
	if err := run(build...); err != nil {
		return nil, err
	}
	lock, err := resolvedLock(ctx, tc, src, o)
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(lock); err == nil {
		_ = os.WriteFile(lockFile, b, 0o644)
	}
	if err := os.Rename(tmp, bin); err != nil {
		return nil, err
	}
	cachedir.Use(dir)
	step.Done()
	fmt.Fprintf(o.Log, "axx: ready in %s\n", time.Since(start).Round(100*time.Millisecond))
	return &Result{Binary: bin, Lock: lock}, nil
}

// local is a custom pack in the project, with the module that contains it.
type local struct {
	entry      packset.Entry
	dir        string // absolute package directory
	moduleRoot string // directory of its go.mod
	modulePath string
	importPath string
}

func resolveLocals(projectDir string, entries []packset.Entry) (map[string]local, error) {
	out := map[string]local{}
	for _, e := range entries {
		if e.Kind != packset.Local {
			continue
		}
		dir := filepath.FromSlash(e.Path)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(projectDir, dir)
		}
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			return nil, axxerr.New(CodeBuild, exitcode.Usage, "%s: custom pack %s is not a directory", packset.FileName, e.Path).
				WithHint("create it with `axx pack new %s`", e.Path)
		}
		root, modPath, err := findModule(dir)
		if err != nil {
			return nil, axxerr.Wrap(err, CodeBuild, exitcode.Usage, "custom pack %s", e.Path).
				WithHint("a custom pack lives in a Go module; `axx pack new %s` creates one", e.Path)
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		imp := modPath
		if rel != "." {
			imp = modPath + "/" + filepath.ToSlash(rel)
		}
		out[e.Key()] = local{entry: e, dir: dir, moduleRoot: root, modulePath: modPath, importPath: imp}
	}
	return out, nil
}

// findModule walks up from dir to the nearest go.mod.
func findModule(dir string) (root, modulePath string, err error) {
	for d := dir; ; {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			sc := bufio.NewScanner(bytes.NewReader(b))
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if rest, ok := strings.CutPrefix(line, "module "); ok {
					return d, strings.Trim(strings.TrimSpace(rest), `"`), nil
				}
			}
			return "", "", fmt.Errorf("%s has no module line", filepath.Join(d, "go.mod"))
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", errors.New("no go.mod found in its directory or above")
		}
		d = parent
	}
}

func buildKey(o Options, locals map[string]local) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "axx=%s\nsource=%s\n%s/%s\ndebug=%t\n", o.AxxVersion, o.AxxSource, runtime.GOOS, runtime.GOARCH, o.Debug)
	// A build from a source directory is of whatever the directory holds, the files its packs
	// embed included, which hashTree does not read: the axx built from it stands for them, by
	// its size and time. A release's build is of the release's module, the same for every copy
	// of that release: the key has nothing of the copy, so that a machine that installs axx
	// afresh for each run (a CI runner) finds the build it kept from the run before.
	if o.AxxSource != "" {
		if self, err := executable(); err == nil {
			if st, err := os.Stat(self); err == nil {
				fmt.Fprintf(h, "self=%d:%d\n", st.Size(), st.ModTime().UnixNano())
			}
		}
	}
	for _, e := range o.Entries {
		fmt.Fprintf(h, "entry=%s pinned=%s\n", e.Raw, o.Lock.Version(e.Module))
	}
	if o.AxxSource != "" {
		if err := hashTree(h, o.AxxSource); err != nil {
			return "", err
		}
	}
	keys := make([]string, 0, len(locals))
	for k := range locals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// A custom pack builds with the rest of its module: a change to a package beside it (one it
	// imports) changes the build as much as one to the pack. Each module is hashed once.
	hashed := map[string]bool{}
	for _, k := range keys {
		l := locals[k]
		if hashed[l.moduleRoot] {
			continue
		}
		hashed[l.moduleRoot] = true
		if err := hashTree(h, l.moduleRoot); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:20], nil
}

// executable is the running axx; a test names another.
var executable = os.Executable

// hashTree hashes the Go sources under dir: their names, from dir, and what they hold. Not their
// times, nor where dir is: a fresh checkout of the same commit, wherever it is, has the same key.
func hashTree(h io.Writer, dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			// A directory with its own go.mod is another module, not built with this one.
			if _, err := os.Stat(filepath.Join(p, "go.mod")); p != dir && err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, f); err != nil {
			return err
		}
		fmt.Fprintf(h, "%s:%x\n", filepath.ToSlash(rel), sum.Sum(nil))
		return nil
	})
}

// ModuleVersion is the module version of an axx version ("0.1.0" is
// "v0.1.0"), or "" for a development build, which has none.
func ModuleVersion(v string) string {
	if v == "" || strings.Contains(v, "dev") || strings.Contains(v, "+") {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func goMod(o Options, locals map[string]local) []byte {
	var b strings.Builder
	b.WriteString("module axx.local/build\n\ngo 1.27\n\n")
	axxVersion := ModuleVersion(o.AxxVersion)
	if o.AxxSource != "" || axxVersion == "" {
		axxVersion = "v0.0.0"
	}
	fmt.Fprintf(&b, "require %s %s\n", AxxModule, axxVersion)
	mods := map[string]string{}
	for _, l := range locals {
		mods[l.modulePath] = l.moduleRoot
	}
	paths := make([]string, 0, len(mods))
	for p := range mods {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(&b, "require %s v0.0.0\n", p)
	}
	b.WriteString("\n")
	if o.AxxSource != "" {
		fmt.Fprintf(&b, "replace %s => %s\n", AxxModule, filepath.ToSlash(o.AxxSource))
	}
	for _, p := range paths {
		fmt.Fprintf(&b, "replace %s => %s\n", p, filepath.ToSlash(mods[p]))
	}
	return []byte(b.String())
}

func mainGo(entries []packset.Entry, locals map[string]local) []byte {
	var imports, regs strings.Builder
	for i, e := range entries {
		imp := e.Module
		switch e.Kind {
		case packset.Local:
			imp = locals[e.Key()].importPath
		case packset.Published:
			p, _ := packset.Lookup(e.Name)
			imp = p.Import
		}
		fmt.Fprintf(&imports, "\tp%d %q\n", i, imp)
		fmt.Fprintf(&regs, "\t\t%q: p%d.Pack(),\n", e.Key(), i)
	}
	return []byte(`// Code generated by axx for this project's packs. DO NOT EDIT.
package main

import (
	"github.com/nimbusxr/axx/app"
	"github.com/nimbusxr/axx/core"

` + imports.String() + `)

func main() {
	app.Main(map[string]core.Pack{
` + regs.String() + `	})
}
`)
}

// resolvedLock records the versions the build resolved for module packs.
func resolvedLock(ctx context.Context, tc *gotool.Toolchain, src string, o Options) (*packset.Lock, error) {
	lock := &packset.Lock{Axx: o.AxxVersion}
	for _, e := range o.Entries {
		if e.Kind != packset.Module {
			continue
		}
		cmd := exec.CommandContext(ctx, tc.Go, "list", "-m", "-f", "{{.Path}} {{.Version}}", "all")
		cmd.Dir = src
		cmd.Env = tc.Env
		out, err := cmd.Output()
		if err != nil {
			return nil, axxerr.Wrap(err, CodeBuild, exitcode.Usage, "cannot list the build's modules")
		}
		best := ""
		version := ""
		for _, line := range strings.Split(string(out), "\n") {
			p, v, ok := strings.Cut(strings.TrimSpace(line), " ")
			// the module that provides the package: the longest module path prefix
			if ok && (e.Module == p || strings.HasPrefix(e.Module, p+"/")) && len(p) > len(best) {
				best, version = p, v
			}
		}
		if best != "" {
			lock.Modules = append(lock.Modules, packset.LockedModule{Path: e.Module, Version: version})
		}
	}
	return lock, nil
}

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}
