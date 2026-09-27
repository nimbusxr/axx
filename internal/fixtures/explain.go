package fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Where a generated value comes from. A fixture's values are its own data
// deep-merged over the prototype overlays above it (nearest first) and the
// factory's prototype; a $ref takes a value from another document, an
// expression computes one, and an identity is pinned by the value the
// sources set or derived from the fixture key. What no source sets, the
// family fills from the factory's defaults:, then from the schema's default.

// Origin kinds.
const (
	OriginIdentity   = "identity"
	OriginReference  = "reference"
	OriginExpression = "expression"
	OriginFixture    = "fixture"
	OriginOverlay    = "prototype-overlay"
	OriginPrototype  = "prototype"
	OriginDefaults   = "defaults"
	OriginSchema     = "schema"
)

// Explanation is where one value of a generated fixture comes from.
type Explanation struct {
	// File is the file asked about, relative to the resource root.
	File    string `json:"file"`
	Factory string `json:"factory"`
	Fixture string `json:"fixture"`
	Path    string `json:"path"`
	// Value is the value as generated, in JSON.
	Value json.RawMessage `json:"value"`
	// Origins is the value's way into the fixture, outermost first: the
	// last one is the source that sets it.
	Origins []Origin `json:"origins"`
}

// Origin is one step of a value's way into a fixture.
type Origin struct {
	Kind string `json:"kind"`
	// File holds the step, relative to the resource root: a source file, or
	// the schema for a schema default.
	File string `json:"file,omitempty"`
	// At is where in File: the value's YAML path (prototype.recipient.country,
	// data.status), the defaults: key or the identity's path.
	At string `json:"at,omitempty"`
	// Detail is the $ref, the expression, or how an identity derives.
	Detail string `json:"detail,omitempty"`
}

// maxHops bounds the $refs one explanation follows.
const maxHops = 32

func explainError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeExplain, exitcode.Usage, format, args...).
		WithHint("give a file `axx fixtures generate` writes (or its *.fixture.yaml) and the path of one value in it, like recipient.postcode or items[0].sku")
}

// Explain says where the value at path in a generated fixture file comes
// from. file is relative to the working directory or to the resource root,
// and may be the fixture's *.fixture.yaml. path is dotted with indices
// (recipient.postcode, items[0].sku; a leading $. is accepted); a dataset
// path starts with the table (parcels.manifest_lines[0].weight_grams). Like
// check, it resolves recorded expression functions from the committed
// pairings only.
func (g *Generator) Explain(file, path string) (ex *Explanation, err error) {
	x, err := g.Expand(ModeVerify)
	if err != nil {
		return nil, err
	}
	var spec *Spec
	var key, out, rel string
	for _, c := range g.candidates(file) {
		if spec, key, out = g.fixtureOf(x, c); spec != nil {
			rel = c
			break
		}
	}
	if spec == nil {
		return nil, explainError("%s is not a file the fixture factory generates, nor a fixture's *.fixture.yaml (%s lists what it generates; paths are relative to the working directory or to %s)", file, ManifestFile, shownDir(g.baseDir))
	}
	steps, err := explainSteps(spec, key, path)
	if err != nil {
		return nil, err
	}
	eval := newEvaluator(g.functions, ModeVerify, g.committed)
	e := &explainer{g: g, ctx: &ExpansionContext{identities: newIdentities(), eval: eval}}
	eval.references = g.referencesAcross(e.ctx)
	defer func() {
		if ferr := eval.finish(); ferr != nil && err == nil {
			ex, err = nil, ferr
		}
	}()
	generated, err := e.generated(spec, key, x.Files[out], out)
	if err != nil {
		return nil, err
	}
	shown := renderSteps(steps)
	value, ok := lookup(generated, steps, namesOf(spec))
	if !ok {
		fields := ""
		if o, isObject := object(generated); isObject {
			fields = " (its fields: " + strings.Join(o.Keys(), ", ") + ")"
		}
		return nil, explainError("%s has no value at %s%s", rel, shown, fields)
	}
	if o, isObject := object(value); isObject {
		return nil, explainError("%s is an object in %s: explain one of its fields (%s)", shown, rel, strings.Join(o.Keys(), ", "))
	}
	r, err := e.explain(spec, key, steps, generated, true, 0)
	if err != nil {
		return nil, err
	}
	if !r.ok {
		return nil, genError("cannot tell where %s in %s comes from", shown, rel)
	}
	return &Explanation{File: rel, Factory: spec.SourceName, Fixture: key, Path: shown, Value: compactJSON(value), Origins: r.origins}, nil
}

