package fixtures

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// Suffixes of the three spec file kinds.
const (
	FactorySuffix   = ".factory.yaml"
	FixtureSuffix   = ".fixture.yaml"
	PrototypeSuffix = ".prototype.yaml"
)

// relPath is baseDir.relativize(file) with forward slashes.
func relPath(baseDir, file string) string {
	rel, err := filepath.Rel(baseDir, file)
	if err != nil {
		return filepath.ToSlash(file)
	}
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// isSourceFile reports whether name is a factory, prototype or fixture spec.
func isSourceFile(name string) bool {
	return strings.HasSuffix(name, FactorySuffix) || strings.HasSuffix(name, PrototypeSuffix) || strings.HasSuffix(name, FixtureSuffix)
}

// sourceRoots are the walk roots: the resource root first, then each
// sources entry. Roots must exist and be disjoint.
func sourceRoots(cfg Config) ([]string, error) {
	base := filepath.Clean(cfg.BaseDir)
	roots := []string{base}
	for _, src := range cfg.Sources {
		root := filepath.Clean(filepath.Join(base, filepath.FromSlash(src)))
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			return nil, configError("sources root '%s' does not exist (resolved to %s)", src, root)
		}
		if within(root, base) {
			return nil, configError("sources root '%s' is inside the resource root - it is already walked; remove it", src)
		}
		if within(base, root) {
			return nil, configError("sources root '%s' contains the resource root - declare only disjoint roots", src)
		}
		roots = append(roots, root)
	}
	return roots, nil
}

// within reports whether path equals dir or lies beneath it (Path.startsWith).
func within(path, dir string) bool {
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// walkFiles lists the regular files under every root, sorted by path and
// de-duplicated.
func walkFiles(roots []string, keep func(path string) bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" && p != root {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				fi, err := os.Stat(p)
				if err != nil || !fi.Mode().IsRegular() {
					return nil //nolint:nilerr // broken links and special files are not fixtures
				}
			}
			if !seen[p] && keep(p) {
				seen[p] = true
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, ioError(err, "cannot scan %s", root)
		}
	}
	sort.Strings(out)
	return out, nil
}

// findFiles resolves globs (matched against resource-root-relative paths,
// so ../ patterns reach files in outside roots) across roots.
func findFiles(baseDir string, roots, globs []string) ([]string, error) {
	m, err := newGlobMatcher(globs)
	if err != nil {
		return nil, err
	}
	return walkFiles(roots, func(p string) bool { return m.match(relPath(baseDir, p)) })
}

func walkForSuffix(roots []string, suffix string) ([]string, error) {
	return walkFiles(roots, func(p string) bool { return strings.HasSuffix(filepath.Base(p), suffix) })
}

// LoadSpecs loads every factory spec, binds every *.fixture.yaml to its
// factory and layers the prototype overlays.
func LoadSpecs(cfg Config) ([]*Spec, error) {
	cfg = cfg.withDefaults()
	base := filepath.Clean(cfg.BaseDir)
	roots, err := sourceRoots(cfg)
	if err != nil {
		return nil, err
	}
	files, err := findFiles(base, roots, cfg.Factories)
	if err != nil {
		return nil, err
	}
	var specs []*Spec
	byDir := map[string][]*Spec{}
	for _, file := range files {
		spec, err := loadFactory(base, file, cfg.OutputIgnored)
		if err != nil {
			return nil, err
		}
		byDir[spec.SourceDir] = append(byDir[spec.SourceDir], spec)
		specs = append(specs, spec)
	}
	if err := bindFixtureFiles(base, roots, specs, byDir); err != nil {
		return nil, err
	}
	if err := bindPrototypeOverlays(base, roots, specs); err != nil {
		return nil, err
	}
	for _, spec := range specs {
		if spec.Fixtures.Len() == 0 {
			return nil, specError("%s: no fixtures - declare fixtures: inline or add *.fixture.yaml files (colocated, or bound via factory:)", spec.SourceName)
		}
		if err := validateSpec(spec); err != nil {
			return nil, err
		}
	}
	return specs, nil
}

func readYAMLFile(path, rel string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ioError(err, "cannot read %s", rel)
	}
	v, err := jyaml.Unmarshal(data)
	if err != nil {
		return nil, specError("cannot parse %s: %v", rel, err)
	}
	return v, nil
}

