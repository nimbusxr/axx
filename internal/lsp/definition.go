package lsp

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"unicode"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/render/md"
)

type location struct {
	URI   string    `json:"uri"`
	Range textRange `json:"range"`
}

// definitionAt locates what is under the cursor: the file a step's value or
// a table cell names, or else the definition of the step (every matching
// definition, for an ambiguous step).
func (s *server) definitionAt(d *document, pr *project, pos position) []location {
	for _, l := range d.fileLinks(pr, pos.Line) {
		if l.rng.Start.Character <= pos.Character && pos.Character <= l.rng.End.Character {
			return []location{fileLocation(l.path, 0)}
		}
	}
	st, ok := d.stepAt(pos.Line)
	if !ok || pr.reg == nil {
		return nil
	}
	var out []location
	for _, m := range pr.reg.Match(d.matchText(st)) {
		if loc, ok := s.locate(pr, m.Def()); ok {
			out = append(out, loc)
		}
	}
	return out
}

// locate finds where a step is defined: its declared source, or the Go code
// that defines it when that code is on this machine, or else its entry in a
// reference page written for the purpose.
func (s *server) locate(pr *project, def *match.Def) (location, bool) {
	if src := def.Step.Source; src.URI != "" {
		if p := sourcePath(src.URI); p != "" {
			return fileLocation(p, max(src.Line-1, 0)), true
		}
	}
	if file, line := funcSource(def.Step.Run); file != "" {
		if f, l, ok := findID(filepath.Dir(file), file, def.Step.ID); ok {
			return fileLocation(f, l), true
		}
		return fileLocation(file, max(line-1, 0)), true
	}
	return s.referenceLocation(pr, def)
}

func fileLocation(path string, line int) location {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if runtime.GOOS == "windows" {
		u.Path = "/" + u.Path
	}
	return location{URI: u.String(), Range: textRange{Start: position{Line: line}, End: position{Line: line}}}
}

// sourcePath turns a declared source (a file:// URI or a path) into an
// existing local path.
func sourcePath(ref string) string {
	p := ref
	if strings.HasPrefix(ref, "file:") {
		p = uriToPath(ref)
	}
	if p != "" && exists(p) {
		return p
	}
	return ""
}

// funcSource returns the local file and line of a step function, resolving
// the module-relative paths of binaries built with -trimpath through the Go
// module cache.
func funcSource(run core.StepFunc) (string, int) {
	if run == nil {
		return "", 0
	}
	fn := runtime.FuncForPC(reflect.ValueOf(run).Pointer())
	if fn == nil {
		return "", 0
	}
	file, line := fn.FileLine(fn.Entry())
	if filepath.IsAbs(file) {
		if exists(file) {
			return file, line
		}
		return "", 0
	}
	if p := moduleCachePath(file); p != "" {
		return p, line
	}
	return "", 0
}

// moduleCachePath finds a -trimpath file name (module/path/file.go) on this
// machine: in the local directory its module is replaced with, or in the
// module cache at the version this binary was built with.
func moduleCachePath(file string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return resolveModuleFile(file, append([]*debug.Module{&info.Main}, info.Deps...), moduleCache())
}

func moduleCache() string {
	if c := os.Getenv("GOMODCACHE"); c != "" {
		return c
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		gopath = filepath.Join(home, "go")
	}
	return filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
}

// resolveModuleFile maps a -trimpath file name to a local file, using the
// module that contains it (the longest matching module path). The main
// module's files are named module/path/file.go, a dependency's
// module@version/path/file.go.
func resolveModuleFile(file string, mods []*debug.Module, cache string) string {
	var best *debug.Module
	var rest string
	for _, m := range mods {
		if m == nil || (best != nil && len(m.Path) <= len(best.Path)) {
			continue
		}
		for _, prefix := range []string{m.Path + "/", m.Path + "@" + m.Version + "/"} {
			if strings.HasPrefix(file, prefix) {
				best, rest = m, filepath.FromSlash(strings.TrimPrefix(file, prefix))
				break
			}
		}
	}
	if best == nil {
		return ""
	}
	m := best
	if r := best.Replace; r != nil {
		if filepath.IsAbs(r.Path) {
			if p := filepath.Join(r.Path, rest); exists(p) {
				return p
			}
			return ""
		}
		m = r
	}
	if m.Version == "" || m.Version == "(devel)" || cache == "" {
		return ""
	}
	if p := filepath.Join(cache, escapeModulePath(m.Path)+"@"+m.Version, rest); exists(p) {
		return p
	}
	return ""
}

// escapeModulePath applies the module cache's case encoding: an upper-case
// letter becomes "!" and its lower-case form.
func escapeModulePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// findID finds the line of the Go code in dir that names a step id as a
// string literal, trying the id and then its shorter prefixes, because the
// variants of a step family (".on", ordinals) are often defined together
// under the family's id. first is searched before the other files.
func findID(dir, first, id string) (string, int, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, false
	}
	var files []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, n))
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i] == first && files[j] != first })
	contents := map[string][]string{}
	for candidate := id; candidate != ""; {
		lit := `"` + candidate + `"`
		for _, f := range files {
			lines, ok := contents[f]
			if !ok {
				data, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				lines = splitLines(string(data))
				contents[f] = lines
			}
			for i, l := range lines {
				if strings.Contains(l, lit) {
					return f, i, true
				}
			}
		}
		dot := strings.LastIndexByte(candidate, '.')
		if dot < 0 {
			break
		}
		candidate = candidate[:dot]
	}
	return "", 0, false
}

// referenceLocation writes the reference page of a step's pack (once per
// project and version) to the user cache and returns the step's entry in it,
// for steps whose code is not on this machine.
func (s *server) referenceLocation(pr *project, def *match.Def) (location, bool) {
	cache := s.opts.CacheDir
	if cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return location{}, false
		}
		cache = dir
	}
	sum := sha256.Sum256([]byte(pr.dir + "\x00" + s.opts.Version))
	path := filepath.Join(cache, "axx", "lsp", hex.EncodeToString(sum[:8]), def.Pack+".md")
	lines, ok := s.pages[path]
	if !ok {
		page, err := md.StepsPage(pr.reg, md.PackInfo{Name: def.Pack, Manifest: pr.manifests[def.Pack]}, false)
		if err != nil {
			return location{}, false
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return location{}, false
		}
		if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
			return location{}, false
		}
		lines = splitLines(page)
		s.pages[path] = lines
	}
	for i, l := range lines {
		if l == "## `"+def.Step.ID+"`" {
			return fileLocation(path, i), true
		}
	}
	return fileLocation(path, 0), true
}
