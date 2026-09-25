package fixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// Adopter turns existing hand-written fixture files into factory sources
// without renaming a single value. The prototype takes the modal value of
// every field present in every file; fields absent from any file stay
// per-fixture; string fields whose values are distinct across every file are
// proposed as identities with their values pinned. Before anything is
// written the emitted spec is parsed back and expanded, and every generated
// fixture must decode equal to its original: reformatting is allowed, a
// silent data change never.
type Adopter struct {
	cfg       Config
	baseDir   string
	families  families
	functions map[string]Function
	opts      Options
}

// AdoptionResult names the factory adopted into and reports what happened.
type AdoptionResult struct {
	Factory string   `json:"factory"`
	Written []string `json:"written"`
	Report  []string `json:"report"`
}

// NewAdopter prepares adoption under cfg.
func NewAdopter(cfg Config, opts Options) (*Adopter, error) {
	cfg = cfg.withDefaults()
	fams, err := newFamilies(opts.Families)
	if err != nil {
		return nil, err
	}
	fns, err := functionMap(opts.Functions)
	if err != nil {
		return nil, err
	}
	return &Adopter{cfg: cfg, baseDir: filepath.Clean(cfg.BaseDir), families: fams, functions: fns, opts: opts}, nil
}

// absent marks "field not present in this file", distinct from null.
type absentValue struct{}

var absent = absentValue{}

func presence(tree *jsonx.Object, field string) any {
	if !has(tree, field) {
		return absent
	}
	return get(tree, field)
}

func fixtureKeyOf(file string) string {
	name := filepath.Base(file)
	if dot := strings.LastIndexByte(name, '.'); dot > 0 || dot == 0 && len(name) > 1 {
		return name[:dot]
	}
	return name
}

func (a *Adopter) matchFiles(glob string) ([]string, error) {
	roots, err := sourceRoots(a.cfg)
	if err != nil {
		return nil, err
	}
	files, err := findFiles(a.baseDir, roots, []string{glob})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, adoptError("glob '%s' matches no files in %s", glob, a.baseDir)
	}
	return files, nil
}

func (a *Adopter) adoption(familyName, schemaRef, where string) (Family, Adoption, error) {
	f, err := a.families.require(familyName, where)
	if err != nil {
		return nil, nil, err
	}
	ad, ok := f.(AdoptingFamily)
	if !ok {
		return nil, nil, adoptError("family '%s' does not support adoption yet; write the factory spec by hand and run `axx fixtures generate`", familyName)
	}
	adoption, err := ad.Adoption(a.baseDir, schemaRef)
	if err != nil {
		return nil, nil, err
	}
	return f, adoption, nil
}

func (a *Adopter) refuseManaged(files []string) error {
	m, err := LoadManifest(a.baseDir)
	if err != nil {
		return err
	}
	var managed []string
	for _, f := range files {
		if rel := relPath(a.baseDir, f); m.Managed(rel) {
			managed = append(managed, rel)
		}
	}
	if len(managed) > 0 {
		return adoptError("already managed by a factory (see %s):\n  %s", ManifestFile, strings.Join(managed, "\n  "))
	}
	return nil
}

