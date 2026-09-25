package fixtures

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// jsonGoverning is what the json and yaml families need from a governing
// schema; the schema reference form picks the backend.
type jsonGoverning interface {
	resolve(tree, defaults *jsonx.Object, key, source string, unresolved *[]string) (*jsonx.Object, error)
	validate(data []byte, fixtureName string) error
	shape() (*FieldShape, error)
	authoringTree(node any, where string) (*jsonx.Object, error)
}

const componentPrefix = "#/components/schemas/"

func loadGoverning(baseDir, ref string) (jsonGoverning, error) {
	if err := checkRef(ref); err != nil {
		return nil, err
	}
	if strings.Contains(ref, componentPrefix) {
		return loadOpenAPIComponent(baseDir, ref)
	}
	return loadJSONSchemaDoc(baseDir, ref)
}

// jsonSchemaDoc is one standalone JSON Schema document (JsonSchemaDocument):
// the whole file governs, the draft is detected from $schema (2020-12 when
// absent) and the oracle is a full JSON Schema validator. The walker mirrors
// the OpenAPI one, with two deviations backed by the oracle: extra fields are
// allowed when an object declares additionalProperties, and composed
// subtrees (oneOf/anyOf) pass through verbatim.
type jsonSchemaDoc struct {
	document *jsonx.Object
	oracle   *jsonschema.Schema
	name     string
}

func loadJSONSchemaDoc(baseDir, ref string) (*jsonSchemaDoc, error) {
	if strings.Contains(ref, "#") {
		return nil, schemaError("json schema ref '%s': fragments into standalone schema files are not supported - point at a whole schema file, or at an OpenAPI component via <spec>.yaml%s<Name>", ref, componentPrefix)
	}
	path := filepath.Join(baseDir, filepath.FromSlash(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, schemaError("cannot read JSON Schema %s: %v", ref, err)
	}
	var root any
	if strings.HasSuffix(strings.ToLower(ref), ".json") {
		root, err = jvalue.ParseJackson(string(data))
	} else {
		root, err = jyaml.Unmarshal(data)
	}
	if err != nil {
		return nil, schemaError("cannot read JSON Schema %s: %v", ref, err)
	}
	doc, ok := root.(*jsonx.Object)
	if !ok {
		return nil, schemaError("JSON Schema %s must be a top-level object", ref)
	}
	name := ref[strings.LastIndexByte(ref, '/')+1:]
	if t, ok := get(doc, "title").(string); ok {
		name = t
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	abs, _ := filepath.Abs(path)
	u := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	if err := c.AddResource(u, toSchemaValue(doc)); err != nil {
		return nil, schemaError("cannot compile JSON Schema %s: %v", ref, err)
	}
	oracle, err := c.Compile(u)
	if err != nil {
		return nil, schemaError("cannot compile JSON Schema %s: %v", ref, err)
	}
	return &jsonSchemaDoc{document: doc, oracle: oracle, name: name}, nil
}

// toSchemaValue converts the value model to what the jsonschema package
// validates (json.Number numbers, plain maps).
func toSchemaValue(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := make(map[string]any, x.Len())
		for _, k := range x.Keys() {
			out[k] = toSchemaValue(get(x, k))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = toSchemaValue(e)
		}
		return out
	case jsonx.Number:
		return json.Number(x.String())
	case []byte:
		return base64Std(x)
	}
	return v
}

// validationMessages flattens a jsonschema validation error into one line
// per leaf failure: "<instance location>: <message>".
func validationMessages(err error) []string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []string{err.Error()}
	}
	var out []string
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "$"
			for _, seg := range e.InstanceLocation {
				loc += "/" + seg
			}
			out = append(out, loc+": "+e.ErrorKind.LocalizedString(printer))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}

func (d *jsonSchemaDoc) validate(data []byte, fixtureName string) error {
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(data)))
	if err != nil {
		return genError("%s is not valid JSON: %v", fixtureName, err)
	}
	if err := d.oracle.Validate(inst); err != nil {
		return genError("%s does not conform to %s:\n  %s", fixtureName, d.name, strings.Join(validationMessages(err), "\n  "))
	}
	return nil
}