// candidates are the resource-root-relative paths file may mean: from the
// working directory when it lies beneath the root, and as root-relative.
func (g *Generator) candidates(file string) []string {
	var out []string
	if base, err := filepath.Abs(g.baseDir); err == nil {
		if abs, err := filepath.Abs(file); err == nil {
			if r := relPath(base, abs); r != "" && r != ".." && !strings.HasPrefix(r, "../") {
				out = append(out, r)
			}
		}
	}
	if r := strings.TrimPrefix(filepath.ToSlash(file), "./"); !filepath.IsAbs(file) && !containsString(out, r) {
		out = append(out, r)
	}
	return out
}

// fixtureOf is the factory and fixture a file belongs to, and the generated
// file holding its values; nil when it is neither generated nor a fixture's
// own file.
func (g *Generator) fixtureOf(x *Expansion, rel string) (*Spec, string, string) {
	if entry, ok := x.Manifest.Get(rel); ok && !strings.HasPrefix(entry.Factory, "<") {
		for _, s := range g.specs {
			if s.SourceName == entry.Factory {
				return s, entry.Fixture, rel
			}
		}
	}
	for _, s := range g.specs {
		for _, k := range s.Fixtures.Keys() {
			if s.Fixtures.Get(k).Source != rel {
				continue
			}
			for _, entry := range x.Manifest.Entries() {
				if entry.Factory == s.SourceName && entry.Fixture == k {
					return s, k, entry.Path
				}
			}
			return s, k, ""
		}
	}
	return nil, "", ""
}