func loadFactory(base, file string, defaultIgnored bool) (*Spec, error) {
	rel := relPath(base, file)
	root, err := readYAMLFile(file, rel)
	if err != nil {
		return nil, err
	}
	spec, err := parseFactory(root, rel)
	if err != nil {
		return nil, err
	}
	spec.SourceName = rel
	spec.SourceDir = relPath(base, filepath.Dir(file))
	spec.BaseName = strings.TrimSuffix(filepath.Base(file), FactorySuffix)
	if spec.ignoredSet == nil {
		spec.OutputIgnored = defaultIgnored
	} else {
		spec.OutputIgnored = *spec.ignoredSet
	}
	if err := loadPrototypeFile(spec, base, file); err != nil {
		return nil, err
	}
	for _, key := range keys(spec.inlineFixtures) {
		data, _ := get(spec.inlineFixtures, key).(*jsonx.Object)
		if data == nil {
			data = jsonx.NewObject()
		}
		if err := putFixture(spec, key, &Fixture{Data: data, Dir: spec.SourceDir, Source: "inline in " + spec.SourceName}); err != nil {
			return nil, err
		}
	}
	return spec, nil
}

var factoryKeys = []string{"defaults", "factory", "fixtures", "identity", "metadata", "prototype"}

func unknownKey(rel, where string, o *jsonx.Object, known []string) error {
	for _, k := range keys(o) {
		found := false
		for _, kk := range known {
			found = found || kk == k
		}
		if !found {
			return specError("cannot parse %s: unknown field \"%s\"%s (known: %s)", rel, k, where, strings.Join(known, ", "))
		}
	}
	return nil
}

// parseFactory binds a factory document to a Spec, refusing unknown fields.
func parseFactory(root any, rel string) (*Spec, error) {
	doc, ok := root.(*jsonx.Object)
	if root == nil {
		return nil, specError("cannot parse %s: the file is empty", rel)
	}
	if !ok {
		return nil, specError("cannot parse %s: expected a mapping", rel)
	}
	if err := unknownKey(rel, "", doc, factoryKeys); err != nil {
		return nil, err
	}
	spec := &Spec{Fixtures: newFixtureSet()}
	var err error
	if f := get(doc, "factory"); f != nil {
		fo, ok := f.(*jsonx.Object)
		if !ok {
			return nil, specError("cannot parse %s: factory must be a mapping", rel)
		}
		if err := unknownKey(rel, " in factory", fo, []string{"family", "options", "output", "schema"}); err != nil {
			return nil, err
		}
		if spec.Family, err = stringField(rel, "factory.family", get(fo, "family")); err != nil {
			return nil, err
		}
		if spec.Schema, err = stringField(rel, "factory.schema", get(fo, "schema")); err != nil {
			return nil, err
		}
		if spec.Options, err = mapField(rel, "factory.options", get(fo, "options")); err != nil {
			return nil, err
		}
		if out := get(fo, "output"); out != nil {
			oo, ok := out.(*jsonx.Object)
			if !ok {
				return nil, specError("cannot parse %s: factory.output must be a mapping", rel)
			}
			if err := unknownKey(rel, " in factory.output", oo, []string{"dir", "ignored"}); err != nil {
				return nil, err
			}
			if spec.OutputDir, err = stringField(rel, "factory.output.dir", get(oo, "dir")); err != nil {
				return nil, err
			}
			if v := get(oo, "ignored"); v != nil {
				b, err := boolField(rel, "factory.output.ignored", v)
				if err != nil {
					return nil, err
				}
				spec.ignoredSet = &b
			}
		}
	}
	if spec.Options == nil {
		spec.Options = jsonx.NewObject()
	}
	for _, f := range []struct {
		name string
		dst  **jsonx.Object
	}{{"defaults", &spec.Defaults}, {"prototype", &spec.Prototype}, {"metadata", &spec.Metadata}, {"fixtures", &spec.inlineFixtures}} {
		if *f.dst, err = mapField(rel, f.name, get(doc, f.name)); err != nil {
			return nil, err
		}
		if *f.dst == nil {
			*f.dst = jsonx.NewObject()
		}
	}
	for _, k := range keys(spec.inlineFixtures) {
		if v := get(spec.inlineFixtures, k); v != nil {
			if _, ok := v.(*jsonx.Object); !ok {
				return nil, specError("cannot parse %s: fixtures.%s must be a mapping", rel, k)
			}
		}
	}
	if ids := get(doc, "identity"); ids != nil {
		list, ok := ids.([]any)
		if !ok {
			return nil, specError("cannot parse %s: identity must be a list", rel)
		}
		for i, item := range list {
			io, ok := item.(*jsonx.Object)
			if !ok {
				return nil, specError("cannot parse %s: identity[%d] must be a mapping", rel, i)
			}
			if err := unknownKey(rel, " in identity", io, []string{"derive", "format", "path", "prefix", "qualifier"}); err != nil {
				return nil, err
			}
			id := Identity{Derive: "fixture-key", Format: "literal"}
			for _, f := range []struct {
				name string
				dst  *string
			}{{"path", &id.Path}, {"prefix", &id.Prefix}, {"derive", &id.Derive}, {"qualifier", &id.Qualifier}, {"format", &id.Format}} {
				if io.Has(f.name) {
					if *f.dst, err = stringField(rel, "identity."+f.name, get(io, f.name)); err != nil {
						return nil, err
					}
				}
			}
			spec.Identity = append(spec.Identity, id)
		}
	}
	return spec, nil
}