func (d *jsonSchemaDoc) resolve(tree, defaults *jsonx.Object, key, source string, unresolved *[]string) (*jsonx.Object, error) {
	root, err := d.effective(d.document)
	if err != nil {
		return nil, err
	}
	if !root.Has("properties") {
		return nil, schemaError("%s must describe an object with properties to govern fixtures", d.name)
	}
	w := &jsonWalk{defaults: defaults, key: key, source: source, unresolved: unresolved}
	return d.object(root, tree, "", w)
}

type jsonWalk struct {
	defaults   *jsonx.Object
	key        string
	source     string
	unresolved *[]string
}

func (w *jsonWalk) suffix() string { return " in fixtures." + w.key + " (" + w.source + ")" }

func (w *jsonWalk) wrongType(path, expected string, value any) error {
	return genError("'%s' expects %s but got %s%s", path, expected, describe(value), w.suffix())
}

func (d *jsonSchemaDoc) shape() (*FieldShape, error) {
	root, err := d.effective(d.document)
	if err != nil {
		return nil, err
	}
	children, err := d.children(root)
	if err != nil {
		return nil, err
	}
	return shapeRoot(children), nil
}

func (d *jsonSchemaDoc) children(schema *jsonx.Object) ([]*FieldShape, error) {
	props, _ := get(schema, "properties").(*jsonx.Object)
	var out []*FieldShape
	for _, name := range keys(props) {
		child, err := d.effective(get(props, name))
		if err != nil {
			return nil, err
		}
		if child.Has("properties") {
			grand, err := d.children(child)
			if err != nil {
				return nil, err
			}
			out = append(out, shapeNested(name, grand))
		} else {
			out = append(out, shapeLeaf(name, schemaType(child) == "string"))
		}
	}
	return out, nil
}

func (d *jsonSchemaDoc) authoringTree(node any, where string) (*jsonx.Object, error) {
	root, err := d.effective(d.document)
	if err != nil {
		return nil, err
	}
	return d.readObject(root, node, where)
}

func (d *jsonSchemaDoc) readObject(schema *jsonx.Object, node any, where string) (*jsonx.Object, error) {
	obj, ok := node.(*jsonx.Object)
	if !ok {
		return nil, adoptError("%s: expected a JSON object", where)
	}
	out := jsonx.NewObject()
	props, _ := get(schema, "properties").(*jsonx.Object)
	for _, name := range keys(props) {
		if obj.Has(name) {
			child, err := d.effective(get(props, name))
			if err != nil {
				return nil, err
			}
			v, err := d.readValue(child, get(obj, name), where)
			if err != nil {
				return nil, err
			}
			out.Set(name, v)
		}
	}
	var extras []string
	for _, name := range obj.Keys() {
		if !has(props, name) {
			extras = append(extras, name)
		}
	}
	if len(extras) > 0 {
		if !extrasAllowed(schema) {
			return nil, adoptError("%s: field(s) %s are not declared by %s", where, javaListString(extras), d.name)
		}
		sortStrings(extras)
		for _, name := range extras {
			out.Set(name, plainJSON(get(obj, name)))
		}
	}
	return out, nil
}