// Adopt adopts every file matched by glob (relative to the resource root)
// into a factory named factoryName, written into the files' common ancestor
// directory as <name>.factory.yaml + <name>.prototype.yaml, with one
// <key>.fixture.yaml next to each adopted file. With dryRun nothing is
// written.
func (a *Adopter) Adopt(familyName, schemaRef, glob, factoryName string, dryRun bool) (*AdoptionResult, error) {
	if strings.Contains(factoryName, "/") || strings.Contains(factoryName, ".yaml") {
		return nil, adoptError("--factory is a NAME (e.g. 'events'), not a path; the factory is written next to the adopted files")
	}
	family, adoption, err := a.adoption(familyName, schemaRef, "adopt")
	if err != nil {
		return nil, err
	}
	files, err := a.matchFiles(glob)
	if err != nil {
		return nil, err
	}
	outputDir := a.commonAncestorDir(files)
	factoryPath := joinDir(outputDir, factoryName+FactorySuffix)
	if _, err := os.Stat(filepath.Join(a.baseDir, filepath.FromSlash(factoryPath))); err == nil {
		return nil, adoptError("%s already exists; adoption never overwrites a factory spec. Remove it first to re-adopt.", factoryPath)
	}
	if err := a.refuseManaged(files); err != nil {
		return nil, err
	}
	decoded := map[string]any{}
	trees := map[string]*jsonx.Object{}
	originals := map[string][]byte{}
	originalNames := map[string]string{}
	fixtureDirs := map[string]string{}
	var fixtureKeys []string
	for _, file := range files {
		key := fixtureKeyOf(file)
		rel := relPath(a.baseDir, file)
		if _, dup := trees[key]; dup {
			return nil, adoptError("two matched files share the fixture key '%s' - fixture keys must be unique within a factory: %s", key, rel)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, ioError(err, "cannot read %s", file)
		}
		originals[key] = data
		originalNames[key] = filepath.Base(file)
		if decoded[key], err = adoption.Decode(data, rel); err != nil {
			return nil, err
		}
		if trees[key], err = adoption.AuthoringTree(data, rel); err != nil {
			return nil, err
		}
		fixtureDirs[key] = relPath(a.baseDir, filepath.Dir(file))
		fixtureKeys = append(fixtureKeys, key)
	}
	sortStrings(fixtureKeys)

	var identityPaths []string
	var prototype *jsonx.Object
	fixtures := map[string]*jsonx.Object{}
	if an, ok := adoption.(Analyzer); ok {
		analysis, err := an.Analyze(fixtureKeys, trees)
		if err != nil {
			return nil, err
		}
		identityPaths, prototype, fixtures = analysis.IdentityPaths, analysis.Prototype, analysis.Fixtures
	} else {
		shape, err := adoption.Shape()
		if err != nil {
			return nil, err
		}
		identityPaths = identityCandidates(shape, fixtureKeys, trees)
		ordered := make([]*jsonx.Object, len(fixtureKeys))
		for i, k := range fixtureKeys {
			ordered[i] = trees[k]
		}
		prototype = modalTree(shape, "", ordered, identityPaths)
		for _, k := range fixtureKeys {
			data := deltaTree(shape, "", prototype, trees[k], identityPaths)
			for _, p := range identityPaths {
				// Identity values pin by presence at their natural position.
				if err := pathSet(data, p, mustGet(trees[k], p)); err != nil {
					return nil, err
				}
			}
			fixtures[k] = data
		}
	}

	emitted, err := a.emit(familyName, schemaRef, factoryName, outputDir, fixtureDirs, prototype, identityPaths, fixtureKeys, trees, fixtures)
	if err != nil {
		return nil, err
	}
	roundTrip, err := a.roundTrip(emitted, factoryPath, outputDir, fixtureDirs, originalNames, family, adoption, decoded, originals, fixtureKeys)
	if err != nil {
		return nil, err
	}
	written, err := writeEmitted(a.baseDir, emitted, dryRun)
	if err != nil {
		return nil, err
	}
	report := []string{
		"decoded " + itoa(len(files)) + "/" + itoa(len(files)) + " files (family: " + familyName + ")",
		"prototype: " + itoa(prototype.Len()) + " field(s) (modal values)",
	}
	if len(identityPaths) == 0 {
		report = append(report, "identity candidates: none (no string field is distinct across every fixture)")
	} else {
		report = append(report, "identity candidates (all-distinct across files): "+strings.Join(identityPaths, ", ")+"  # review!")
	}
	wrote := "wrote " + factoryPath + " + " + itoa(len(fixtures)) + " *.fixture.yaml"
	if dryRun {
		wrote = "would write " + factoryPath + " + " + itoa(len(fixtures)) + " *.fixture.yaml"
	}
	if prototype.Len() > 0 {
		wrote += " + " + factoryName + PrototypeSuffix
	}
	report = append(report, wrote, roundTrip, "next: axx fixtures generate")
	return &AdoptionResult{Factory: factoryPath, Written: written, Report: report}, nil
}

