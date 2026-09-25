package fixtures

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Options extend the engine: extra families and expression functions (the
// built-in families are always present).
type Options struct {
	Families  []Family
	Functions []Function
}

// Generator expands factory specs into canonical fixture bytes, the
// manifest, the managed .gitignores, the pairings lock and the generated
// lint rules. Generation refuses to overwrite a managed file whose content
// no longer matches the manifest (a hand-edit): the edit must be lifted into
// the factory spec or the file re-adopted.
type Generator struct {
	cfg        Config
	baseDir    string
	specs      []*Spec
	families   families
	functions  map[string]Function
	identities *identities
	committed  *pairingsLock
}

// NewGenerator loads the specs under cfg and verifies the pairings lock.
func NewGenerator(cfg Config, opts Options) (*Generator, error) {
	cfg = cfg.withDefaults()
	fams, err := newFamilies(opts.Families)
	if err != nil {
		return nil, err
	}
	fns, err := functionMap(opts.Functions)
	if err != nil {
		return nil, err
	}
	specs, err := LoadSpecs(cfg)
	if err != nil {
		return nil, err
	}
	base := filepath.Clean(cfg.BaseDir)
	if err := verifyPairingsIntegrity(base); err != nil {
		return nil, err
	}
	committed, err := loadPairings(base)
	if err != nil {
		return nil, err
	}
	return &Generator{cfg: cfg, baseDir: base, specs: specs, families: fams, functions: fns, identities: newIdentities(), committed: committed}, nil
}

// verifyPairingsIntegrity refuses a hand-edited pairings lock: it feeds
// expansion, so an edit would make the output self-consistently wrong.
func verifyPairingsIntegrity(base string) error {
	lock := filepath.Join(base, PairingsFile)
	manifest, err := LoadManifest(base)
	if err != nil {
		return err
	}
	claim, ok := manifest.Get(PairingsFile)
	if !ok {
		return nil
	}
	data, err := os.ReadFile(lock)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return ioError(err, "cannot read %s", lock)
	}
	if claim.SHA256 != SHA256(data) {
		return axxerr.New(CodeHandEdit, exitcode.Failed, "%s was hand-edited - recorded pairings are trusted input", PairingsFile).
			WithHint("revert it, or delete it and run `axx fixtures generate` with your local stack up to re-record")
	}
	return nil
}

// Specs returns the loaded factory specs.
func (g *Generator) Specs() []*Spec { return g.specs }

// BaseDir is the resource root.
func (g *Generator) BaseDir() string { return g.baseDir }

// Config returns the effective configuration.
func (g *Generator) Config() Config { return g.cfg }

func (g *Generator) family(spec *Spec) (Family, error) {
	return g.families.require(spec.Family, spec.SourceName)
}

// FamilyByName resolves a family; conformance rules select their oracle
// this way.
func (g *Generator) FamilyByName(name, where string) (Family, error) {
	f, ok := g.families[name]
	if !ok {
		return nil, configError("%s: unsupported family '%s' (supported: %s)", where, name, javaListString(g.families.names()))
	}
	return f, nil
}

// Expansion is one full expansion: every produced file's bytes (keyed by
// resource-root-relative path), the manifest they imply, and the ignored
// outputs (gitignored derivations clean may delete and untrack takes out of
// the index).
type Expansion struct {
	Files          map[string][]byte
	Manifest       *Manifest
	IgnoredOutputs []string
}

// Paths returns the produced paths in manifest order.
func (x *Expansion) Paths() []string {
	out := make([]string, 0, len(x.Files))
	for p := range x.Files {
		out = append(out, p)
	}
	sortStrings(out)
	return out
}

func (x *Expansion) ignored(path string) bool {
	for _, p := range x.IgnoredOutputs {
		if p == path {
			return true
		}
	}
	return false
}