func (d *jsonSchemaDoc) readValue(schema *jsonx.Object, node any, where string) (any, error) {
	switch x := node.(type) {
	case nil:
		return nil, nil
	case *jsonx.Object:
		if schema.Has("properties") {
			return d.readObject(schema, x, where)
		}
	case []any:
		items := jsonx.NewObject()
		if io, ok := get(schema, "items").(*jsonx.Object); ok {
			var err error
			if items, err = d.effective(io); err != nil {
				return nil, err
			}
		}
		out := make([]any, 0, len(x))
		for _, e := range x {
			v, err := d.readValue(items, e, where)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	return plainJSON(node), nil
}

func (d *jsonSchemaDoc) object(schema, values *jsonx.Object, path string, w *jsonWalk) (*jsonx.Object, error) {
	remaining := copyObjectShallow(values)
	node := jsonx.NewObject()
	props, _ := get(schema, "properties").(*jsonx.Object)
	required := map[string]bool{}
	if req, ok := get(schema, "required").([]any); ok {
		for _, r := range req {
			required[jvalue.Stringify(r)] = true
		}
	}
	for _, name := range keys(props) {
		property, err := d.effective(get(props, name))
		if err != nil {
			return nil, err
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
		if !present && property.Has("default") {
			node.Set(name, copyValue(get(property, "default")))
			continue
		}
		if present {
			v, err := d.value(property, value, fieldPath, w)
			if err != nil {
				return nil, err
			}
			node.Set(name, v)
		} else if required[name] {
			*w.unresolved = append(*w.unresolved, "field '"+fieldPath+"' (required by "+d.name+") in fixtures."+w.key+" ("+w.source+")")
		}
	}
	if remaining.Len() > 0 {
		if !extrasAllowed(schema) {
			at := path
			if at == "" {
				at = "<root>"
			}
			return nil, genError("unknown field(s) %s at '%s' in fixtures.%s - not declared by %s and the schema does not allow additional properties (%s)",
				javaSetString(remaining), at, w.key, d.name, w.source)
		}
		var extraSchema *jsonx.Object
		if ap, ok := get(schema, "additionalProperties").(*jsonx.Object); ok {
			var err error
			if extraSchema, err = d.effective(ap); err != nil {
				return nil, err
			}
		}
		for _, k := range sortedKeys(remaining) {
			extraPath := k
			if path != "" {
				extraPath = path + "." + k
			}
			if extraSchema == nil {
				node.Set(k, plainNode(get(remaining, k)))
				continue
			}
			v, err := d.value(extraSchema, get(remaining, k), extraPath, w)
			if err != nil {
				return nil, err
			}
			node.Set(k, v)
		}
	}
	return node, nil
}

func (d *jsonSchemaDoc) value(schema *jsonx.Object, value any, path string, w *jsonWalk) (any, error) {
	if value == nil {
		if !isNullable(schema) {
			return nil, genError("'%s' is null but the schema does not allow null%s", path, w.suffix())
		}
		return nil, nil
	}
	if err := guardEnum(schema, value, path, w); err != nil {
		return nil, err
	}
	typ := schemaType(schema)
	if typ == "" && schema.Has("properties") {
		typ = "object"
	}
	if typ == "" {
		// Composed schema (oneOf/anyOf/multi-type): the authored value passes
		// through; the oracle validates the final bytes.
		return plainNode(value), nil
	}
	switch typ {
	case "object":
		m, ok := value.(*jsonx.Object)
		if !ok {
			return nil, w.wrongType(path, "a map", value)
		}
		if schema.Has("properties") {
			return d.object(schema, m, path, w)
		}
		var valueSchema *jsonx.Object
		if ap, ok := get(schema, "additionalProperties").(*jsonx.Object); ok {
			var err error
			if valueSchema, err = d.effective(ap); err != nil {
				return nil, err
			}
		}
		node := jsonx.NewObject()
		for _, k := range sortedKeys(m) {
			if valueSchema == nil {
				node.Set(k, plainNode(get(m, k)))
				continue
			}
			v, err := d.value(valueSchema, get(m, k), path+"."+k, w)
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
		var items *jsonx.Object
		if io, ok := get(schema, "items").(*jsonx.Object); ok {
			var err error
			if items, err = d.effective(io); err != nil {
				return nil, err
			}
		}
		out := make([]any, len(list))
		for i, e := range list {
			if items == nil {
				out[i] = plainNode(e)
				continue
			}
			v, err := d.value(items, e, fmt.Sprintf("%s[%d]", path, i), w)
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

func guardEnum(schema *jsonx.Object, value any, path string, w *jsonWalk) error {
	switch value.(type) {
	case *jsonx.Object, []any:
		return nil // structured enum/const values are rare; the oracle covers them
	}
	var allowed []string
	if e, ok := get(schema, "enum").([]any); ok {
		for _, v := range e {
			allowed = append(allowed, jvalue.Stringify(v))
		}
	} else if schema.Has("const") {
		allowed = append(allowed, jvalue.Stringify(get(schema, "const")))
	} else {
		return nil
	}
	s := valueOf(value)
	for _, a := range allowed {
		if a == s {
			return nil
		}
	}
	return genError("'%s' = \"%s\" is not one of %s%s", path, s, javaListString(allowed), w.suffix())
}

// effective is the merged view of a schema node: internal $refs resolved
// against the document root and allOf members flattened (properties merged
// in declaration order, required unioned, other keywords last-wins).
func (d *jsonSchemaDoc) effective(node any) (*jsonx.Object, error) {
	out := jsonx.NewObject()
	if err := d.mergeInto(out, node, map[string]bool{}); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *jsonSchemaDoc) mergeInto(out *jsonx.Object, node any, seen map[string]bool) error {
	n, ok := node.(*jsonx.Object)
	if !ok {
		return nil
	}
	if ref, ok := get(n, "$ref").(string); ok {
		if !strings.HasPrefix(ref, "#") {
			return schemaError("%s: external $ref '%s' is not supported - inline the schema", d.name, ref)
		}
		if seen[ref] {
			return schemaError("%s: $ref cycle through '%s'", d.name, ref)
		}
		seen[ref] = true
		target := jsonPointer(d.document, ref[1:])
		if target == nil && !jsonPointerExists(d.document, ref[1:]) {
			return schemaError("%s: $ref '%s' resolves to nothing", d.name, ref)
		}
		if err := d.mergeInto(out, target, seen); err != nil {
			return err
		}
	}
	if all, ok := get(n, "allOf").([]any); ok {
		for _, member := range all {
			if err := d.mergeInto(out, member, seen); err != nil {
				return err
			}
		}
	}
	for _, name := range n.Keys() {
		if name == "$ref" || name == "allOf" {
			continue
		}
		value := get(n, name)
		switch vo := value.(type) {
		case *jsonx.Object:
			if name == "properties" {
				merged, ok := get(out, "properties").(*jsonx.Object)
				if !ok {
					merged = jsonx.NewObject()
					out.Set("properties", merged)
				}
				for _, k := range vo.Keys() {
					merged.Set(k, get(vo, k))
				}
				continue
			}
		case []any:
			if name == "required" {
				var union []any
				seenReq := map[string]bool{}
				existing, _ := get(out, "required").([]any)
				for _, r := range append(append([]any(nil), existing...), vo...) {
					s := jvalue.Stringify(r)
					if !seenReq[s] {
						seenReq[s] = true
						union = append(union, s)
					}
				}
				out.Set("required", union)
				continue
			}
		}
		out.Set(name, value)
	}
	return nil
}

func jsonPointerExists(root any, pointer string) bool {
	if pointer == "" {
		return true
	}
	parent := pointer[:strings.LastIndexByte(pointer, '/')+1]
	last := pointer[strings.LastIndexByte(pointer, '/')+1:]
	p, ok := jsonPointer(root, strings.TrimSuffix(parent, "/")).(*jsonx.Object)
	return ok && p.Has(strings.ReplaceAll(strings.ReplaceAll(last, "~1", "/"), "~0", "~"))
}

func extrasAllowed(schema *jsonx.Object) bool {
	if !schema.Has("additionalProperties") {
		return false
	}
	b, isBool := get(schema, "additionalProperties").(bool)
	return !isBool || b
}

// schemaType is the effective single type: a type list minus "null" with one
// member is that member, empty is "null", several is "" (composed).
func schemaType(schema *jsonx.Object) string {
	switch t := get(schema, "type").(type) {
	case string:
		return t
	case []any:
		set := map[string]bool{}
		for _, e := range t {
			set[jvalue.Stringify(e)] = true
		}
		delete(set, "null")
		if len(set) == 1 {
			for k := range set {
				return k
			}
		}
		if len(set) == 0 {
			return "null"
		}
		return ""
	}
	if schema.Has("properties") {
		return "object"
	}
	return ""
}

func isNullable(schema *jsonx.Object) bool {
	switch t := get(schema, "type").(type) {
	case string:
		return t == "null"
	case []any:
		for _, e := range t {
			if jvalue.Stringify(e) == "null" {
				return true
			}
		}
		return false
	}
	return !schema.Has("type")
}