func stringField(rel, name string, v any) (string, error) {
	switch v.(type) {
	case nil:
		return "", nil
	case *jsonx.Object, []any:
		return "", specError("cannot parse %s: %s must be a string", rel, name)
	}
	return valueOf(v), nil
}

func boolField(rel, name string, v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	case jsonx.Number:
		if isIntegral(x) {
			return longValue(x) != 0, nil
		}
	}
	return false, specError("cannot parse %s: %s must be true or false, got %s", rel, name, valueOf(v))
}

func mapField(rel, name string, v any) (*jsonx.Object, error) {
	if v == nil {
		return nil, nil
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, specError("cannot parse %s: %s must be a mapping", rel, name)
	}
	return o, nil
}

// envelope is the data:/metadata:/factory: body of a fixture or prototype
// file.
type envelope struct {
	factory  string
	hasRef   bool
	data     *jsonx.Object
	metadata *jsonx.Object
}

func readEnvelope(path, rel string, fixture bool) (*envelope, error) {
	root, err := readYAMLFile(path, rel)
	if err != nil {
		return nil, err
	}
	env := &envelope{}
	if root == nil {
		return env, nil
	}
	known := []string{"data", "metadata"}
	hint := " (a prototype file's top-level keys are data: and metadata:)"
	if fixture {
		known = []string{"data", "factory", "metadata"}
		hint = " (a fixture file's top-level keys are factory:, data: and metadata:)"
	}
	doc, ok := root.(*jsonx.Object)
	if !ok {
		return nil, specError("cannot parse %s: expected a mapping%s", rel, hint)
	}
	if err := unknownKey(rel, "", doc, known); err != nil {
		return nil, specError("%s%s", errText(err), hint)
	}
	if env.data, err = mapField(rel, "data", get(doc, "data")); err != nil {
		return nil, err
	}
	if env.metadata, err = mapField(rel, "metadata", get(doc, "metadata")); err != nil {
		return nil, err
	}
	if fixture && get(doc, "factory") != nil {
		env.hasRef = true
		if env.factory, err = stringField(rel, "factory", get(doc, "factory")); err != nil {
			return nil, err
		}
	}
	return env, nil
}

// loadPrototypeFile reads the sibling <name>.prototype.yaml: the prototype
// without ballooning the factory.
func loadPrototypeFile(spec *Spec, base, factoryFile string) error {
	file := filepath.Join(filepath.Dir(factoryFile), spec.BaseName+PrototypeSuffix)
	if _, err := os.Stat(file); err != nil {
		return nil //nolint:nilerr // no sibling prototype file
	}
	if spec.Prototype.Len() > 0 {
		return specError("%s declares an inline prototype AND %s exists - keep exactly one", spec.SourceName, filepath.Base(file))
	}
	env, err := readEnvelope(file, relPath(base, file), false)
	if err != nil {
		return err
	}
	if env.data != nil {
		spec.Prototype = env.data
	}
	if env.metadata != nil {
		spec.Metadata = env.metadata
	}
	return nil
}

func putFixture(spec *Spec, key string, f *Fixture) error {
	if prev := spec.Fixtures.put(key, f); prev != nil {
		return specError("two fixtures claim the key '%s' in %s:\n  %s\n  %s", key, spec.SourceName, prev.Source, f.Source)
	}
	return nil
}

func specNames(specs []*Spec) string {
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.SourceName
	}
	sortStrings(names)
	return javaListString(names)
}

