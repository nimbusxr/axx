package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/packbuild"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/version"
)

// CodePackArg: an axx pack argument is invalid.
const CodePackArg = "AXX-E0011"

func newPackCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Manage the packs this project uses (axx-packs.yaml)",
		Long: `Packs are sets of steps. A project lists the packs it uses in axx-packs.yaml:
axx's packs by name (rest, sql, kafka, logs, aws-s3, gcp-pubsub, ...; see
` + "`axx pack list`" + `), and packs of its own, Go packages that build on the public
core and the packs' contexts, by path (./steps) or Go module path
(github.com/team/axx-grpc@v1.2.0). axx prepares itself with the listed packs
on first use and caches the result.`,
	}
	cmd.AddCommand(newPackAddCmd(app), newPackRemoveCmd(app), newPackListCmd(app), newPackUpdateCmd(app), newPackNewCmd(app))
	return cmd
}

// packFile loads axx-packs.yaml, or a new, empty one.
func (a *App) packFile() (dir string, f *packset.File, existed bool, err error) {
	dir, err = config.ProjectDir(a.Config)
	if err != nil {
		return "", nil, false, err
	}
	f, existed, err = packset.Load(dir)
	if err != nil {
		return "", nil, false, axxerr.Wrap(err, engine.CodeUnknownPack, exitcode.Usage, "cannot read %s", packset.FileName)
	}
	if !existed {
		f = &packset.File{}
	}
	return dir, f, existed, nil
}

func checkEntry(dir, raw string) error {
	e, err := packset.Parse(raw)
	if err != nil {
		return axxerr.Wrap(err, CodePackArg, exitcode.Usage, "invalid pack %q", raw)
	}
	switch e.Kind {
	case packset.Published:
		if _, ok := packset.Lookup(e.Name); !ok {
			return axxerr.New(CodePackArg, exitcode.Usage, "%q is not a pack axx publishes", e.Name).
				WithHint("axx's packs: %s; other packs are added by path (./steps) or Go module path", strings.Join(packset.Names(), ", "))
		}
	case packset.Local:
		p := filepath.FromSlash(e.Path)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			return axxerr.New(CodePackArg, exitcode.Usage, "pack %s does not exist", e.Path).
				WithHint("create it with `axx pack new %s`", e.Path)
		}
	}
	return nil
}

func newPackAddCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "add <pack>...",
		Short: "Add packs: axx's by name, others by path or Go module",
		Example: `  axx pack add rest sql
  axx pack add ./steps
  axx pack add github.com/team/axx-grpc@v1.2.0`,
		Args: wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			dir, f, existed, err := app.packFile()
			if err != nil {
				return err
			}
			for _, a := range args {
				if err := checkEntry(dir, a); err != nil {
					return err
				}
			}
			added, err := f.Add(args...)
			if err != nil {
				return axxerr.Wrap(err, CodePackArg, exitcode.Usage, "cannot add packs")
			}
			if err := f.Save(dir); err != nil {
				return err
			}
			return app.Emit(map[string]any{"added": added, "packs": f.Packs, "file": filepath.Join(dir, packset.FileName)}, func(w io.Writer) error {
				if !existed {
					fmt.Fprintf(w, "created %s\n", packset.FileName)
				}
				if len(added) == 0 {
					_, err := fmt.Fprintln(w, "nothing to add; already listed")
					return err
				}
				_, err := fmt.Fprintf(w, "added %s\n", strings.Join(added, ", "))
				return err
			})
		},
	}
}

func newPackRemoveCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <pack>...",
		Aliases: []string{"rm"},
		Short:   "Remove packs from this project",
		Args:    wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			dir, f, _, err := app.packFile()
			if err != nil {
				return err
			}
			removed, err := f.Remove(args...)
			if err != nil {
				return axxerr.Wrap(err, CodePackArg, exitcode.Usage, "cannot remove packs")
			}
			if len(removed) < len(args) {
				return axxerr.New(CodePackArg, exitcode.Usage, "not every pack given is listed in %s", packset.FileName).
					WithHint("`axx pack list` shows the project's packs")
			}
			if err := f.Save(dir); err != nil {
				return err
			}
			return app.Emit(map[string]any{"removed": removed, "packs": f.Packs}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "removed %s\n", strings.Join(removed, ", "))
				return err
			})
		},
	}
}

