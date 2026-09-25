package fixtures

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

var printer = message.NewPrinter(language.English)

// jsonFamily is family: json: JSON fixture files governed by a standalone
// JSON Schema document (any draft, detected from $schema) or by an OpenAPI
// 3.0/3.1 component (spec.yaml#/components/schemas/Name). Fields resolve
// fixture override, prototype, factory defaults, schema default; a required
// field left unresolved is a hard error, an optional one is omitted.
type jsonFamily struct{}

func (jsonFamily) Name() string { return "json" }

func requireSchema(spec *Spec, family string) error {
	if strings.TrimSpace(spec.Schema) == "" {
		return specError("%s: factory.schema is required for family %s", spec.SourceName, family)
	}
	return nil
}

func (f jsonFamily) Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	if err := requireSchema(spec, f.Name()); err != nil {
		return nil, err
	}
	g, err := loadGoverning(baseDir, spec.SchemaRef())
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]byte{}
	var unresolved []string
	for _, key := range spec.Fixtures.SortedKeys() {
		tree, err := resolveFixture(spec, key, ctx)
		if err != nil {
			return nil, err
		}
		node, err := g.resolve(tree, spec.Defaults, key, spec.SourceName, &unresolved)
		if err != nil {
			return nil, err
		}
		if len(unresolved) == 0 {
			data := canonicalJSON(node)
			if err := g.validate(data, where(spec, key)); err != nil {
				return nil, err
			}
			out[key] = map[string][]byte{key + ".json": data}
		}
	}
	if len(unresolved) > 0 {
		return nil, unresolvedError("field", unresolved, spec.SourceName)
	}
	return out, nil
}

func (jsonFamily) Validate(data []byte, baseDir, schemaRef, fixtureName string) error {
	g, err := loadGoverning(baseDir, schemaRef)
	if err != nil {
		return err
	}
	return g.validate(data, fixtureName)
}

func (jsonFamily) Adoption(baseDir, schemaRef string) (Adoption, error) {
	g, err := loadGoverning(baseDir, schemaRef)
	if err != nil {
		return nil, err
	}
	return &jsonAdoption{g: g}, nil
}

type jsonAdoption struct{ g jsonGoverning }

func (a *jsonAdoption) Decode(data []byte, where string) (any, error) {
	if err := a.g.validate(data, where); err != nil { // the oracle gates adoption
		return nil, err
	}
	return parseJSON(data, where)
}

func (a *jsonAdoption) AuthoringTree(data []byte, where string) (*jsonx.Object, error) {
	node, err := parseJSON(data, where)
	if err != nil {
		return nil, err
	}
	return a.g.authoringTree(node, where)
}

func (a *jsonAdoption) Shape() (*FieldShape, error) { return a.g.shape() }

// yamlFamily is family: yaml: record-shaped YAML governed by the same schema
// languages and walkers as json; only the rendering differs (deterministic
// YAML). Structural lint rules are JSON-based, so it emits none.
type yamlFamily struct{}

func (yamlFamily) Name() string { return "yaml" }

func (f yamlFamily) Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	if err := requireSchema(spec, f.Name()); err != nil {
		return nil, err
	}
	g, err := loadGoverning(baseDir, spec.SchemaRef())
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]byte{}
	var unresolved []string
	for _, key := range spec.Fixtures.SortedKeys() {
		tree, err := resolveFixture(spec, key, ctx)
		if err != nil {
			return nil, err
		}
		node, err := g.resolve(tree, spec.Defaults, key, spec.SourceName, &unresolved)
		if err != nil {
			return nil, err
		}
		if len(unresolved) == 0 {
			// The oracle validates the tree (as JSON); YAML carries it.
			if err := g.validate(compactJSON(node), where(spec, key)); err != nil {
				return nil, err
			}
			data, err := jyaml.Marshal(node, yamlFactory)
			if err != nil {
				return nil, genError("cannot serialize fixture YAML: %v", err)
			}
			out[key] = map[string][]byte{key + ".yaml": data}
		}
	}
	if len(unresolved) > 0 {
		return nil, unresolvedError("field", unresolved, spec.SourceName)
	}
	return out, nil
}

func readYAMLMapping(data []byte, where string) (*jsonx.Object, error) {
	v, err := jyaml.Unmarshal(data)
	if err != nil {
		return nil, genError("%s is not valid YAML: %v", where, err)
	}
	obj, ok := v.(*jsonx.Object)
	if !ok {
		return nil, genError("%s: expected a top-level YAML mapping", where)
	}
	return obj, nil
}

// Validate parses the YAML into the identical tree and runs the json
// oracles over it.
func (yamlFamily) Validate(data []byte, baseDir, schemaRef, fixtureName string) error {
	g, err := loadGoverning(baseDir, schemaRef)
	if err != nil {
		return err
	}
	node, err := readYAMLMapping(data, fixtureName)
	if err != nil {
		return err
	}
	return g.validate(compactJSON(node), fixtureName)
}

func (yamlFamily) LintFilePatterns(*Spec) []string { return nil }

func (yamlFamily) LintJSONPath(_ *Spec, p string) string { return p }

func (yamlFamily) Adoption(baseDir, schemaRef string) (Adoption, error) {
	g, err := loadGoverning(baseDir, schemaRef)
	if err != nil {
		return nil, err
	}
	return &yamlAdoption{g: g}, nil
}

type yamlAdoption struct{ g jsonGoverning }

func (a *yamlAdoption) Decode(data []byte, where string) (any, error) {
	node, err := readYAMLMapping(data, where)
	if err != nil {
		return nil, err
	}
	if err := a.g.validate(compactJSON(node), where); err != nil {
		return nil, err
	}
	return node, nil
}

func (a *yamlAdoption) AuthoringTree(data []byte, where string) (*jsonx.Object, error) {
	node, err := readYAMLMapping(data, where)
	if err != nil {
		return nil, err
	}
	return a.g.authoringTree(node, where)
}

func (a *yamlAdoption) Shape() (*FieldShape, error) { return a.g.shape() }