// Expand expands every spec in memory. ModeVerify (check, clean, untrack)
// resolves recorded functions from the committed pairings only.
func (g *Generator) Expand(mode Mode) (x *Expansion, err error) {
	files := map[string][]byte{}
	manifest := NewManifest()
	eval := newEvaluator(g.functions, mode, g.committed)
	ctx := &ExpansionContext{identities: g.identities, eval: eval}
	eval.references = g.referencesAcross(ctx)
	ignoredByDir := map[string]map[string]bool{}
	defer func() {
		if ferr := eval.finish(); ferr != nil && err == nil {
			x, err = nil, ferr
		}
	}()
	if err := g.expandInto(files, manifest, ignoredByDir, ctx); err != nil {
		return nil, err
	}
	var dirs []string
	for d := range ignoredByDir {
		dirs = append(dirs, d)
	}
	sortStrings(dirs)
	var ignoredOutputs []string
	for _, dir := range dirs {
		var names []string
		for n := range ignoredByDir[dir] {
			names = append(names, n)
		}
		sortStrings(names)
		path := ".gitignore"
		if dir != "" {
			path = dir + "/.gitignore"
		}
		gi := gitignoreFor(names)
		files[path] = gi
		manifest.Put(path, gi, Owner, "<ignored outputs>")
		for _, n := range names {
			if dir == "" {
				ignoredOutputs = append(ignoredOutputs, n)
			} else {
				ignoredOutputs = append(ignoredOutputs, dir+"/"+n)
			}
		}
	}
	sortStrings(ignoredOutputs)
	if !eval.used.empty() {
		p := eval.used.serialize()
		files[PairingsFile] = p
		manifest.Put(PairingsFile, p, Owner, "<recorded pairings>")
	}
	if g.cfg.LintEmit {
		rules, err := emitLintRules(g.specs, g.family)
		if err != nil {
			return nil, err
		}
		if rules != nil {
			files[g.cfg.LintOutput] = rules
			manifest.Put(g.cfg.LintOutput, rules, Owner, "<generated lint rules>")
		}
	}
	return &Expansion{Files: files, Manifest: manifest, IgnoredOutputs: ignoredOutputs}, nil
}

// referencesAcross resolves $ref against the target document's own values,
// each resolved at most once and never in a cycle.
func (g *Generator) referencesAcross(ctx *ExpansionContext) References {
	resolved := map[string]any{}
	var resolving []string
	return func(document, pointer string) (any, error) {
		tree, ok := resolved[document]
		if !ok {
			for _, d := range resolving {
				if d == document {
					return nil, genError("documents reference each other in a cycle: %s", strings.Join(resolving, " -> "))
				}
			}
			resolving = append(resolving, document)
			t, err := g.resolveReferenced(document, ctx)
			resolving = resolving[:len(resolving)-1]
			if err != nil {
				return nil, err
			}
			resolved[document] = t
			tree = t
		}
		return jsonPointer(tree, pointer), nil
	}
}

// resolveReferenced returns a document's resolved values: a fixture file is
// one fixture; a factory file holds several, keyed by fixture.
func (g *Generator) resolveReferenced(document string, ctx *ExpansionContext) (any, error) {
	for _, spec := range g.specs {
		for _, k := range spec.Fixtures.Keys() {
			if spec.Fixtures.Get(k).Source == document {
				return resolveValues(spec, k, ctx)
			}
		}
	}
	for _, spec := range g.specs {
		if spec.SourceName == document {
			byFixture := jsonx.NewObject()
			for _, k := range spec.Fixtures.Keys() {
				v, err := resolveValues(spec, k, ctx)
				if err != nil {
					return nil, err
				}
				byFixture.Set(k, v)
			}
			return byFixture, nil
		}
	}
	var known []string
	for _, spec := range g.specs {
		for _, k := range spec.Fixtures.Keys() {
			known = append(known, spec.Fixtures.Get(k).Source)
		}
	}
	sortStrings(known)
	return nil, genError("$ref names no document: %s (known: %s)", document, javaListString(known))
}