// PackInfo is one row of `axx pack list`.
type PackInfo struct {
	Pack  string `json:"pack"`
	Kind  string `json:"kind"`
	Steps int    `json:"steps"`
	Used  bool   `json:"used"`
}

func newPackListCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the project's packs and axx's packs it does not use",
		Args:    wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			_, f, existed, err := app.packFile()
			if err != nil {
				return err
			}
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			steps := map[string]int{}
			for _, d := range e.Registry.Defs() {
				steps[d.Pack]++
			}
			var rows []PackInfo
			used := map[string]bool{}
			entries, _ := f.Entries()
			for _, en := range packset.WithRequired(entries) {
				name := en.Key()
				if en.Kind != packset.Published {
					if pk, ok := engine.Compiled()[en.Key()]; ok {
						name = pk.Manifest().Name
					}
				}
				used[en.Key()] = true
				rows = append(rows, PackInfo{Pack: en.Raw, Kind: en.Kind.String(), Steps: steps[name], Used: true})
			}
			for _, p := range packset.Catalog {
				if !used[p.Name] {
					rows = append(rows, PackInfo{Pack: p.Name, Kind: packset.Published.String()})
				}
			}
			return app.Emit(map[string]any{"file": existed, "packs": rows}, func(w io.Writer) error {
				if !existed {
					fmt.Fprintf(w, "no %s: this project uses no packs yet; add them with `axx pack add rest sql ...`\n", packset.FileName)
				}
				for _, r := range rows {
					state := "not used"
					if r.Used {
						state = fmt.Sprintf("%d steps", r.Steps)
					}
					fmt.Fprintf(w, "  %-40s %-9s %s\n", r.Pack, r.Kind, state)
				}
				return nil
			})
		},
	}
	cf.register(cmd)
	return cmd
}

func newPackUpdateCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "update [module]...",
		Short: "Move module packs to their newest (or requested) versions and rebuild",
		Long: `Clear the pinned versions of module packs (all of them, or the ones named) in
axx-packs.lock and rebuild, so each resolves to the version in axx-packs.yaml
or, without one, the latest.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, f, _, err := app.packFile()
			if err != nil {
				return err
			}
			entries, _ := f.Entries()
			modules := 0
			for _, e := range entries {
				if e.Kind == packset.Module {
					modules++
				}
			}
			lock, err := packset.LoadLock(dir)
			if err != nil {
				return err
			}
			kept := lock.Modules[:0]
			for _, m := range lock.Modules {
				if len(args) > 0 && !slices.Contains(args, m.Path) {
					kept = append(kept, m)
				}
			}
			lock.Modules = kept
			if modules == 0 {
				return app.Emit(map[string]any{"modules": []any{}}, func(w io.Writer) error {
					_, err := fmt.Fprintln(w, "no module packs to update")
					return err
				})
			}
			info := version.Get()
			res, err := packbuild.Ensure(cmd.Context(), packbuild.Options{
				ProjectDir: dir, Entries: entries, Lock: lock, AxxVersion: info.Version, AxxSource: axxSource(info), Log: app.Stderr,
			})
			if err != nil {
				return err
			}
			res.Lock.Axx = info.Version
			if err := res.Lock.Save(dir); err != nil {
				return err
			}
			return app.Emit(map[string]any{"modules": res.Lock.Modules}, func(w io.Writer) error {
				for _, m := range res.Lock.Modules {
					fmt.Fprintf(w, "  %s %s\n", m.Path, m.Version)
				}
				return nil
			})
		},
	}
}

var packNameRE = regexp.MustCompile(`[^a-z0-9]+`)

func newPackNewCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "new <dir>",
		Short: "Create a pack in <dir> and add it to the project",
		Long: `Create a pack of your own: a Go package that builds on the public core
