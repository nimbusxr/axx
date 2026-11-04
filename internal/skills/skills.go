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
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/fixtures"
	"github.com/nimbusxr/axx/internal/render/md"
	"github.com/nimbusxr/axx/internal/version"
	plugin "github.com/nimbusxr/axx/plugins/axx"
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
	// No skill carries the error codes: `axx explain <code>` gives each one
	// offline, where a copy of all of them was some 8,000 tokens a read.
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
		case "axx-acceptance-tests":
			sk.Files = append(sk.Files, refs...)
		case "axx-custom-steps":
			// The step pages are the acceptance-tests skill's; a custom
			// step needs the parameter types.
			for _, f := range refs {
				if f.Path == "references/parameter-types.md" {
					sk.Files = append(sk.Files, f)
				}
			}
		case "axx-test-data":
			sk.Files = append(sk.Files, File{Path: "references/spec-files.md", Content: []byte(specFilesRef())})
		}
		sort.Slice(sk.Files, func(i, j int) bool { return sk.Files[i].Path < sk.Files[j].Path })
		out = append(out, sk)
	}
	return out, nil
}

// Published returns the skills as axx publishes them, with the references
// of every pack axx publishes: those of its plugin for Claude Code.
func Published() ([]Skill, error) {
	var out []Skill
	for _, name := range Names() {
		sk := Skill{Name: name}
		root := "skills/" + name
		err := fs.WalkDir(plugin.Skills, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := plugin.Skills.ReadFile(p)
			if err != nil {
				return err
			}
			sk.Files = append(sk.Files, File{Path: strings.TrimPrefix(p, root+"/"), Content: b})
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, nil
}

// references are the reference files of the skills. The steps are not
// among them: agents read step pages a chunk at a time, about 3,500 tokens a
// read, where `axx steps` lists every step a line each and `axx steps show
// <id>` gives one step's documentation in about 300, for the project's own
// packs too.
func references(e *engine.Engine) ([]File, error) {
	params, err := md.ParamsPage(e.Registry, false)
	if err != nil {
		return nil, err
	}
	return []File{
		{Path: "references/parameter-types.md", Content: []byte(params)},
		{Path: "references/config.md", Content: []byte(configRef)},
	}, nil
}

const configRef = md.GeneratedHeader + `
# axx.yaml essentials

The full JSON Schema is printed by ` + "`axx schema`" + ` (also at https://raw.githubusercontent.com/nimbusxr/axx/main/schemas/v0/axx.schema.json).

` + "```yaml" + `
# yaml-language-server: $schema=https://raw.githubusercontent.com/nimbusxr/axx/main/schemas/v0/axx.schema.json
version: 1
run:
  paths: [features]            # feature files/directories
  tags: "not @wip"             # default tag filter
  workers: auto                # parallel scenarios
  exclusive: ["@isolated"]     # tags that run alone after the parallel phase
  exclusiveUses: [sql.trigger] # packs or steps whose scenarios run alone too
  timeouts: {step: 60s}
  reporters: [pretty, {junit: build/axx/junit.xml}]
resources: ["."]               # where seed/payload/schema paths in steps are looked up
properties: {local.host: localhost}   # ${sys:local.host}; override with -D local.host=...
services:
  api:
    command: docker compose up --build
    ready: {http: {url: http://localhost:8080/health}, timeout: 120s}
    cleanup: docker compose down -v
profiles:
  ci: {properties: {local.host: docker}}
` + "```" + `
`

// specFilesRef documents the fixture spec files with their JSON Schemas,
// the ones `axx schema --kind factory|fixture|prototype` prints.
func specFilesRef() string {
	var b strings.Builder
	b.WriteString(md.GeneratedHeader + `
# Fixture spec files

The JSON Schemas of the files ` + "`axx fixtures`" + ` reads, as ` + "`axx schema --kind <kind>`" + ` prints them. Reference one from the top of a file for completion in editors:

` + "```yaml" + `
# yaml-language-server: $schema=` + fixtures.SchemaID("factory") + `
` + "```" + `
`)
	for _, kind := range fixtures.SchemaKinds {
		schema, _ := fixtures.SchemaJSON(kind)
		fmt.Fprintf(&b, "\n## `*.%s.yaml`\n\n```json\n%s\n```\n", kind, strings.TrimSpace(string(schema)))
	}
	return b.String()
}

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
	// Ignore keeps the skills out of the repository (see EnsureIgnored): a
	// project's, not a home directory's.
	Ignore bool
}

// Result reports what Install did.
type Result struct {
	Dir     string   `json:"dir"`
	Written []string `json:"written"`
	Kept    []string `json:"kept,omitempty"` // modified locally, not overwritten
	// Removed are files an earlier install wrote that the skills no longer
	// have; one modified locally is kept instead.
	Removed  []string `json:"removed,omitempty"`
	Linked   []string `json:"linked,omitempty"`
	Skills   []string `json:"skills"`
	Location string   `json:"claudeDir,omitempty"`
	// PreviousVersion is the axx whose skills these replaced, when there were any.
	PreviousVersion string `json:"previousVersion,omitempty"`
	// Ignored is whether this install added the skills to .gitignore.
	Ignored bool `json:"ignored,omitempty"`
	// Tracked are the skills' files the repository holds: .gitignore does not
	// govern those, so it was left alone, and UntrackCommand stops tracking them.
	Tracked []string `json:"tracked,omitempty"`
}

// Install writes skills to <root>/.agents/skills and optionally links them
// into <root>/.claude/skills.
func Install(skills []Skill, opts InstallOptions) (*Result, error) {
	dir := filepath.Join(opts.Root, ".agents", "skills")
	res := &Result{Dir: dir}
	lockPath := filepath.Join(dir, LockFile)
	old := Lock{Files: map[string]string{}}
	if b, err := os.ReadFile(lockPath); err == nil {
		_ = json.Unmarshal(b, &old)
		if old.Files == nil {
			old.Files = map[string]string{}
		}
	}
	res.PreviousVersion = old.AxxVersion
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
	// Files an earlier install wrote that the skills dropped go, unless
	// edited since.
	for rel, h := range old.Files {
		if _, ok := lock.Files[rel]; ok {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		cur, err := os.ReadFile(dst)
		switch {
		case err != nil:
		case hash(cur) != h && !opts.Force:
			res.Kept = append(res.Kept, rel)
		default:
			if err := os.Remove(dst); err != nil {
				return nil, err
			}
			res.Removed = append(res.Removed, rel)
		}
	}
	sort.Strings(res.Removed)
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
	if opts.Ignore {
		var err error
		if res.Ignored, res.Tracked, err = EnsureIgnored(opts.Root); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// LockFile is where an install records what it wrote, in the skills directory.
const LockFile = ".axx-lock.json"

// InstalledVersion is the version of axx whose skills are installed under
// root (a project or a home directory), and whether any are.
func InstalledVersion(root string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(root, ".agents", "skills", LockFile))
	if err != nil {
		return "", false
	}
	var l Lock
	if json.Unmarshal(b, &l) != nil {
		return "", true
	}
	return l.AxxVersion, true
}

// ignoreComment and ignorePatterns are what .gitignore gets: the skills are
// generated from axx and its packs, and every axx upgrade changes them. The
// names axx gives them, not the whole directories: a project's own skills
// stay in the repository.
const ignoreComment = "# axx's agent skills: `axx skills install` writes them"

var ignorePatterns = []string{".agents/skills/axx-*", ".agents/skills/" + LockFile, ".claude/skills/axx-*"}

// MergeGitignore adds the skills' lines a .gitignore lacks, and reports whether
// it did.
func MergeGitignore(s string) (string, bool) {
	have := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, p := range ignorePatterns {
		if !have[p] {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return s, false
	}
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if s != "" {
		s += "\n"
	}
	if !have[ignoreComment] {
		s += ignoreComment + "\n"
	}
	return s + strings.Join(add, "\n") + "\n", true
}

// Tracked are the skills' files the repository around dir holds: none outside
// a repository, or without git.
func Tracked(dir string) []string {
	cmd := exec.Command("git", append([]string{"ls-files", "--"}, ignorePatterns...)...) //nolint:noctx // reads the index, at once
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// UntrackCommand takes the skills out of a repository that holds them, leaving
// their files in place.
const UntrackCommand = "git rm -r --cached --ignore-unmatch -- '.agents/skills/axx-*' .agents/skills/" + LockFile + " '.claude/skills/axx-*'"

// EnsureIgnored adds the skills to the .gitignore of root, unless the repository
// holds them: .gitignore does not govern tracked files, so ignoring them would
// only keep the new ones out. It says whether it added them, and what is tracked.
func EnsureIgnored(root string) (bool, []string, error) {
	if tracked := Tracked(root); len(tracked) > 0 {
		return false, tracked, nil
	}
	path := filepath.Join(root, ".gitignore")
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, nil, err
	}
	merged, changed := MergeGitignore(string(old))
	if !changed {
		return false, nil, nil
	}
	return true, nil, os.WriteFile(path, []byte(merged), 0o644)
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
		// The export is generated: a file the skill dropped goes with it.
		if err := os.RemoveAll(filepath.Join(dir, sk.Name)); err != nil {
			return nil, err
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
