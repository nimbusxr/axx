package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jyaml"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// Hints suggest improvements that need no fix: they are neither errors nor
// warnings and never change the exit code.

// hintMinFiles is how many files of one shape make a hint: fewer repeat too
// little to be worth a factory.
const hintMinFiles = 3

// hintMax caps the hints of one run.
const hintMax = 3

// hintMaxSize skips files too large to be hand-written fixtures.
const hintMaxSize = 1 << 20

// FixtureSources says where scenarios' data files come from.
type FixtureSources struct {
	// Resolve finds a file a step names, as the steps do.
	Resolve func(path string) (string, error)
	// Generated reports whether a fixture factory generates the file.
	Generated func(abs string) bool
}

// FixtureHints suggests fixture factories for data files scenarios read
// that repeat one shape: at least three .json or .yaml files, named by the
// steps' {filepath} arguments, in one directory, whose top-level keys are the
// same, and that no fixture factory generates. Every other file is left out,
// so a hint names files the scenarios use as data.
func FixtureHints(reg *match.Registry, pickles []*feature.Pickle, src FixtureSources, opts Options) []string {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	files := map[string]bool{}
	for _, p := range pickles {
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				continue
			}
			for _, a := range ms[0].Args {
				if a.Param != "filepath" || !a.Present || strings.Contains(a.Raw, "${") {
					continue
				}
				switch strings.ToLower(filepath.Ext(a.Raw)) {
				case ".json", ".yaml", ".yml":
				default:
					continue
				}
				if abs, err := src.Resolve(a.Raw); err == nil {
					files[abs] = true
				}
			}
		}
	}
	type group struct {
		id, dir, ext string
		keys         []string
		files        []string
	}
	groups := map[string]*group{}
	for abs := range files {
		if src.Generated != nil && src.Generated(abs) {
			continue
		}
		keys, ok := topLevelKeys(abs)
		if !ok {
			continue
		}
		ext := strings.ToLower(filepath.Ext(abs))
		if ext == ".yml" {
			ext = ".yaml"
		}
		dir := filepath.Dir(abs)
		id := dir + "\x00" + ext + "\x00" + strings.Join(keys, "\x00")
		g := groups[id]
		if g == nil {
			g = &group{id: id, dir: dir, ext: ext, keys: keys}
			groups[id] = g
		}
		g.files = append(g.files, abs)
	}
	within := withinPaths(opts)
	var found []*group
	for _, g := range groups {
		if len(g.files) < hintMinFiles {
			continue
		}
		if len(opts.Paths) > 0 && !anyWithin(g.files, within) {
			continue
		}
		found = append(found, g)
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if len(a.files) != len(b.files) {
			return len(a.files) > len(b.files)
		}
		return a.id < b.id
	})
	var out []string
	for i, g := range found {
		if i == hintMax {
			break
		}
		out = append(out, fmt.Sprintf("%s/ has %d hand-written %s files of one shape (%s) that no fixture factory generates; "+
			"a factory would keep what they share in one place (optional; `axx fixtures adopt --help`)",
			relSlash(opts.WorkDir, g.dir), len(g.files), g.ext, shape(g.keys)))
	}
	return out
}

// topLevelKeys are the sorted keys of a file whose document is a mapping.
func topLevelKeys(path string) ([]string, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > hintMaxSize {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	doc, err := jyaml.UnmarshalMapping(b)
	if err != nil || doc == nil || len(doc.Keys()) == 0 {
		return nil, false
	}
	keys := append([]string(nil), doc.Keys()...)
	sort.Strings(keys)
	return keys, true
}

func shape(keys []string) string {
	if len(keys) > 4 {
		return strings.Join(keys[:4], ", ") + ", ..."
	}
	return strings.Join(keys, ", ")
}

// withinPaths reports whether a file lies in one of opts.Paths (every file
// when there are none).
func withinPaths(opts Options) func(abs string) bool {
	var roots []string
	for _, p := range opts.Paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(opts.WorkDir, p)
		}
		roots = append(roots, filepath.Clean(p))
	}
	return func(abs string) bool {
		if len(roots) == 0 {
			return true
		}
		for _, root := range roots {
			if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
}

func anyWithin(files []string, within func(string) bool) bool {
	for _, f := range files {
		if within(f) {
			return true
		}
	}
	return false
}