(github.com/nimbusxr/axx/core) and can use the contexts of axx's packs
(github.com/nimbusxr/axx/packs/rest, .../sql, ...). It is added to
axx-packs.yaml; the next command that needs the project's steps prepares axx
with it.`,
		Args: wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, f, existed, err := app.packFile()
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(filepath.Clean(args[0]))
			if !strings.HasPrefix(rel, ".") && !filepath.IsAbs(args[0]) {
				rel = "./" + rel
			}
			target := filepath.Join(dir, filepath.FromSlash(rel))
			if _, err := os.Stat(filepath.Join(target, "pack.go")); err == nil {
				return axxerr.New(CodePackArg, exitcode.Usage, "%s already has a pack.go", rel)
			}
			name := strings.Trim(packNameRE.ReplaceAllString(strings.ToLower(filepath.Base(target)), "-"), "-")
			if name == "" {
				name = "custom"
			}
			pkg := strings.ReplaceAll(name, "-", "")
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			written := []string{filepath.Join(rel, "pack.go")}
			if err := os.WriteFile(filepath.Join(target, "pack.go"), []byte(packTemplate(pkg, name)), 0o644); err != nil {
				return err
			}
			if !inGoModule(target) {
				info := version.Get()
				if err := os.WriteFile(filepath.Join(target, "go.mod"), []byte(packGoMod(name, info, axxSource(info))), 0o644); err != nil {
					return err
				}
				written = append(written, filepath.Join(rel, "go.mod"))
				// go.sum, so editors and Go tools resolve the pack's imports.
				if err := tidy(cmd.Context(), target); err != nil {
					fmt.Fprintf(app.Stderr, "axx: run `go mod tidy` in %s so editors can resolve its imports (%v)\n", rel, err)
				} else if _, err := os.Stat(filepath.Join(target, "go.sum")); err == nil {
					written = append(written, filepath.Join(rel, "go.sum"))
				}
			}
			if _, err := f.Add(rel); err != nil {
				return err
			}
			if err := f.Save(dir); err != nil {
				return err
			}
			return app.Emit(map[string]any{"written": written, "packs": f.Packs}, func(w io.Writer) error {
				if !existed {
					fmt.Fprintf(w, "created %s\n", packset.FileName)
				}
				_, err := fmt.Fprintf(w, "created the %s pack in %s and added it to %s\nits example step: Then the %s pack is loaded\n", name, rel, packset.FileName, name)
				return err
			})
		},
	}
}

// inGoModule reports whether dir is inside a Go module.
// packGoMod is the go.mod of a new pack's module. A development build of
// axx has no published version to require, so the module uses the checkout
// that build came from.
func packGoMod(name string, info version.Info, source string) string {
	v := packbuild.ModuleVersion(info.Version)
	if info.Channel == "dev" || v == "" {
		v = "v0.0.0"
	}
	mod := fmt.Sprintf("module axx.local/%s\n\ngo 1.27\n\nrequire %s %s\n", name, packbuild.AxxModule, v)
	if info.Channel == "dev" && source != "" {
		mod += fmt.Sprintf("\nreplace %s => %s\n", packbuild.AxxModule, filepath.ToSlash(source))
	}
	return mod
}

// tidy runs `go mod tidy` in dir, when Go is installed.
func tidy(ctx context.Context, dir string) error {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return errors.New("go is not on PATH")
	}
	c := exec.CommandContext(ctx, goBin, "mod", "tidy")
	c.Dir = dir
	c.Env = append(os.Environ(), "GOWORK=off")
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func inGoModule(dir string) bool {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}

func packTemplate(pkg, name string) string {
	return `// Package ` + pkg + ` is an axx pack. axx loads it because it is listed in
// axx-packs.yaml.
//
// Steps receive the scenario, which gives access to every pack's context:
// the same services, requests, responses and selections the steps of axx's
// packs use. For example rest.Context(sc).Service() (github.com/nimbusxr/axx/packs/rest)
// or sql.Context(sc).Service("db") (github.com/nimbusxr/axx/packs/sql).
package ` + pkg + `

import "github.com/nimbusxr/axx/core"

// Pack returns the pack. axx calls it once per run.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: "` + name + `",
		Doc:  "Custom steps for this project.",
		Steps: []core.StepDef{{
			ID:       "` + name + `.loaded",
			Keyword:  "Then",
			Expr:     "the ` + name + ` pack is loaded",
			Doc:      "An example step; replace it with your own.",
			Examples: []string{"Then the ` + name + ` pack is loaded"},
			Run: func(sc *core.Scenario, _ core.Args) error {
				sc.Log("the ` + name + ` pack is loaded")
				return nil
			},
		}},
	}
}
`
}