func bindFixtureFiles(base string, roots []string, specs []*Spec, byDir map[string][]*Spec) error {
	files, err := walkForSuffix(roots, FixtureSuffix)
	if err != nil {
		return err
	}
	for _, file := range files {
		rel := relPath(base, file)
		dir := relPath(base, filepath.Dir(file))
		env, err := readEnvelope(file, rel, true)
		if err != nil {
			return err
		}
		target, err := bind(env, dir, rel, specs, byDir)
		if err != nil {
			return err
		}
		data := env.data
		if data == nil {
			data = jsonx.NewObject()
		}
		key := strings.TrimSuffix(filepath.Base(file), FixtureSuffix)
		if err := putFixture(target, key, &Fixture{Data: data, Metadata: env.metadata, Dir: dir, Source: rel}); err != nil {
			return err
		}
	}
	return nil
}

func bind(env *envelope, dir, rel string, specs []*Spec, byDir map[string][]*Spec) (*Spec, error) {
	ref := env.factory
	if env.hasRef && strings.Contains(ref, "/") {
		name := ref
		if !strings.HasSuffix(name, FactorySuffix) {
			name += FactorySuffix
		}
		for _, s := range specs {
			if s.SourceName == name {
				return s, nil
			}
		}
		return nil, specError("%s: factory: '%s' matches no factory (known: %s)", rel, ref, specNames(specs))
	}
	for ancestor := dir; ; ancestor = parentDir(ancestor) {
		here := byDir[ancestor]
		switch {
		case env.hasRef:
			for _, s := range here {
				if s.BaseName == ref {
					return s, nil
				}
			}
		case len(here) == 1:
			return here[0], nil
		case len(here) > 1:
			names := make([]string, len(here))
			for i, s := range here {
				names[i] = s.BaseName
			}
			sortStrings(names)
			return nil, specError("%s: '%s' holds %d factories - declare factory: <name> (one of %s)", rel, ancestor, len(here), javaListString(names))
		}
		if ancestor == "" {
			break
		}
	}
	what := ": no factory in this directory or any ancestor"
	if env.hasRef {
		what = ": no '" + ref + ".factory.yaml' in this directory or any ancestor"
	}
	return nil, specError("%s%s (known factories: %s); bind explicitly with factory: <root-relative-path>", rel, what, specNames(specs))
}

// bindPrototypeOverlays layers every <name>.prototype.yaml that is not a
// factory's sibling over that factory's prototype for fixtures beneath it.
func bindPrototypeOverlays(base string, roots []string, specs []*Spec) error {
	files, err := walkForSuffix(roots, PrototypeSuffix)
	if err != nil {
		return err
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), PrototypeSuffix)
		dir := relPath(base, filepath.Dir(file))
		rel := relPath(base, file)
		var named []*Spec
		sibling := false
		for _, s := range specs {
			if s.BaseName == name {
				named = append(named, s)
				sibling = sibling || s.SourceDir == dir
			}
		}
		if len(named) == 0 {
			return specError("%s: no factory named '%s' exists - prototype files bind to factories by name", rel, name)
		}
		if sibling {
			continue // the factory's own base prototype, already loaded
		}
		env, err := readEnvelope(file, rel, false)
		if err != nil {
			return err
		}
		overlay := env.data
		if overlay == nil {
			overlay = jsonx.NewObject()
		}
		covers := false
		for _, s := range named {
			coversHere := false
			for _, k := range s.Fixtures.Keys() {
				fd := s.Fixtures.Get(k).Dir
				coversHere = coversHere || fd == dir || strings.HasPrefix(fd, dir+"/")
			}
			if coversHere {
				if s.prototypeOverlays == nil {
					s.prototypeOverlays = map[string]*jsonx.Object{}
					s.metadataOverlays = map[string]*jsonx.Object{}
				}
				s.prototypeOverlays[dir] = overlay
				if env.metadata != nil {
					s.metadataOverlays[dir] = env.metadata
				}
				covers = true
			}
		}
		if !covers {
			return specError("%s: this overlay covers no fixtures of factory '%s' - move it above the fixtures it should shape, or delete it", rel, name)
		}
	}
	return nil
}

func validateSpec(spec *Spec) error {
	if strings.TrimSpace(spec.Family) == "" {
		return specError("%s: factory.family is required", spec.SourceName)
	}
	for _, id := range spec.Identity {
		if strings.TrimSpace(id.Path) == "" {
			return specError("%s: identity entries need a path", spec.SourceName)
		}
		if id.Derive != "fixture-key" && id.Derive != "authored" {
			return specError("%s: identity %s derive must be fixture-key or authored, got %s", spec.SourceName, id.Path, id.Derive)
		}
		if id.Format != "literal" && id.Format != "uuid-name-based" {
			return specError("%s: identity %s format must be literal or uuid-name-based, got %s", spec.SourceName, id.Path, id.Format)
		}
	}
	return nil
}