func joinDir(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func writeEmitted(baseDir string, emitted map[string][]byte, dryRun bool) ([]string, error) {
	paths := make([]string, 0, len(emitted))
	for p := range emitted {
		paths = append(paths, p)
	}
	sortStrings(paths)
	if !dryRun {
		for _, p := range paths {
			if err := writeFile(filepath.Join(baseDir, filepath.FromSlash(p)), emitted[p]); err != nil {
				return nil, err
			}
		}
	}
	return paths, nil
}

func yamlSection(name string, value any) (string, error) {
	out, err := jyaml.Marshal(newObject(name, value), yamlFactory)
	if err != nil {
		return "", genError("cannot serialize adopted spec: %v", err)
	}
	return string(out), nil
}

func fixtureFileBody(binding string, data *jsonx.Object) ([]byte, error) {
	var b strings.Builder
	b.WriteString("factory: " + binding + "\n")
	if data.Len() == 0 {
		b.WriteString("# Pure prototype: this fixture is exactly the shared shape.\n")
	} else {
		s, err := yamlSection("data", data)
		if err != nil {
			return nil, err
		}
		b.WriteString(s)
	}
	return []byte(b.String()), nil
}

// emit renders the colocated file set: factory spec, optional prototype
// file, fixture envelopes.
func (a *Adopter) emit(familyName, schemaRef, factoryName, outputDir string, fixtureDirs map[string]string, prototype *jsonx.Object,
	identityPaths, fixtureKeys []string, trees, fixtures map[string]*jsonx.Object,
) (map[string][]byte, error) {
	out := map[string][]byte{}
	var factory strings.Builder
	factory.WriteString("# Adopted by axx fixtures. Values are preserved verbatim from the\n")
	factory.WriteString("# original files; review the proposed identity candidates below.\n")
	s, err := yamlSection("factory", newObject("family", familyName, "schema", a.factoryRelative(schemaRef, outputDir)))
	if err != nil {
		return nil, err
	}
	factory.WriteString(s)
	if len(identityPaths) > 0 {
		factory.WriteString("# review: every value of these string fields is distinct across the\n")
		factory.WriteString("# adopted fixtures, so they are declared as identities. Remove any that\n")
		factory.WriteString("# are not identities; values stay pinned in the fixture data either way.\n")
		var ids []any
		for _, p := range identityPaths {
			id := newObject("path", p)
			if prefix := commonPrefix(fixtureKeys, trees, p); prefix != "" {
				id.Set("prefix", prefix)
			}
			ids = append(ids, id)
		}
		s, err := yamlSection("identity", ids)
		if err != nil {
			return nil, err
		}
		factory.WriteString(s)
	}
	out[joinDir(outputDir, factoryName+FactorySuffix)] = []byte(factory.String())
	if prototype.Len() > 0 {
		s, err := yamlSection("data", prototype)
		if err != nil {
			return nil, err
		}
		out[joinDir(outputDir, factoryName+PrototypeSuffix)] = []byte("# Shared shape of every " + factoryName +
			" fixture (modal values from adoption);\n# fixture files carry only their deltas.\n" + s)
	}
	for _, k := range fixtureKeys {
		body, err := fixtureFileBody(factoryName, fixtures[k])
		if err != nil {
			return nil, err
		}
		out[joinDir(fixtureDirs[k], k+FixtureSuffix)] = body
	}
	return out, nil
}

// factoryRelative rewrites a resource-root-relative schema ref relative to
// the factory directory, as every ref a factory declares is.
func (a *Adopter) factoryRelative(ref, factoryDir string) string {
	if ref == "" || strings.HasPrefix(ref, "classpath:") || strings.HasPrefix(ref, "class:") {
		return ref
	}
	path, fragment := ref, ""
	if hash := strings.IndexByte(ref, '#'); hash >= 0 {
		path, fragment = ref[:hash], ref[hash:]
	}
	from := filepath.Join(a.baseDir, filepath.FromSlash(factoryDir))
	to := filepath.Join(a.baseDir, filepath.FromSlash(path))
	rel, err := filepath.Rel(from, to)
	if err != nil {
		return ref
	}
	return filepath.ToSlash(rel) + fragment
}

func (a *Adopter) context() (*ExpansionContext, error) {
	committed, err := loadPairings(a.baseDir)
	if err != nil {
		return nil, err
	}
	return &ExpansionContext{identities: newIdentities(), eval: newEvaluator(a.functions, ModeVerify, committed)}, nil
}

func (a *Adopter) compare(generated map[string]map[string][]byte, fixtureKeys []string, originalNames map[string]string,
	adoption Adoption, decoded map[string]any, originals map[string][]byte, via string,
) (string, error) {
	reformatted := 0
	for _, key := range fixtureKeys {
		regeneratedBytes, ok := generated[key][originalNames[key]]
		if !ok {
			var produced []string
			for k := range generated {
				produced = append(produced, k)
			}
			sortStrings(produced)
			return "", adoptError("round-trip failure: %s does not regenerate fixture '%s' (regenerated: %s)", via, key, javaListString(produced))
		}
		regenerated, err := adoption.Decode(regeneratedBytes, "regenerated "+key)
		if err != nil {
			return "", err
		}
		if !deepEquals(regenerated, decoded[key]) {
			return "", adoptError("round-trip failure: regenerating fixture '%s' through %s changes its decoded content (%s). Adoption refuses to proceed; nothing was written.",
				key, via, valueDifference("", decoded[key], regenerated))
		}
		if !bytes.Equal(regeneratedBytes, originals[key]) {
			reformatted++
		}
	}
	n := itoa(len(fixtureKeys))
	return "semantic round-trip: " + n + "/" + n + " deep-equal. " + itoa(reformatted) +
		" file(s) will be reformatted by `axx fixtures generate`, 0 semantic changes.", nil
}

// roundTrip expands the emitted files, parsed back exactly as the loader
// would, and proves every fixture decodes equal to its original.
func (a *Adopter) roundTrip(emitted map[string][]byte, factoryPath, outputDir string, fixtureDirs, originalNames map[string]string,
	family Family, adoption Adoption, decoded map[string]any, originals map[string][]byte, fixtureKeys []string,
) (string, error) {
	root, err := jyaml.Unmarshal(emitted[factoryPath])
	if err != nil {
		return "", genError("internal: emitted spec does not parse back: %v", err)
	}
	spec, err := parseFactory(root, factoryPath)
	if err != nil {
		return "", genError("internal: emitted spec does not parse back: %s", errText(err))
	}
	spec.SourceName, spec.SourceDir = factoryPath, outputDir
	spec.BaseName = strings.TrimSuffix(filepath.Base(factoryPath), FactorySuffix)
	prefix := ""
	if outputDir != "" {
		prefix = outputDir + "/"
	}
	for _, path := range sortedMapKeys(emitted) {
		name := strings.TrimPrefix(path, prefix)
		doc, err := jyaml.Unmarshal(emitted[path])
		if err != nil {
			return "", genError("internal: emitted spec does not parse back: %v", err)
		}
		data, _ := get(asObject(doc), "data").(*jsonx.Object)
		switch {
		case strings.HasSuffix(name, PrototypeSuffix):
			if data != nil {
				spec.Prototype = data
			}
		case strings.HasSuffix(name, FixtureSuffix):
			if data == nil {
				data = jsonx.NewObject()
			}
			key := strings.TrimSuffix(filepath.Base(name), FixtureSuffix)
			spec.Fixtures.put(key, &Fixture{Data: data, Dir: fixtureDirs[key], Source: path})
		}
	}
	ctx, err := a.context()
	if err != nil {
		return "", err
	}
	generated, err := family.Expand(spec, a.baseDir, ctx)
	if err != nil {
		return "", err
	}
	return a.compare(generated, fixtureKeys, originalNames, adoption, decoded, originals, "the adopted spec")
}

func asObject(v any) *jsonx.Object {
	o, _ := v.(*jsonx.Object)
	return o
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// AdoptInto binds new hand-written files into an existing factory. Its
// family, schema, identities and prototype are the contract and do not
// change: each new file becomes a *.fixture.yaml next to it carrying only
// its deltas against the factory's effective prototype there, bound by an
// explicit factory: path.
func (a *Adopter) AdoptInto(factoryName, glob string, dryRun bool) (*AdoptionResult, error) {
	g, err := NewGenerator(a.cfg, a.opts)
	if err != nil {
		return nil, err
	}
	var matches []*Spec
	for _, s := range g.Specs() {
		if s.BaseName == factoryName || s.SourceDir+"/"+s.BaseName == factoryName {
			matches = append(matches, s)
		}
	}
	switch {
	case len(matches) == 0:
		return nil, adoptError("--into='%s' matches no factory; known: %s", factoryName, specNames(g.Specs()))
	case len(matches) > 1:
		return nil, adoptError("--into='%s' is ambiguous (%s) - use the root-relative path form, e.g. '%s/%s'",
			factoryName, specNames(matches), matches[0].SourceDir, matches[0].BaseName)
	}
	spec := matches[0]
	family, adoption, err := a.adoption(spec.Family, spec.SchemaRef(), spec.SourceName)
	if err != nil {
		return nil, err
	}
	files, err := a.matchFiles(glob)
	if err != nil {
		return nil, err
	}
	if err := a.refuseManaged(files); err != nil {
		return nil, err
	}
	decoded := map[string]any{}
	originals := map[string][]byte{}
	originalNames := map[string]string{}
	fixtureDirs := map[string]string{}
	fixtures := map[string]*jsonx.Object{}
	var fixtureKeys []string
	binding := joinDir(spec.SourceDir, spec.BaseName)
	for _, file := range files {
		key := fixtureKeyOf(file)
		rel := relPath(a.baseDir, file)
		if existing := spec.Fixtures.Get(key); existing != nil {
			return nil, adoptError("fixture key '%s' already exists in %s (from %s) - rename %s", key, spec.SourceName, existing.Source, rel)
		}
		if _, dup := fixtures[key]; dup {
			return nil, adoptError("two matched files share the fixture key '%s': %s", key, rel)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, ioError(err, "cannot read %s", file)
		}
		dir := relPath(a.baseDir, filepath.Dir(file))
		originals[key], originalNames[key], fixtureDirs[key] = data, filepath.Base(file), dir
		if decoded[key], err = adoption.Decode(data, rel); err != nil {
			return nil, err
		}
		tree, err := adoption.AuthoringTree(data, rel)
		if err != nil {
			return nil, err
		}
		delta := subtract(tree, spec.PrototypeFor(dir))
		for _, id := range spec.Identity {
			// Identity values pin by presence; re-inject any the prototype covered.
			if v := mustGet(tree, id.Path); v != nil && mustGet(delta, id.Path) == nil {
				if err := pathSet(delta, id.Path, v); err != nil {
					return nil, err
				}
			}
		}
		fixtures[key] = delta
		fixtureKeys = append(fixtureKeys, key)
	}
	sortStrings(fixtureKeys)
	emitted := map[string][]byte{}
	for _, k := range fixtureKeys {
		body, err := fixtureFileBody(binding, fixtures[k])
		if err != nil {
			return nil, err
		}
		emitted[joinDir(fixtureDirs[k], k+FixtureSuffix)] = body
	}

	verify := &Spec{
		SourceName: spec.SourceName, SourceDir: spec.SourceDir, BaseName: spec.BaseName,
		Family: spec.Family, Schema: spec.Schema, OutputDir: spec.OutputDir, OutputIgnored: spec.OutputIgnored,
		Options: spec.Options, Defaults: spec.Defaults, Prototype: spec.Prototype, Metadata: spec.Metadata,
		Identity: spec.Identity, Fixtures: newFixtureSet(), prototypeOverlays: spec.prototypeOverlays,
	}
	for _, k := range fixtureKeys {
		verify.Fixtures.put(k, &Fixture{Data: fixtures[k], Dir: fixtureDirs[k], Source: k + FixtureSuffix + " (adopted)"})
	}
	ctx, err := a.context()
	if err != nil {
		return nil, err
	}
	generated, err := family.Expand(verify, a.baseDir, ctx)
	if err != nil {
		return nil, err
	}
	roundTrip, err := a.compare(generated, fixtureKeys, originalNames, adoption, decoded, originals, spec.SourceName)
	if err != nil {
		return nil, err
	}
	written, err := writeEmitted(a.baseDir, emitted, dryRun)
	if err != nil {
		return nil, err
	}
	n := itoa(len(files))
	verb := "wrote "
	if dryRun {
		verb = "would write "
	}
	report := []string{
		"decoded " + n + "/" + n + " files (family: " + spec.Family + ")",
		"bound " + itoa(len(fixtures)) + " new fixture(s) into " + spec.SourceName + " (" + itoa(spec.Fixtures.Len()) + " existing fixture(s) untouched)",
		verb + itoa(len(fixtures)) + " *.fixture.yaml next to the adopted files",
		roundTrip,
		"next: axx fixtures generate  (module-wide identity uniqueness is enforced there)",
	}
	return &AdoptionResult{Factory: spec.SourceName, Written: written, Report: report}, nil
}

// subtract is everything in tree the prototype does not already say: maps
// recurse, a list under a map prototype entry is the row-template case (each
// row subtracts the template), anything else stays when it differs.
func subtract(tree, prototype *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range tree.Keys() {
		actual := get(tree, k)
		if !has(prototype, k) {
			out.Set(k, actual)
			continue
		}
		expected := get(prototype, k)
		am, aIsMap := actual.(*jsonx.Object)
		em, eIsMap := expected.(*jsonx.Object)
		switch rows, isList := actual.([]any); {
		case aIsMap && eIsMap:
			if nested := subtract(am, em); nested.Len() > 0 {
				out.Set(k, nested)
			}
		case isList && eIsMap:
			deltas := make([]any, len(rows))
			for i, r := range rows {
				if rm, ok := r.(*jsonx.Object); ok {
					deltas[i] = subtract(rm, em)
				} else {
					deltas[i] = r
				}
			}
			out.Set(k, deltas)
		default:
			if !deepEquals(actual, expected) {
				out.Set(k, actual)
			}
		}
	}
	return out
}

// commonPrefix is the longest common prefix of an identity's values, the
// suggested prefix.
func commonPrefix(fixtureKeys []string, trees map[string]*jsonx.Object, path string) string {
	var prefix []rune
	first := true
	for _, k := range fixtureKeys {
		v := mustGet(trees[k], path)
		if v == nil {
			return ""
		}
		value := []rune(valueOf(v))
		if first {
			prefix, first = value, false
			continue
		}
		i := 0
		for i < len(prefix) && i < len(value) && prefix[i] == value[i] {
			i++
		}
		prefix = prefix[:i]
	}
	return string(prefix)
}

// identityCandidates are the dotted paths of string leaves whose values are
// present and distinct across every file (two files at least); a candidate
// whose values overlap an earlier one's is dropped, since identity values
// share one namespace.
func identityCandidates(shape *FieldShape, fixtureKeys []string, trees map[string]*jsonx.Object) []string {
	if len(fixtureKeys) < 2 {
		return nil
	}
	var all []string
	collectStringPaths(shape, "", &all)
	var distinct []string
	for _, p := range all {
		seen := map[string]bool{}
		missing := false
		for _, k := range fixtureKeys {
			v := mustGet(trees[k], p)
			missing = missing || v == nil
			seen[javaKey(v)] = true
		}
		if !missing && len(seen) == len(fixtureKeys) {
			distinct = append(distinct, p)
		}
	}
	claimed := map[string]bool{}
	var out []string
	for _, p := range distinct {
		var values []string
		overlap := false
		for _, k := range fixtureKeys {
			v := javaKey(mustGet(trees[k], p))
			values = append(values, v)
			overlap = overlap || claimed[v]
		}
		if overlap {
			continue
		}
		for _, v := range values {
			claimed[v] = true
		}
		out = append(out, p)
	}
	return out
}

func javaKey(v any) string { return javaSimpleName(v) + ":" + valueOf(v) }

func collectStringPaths(shape *FieldShape, path string, out *[]string) {
	for _, f := range shape.Children {
		p := joinPath(path, f.Name)
		if f.Nested() {
			collectStringPaths(f, p, out)
		} else if f.StringLeaf {
			*out = append(*out, p)
		}
	}
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// modalTree takes the modal value per field in schema order; identity fields
// and fields absent from any file are left out (an override can add a field
// but cannot say "omit this field").
func modalTree(shape *FieldShape, path string, trees []*jsonx.Object, identityPaths []string) *jsonx.Object {
	out := jsonx.NewObject()
	for _, f := range shape.Children {
		p := joinPath(path, f.Name)
		if containsString(identityPaths, p) {
			continue
		}
		values := make([]any, len(trees))
		anyAbsent := false
		allMaps := true
		for i, t := range trees {
			values[i] = presence(t, f.Name)
			anyAbsent = anyAbsent || values[i] == absent
			_, isMap := values[i].(*jsonx.Object)
			allMaps = allMaps && isMap
		}
		if anyAbsent {
			continue
		}
		if f.Nested() && allMaps {
			nested := make([]*jsonx.Object, len(values))
			for i, v := range values {
				nested[i] = v.(*jsonx.Object)
			}
			out.Set(f.Name, modalTree(f, p, nested, identityPaths))
		} else {
			best, _ := modal(values)
			out.Set(f.Name, best)
		}
	}
	return out
}

// modal is the most frequent value by equality; ties go to the earliest.
func modal(values []any) (any, int) {
	type group struct {
		value any
		count int
	}
	var groups []*group
	for _, v := range values {
		found := false
		for _, g := range groups {
			if deepEquals(g.value, v) {
				g.count++
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, &group{value: v, count: 1})
		}
	}
	var best any
	bestCount := -1
	for _, g := range groups {
		if g.count > bestCount {
			best, bestCount = g.value, g.count
		}
	}
	return best, bestCount
}

// deltaTree is the minimal override: fields that differ from the prototype.
func deltaTree(shape *FieldShape, path string, prototype, tree *jsonx.Object, identityPaths []string) *jsonx.Object {
	out := jsonx.NewObject()
	for _, f := range shape.Children {
		p := joinPath(path, f.Name)
		if containsString(identityPaths, p) {
			continue
		}
		expected, actual := presence(prototype, f.Name), presence(tree, f.Name)
		if actual == absent {
			continue // absent fields were never prototyped
		}
		em, eIsMap := expected.(*jsonx.Object)
		am, aIsMap := actual.(*jsonx.Object)
		switch {
		case f.Nested() && eIsMap && aIsMap:
			if nested := deltaTree(f, p, em, am, identityPaths); nested.Len() > 0 {
				out.Set(f.Name, nested)
			}
		case expected == absent || !deepEquals(expected, actual):
			out.Set(f.Name, actual)
		}
	}
	return out
}

// commonAncestorDir is where the factory lands: the adopted files' common
// ancestor directory.
func (a *Adopter) commonAncestorDir(files []string) string {
	var common string
	for i, f := range files {
		dir := relPath(a.baseDir, filepath.Dir(f))
		if i == 0 {
			common = dir
			continue
		}
		for common != "" && dir != common && !strings.HasPrefix(dir, common+"/") {
			common = parentDir(common)
		}
	}
	return common
}

// valueDifference names the first differing path of two decoded values.
func valueDifference(path string, a, b any) string {
	at := path
	if at == "" {
		at = "<root>"
	}
	if deepEquals(a, b) {
		return "no difference found at " + at
	}
	a, b = orderedNative(a), orderedNative(b)
	if ma, ok := a.(*jsonx.Object); ok {
		if mb, ok := b.(*jsonx.Object); ok {
			ks := ma.Keys()
			for _, k := range mb.Keys() {
				if !ma.Has(k) {
					ks = append(ks, k)
				}
			}
			for _, k := range ks {
				p := joinPath(path, k)
				switch {
				case !ma.Has(k):
					return p + ": absent in original, regenerated=" + valueOf(get(mb, k))
				case !mb.Has(k):
					return p + ": original=" + valueOf(get(ma, k)) + ", absent in regenerated"
				case !deepEquals(get(ma, k), get(mb, k)):
					return valueDifference(p, get(ma, k), get(mb, k))
				}
			}
		}
	}
	if la, ok := a.([]any); ok {
		if lb, ok := b.([]any); ok {
			if len(la) != len(lb) {
				return path + ": original has " + itoa(len(la)) + " element(s), regenerated " + itoa(len(lb))
			}
			for i := range la {
				if !deepEquals(la[i], lb[i]) {
					return valueDifference(path+"["+itoa(i)+"]", la[i], lb[i])
				}
			}
		}
	}
	return at + ": original=" + valueOf(a) + " (" + javaSimpleName(a) + "), regenerated=" + valueOf(b) + " (" + javaSimpleName(b) + ")"
}

// orderedNative turns a decoded native map (Avro records and maps) into an
// object with sorted keys so differences can be walked like any other.
func orderedNative(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := jsonx.NewObject()
	for _, k := range sortedMapKeys(m) {
		out.Set(k, m[k])
	}
	return out
}
