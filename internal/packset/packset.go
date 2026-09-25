// Package packset reads and writes axx-packs.yaml, the list of packs a
// project uses, and axx-packs.lock, which pins the versions of packs that
// come from Go modules.
//
// An entry is one of:
//
//	rest                            a pack axx publishes, by name (see Catalog)
//	./steps                         a pack in this repository (a Go package)
//	github.com/team/axx-grpc@v1.2.0 a pack from a Go module (version optional)
//
// Every pack gets into axx the same way: axx builds itself with the packs a
// project lists.
package packset

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// File names, next to axx.yaml.
const (
	FileName = "axx-packs.yaml"
	LockName = "axx-packs.lock"
)

// Kind is where a pack comes from.
type Kind int

const (
	// Published packs are the ones axx publishes, listed by name.
	Published Kind = iota
	// Local packs are Go packages in the project.
	Local
	// Module packs are Go packages fetched from a module path.
	Module
)

func (k Kind) String() string {
	switch k {
	case Local:
		return "local"
	case Module:
		return "module"
	}
	return "axx"
}

// Entry is one line of axx-packs.yaml.
type Entry struct {
	Raw     string // as written
	Kind    Kind
	Name    string // Published: the pack name
	Path    string // Local: the path as written (slash-separated, relative to the file)
	Module  string // Module: the package import path
	Version string // Module: the requested version ("" = latest)
}

// Key identifies the entry independently of its version: the name, the
// local path or the module path.
func (e Entry) Key() string {
	switch e.Kind {
	case Local:
		return e.Path
	case Module:
		return e.Module
	}
	return e.Name
}

// Parse classifies an entry.
func Parse(raw string) (Entry, error) {
	s := strings.TrimSpace(raw)
	e := Entry{Raw: s}
	switch {
	case s == "":
		return e, errors.New("empty pack entry")
	case strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || s == "." || filepath.IsAbs(s):
		e.Kind, e.Path = Local, filepath.ToSlash(filepath.Clean(s))
		if !strings.HasPrefix(e.Path, ".") && !filepath.IsAbs(s) {
			e.Path = "./" + e.Path
		}
	case strings.Contains(s, "/") || strings.Contains(s, "."):
		mod, ver, _ := strings.Cut(s, "@")
		if !strings.Contains(strings.SplitN(mod, "/", 2)[0], ".") {
			return e, fmt.Errorf("%q is not a pack name, a local path (./dir) or a module path (host/path[@version])", s)
		}
		e.Kind, e.Module, e.Version = Module, mod, ver
	default:
		e.Kind, e.Name = Published, s
	}
	return e, nil
}

// File is axx-packs.yaml.
type File struct {
	Packs []string `yaml:"packs"`
}

// Entries parses every entry; duplicates (by Key) are an error.
func (f *File) Entries() ([]Entry, error) {
	seen := map[string]bool{}
	out := make([]Entry, 0, len(f.Packs))
	for _, raw := range f.Packs {
		e, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", FileName, err)
		}
		if seen[e.Key()] {
			return nil, fmt.Errorf("%s: pack %s is listed twice", FileName, e.Key())
		}
		seen[e.Key()] = true
		out = append(out, e)
	}
	return out, nil
}

// Load reads dir/axx-packs.yaml. found is false when the file does not exist.
func Load(dir string) (f *File, found bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	f = &File{}
	if err := yaml.Unmarshal(b, f); err != nil {
		return nil, true, fmt.Errorf("%s: %w", FileName, err)
	}
	if _, err := f.Entries(); err != nil {
		return nil, true, err
	}
	return f, true, nil
}

const fileHeader = "# The packs this project uses. Manage it with `axx pack add|remove`.\n" +
	"# axx's packs by name (`axx pack list`), others by path (./steps) or Go module (host/path@version).\n"

// String is the file's content.
func (f *File) String() string {
	var b strings.Builder
	b.WriteString(fileHeader)
	b.WriteString("packs:\n")
	for _, p := range f.Packs {
		fmt.Fprintf(&b, "  - %s\n", p)
	}
	return b.String()
}

// Save writes dir/axx-packs.yaml.
func (f *File) Save(dir string) error {
	return os.WriteFile(filepath.Join(dir, FileName), []byte(f.String()), 0o644)
}

// Add appends entries not already present (by Key); a module entry with a
// new version replaces the old one in place. It reports what changed.
func (f *File) Add(raws ...string) (added []string, err error) {
	for _, raw := range raws {
		e, err := Parse(raw)
		if err != nil {
			return added, err
		}
		replaced := false
		for i, cur := range f.Packs {
			c, _ := Parse(cur)
			if c.Key() == e.Key() {
				if cur != e.Raw {
					f.Packs[i] = e.Raw
					added = append(added, e.Raw)
				}
				replaced = true
				break
			}
		}
		if !replaced {
			f.Packs = append(f.Packs, e.Raw)
			added = append(added, e.Raw)
		}
	}
	return added, nil
}

// Remove deletes entries by Key (or as written). It reports what was removed.
func (f *File) Remove(raws ...string) (removed []string, err error) {
	for _, raw := range raws {
		e, err := Parse(raw)
		if err != nil {
			return removed, err
		}
		kept := f.Packs[:0]
		for _, cur := range f.Packs {
			c, _ := Parse(cur)
			if c.Key() == e.Key() {
				removed = append(removed, cur)
				continue
			}
			kept = append(kept, cur)
		}
		f.Packs = kept
	}
	return removed, nil
}

// Lock pins module packs to the versions a build resolved.
type Lock struct {
	Axx     string         `yaml:"axx"`
	Modules []LockedModule `yaml:"modules,omitempty"`
}

// LockedModule is one pinned Go module.
type LockedModule struct {
	Path    string `yaml:"path"`
	Version string `yaml:"version"`
}

// LoadLock reads dir/axx-packs.lock (an empty lock when absent).
func LoadLock(dir string) (*Lock, error) {
	b, err := os.ReadFile(filepath.Join(dir, LockName))
	if errors.Is(err, fs.ErrNotExist) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, err
	}
	l := &Lock{}
	if err := yaml.Unmarshal(b, l); err != nil {
		return nil, fmt.Errorf("%s: %w", LockName, err)
	}
	return l, nil
}

// Save writes dir/axx-packs.lock.
func (l *Lock) Save(dir string) error {
	b, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, LockName), append([]byte("# Generated by axx. Pins the versions of module packs; commit it.\n"), b...), 0o644)
}

// Version returns the pinned version of a module, if any.
func (l *Lock) Version(module string) string {
	for _, m := range l.Modules {
		if m.Path == module {
			return m.Version
		}
	}
	return ""
}
