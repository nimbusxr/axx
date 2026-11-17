package packbuild

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/packset"
)

// Inputs are the files of the project that a build with entries compiles:
// each custom pack's module (its Go and assembly sources without the tests,
// its go.mod and go.sum) and the files those sources embed. Sorted,
// absolute, each once.
//
// It is what of a project's own files becomes code in its axx: the axx host
// compares it before it restarts by itself, so that a pack that changed
// since a person started the host runs there only with their word.
func Inputs(projectDir string, entries []packset.Entry) ([]string, error) {
	locals, err := resolveLocals(projectDir, packset.WithRequired(entries))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	roots := map[string]bool{}
	for _, l := range locals {
		if roots[l.moduleRoot] {
			continue
		}
		roots[l.moduleRoot] = true
		err := walkModule(l.moduleRoot, func(p string, d fs.DirEntry) error {
			name := d.Name()
			switch {
			case name == "go.mod", name == "go.sum", strings.HasSuffix(name, ".s"):
				add(p)
			case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
				add(p)
				for _, e := range embedded(p) {
					add(e)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(out)
	return out, nil
}

// walkModule calls fn for the files of the module at root: not those of a
// folder the go command leaves out, nor those of a module inside it.
func walkModule(root string, fn func(p string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); p != root && err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return fn(p, d)
	})
}

// embedded are the files the Go source at path embeds (its //go:embed
// lines): each pattern's files, and every file under a folder it names.
func embedded(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "//go:embed ")
		if !ok {
			continue
		}
		for _, pattern := range embedPatterns(rest) {
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), filepath.FromSlash(strings.TrimPrefix(pattern, "all:"))))
			if err != nil {
				continue
			}
			for _, m := range matches {
				_ = filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
					if err == nil && d.Type().IsRegular() {
						out = append(out, p)
					}
					return nil
				})
			}
		}
	}
	return out
}

// embedPatterns splits a //go:embed line's patterns: words, of which one
// with a space is a quoted Go string.
func embedPatterns(line string) []string {
	var out []string
	for line = strings.TrimSpace(line); line != ""; line = strings.TrimSpace(line) {
		if line[0] == '"' || line[0] == '`' {
			if quoted, err := strconv.QuotedPrefix(line); err == nil {
				if s, err := strconv.Unquote(quoted); err == nil {
					out = append(out, s)
				}
				line = line[len(quoted):]
				continue
			}
		}
		word, rest, _ := strings.Cut(line, " ")
		out = append(out, word)
		line = rest
	}
	return out
}
