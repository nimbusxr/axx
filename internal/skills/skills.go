// Package skills builds and installs axx's agent skills: hand-written
// SKILL.md files embedded in the binary plus reference files generated from
// the step registry at install time (so a project's custom packs are
// included and nothing can drift).
package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/render/md"
	"github.com/nimbusxr/axx/internal/version"
)

//go:embed assets
var assets embed.FS

// File is one file of a skill, relative to the skill directory.
type File struct {
	Path    string
	Content []byte
}

// Skill is a named skill and its files.
type Skill struct {
	Name  string
	Files []File
}

// Names returns the embedded skill names.
func Names() []string {
	entries, _ := fs.ReadDir(assets, "assets")
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Build renders every skill. References come from e's registry.
func Build(e *engine.Engine) ([]Skill, error) {
	refs, err := references(e)
	if err != nil {
		return nil, err
	}
	codes, err := md.ErrorCodesPage(false)
	if err != nil {
		return nil, err
	}
	errorCodes := File{Path: "references/error-codes.md", Content: []byte(codes)}
	var out []Skill
	for _, name := range Names() {
		sk := Skill{Name: name}
		err := fs.WalkDir(assets, "assets/"+name, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := assets.ReadFile(p)
			if err != nil {
				return err
			}
			sk.Files = append(sk.Files, File{Path: strings.TrimPrefix(p, "assets/"+name+"/"), Content: b})
			return nil
		})
		if err != nil {
			return nil, err
		}
		switch name {
		case "axx-acceptance-tests", "axx-custom-steps":
			sk.Files = append(sk.Files, refs...)
		case "axx-debugging", "axx-setup":
			sk.Files = append(sk.Files, errorCodes)
		}
		sort.Slice(sk.Files, func(i, j int) bool { return sk.Files[i].Path < sk.Files[j].Path })
		out = append(out, sk)
	}
	return out, nil
}

func references(e *engine.Engine) ([]File, error) {
	var out []File
	idx, err := md.StepIndex(e.Registry)
	if err != nil {
		return nil, err
	}
	out = append(out, File{Path: "references/step-index.md", Content: []byte(idx)})
	params, err := md.ParamsPage(e.Registry, false)
	if err != nil {
		return nil, err
	}
	out = append(out, File{Path: "references/parameter-types.md", Content: []byte(params)})
	manifests := e.Manifests()
	for _, name := range e.PackNames() {
		m := manifests[name]
		if len(m.Steps) == 0 {
			continue
		}
		page, err := md.StepsPage(e.Registry, md.PackInfo{Name: name, Manifest: m}, false)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: "references/steps-" + name + ".md", Content: []byte(page)})
	}
	out = append(out, File{Path: "references/config.md", Content: []byte(configRef)})
	return out, nil
}

const configRef = md.GeneratedHeader + `
# axx.yaml essentials

The full JSON Schema is printed by ` + "`axx schema`" + ` (also at https://axx.nimbusxr.us/schemas/v0/axx.schema.json).

` + "```yaml" + `
# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx.schema.json
version: 1
run:
  paths: [features]            # feature files/directories
  tags: "not @wip"             # default tag filter
  workers: auto                # parallel scenarios
  exclusive: ["@isolated"]     # tags that run alone after the parallel phase
  timeouts: {step: 60s}
  reporters: [pretty, {junit: build/axx/junit.xml}]
resources: ["."]               # where seed/payload/schema paths in steps are looked up
properties: {local.host: localhost}   # ${sys:local.host}; override with -D local.host=...
apps:
  api:
    command: docker compose up --build
    ready: {http: {url: http://localhost:8080/health}, timeout: 120s}
    cleanup: docker compose down -v
profiles:
  ci: {properties: {local.host: docker}}
` + "```" + `
`

// Lock records installed files so updates never overwrite local edits.
type Lock struct {
	AxxVersion string            `json:"axxVersion"`
	Files      map[string]string `json:"files"` // path relative to the skills dir -> sha256
}

// InstallOptions controls Install.
type InstallOptions struct {
	// Root is the project directory (scope project) or home directory.
	Root string
	// Claude also links skills into .claude/skills (Claude Code reads only there).
	Claude bool
	// Force overwrites locally modified files.
	Force bool
}

// Result reports what Install did.
type Result struct {
	Dir      string   `json:"dir"`
	Written  []string `json:"written"`
	Kept     []string `json:"kept,omitempty"` // modified locally, not overwritten
	Linked   []string `json:"linked,omitempty"`
	Skills   []string `json:"skills"`
	Location string   `json:"claudeDir,omitempty"`
}

// Install writes skills to <root>/.agents/skills and optionally links them
// into <root>/.claude/skills.
func Install(skills []Skill, opts InstallOptions) (*Result, error) {
	dir := filepath.Join(opts.Root, ".agents", "skills")
	res := &Result{Dir: dir}
	lockPath := filepath.Join(dir, ".axx-lock.json")
	old := Lock{Files: map[string]string{}}
	if b, err := os.ReadFile(lockPath); err == nil {
		_ = json.Unmarshal(b, &old)
		if old.Files == nil {
			old.Files = map[string]string{}
		}
	}
	lock := Lock{AxxVersion: version.Get().Version, Files: map[string]string{}}
	for _, sk := range skills {
		res.Skills = append(res.Skills, sk.Name)
		for _, f := range sk.Files {
			rel := path.Join(sk.Name, f.Path)
			dst := filepath.Join(dir, filepath.FromSlash(rel))
			newHash := hash(f.Content)
			if cur, err := os.ReadFile(dst); err == nil {
				curHash := hash(cur)
				if curHash != newHash && curHash != old.Files[rel] && !opts.Force {
					res.Kept = append(res.Kept, rel)
					lock.Files[rel] = old.Files[rel]
					continue
				}
				if curHash == newHash {
					lock.Files[rel] = newHash
					continue
				}
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(dst, f.Content, 0o644); err != nil {
				return nil, err
			}
			lock.Files[rel] = newHash
			res.Written = append(res.Written, rel)
		}
	}
	b, _ := json.MarshalIndent(lock, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(lockPath, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	if opts.Claude {
		cdir := filepath.Join(opts.Root, ".claude", "skills")
		res.Location = cdir
		for _, sk := range skills {
			linked, err := linkSkill(filepath.Join(dir, sk.Name), filepath.Join(cdir, sk.Name))
			if err != nil {
				return nil, err
			}
			if linked {
				res.Linked = append(res.Linked, sk.Name)
			}
		}
	}
	return res, nil
}

// linkSkill makes dst point at src: a relative symlink, or a copy where
// symlinks are unavailable (Windows without developer mode).
func linkSkill(src, dst string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return false, nil // already linked
		}
		if fi.IsDir() {
			// an existing copy: refresh it
			return true, copyDir(src, dst)
		}
		return false, fmt.Errorf("%s exists and is not a skill directory", dst)
	}
	rel, err := filepath.Rel(filepath.Dir(dst), src)
	if err != nil {
		rel = src
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(rel, dst); err == nil {
			return true, nil
		}
	}
	return true, copyDir(src, dst)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// Export writes skills to dir (used to generate the plugin marketplace copy).
func Export(skills []Skill, dir string) ([]string, error) {
	var written []string
	for _, sk := range skills {
		if len(sk.Files) == 0 {
			return nil, errors.New("skill " + sk.Name + " is empty")
		}
		for _, f := range sk.Files {
			p := filepath.Join(dir, sk.Name, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(p, f.Content, 0o644); err != nil {
				return nil, err
			}
			written = append(written, p)
		}
	}
	return written, nil
}

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
