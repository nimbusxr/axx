package fixtures

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/schema_validation"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"go.yaml.in/yaml/v4"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// openAPIComponent is one resolved OpenAPI component schema, the json
// family's OpenAPI backend (spec.yaml#/components/schemas/Name): the layered
// value resolver, the unknown-property typo guard and the conformance oracle,
// an OpenAPI schema validator like the one WireMock's OpenAPI extension runs
// at serve time.
type openAPIComponent struct {
	component *base.Schema
	name      string
	version   float32
}

func loadOpenAPIComponent(baseDir, ref string) (*openAPIComponent, error) {
	hash := strings.IndexByte(ref, '#')
	if hash < 0 || !strings.HasPrefix(ref[hash:], componentPrefix) {
		return nil, schemaError("OpenAPI component ref must be <spec>.yaml%s<Name>, got '%s'", componentPrefix, ref)
	}
	specPath, name := ref[:hash], ref[hash+len(componentPrefix):]
	path := filepath.Join(baseDir, filepath.FromSlash(specPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, schemaError("cannot parse OpenAPI spec %s: %v", specPath, err)
	}
	doc, err := libopenapi.NewDocumentWithConfiguration(data, &datamodel.DocumentConfiguration{
		BasePath:            filepath.Dir(path),
		SpecFilePath:        path,
		AllowFileReferences: true,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return nil, schemaError("cannot parse OpenAPI spec %s: %v", specPath, err)
	}
	model, err := doc.BuildV3Model()
	if model == nil {
		return nil, schemaError("cannot parse OpenAPI spec %s: %v", specPath, err)
	}
	var proxy *base.SchemaProxy
	if model.Model.Components != nil && model.Model.Components.Schemas != nil {
		proxy = model.Model.Components.Schemas.GetOrZero(name)
	}
	if proxy == nil {
		return nil, schemaError("%s has no component schema '%s' under #/components/schemas", specPath, name)
	}
	schema := proxy.Schema()
	if schema == nil {
		return nil, schemaError("cannot resolve %s%s%s: %v", specPath, componentPrefix, name, proxy.GetBuildError())
	}
	return &openAPIComponent{component: schema, name: name, version: doc.GetSpecInfo().VersionNumeric}, nil
}

func resolved(p *base.SchemaProxy) *base.Schema {
	if p == nil {
		return nil
	}
	return p.Schema()
}

func (c *openAPIComponent) validate(data []byte, fixtureName string) error {
	v := schema_validation.NewSchemaValidator(config.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	version := c.version
	if version == 0 {
		version = 3.1
	}
	ok, errs := v.ValidateSchemaBytesWithVersion(c.component, data, version)
	if ok {
		return nil
	}
	var lines []string
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			lines = append(lines, e.Reason)
			continue
		}
		for _, f := range e.SchemaValidationErrors {
			where := f.FieldPath
			if where == "" {
				where = "$"
			}
			lines = append(lines, where+": "+f.Reason)
		}
	}
	return genError("%s does not conform to %s:\n  %s", fixtureName, c.name, strings.Join(lines, "\n  "))
}

func (c *openAPIComponent) resolve(tree, defaults *jsonx.Object, key, source string, unresolved *[]string) (*jsonx.Object, error) {
	return c.object(c.component, tree, "", &jsonWalk{defaults: defaults, key: key, source: source, unresolved: unresolved})
}

func (c *openAPIComponent) shape() (*FieldShape, error) {
	return shapeRoot(c.children(c.component)), nil
}

func (c *openAPIComponent) children(s *base.Schema) []*FieldShape {
	var out []*FieldShape
	if s.Properties == nil {
		return out
	}
	for name, p := range s.Properties.FromOldest() {
		child := resolved(p)
		if child != nil && oaIsObject(child) && child.Properties != nil {
			out = append(out, shapeNested(name, c.children(child)))
		} else {
			out = append(out, shapeLeaf(name, child != nil && oaType(child) == "string"))
		}
	}
	return out
}

func (c *openAPIComponent) authoringTree(node any, where string) (*jsonx.Object, error) {
	return c.readObject(c.component, node, where)
}

func (c *openAPIComponent) readObject(s *base.Schema, node any, where string) (*jsonx.Object, error) {
	obj, ok := node.(*jsonx.Object)
	if !ok {
		return nil, adoptError("%s: expected a JSON object", where)
	}
	out := jsonx.NewObject()
	declared := map[string]bool{}
	if s.Properties != nil {
		for name, p := range s.Properties.FromOldest() {
			declared[name] = true
			if obj.Has(name) {
				v, err := c.readValue(resolved(p), get(obj, name), where)
				if err != nil {
					return nil, err
				}
				out.Set(name, v)
			}
		}
	}
	for _, name := range obj.Keys() {
		if !declared[name] {
			return nil, adoptError("%s: field '%s' is not declared by %s", where, name, c.name)
		}
	}
	return out, nil
}

func (c *openAPIComponent) readValue(s *base.Schema, node any, where string) (any, error) {
	switch x := node.(type) {
	case nil:
		return nil, nil
	case *jsonx.Object:
		if s != nil && s.Properties != nil {
			return c.readObject(s, x, where)
		}
		return plainJSON(x), nil
	case []any:
		items := s
		if s != nil && s.Items != nil && s.Items.IsA() && s.Items.A != nil {
			items = resolved(s.Items.A)
		}
		out := make([]any, 0, len(x))
		for _, e := range x {
			v, err := c.readValue(items, e, where)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	return plainJSON(node), nil
}

func (c *openAPIComponent) object(s *base.Schema, values *jsonx.Object, path string, w *jsonWalk) (*jsonx.Object, error) {
	remaining := copyObjectShallow(values)
	node := jsonx.NewObject()
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	if s.Properties != nil {
		for name, p := range s.Properties.FromOldest() {
			property := resolved(p)
			if property == nil {
				return nil, schemaError("%s: property '%s' does not resolve: %v", c.name, name, p.GetBuildError())
			}
			fieldPath := name
			if path != "" {
				fieldPath = path + "." + name
			}
			present := remaining.Has(name)
			value := get(remaining, name)
			remaining.Delete(name)
			if !present {
				if fb := defaultsLookup(w.defaults, fieldPath, name); fb != nil {
					value, present = fb, true
				}
			}
			if !present && property.Default != nil {
				value, present = oaDefault(property), true
			}
			if present {
				v, err := c.value(property, value, fieldPath, w)
				if err != nil {
					return nil, err
				}
				node.Set(name, v)
			} else if required[name] {
				*w.unresolved = append(*w.unresolved, "field '"+fieldPath+"' (required by "+c.name+") in fixtures."+w.key+" ("+w.source+")")
			}
		}
	}
	if remaining.Len() > 0 {
		at := path
		if at == "" {
			at = "<root>"
		}
		return nil, genError("unknown field(s) %s at '%s' in fixtures.%s - not declared by %s (%s)", javaSetString(remaining), at, w.key, c.name, w.source)
	}
	return node, nil
}

func (c *openAPIComponent) value(s *base.Schema, value any, path string, w *jsonWalk) (any, error) {
	if value == nil {
		if !oaNullable(s) {
			return nil, genError("'%s' is null but the schema is not nullable%s", path, w.suffix())
		}
		return nil, nil
	}
	typ := oaType(s)
	if typ == "" && s.Properties != nil {
		typ = "object"
	}
	if typ == "" {
		return nil, genError("'%s': unsupported or composed schema (allOf/oneOf/anyOf/multi-type) - not supported by the OpenAPI backend yet (%s)", path, w.source)
	}
	if len(s.Enum) > 0 && !oaEnumContains(s, typ, value) {
		allowed := make([]string, len(s.Enum))
		for i, e := range s.Enum {
			allowed[i] = valueOf(yamlNodeValue(e))
		}
		return nil, genError("'%s' = \"%s\" is not one of %s%s", path, valueOf(value), javaListString(allowed), w.suffix())
	}
	switch typ {
	case "object":
		m, ok := value.(*jsonx.Object)
		if !ok {
			return nil, w.wrongType(path, "a map", value)
		}
		if s.Properties != nil {
			return c.object(s, m, path, w)
		}
		var valueSchema *base.Schema
		if s.AdditionalProperties != nil && s.AdditionalProperties.IsA() {
			valueSchema = resolved(s.AdditionalProperties.A)
		}
		node := jsonx.NewObject()
		for _, k := range sortedKeys(m) {
			if valueSchema == nil {
				node.Set(k, plainNode(get(m, k)))
				continue
			}
			v, err := c.value(valueSchema, get(m, k), path+"."+k, w)
			if err != nil {
				return nil, err
			}
			node.Set(k, v)
		}
		return node, nil
	case "array":
		list, ok := value.([]any)
		if !ok {
			return nil, w.wrongType(path, "a list", value)
		}
		var items *base.Schema
		if s.Items != nil && s.Items.IsA() {
			items = resolved(s.Items.A)
		}
		out := make([]any, len(list))
		for i, e := range list {
			if items == nil {
				out[i] = plainNode(e)
				continue
			}
			v, err := c.value(items, e, fmt.Sprintf("%s[%d]", path, i), w)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case "string":
		return valueOf(value), nil
	case "integer":
		if !isNumber(value) {
			return nil, w.wrongType(path, "an integer", value)
		}
		return longNode(longValue(value)), nil
	case "number":
		if !isNumber(value) {
			return nil, w.wrongType(path, "a number", value)
		}
		if isIntegral(value) {
			return longNode(longValue(value)), nil
		}
		return doubleNode(doubleValue(value)), nil
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return nil, w.wrongType(path, "a boolean", value)
		}
		return b, nil
	case "null":
		return nil, nil
	}
	return nil, genError("'%s': unsupported schema type '%s'", path, typ)
}

func oaIsObject(s *base.Schema) bool { return oaType(s) == "object" || s.Properties != nil }

// oaType is the single effective type: "null" dropped from a type list, ""
// for none or several (composed).
func oaType(s *base.Schema) string {
	if len(s.Type) > 0 {
		set := map[string]bool{}
		for _, t := range s.Type {
			if t != "null" {
				set[t] = true
			}
		}
		if len(set) == 1 {
			for t := range set {
				return t
			}
		}
		if len(set) == 0 {
			return "null"
		}
		return ""
	}
	if s.Properties != nil {
		return "object"
	}
	return ""
}

func oaNullable(s *base.Schema) bool {
	if s.Nullable != nil && *s.Nullable {
		return true
	}
	for _, t := range s.Type {
		if t == "null" {
			return true
		}
	}
	return false
}

// oaDefault is a property's default, typed by its schema the way the
// OpenAPI parser casts it (numbers become Double, integers Integer/Long,
// strings their text).
func oaDefault(s *base.Schema) any {
	v := yamlNodeValue(s.Default)
	switch oaType(s) {
	case "string":
		if v != nil {
			return valueOf(v)
		}
	case "number":
		if isNumber(v) {
			return jsonx.DoubleNumber(doubleValue(v))
		}
		if str, ok := v.(string); ok {
			if f, err := strconv.ParseFloat(str, 64); err == nil {
				return jsonx.DoubleNumber(f)
			}
		}
	case "integer":
		if str, ok := v.(string); ok {
			if i, err := strconv.ParseInt(str, 10, 64); err == nil {
				return jsonx.LongNumber(i)
			}
		}
	case "boolean":
		if str, ok := v.(string); ok {
			return str == "true"
		}
	}
	return v
}

func oaEnumContains(s *base.Schema, typ string, value any) bool {
	for _, e := range s.Enum {
		ev := yamlNodeValue(e)
		switch typ {
		case "string":
			if sv, ok := value.(string); ok && sv == valueOf(ev) {
				return true
			}
		case "integer", "number":
			if isNumber(value) && isNumber(ev) && doubleValue(value) == doubleValue(ev) {
				return true
			}
		default:
			if deepEquals(ev, value) {
				return true
			}
		}
	}
	return false
}

// yamlNodeValue converts a YAML node of the OpenAPI document to the value
// model (YAML 1.2 core typing of the document parser).
func yamlNodeValue(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return yamlNodeValue(n.Content[0])
		}
		return nil
	case yaml.AliasNode:
		return yamlNodeValue(n.Alias)
	case yaml.MappingNode:
		out := jsonx.NewObject()
		for i := 0; i+1 < len(n.Content); i += 2 {
			out.Set(n.Content[i].Value, yamlNodeValue(n.Content[i+1]))
		}
		return out
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, c := range n.Content {
			out[i] = yamlNodeValue(c)
		}
		return out
	}
	switch n.ShortTag() {
	case "!!null":
		return nil
	case "!!bool":
		return strings.EqualFold(n.Value, "true")
	case "!!int":
		if i, err := strconv.ParseInt(n.Value, 0, 64); err == nil {
			if i >= -2147483648 && i <= 2147483647 {
				return jsonx.IntegerNumber(int32(i))
			}
			return jsonx.LongNumber(i)
		}
	case "!!float":
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return jsonx.DoubleNumber(f)
		}
	}
	return n.Value
}