func (g *Generator) expandInto(files map[string][]byte, manifest *Manifest, ignoredByDir map[string]map[string]bool, ctx *ExpansionContext) error {
	for _, spec := range g.specs {
		f, err := g.family(spec)
		if err != nil {
			return err
		}
		produced, err := f.Expand(spec, g.baseDir, ctx)
		if err != nil {
			return withCode(err)
		}
		fixtureKeys := make([]string, 0, len(produced))
		for k := range produced {
			fixtureKeys = append(fixtureKeys, k)
		}
		sortStrings(fixtureKeys)
		for _, key := range fixtureKeys {
			dir := spec.OutputDirFor(key)
			names := make([]string, 0, len(produced[key]))
			for n := range produced[key] {
				names = append(names, n)
			}
			sortStrings(names)
			for _, name := range names {
				rel := name
				if dir != "" {
					rel = dir + "/" + name
				}
				if _, dup := files[rel]; dup {
					return genError("two factories produce the same file %s (second: %s)", rel, spec.SourceName)
				}
				content := produced[key][name]
				files[rel] = content
				manifest.Put(rel, content, spec.SourceName, key)
				if spec.OutputIgnored {
					if ignoredByDir[dir] == nil {
						ignoredByDir[dir] = map[string]bool{}
					}
					ignoredByDir[dir][name] = true
				}
			}
		}
	}
	return nil
}

// gitignoreFor lists ignored outputs as exact paths, never wildcards, so
// hand-written neighbors stay tracked.
func gitignoreFor(names []string) []byte {
	var b strings.Builder
	b.WriteString("# GENERATED by axx fixtures - do not edit.\n" +
		"# These generated files are derivations of committed factory sources;\n" +
		"# `axx fixtures generate` materializes them before tests run.\n")
	for _, n := range names {
		b.WriteString("/" + n + "\n")
	}
	return []byte(b.String())
}

// GenerationResult lists what generate wrote and what was already current.
type GenerationResult struct {
	Written   []string `json:"written"`
	Unchanged []string `json:"unchanged"`
}

// Generate writes the expansion, refusing to clobber hand-edited managed
// files, and rewrites the manifest.
func (g *Generator) Generate() (*GenerationResult, error) { return g.generate(false) }

// Plan is Generate without writing: what would be written and what is
// current. Hand-edits are refused exactly as Generate refuses them.
func (g *Generator) Plan() (*GenerationResult, error) { return g.generate(true) }

func (g *Generator) generate(dryRun bool) (*GenerationResult, error) {
	x, err := g.Expand(ModeGenerate)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(g.baseDir, ManifestFile)
	if len(x.Files) == 0 && !exists(manifestPath) {
		// Zero adoption is zero behavior: nothing to write, not even a manifest.
		return &GenerationResult{Written: []string{}, Unchanged: []string{}}, nil
	}
	committed, err := LoadManifest(g.baseDir)
	if err != nil {
		return nil, err
	}
	res := &GenerationResult{Written: []string{}, Unchanged: []string{}}
	var handEdited []string
	for _, rel := range x.Paths() {
		content := x.Files[rel]
		target := filepath.Join(g.baseDir, filepath.FromSlash(rel))
		onDisk, err := readIfExists(target)
		if err != nil {
			return nil, err
		}
		if onDisk != nil && bytes.Equal(onDisk, content) {
			res.Unchanged = append(res.Unchanged, rel)
			continue
		}
		if claim, ok := committed.Get(rel); ok && onDisk != nil && claim.SHA256 != SHA256(onDisk) {
			handEdited = append(handEdited, rel)
			continue
		}
		if !dryRun {
			if err := writeFile(target, content); err != nil {
				return nil, err
			}
		}
		res.Written = append(res.Written, rel)
	}
	if len(handEdited) > 0 {
		return nil, axxerr.New(CodeHandEdit, exitcode.Failed, "refusing to overwrite hand-edited managed file(s):\n  %s", strings.Join(handEdited, "\n  ")).
			WithHint("lift the change into the factory spec, or re-adopt the file(s); there is no force option that discards edits")
	}
	if dryRun {
		return res, nil
	}
	if err := x.Manifest.Write(g.baseDir); err != nil {
		return nil, err
	}
	return res, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ioError(err, "cannot read %s", path)
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ioError(err, "cannot write %s", path)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return ioError(err, "cannot write %s", path)
	}
	return nil
}