// shownDir is dir from the working directory when it lies beneath it.
func shownDir(dir string) string {
	wd, err := os.Getwd()
	if err != nil {
		return dir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if r := relPath(wd, abs); !strings.HasPrefix(r, "../") && r != ".." {
		if r == "" {
			return "."
		}
		return r
	}
	return dir
}

// explainer answers where values come from within one expansion.
type explainer struct {
	g   *Generator
	ctx *ExpansionContext
}

// found is what a walk found: the origins of a value, or, when there is
// none, the first step no source has (miss; -1 when the path runs into a
// value or past a list's end, where nothing fills it).
type found struct {
	ok      bool
	origins []Origin
	miss    int
}

// source is one layer's value at the walked prefix: kind and file say which
// layer, base is the YAML path of its root in file.
type source struct {
	kind, file, base string
	node             any
}

func (s source) with(node any) source {
	s.node = node
	return s
}

// document is one fixture being walked.
type document struct {
	spec  *Spec
	key   string
	refs  bool // whether $refs resolve in it (dataset rows keep them)
	names func(string) []string
	hops  int
}

// pendingRef is a $ref met on the way down; from is the step its node sits
// at, so the steps from there on continue inside its target.
type pendingRef struct {
	text   string
	from   int
	origin Origin
}

// plainer is implemented by families whose generated files encode values
// other than as their sources write them (Avro's union branches): it reads
// a generated file back in the sources' shape.
type plainer interface {
	plain(spec *Spec, baseDir string, data []byte, where string) (any, error)
}

// generated is the fixture as generated: what the family hands a $ref when
// it can, else the generated file read back, else its resolved values.
func (e *explainer) generated(spec *Spec, key string, data []byte, out string) (any, error) {
	f, err := e.g.family(spec)
	if err != nil {
		return nil, err
	}
	if r, ok := f.(Referencer); ok {
		return r.Referenced(spec, e.g.baseDir, key, e.ctx)
	}
	if p, ok := f.(plainer); ok && data != nil {
		return p.plain(spec, e.g.baseDir, data, out)
	}
	if strings.HasSuffix(out, ".json") && data != nil {
		return parseJSON(data, out)
	}
	tree, err := resolveFixture(spec, key, e.ctx)
	if err != nil {
		return nil, err
	}
	if spec.Family == "xml" {
		applyDottedDefaults(spec.Defaults, tree)
	}
	return tree, nil
}

// explain is where the value at steps of a fixture comes from: an identity,
// else the fixture's sources, else (when fill) what the family fills: its
// defaults:, then the schema's default when generated holds a value there.
func (e *explainer) explain(spec *Spec, key string, steps []step, generated any, fill bool, hops int) (found, error) {
	if hops > maxHops {
		return found{}, genError("more than %d $refs lead to %s of fixtures.%s (%s)", maxHops, renderSteps(steps), key, spec.SourceName)
	}
	if spec.Family == "dataset" {
		return e.row(spec, key, steps, hops)
	}
	d := &document{spec: spec, key: key, refs: true, names: namesOf(spec), hops: hops}
	r, err := e.walk(d, e.sources(spec, key), steps, 0, nil)
	if err != nil {
		return found{}, err
	}
	for _, id := range spec.Identity {
		if ids, perr := parseSteps(id.Path); perr != nil || !sameSteps(ids, steps, d.names) {
			continue
		}
		if r.ok {
			return found{ok: true, origins: append([]Origin{pinned(spec, id)}, r.origins...)}, nil
		}
		return found{ok: true, origins: []Origin{derived(spec, id, "the fixture key", key)}}, nil
	}
	if r.ok || r.miss < 0 || !fill {
		return r, nil
	}
	if o, ok := e.defaults(d, steps, r.miss); ok {
		return found{ok: true, origins: []Origin{o}}, nil
	}
	if _, ok := lookup(generated, steps, d.names); ok && generated != nil {
		return found{ok: true, origins: []Origin{{Kind: OriginSchema, File: spec.SchemaRef(), Detail: "the schema's default"}}}, nil
	}
	return r, nil
}

// sources are a fixture's layers, topmost first: its data, then the
// prototype layers.
func (e *explainer) sources(spec *Spec, key string) []source {
	fx := spec.Fixtures.Get(key)
	file, base := fixtureSource(spec, key)
	var out []source
	if fx.Data != nil {
		out = append(out, source{kind: OriginFixture, file: file, base: base, node: fx.Data})
	}
	return append(out, prototypes(spec, fx.Dir)...)
}

// fixtureSource is the file a fixture's data is written in and its YAML
// path there.
func fixtureSource(spec *Spec, key string) (file, base string) {
	fx := spec.Fixtures.Get(key)
	if fx.Source == "inline in "+spec.SourceName {
		return spec.SourceName, "fixtures." + key
	}
	return fx.Source, "data"
}

// prototypes are the prototype layers of a fixture in dir, topmost first:
// the overlays above it (nearest first), then the factory's prototype.
func prototypes(spec *Spec, dir string) []source {
	var out []source
	overlays := chain(spec.prototypeOverlays, dir)
	for i := len(overlays) - 1; i >= 0; i-- {
		d := overlays[i]
		file := spec.BaseName + PrototypeSuffix
		if d != "" {
			file = d + "/" + file
		}
		if o := spec.prototypeOverlays[d]; o != nil {
			out = append(out, source{kind: OriginOverlay, file: file, base: "data", node: o})
		}
	}
	switch {
	case spec.Prototype == nil:
	case spec.prototypeFile != "":
		out = append(out, source{kind: OriginPrototype, file: spec.prototypeFile, base: "data", node: spec.Prototype})
	default:
		out = append(out, source{kind: OriginPrototype, file: spec.SourceName, base: "prototype", node: spec.Prototype})
	}
	return out
}

// walk follows steps[i:] from sources, which hold the value at steps[:i],
// topmost first. The topmost source with a key sets it: an object merges
// with the objects below it, anything else replaces them. ref is the
// nearest $ref above, whose target holds what no source sets.
func (e *explainer) walk(d *document, sources []source, steps []step, i int, ref *pendingRef) (found, error) {
	top := sources[0]
	_, topObject := object(top.node)
	if topObject && d.refs && i > 0 {
		for _, s := range sources {
			o, _ := object(s.node)
			if has(o, refKeyword) {
				text := valueOf(get(o, refKeyword))
				ref = &pendingRef{text: text, from: i, origin: Origin{Kind: OriginReference, File: s.file, At: joinAt(s.base, steps[:i]) + "." + refKeyword, Detail: text}}
				break
			}
		}
	}
	if i == len(steps) {
		if ref != nil && ref.from == i {
			return e.follow(d, ref, steps)
		}
		return found{ok: true, origins: written(top, steps)}, nil
	}
	// The step is recorded as the sources write it (a list index, the name
	// a protobuf field is written under), which is what At shows.
	s := steps[i]
	var next []source
	if list, ok := top.node.([]any); ok {
		if n, ok := s.position(); ok && n < len(list) {
			next, steps[i] = []source{top.with(list[n])}, step{index: n}
		}
	} else if s.index < 0 {
		for _, src := range sources {
			o, ok := object(src.node)
			if !ok {
				break
			}
			name, ok := memberName(o, s.key, d.names)
			if !ok {
				continue
			}
			v := get(o, name)
			_, isObject := object(v)
			if next != nil && !isObject {
				break
			}
			if next == nil {
				steps[i] = keyStep(name)
			}
			next = append(next, src.with(v))
			if !isObject {
				break
			}
		}
	}
	if next == nil {
		switch {
		case !topObject:
			return found{miss: -1}, nil
		case ref != nil:
			return e.follow(d, ref, steps)
		}
		return found{miss: i}, nil
	}
	if _, isObject := object(next[0].node); !isObject {
		ref = nil // a value or a list replaces whatever a $ref above holds
	}
	return e.walk(d, next, steps, i+1, ref)
}

// written is the origin of a value a source sets; an expression there
// computes it.
func written(s source, steps []step) []Origin {
	o := Origin{Kind: s.kind, File: s.file, At: joinAt(s.base, steps)}
	if isExpression(s.node) {
		return []Origin{{Kind: OriginExpression, Detail: javaStrip(valueOf(s.node))}, o}
	}
	return []Origin{o}
}

// follow explains steps[ref.from:] inside what a $ref points at. What its
// target lacks is missing at the matching step here, so this fixture's
// defaults: can still fill it.
func (e *explainer) follow(d *document, ref *pendingRef, steps []step) (found, error) {
	path, pointer := splitRef(ref.text)
	lead := pointerSteps(pointer)
	target := slices.Concat(lead, steps[ref.from:])
	rest := target
	var r found
	var err error
	if path == "" {
		// A same-document reference sees the document's own values.
		same := *d
		same.hops++
		if same.hops > maxHops {
			return found{}, genError("more than %d $refs lead to %s of fixtures.%s (%s)", maxHops, renderSteps(steps), d.key, d.spec.SourceName)
		}
		r, err = e.walk(&same, e.sources(d.spec, d.key), target, 0, nil)
	} else {
		fx := d.spec.Fixtures.Get(d.key)
		var spec *Spec
		var key string
		var ok bool
		if spec, key, rest, ok = e.target(normalizeRef(fx.Dir, path), target); !ok {
			return found{miss: -1}, nil
		}
		r, err = e.referenced(spec, key, rest, d.hops+1)
	}
	if err != nil {
		return found{}, err
	}
	if !r.ok {
		// The step of target the walk missed, mapped back to steps.
		if at := r.miss + len(target) - len(rest); r.miss >= 0 && at >= len(lead) {
			return found{miss: ref.from + at - len(lead)}, nil
		}
		return found{miss: -1}, nil
	}
	r.origins = append([]Origin{ref.origin}, r.origins...)
	return r, nil
}

// target is the fixture a $ref's document names and the steps inside it: a
// fixture file is one fixture, a factory file holds its fixtures by key.
func (e *explainer) target(doc string, steps []step) (*Spec, string, []step, bool) {
	for _, s := range e.g.specs {
		for _, k := range s.Fixtures.Keys() {
			if s.Fixtures.Get(k).Source == doc {
				return s, k, steps, true
			}
		}
	}
	for _, s := range e.g.specs {
		if s.SourceName == doc && len(steps) > 0 && s.Fixtures.Get(steps[0].key) != nil {
			return s, steps[0].key, steps[1:], true
		}
	}
	return nil, "", nil, false
}

// referenced explains a value of another document as a $ref sees it: as
// its family generates it when the family hands $refs that (its defaults
// and schema applied), else its resolved values and identities.
func (e *explainer) referenced(spec *Spec, key string, steps []step, hops int) (found, error) {
	f, err := e.g.family(spec)
	if err != nil {
		return found{}, err
	}
	r, ok := f.(Referencer)
	if !ok {
		return e.explain(spec, key, steps, nil, false, hops)
	}
	generated, err := r.Referenced(spec, e.g.baseDir, key, e.ctx)
	if err != nil {
		return found{}, err
	}
	return e.explain(spec, key, steps, generated, true, hops)
}

// defaults is the defaults: entry that fills a field no source sets, the
// field at steps[miss] being the first one missing.
func (e *explainer) defaults(d *document, steps []step, miss int) (Origin, bool) {
	spec := d.spec
	var key string
	switch spec.Family {
	case "xml":
		// The xml family fills a dotted path whose parents exist.
		key = renderSteps(steps)
		if miss != len(steps)-1 || !strings.Contains(key, ".") {
			return Origin{}, false
		}
	case "protobuf":
		// Its walker names fields by their proto names.
		named := make([]step, miss+1)
		for i, s := range steps[:miss+1] {
			if s.index < 0 {
				s.key = snakeCase(s.key)
			}
			named[i] = s
		}
		key = defaultsKey(spec.Defaults, renderSteps(named), named[miss].key)
	default:
		key = defaultsKey(spec.Defaults, renderSteps(steps[:miss+1]), steps[miss].key)
	}
	v := get(spec.Defaults, key)
	if v == nil {
		return Origin{}, false
	}
	if _, ok := lookup(v, steps[miss+1:], d.names); !ok {
		return Origin{}, false
	}
	return Origin{Kind: OriginDefaults, File: spec.SourceName, At: key}, true
}

// row explains a dataset value, steps being the table, the row and the
// column: the row over its table's row templates in the prototype layers,
// then the table.column identities, then the factory's defaults:.
func (e *explainer) row(spec *Spec, key string, steps []step, hops int) (found, error) {
	if len(steps) != 3 || steps[2].index >= 0 {
		return found{miss: -1}, nil
	}
	n, ok := steps[1].position()
	if !ok {
		return found{miss: -1}, nil
	}
	fx := spec.Fixtures.Get(key)
	table, column := strings.ToLower(steps[0].key), steps[2].key
	rawTable := ""
	for _, t := range keys(fx.Data) {
		if strings.ToLower(t) == table {
			rawTable = t
			break
		}
	}
	rows, _ := get(fx.Data, rawTable).([]any)
	if rawTable == "" || n >= len(rows) {
		return found{miss: -1}, nil
	}
	file, base := fixtureSource(spec, key)
	sources := []source{{kind: OriginFixture, file: file, base: base + "." + rawTable + "[" + itoa(n) + "]", node: rows[n]}}
	for _, p := range prototypes(spec, fx.Dir) {
		o, _ := object(p.node)
		if template, ok := object(get(o, rawTable)); ok {
			p.base, p.node = p.base+"."+rawTable, template
			sources = append(sources, p)
		}
	}
	d := &document{spec: spec, key: key, names: exactName, hops: hops}
	r, err := e.walk(d, sources, steps[2:], 0, nil)
	if err != nil {
		return found{}, err
	}
	for _, id := range spec.Identity {
		dot := strings.LastIndexByte(id.Path, '.')
		if dot < 0 || strings.ToLower(id.Path[:dot]) != table || id.Path[dot+1:] != column {
			continue
		}
		if r.ok {
			return found{ok: true, origins: append([]Origin{pinned(spec, id)}, r.origins...)}, nil
		}
		if n == 0 {
			return found{ok: true, origins: []Origin{derived(spec, id, "the fixture key", key)}}, nil
		}
		return found{ok: true, origins: []Origin{derived(spec, id, "the fixture key and row number", key+"-"+itoa(n+1))}}, nil
	}
	if r.ok || r.miss < 0 {
		return r, nil
	}
	candidates := []string{table + "." + column}
	if strings.TrimSpace(spec.Schema) != "" {
		candidates = append(candidates, column) // bare names need the DDL
	}
	for _, k := range candidates {
		if has(spec.Defaults, k) {
			return found{ok: true, origins: []Origin{{Kind: OriginDefaults, File: spec.SourceName, At: k}}}, nil
		}
	}
	return found{miss: -1}, nil
}

// pinned is an identity whose value the sources set.
func pinned(spec *Spec, id Identity) Origin {
	return Origin{Kind: OriginIdentity, File: spec.SourceName, At: id.Path, Detail: "the value its sources set"}
}

// derived is an identity no source sets, derived from name.
func derived(spec *Spec, id Identity, what, name string) Origin {
	detail := fmt.Sprintf("derived from %s %q", what, name)
	if id.Prefix != "" {
		detail += fmt.Sprintf(" with the prefix %q", id.Prefix)
	}
	if q := strings.TrimSpace(id.Qualifier); q != "" {
		detail += fmt.Sprintf(" and the qualifier %q", q)
	}
	if id.Format == "uuid-name-based" {
		detail += ", as a name-based UUID"
	}
	return Origin{Kind: OriginIdentity, File: spec.SourceName, At: id.Path, Detail: detail}
}

// Paths.

// step is one step of a path: a key, or a list index (index >= 0).
type step struct {
	key   string
	index int
}

func keyStep(k string) step { return step{key: k, index: -1} }

// position is the list index a step names: its index, or a JSON Pointer's
// numeric token.
func (s step) position() (int, bool) {
	if s.index >= 0 {
		return s.index, true
	}
	n, err := strconv.Atoi(s.key)
	return n, err == nil && n >= 0
}

// explainSteps reads the path asked about; a dataset's starts with its
// table, whose name may hold dots.
func explainSteps(spec *Spec, key, path string) ([]step, error) {
	p := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$"), ".")
	if spec.Family != "dataset" {
		return parseSteps(p)
	}
	table, rest, _ := strings.Cut(p, "[")
	steps, err := parseSteps("[" + rest)
	if table == "" || err != nil || len(steps) != 2 || steps[0].index < 0 || steps[1].index >= 0 {
		example := "orders"
		if tables := keys(spec.Fixtures.Get(key).Data); len(tables) > 0 {
			example = strings.ToLower(tables[0])
		}
		return nil, explainError("fixture %s is a dataset: give <table>[<row>].<column>, like %s[0].<column>", key, example)
	}
	return append([]step{keyStep(strings.ToLower(table))}, steps...), nil
}

// parseSteps reads a dotted path with indices: recipient.postcode,
// items[0].sku.
func parseSteps(p string) ([]step, error) {
	bad := explainError("'%s' is not a path: write it dotted, with indices, like items[0].sku", p)
	if p == "" {
		return nil, bad
	}
	var out []step
	for p != "" {
		if p[0] == '[' {
			end := strings.IndexByte(p, ']')
			if end < 0 {
				return nil, bad
			}
			n, err := strconv.Atoi(p[1:end])
			if err != nil || n < 0 {
				return nil, bad
			}
			out = append(out, step{index: n})
			p = strings.TrimPrefix(p[end+1:], ".")
			continue
		}
		end := strings.IndexAny(p, ".[")
		if end < 0 {
			end = len(p)
		}
		if end == 0 {
			return nil, bad
		}
		out = append(out, keyStep(p[:end]))
		p = p[end:]
		if strings.HasPrefix(p, ".") {
			if p = p[1:]; p == "" {
				return nil, bad
			}
		}
	}
	return out, nil
}

// pointerSteps reads a JSON Pointer's tokens as keys; position reads a
// numeric one as a list index.
func pointerSteps(pointer string) []step {
	if pointer == "" || pointer == "/" {
		return nil
	}
	var out []step
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		out = append(out, keyStep(strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")))
	}
	return out
}

func renderSteps(steps []step) string {
	var b strings.Builder
	for _, s := range steps {
		if s.index >= 0 {
			b.WriteString("[" + itoa(s.index) + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s.key)
	}
	return b.String()
}

// joinAt is a YAML path: base, then steps.
func joinAt(base string, steps []step) string {
	r := renderSteps(steps)
	switch {
	case r == "":
		return base
	case base == "" || strings.HasPrefix(r, "["):
		return base + r
	}
	return base + "." + r
}

// sameSteps reports whether two paths name the same value.
func sameSteps(a, b []step, names func(string) []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].index != b[i].index || (a[i].index < 0 && !containsString(names(a[i].key), b[i].key)) {
			return false
		}
	}
	return true
}

// lookup is the value at steps; ok is false when there is none.
func lookup(v any, steps []step, names func(string) []string) (any, bool) {
	for _, s := range steps {
		switch x := v.(type) {
		case *jsonx.Object:
			if s.index >= 0 || x == nil {
				return nil, false
			}
			m, ok := member(x, s.key, names)
			if !ok {
				return nil, false
			}
			v = m
		case []any:
			n, ok := s.position()
			if !ok || n >= len(x) {
				return nil, false
			}
			v = x[n]
		default:
			return nil, false
		}
	}
	return v, true
}

// member is an object's value under a key, or one of its other names.
func member(o *jsonx.Object, key string, names func(string) []string) (any, bool) {
	name, ok := memberName(o, key, names)
	return get(o, name), ok
}

// memberName is the name an object holds a key under.
func memberName(o *jsonx.Object, key string, names func(string) []string) (string, bool) {
	for _, n := range names(key) {
		if has(o, n) {
			return n, true
		}
	}
	return "", false
}

// object is v as an object; false for anything else, a nil one included.
func object(v any) (*jsonx.Object, bool) {
	o, ok := v.(*jsonx.Object)
	return o, ok && o != nil
}

func exactName(k string) []string { return []string{k} }

// namesOf is the keys a field may be written under: a protobuf field
// answers to its proto name and its JSON name.
func namesOf(spec *Spec) func(string) []string {
	if spec.Family != "protobuf" {
		return exactName
	}
	return func(k string) []string {
		out := []string{k}
		for _, alt := range []string{snakeCase(k), lowerCamel(k)} {
			if !containsString(out, alt) {
				out = append(out, alt)
			}
		}
		return out
	}
}

// snakeCase is a JSON name's proto name: trackingNumber, tracking_number.
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lowerCamel is a proto name's JSON name: tracking_number, trackingNumber.
func lowerCamel(s string) string {
	var b strings.Builder
	upper := false
	for _, r := range s {
		switch {
		case r == '_':
			upper = true
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
