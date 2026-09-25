package fixtures

import (
	"regexp"
	"sort"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Family is one fixture family (avro, json, yaml, xml, protobuf, dataset):
// how its governing schema loads, how a resolved fixture renders to
// canonical bytes, and the conformance oracle, which is the code path the
// fixture travels at test time so a build-time failure is the runtime one.
//
// The interface is internal; a future plugin capability (factory.family)
// can back it.
type Family interface {
	// Name is the factory.family value this strategy implements.
	Name() string
	// Expand renders every fixture of spec: fixture key to its files
	// (fixture-relative name to bytes). The engine places each fixture's
	// files in that fixture's own directory.
	Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error)
	// Validate checks bytes against a schema through the family's oracle;
	// it backs conformance rules, whose schemaType is the family name.
	Validate(data []byte, baseDir, schemaRef, fixtureName string) error
}

// LintRuler is implemented by families whose output is not JSON (or whose
// identity paths land elsewhere in the output) to steer the generated lint
// rules. Families without it get one rule per identity over every .json
// file in their output directories.
type LintRuler interface {
	// LintFilePatterns are the files the identity rules cover; empty means
	// no rules for this spec.
	LintFilePatterns(spec *Spec) []string
	// LintJSONPath maps an identity path to the JSON path inside emitted
	// files; "" skips the rule.
	LintJSONPath(spec *Spec, identityPath string) string
}

// AdoptingFamily is implemented by families that can adopt existing files.
type AdoptingFamily interface {
	Adoption(baseDir, schemaRef string) (Adoption, error)
}

// Adoption is a family's adoption capability bound to one schema.
type Adoption interface {
	// Decode reads fixture bytes into the family's semantic value through
	// the oracle; two fixtures are the same data exactly when their decoded
	// values are equal.
	Decode(data []byte, where string) (any, error)
	// AuthoringTree reads fixture bytes back into the plain authoring form
	// the spec vocabulary uses, keys in schema order.
	AuthoringTree(data []byte, where string) (*jsonx.Object, error)
	// Shape is the schema's ordered field tree.
	Shape() (*FieldShape, error)
}

// Analyzer replaces the generic field-shape analysis when a family's
// fixture unit is not a single record (datasets).
type Analyzer interface {
	Analyze(keys []string, trees map[string]*jsonx.Object) (*AdoptionAnalysis, error)
}

// AdoptionAnalysis is a complete custom analysis.
type AdoptionAnalysis struct {
	IdentityPaths []string
	Prototype     *jsonx.Object
	Fixtures      map[string]*jsonx.Object
}

// FieldShape is one node of a schema's field tree, in declaration order.
// Adoption descends into nodes with Children (nested records); StringLeaf
// marks the only identity-candidate type.
type FieldShape struct {
	Name       string
	Children   []*FieldShape // nil for a leaf
	StringLeaf bool
}

func shapeRoot(children []*FieldShape) *FieldShape {
	if children == nil {
		children = []*FieldShape{}
	}
	return &FieldShape{Children: children}
}

func shapeNested(name string, children []*FieldShape) *FieldShape {
	if children == nil {
		children = []*FieldShape{}
	}
	return &FieldShape{Name: name, Children: children}
}

func shapeLeaf(name string, stringLeaf bool) *FieldShape {
	return &FieldShape{Name: name, StringLeaf: stringLeaf}
}

// Nested reports whether adoption descends into the node.
func (f *FieldShape) Nested() bool { return f.Children != nil }

// ExpansionContext is what a family needs while expanding one module: the
// module-wide identity registry and the mode-aware expression evaluator.
type ExpansionContext struct {
	identities *identities
	eval       *evaluator
}

// families indexes families by name.
type families map[string]Family

func newFamilies(extra []Family) (families, error) {
	out := families{}
	for _, f := range append(builtinFamilies(), extra...) {
		if prev, ok := out[f.Name()]; ok {
			return nil, configError("two fixture families claim the name '%s': %T and %T", f.Name(), prev, f)
		}
		out[f.Name()] = f
	}
	return out, nil
}

func (fs families) names() []string {
	out := make([]string, 0, len(fs))
	for n := range fs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// require resolves a family by name, failing with the supported set.
func (fs families) require(name, where string) (Family, error) {
	f, ok := fs[name]
	if !ok {
		return nil, specError("%s: unsupported family '%s' (supported: %s)", where, name, javaListString(fs.names()))
	}
	return f, nil
}

func builtinFamilies() []Family {
	return []Family{avroFamily{}, jsonFamily{}, yamlFamily{}, xmlFamily{}, protobufFamily{}, datasetFamily{}}
}

var indexRE = regexp.MustCompile(`\[\d+\]`)

// defaultsLookup is the factory defaults: lookup shared by every
// schema-walking family, most specific key first: the exact indexed path
// (order.payments[0].channel), the [] wildcard path (order.payments[].channel)
// and the bare field name, which fills wherever the field is unresolved.
func defaultsLookup(defaults *jsonx.Object, fieldPath, fieldName string) any {
	if defaults.Has(fieldPath) {
		return get(defaults, fieldPath)
	}
	wildcard := indexRE.ReplaceAllString(fieldPath, "[]")
	if wildcard != fieldPath && defaults.Has(wildcard) {
		return get(defaults, wildcard)
	}
	return get(defaults, fieldName)
}

// unresolvedError reports required fields no layer resolved.
func unresolvedError(kind string, unresolved []string, sourceName string) error {
	return genError("unresolved required %s(s) with no schema default:\n  %s\nfix: add the %s to defaults:, prototype:, or per-fixture overrides in %s",
		kind, joinLines(unresolved), kind, sourceName)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += l
	}
	return out
}
