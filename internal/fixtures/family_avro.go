package fixtures

import (
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iskorotkov/avro/v2"

	"github.com/nimbusxr/axx/internal/avrojson"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// avroFamily is family: avro: JSON payload fixtures governed by a local
// .avsc. Fields resolve in schema declaration order: fixture override, then
// prototype, then factory defaults, then the schema default, then a hard
// error naming the field and fixture. The emitter owns Avro-JSON union
// encoding; authors write plain values. The conformance oracle is the Avro
// JSON decoder the Kafka pack publishes with.
type avroFamily struct{}

func (avroFamily) Name() string { return "avro" }

// avroDecode is the oracle; a variable so tests can run without it.
var avroDecode = avrojson.Decode

// checkRef refuses the Java-only schema reference forms.
func checkRef(ref string) error {
	switch {
	case strings.HasPrefix(ref, "classpath:"):
		return schemaError("schema ref '%s': classpath: refs name resources on a JVM classpath, which axx does not have - point at the file relative to the factory (or to fixtures.baseDir for conformance rules)", ref)
	case strings.HasPrefix(ref, "class:"):
		return schemaError("schema ref '%s': class: refs name generated JVM classes, which axx cannot load - point at the .proto file or a protoc descriptor set instead", ref)
	}
	return nil
}

func deref(s avro.Schema) avro.Schema {
	for s != nil && s.Type() == avro.Ref {
		s = s.(*avro.RefSchema).Schema()
	}
	return s
}

func fullName(s avro.Schema) string {
	if n, ok := deref(s).(avro.NamedSchema); ok {
		return n.FullName()
	}
	return string(s.Type())
}

// unionBranchName is the Avro-JSON union branch key: the full name for named
// types, the type name otherwise.
func unionBranchName(s avro.Schema) string {
	s = deref(s)
	switch s.Type() {
	case avro.Record, avro.Enum, avro.Fixed:
		return s.(avro.NamedSchema).FullName()
	}
	return string(s.Type())
}

func parseAvroSchema(baseDir, ref string, spec *Spec) (*avro.RecordSchema, error) {
	if err := checkRef(ref); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(ref, ".avsc") {
		return nil, schemaError("family: avro requires a .avsc schema, got %s", ref)
	}
	from := ""
	if spec != nil {
		from = " (referenced by " + spec.SourceName + ")"
	}
	data, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(ref)))
	if err != nil {
		return nil, schemaError("cannot read Avro schema %s%s: %v", ref, from, err)
	}
	s, err := avro.ParseBytesWithCache(data, "", &avro.SchemaCache{})
	if err != nil {
		return nil, schemaError("cannot read Avro schema %s%s: %v", ref, from, err)
	}
	rec, ok := deref(s).(*avro.RecordSchema)
	if !ok {
		return nil, schemaError("top-level Avro schema must be a record: %s", ref)
	}
	return rec, nil
}

func avroOracle(data []byte, schema *avro.RecordSchema, fixtureName string) error {
	if _, err := avroDecode(schema, data, avrojson.Options{}); err != nil {
		return genError("%s does not conform to schema %s: %v", fixtureName, schema.FullName(), err)
	}
	return nil
}

func (a avroFamily) Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	if strings.TrimSpace(spec.Schema) == "" {
		return nil, specError("%s: factory.schema is required for family %s", spec.SourceName, a.Name())
	}
	schema, err := parseAvroSchema(baseDir, spec.SchemaRef(), spec)
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
		r := &avroResolver{spec: spec, key: key, unresolved: &unresolved}
		node, err := r.record(schema, tree, "")
		if err != nil {
			return nil, err
		}
		if len(unresolved) == 0 {
			data := canonicalJSON(node)
			if err := avroOracle(data, schema, where(spec, key)); err != nil {
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

func (avroFamily) Validate(data []byte, baseDir, schemaRef, fixtureName string) error {
	schema, err := parseAvroSchema(baseDir, schemaRef, nil)
	if err != nil {
		return err
	}
	return avroOracle(data, schema, fixtureName)
}

// avroResolver walks the schema in declaration order, resolving values
// through the layer stack.
type avroResolver struct {
	spec       *Spec
	key        string
	unresolved *[]string
}

func (r *avroResolver) suffix() string {
	return " in fixtures." + r.key + " (" + r.spec.SourceName + ")"
}

func (r *avroResolver) record(schema *avro.RecordSchema, values *jsonx.Object, path string) (*jsonx.Object, error) {
	remaining := copyObjectShallow(values)
	node := jsonx.NewObject()
	for _, f := range schema.Fields() {
		fieldPath := f.Name()
		if path != "" {
			fieldPath = path + "." + f.Name()
		}
		present := remaining.Has(f.Name())
		value := get(remaining, f.Name())
		remaining.Delete(f.Name())
		if !present {
			if fb := defaultsLookup(r.spec.Defaults, fieldPath, f.Name()); fb != nil {
				value, present = fb, true
			}
		}
		switch {
		case present:
			v, err := r.value(f.Type(), value, fieldPath)
			if err != nil {
				return nil, err
			}
			node.Set(f.Name(), v)
		case f.HasDefault():
			v, err := r.schemaDefault(f)
			if err != nil {
				return nil, err
			}
			node.Set(f.Name(), v)
		default:
			*r.unresolved = append(*r.unresolved, "field '"+fieldPath+"' in fixtures."+r.key+" ("+r.spec.SourceName+")")
		}
	}
	if remaining.Len() > 0 {
		at := path
		if at == "" {
			at = "<root>"
		}
		return nil, genError("unknown field(s) %s at '%s' in fixtures.%s - not in schema record %s (%s)",
			javaSetString(remaining), at, r.key, schema.FullName(), r.spec.SourceName)
	}
	return node, nil
}

func (r *avroResolver) wrongType(path, expected string, value any) error {
	return genError("'%s' expects %s but got %s%s", path, expected, describe(value), r.suffix())
}

func (r *avroResolver) value(schema avro.Schema, value any, path string) (any, error) {
	schema = deref(schema)
	switch schema.Type() {
	case avro.Record:
		m, ok := value.(*jsonx.Object)
		if !ok {
			return nil, r.wrongType(path, "a map for record "+fullName(schema), value)
		}
		return r.record(schema.(*avro.RecordSchema), m, path)
	case avro.Union:
		return r.union(schema.(*avro.UnionSchema), value, path)
	case avro.Array:
		list, ok := value.([]any)
		if !ok {
			return nil, r.wrongType(path, "a list", value)
		}
		arr := make([]any, len(list))
		for i, e := range list {
			v, err := r.value(schema.(*avro.ArraySchema).Items(), e, path+"["+itoa(i)+"]")
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return arr, nil
	case avro.Map:
		m, ok := value.(*jsonx.Object)
		if !ok {
			return nil, r.wrongType(path, "a map", value)
		}
		node := jsonx.NewObject()
		for _, k := range sortedKeys(m) {
			v, err := r.value(schema.(*avro.MapSchema).Values(), get(m, k), path+"."+k)
			if err != nil {
				return nil, err
			}
			node.Set(k, v)
		}
		return node, nil
	case avro.Enum:
		symbol := valueOf(value)
		es := schema.(*avro.EnumSchema)
		for _, s := range es.Symbols() {
			if s == symbol {
				return symbol, nil
			}
		}
		return nil, genError("'%s' = \"%s\" is not a symbol of enum %s %s%s", path, symbol, es.FullName(), javaListString(es.Symbols()), r.suffix())
	case avro.String:
		return valueOf(value), nil
	case avro.Int, avro.Long:
		if !isNumber(value) {
			return nil, r.wrongType(path, "an integer", value)
		}
		return longNode(longValue(value)), nil
	case avro.Float, avro.Double:
		if !isNumber(value) {
			return nil, r.wrongType(path, "a number", value)
		}
		return doubleNode(doubleValue(value)), nil
	case avro.Boolean:
		b, ok := value.(bool)
		if !ok {
			return nil, r.wrongType(path, "a boolean", value)
		}
		return b, nil
	case avro.Bytes, avro.Fixed:
		return valueOf(value), nil
	case avro.Null:
		if value != nil {
			return nil, r.wrongType(path, "null", value)
		}
		return nil, nil
	}
	return nil, genError("'%s': unsupported Avro type %s%s", path, schema.Type(), r.suffix())
}

// union is Avro-JSON union encoding: null bare, otherwise {branch: value}.
func (r *avroResolver) union(u *avro.UnionSchema, value any, path string) (any, error) {
	if value == nil {
		for _, b := range u.Types() {
			if deref(b).Type() == avro.Null {
				return nil, nil
			}
		}
		return nil, genError("'%s' is null but union %s has no null branch%s", path, u.String(), r.suffix())
	}
	// An explicit single-key wrapper naming a branch disambiguates unions
	// with several record branches; adoption emits it, authors may write it.
	if m, ok := value.(*jsonx.Object); ok && m.Len() == 1 {
		soleKey := m.Keys()[0]
		for _, b := range u.Types() {
			if unionBranchName(b) == soleKey {
				v, err := r.value(b, get(m, soleKey), path)
				if err != nil {
					return nil, err
				}
				return newObject(unionBranchName(b), v), nil
			}
		}
	}
	for _, b := range u.Types() {
		if avroAccepts(deref(b), value) {
			v, err := r.value(b, value, path)
			if err != nil {
				return nil, err
			}
			return newObject(unionBranchName(b), v), nil
		}
	}
	return nil, genError("'%s' value %s matches no branch of union %s%s", path, describe(value), u.String(), r.suffix())
}

// avroAccepts is the first-accepting-branch test, in declaration order.
func avroAccepts(b avro.Schema, value any) bool {
	switch b.Type() {
	case avro.Record, avro.Map:
		_, ok := value.(*jsonx.Object)
		return ok
	case avro.Array:
		_, ok := value.([]any)
		return ok
	case avro.Enum:
		s, ok := value.(string)
		if !ok {
			return false
		}
		for _, sym := range b.(*avro.EnumSchema).Symbols() {
			if sym == s {
				return true
			}
		}
		return false
	case avro.String, avro.Bytes, avro.Fixed:
		_, ok := value.(string)
		return ok
	case avro.Int, avro.Long:
		return isIntegral(value)
	case avro.Float, avro.Double:
		return isNumber(value)
	case avro.Boolean:
		_, ok := value.(bool)
		return ok
	}
	return false
}

// schemaDefault renders a field's schema default in Avro-JSON form (a union
// default matches the first branch and is wrapped unless that branch is
// null).
func (r *avroResolver) schemaDefault(f *avro.Field) (any, error) {
	def := avroNative(f.Default())
	if def == nil {
		return nil, nil
	}
	target := deref(f.Type())
	isUnion := target.Type() == avro.Union
	if isUnion {
		target = deref(target.(*avro.UnionSchema).Types()[0])
	}
	v, err := r.value(target, def, f.Name()+"<default>")
	if err != nil {
		return nil, genError("cannot materialize schema default for field '%s': %s", f.Name(), errText(err))
	}
	if isUnion && target.Type() != avro.Null {
		return newObject(unionBranchName(target), v), nil
	}
	return v, nil
}

// avroNative converts a the avro library default value to the value model.
func avroNative(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string, bool:
		return x
	case int:
		return jsonx.IntegerNumber(int32(x))
	case int32:
		return jsonx.IntegerNumber(x)
	case int64:
		return jsonx.LongNumber(x)
	case float32:
		return jsonx.DoubleNumber(float64(x))
	case float64:
		return jsonx.DoubleNumber(x)
	case []byte:
		return latin1(x)
	case *big.Int:
		return jsonx.BigIntegerNumber(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = avroNative(e)
		}
		return out
	case map[string]any:
		ks := make([]string, 0, len(x))
		for k := range x {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		out := jsonx.NewObject()
		for _, k := range ks {
			out.Set(k, avroNative(x[k]))
		}
		return out
	}
	if arr, ok := fixedBytes(v); ok {
		return latin1(arr)
	}
	return valueOf(v)
}

// latin1 renders bytes the way Avro JSON writes bytes and fixed values.
func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// Adoption: oracle decode, authoring trees, field shape.
func (a avroFamily) Adoption(baseDir, schemaRef string) (Adoption, error) {
	schema, err := parseAvroSchema(baseDir, schemaRef, nil)
	if err != nil {
		return nil, err
	}
	return &avroAdoption{schema: schema}, nil
}

type avroAdoption struct {
	schema *avro.RecordSchema
}

func (a *avroAdoption) Decode(data []byte, where string) (any, error) {
	v, err := avroDecode(a.schema, data, avrojson.Options{})
	if err != nil {
		return nil, adoptError("%s does not decode against %s: %v", where, a.schema.FullName(), err)
	}
	return v, nil
}

func (a *avroAdoption) AuthoringTree(data []byte, where string) (*jsonx.Object, error) {
	node, err := parseJSON(data, where)
	if err != nil {
		return nil, err
	}
	return a.tree(a.schema, node, where)
}

func (a *avroAdoption) Shape() (*FieldShape, error) { return shapeRoot(avroChildren(a.schema)), nil }

func avroChildren(rec *avro.RecordSchema) []*FieldShape {
	var out []*FieldShape
	for _, f := range rec.Fields() {
		t := deref(f.Type())
		if r, ok := t.(*avro.RecordSchema); ok {
			out = append(out, shapeNested(f.Name(), avroChildren(r)))
		} else {
			out = append(out, shapeLeaf(f.Name(), t.Type() == avro.String))
		}
	}
	return out
}

func (a *avroAdoption) tree(rec *avro.RecordSchema, node any, where string) (*jsonx.Object, error) {
	obj, _ := node.(*jsonx.Object)
	out := jsonx.NewObject()
	for _, f := range rec.Fields() {
		if !has(obj, f.Name()) {
			return nil, adoptError("%s: field '%s' missing (not valid Avro-JSON)", where, f.Name())
		}
		v, err := a.value(f.Type(), get(obj, f.Name()), where)
		if err != nil {
			return nil, err
		}
		out.Set(f.Name(), v)
	}
	return out, nil
}

// value converts decoded Avro-JSON into the plain authoring form.
func (a *avroAdoption) value(schema avro.Schema, node any, where string) (any, error) {
	schema = deref(schema)
	switch schema.Type() {
	case avro.Record:
		return a.tree(schema.(*avro.RecordSchema), node, where)
	case avro.Union:
		if node == nil {
			return nil, nil
		}
		wrapper, _ := node.(*jsonx.Object)
		if wrapper == nil || wrapper.Len() == 0 {
			return nil, adoptError("%s: union value is not a branch wrapper", where)
		}
		branchName := wrapper.Keys()[0]
		nonNull := 0
		for _, b := range schema.(*avro.UnionSchema).Types() {
			if deref(b).Type() != avro.Null {
				nonNull++
			}
		}
		for _, b := range schema.(*avro.UnionSchema).Types() {
			if unionBranchName(b) == branchName {
				inner, err := a.value(b, get(wrapper, branchName), where)
				if err != nil {
					return nil, err
				}
				if nonNull > 1 {
					// Ambiguous union: keep the wrapper so regeneration picks
					// the exact branch the original chose.
					return newObject(branchName, inner), nil
				}
				return inner, nil
			}
		}
		return nil, adoptError("%s: union wrapper '%s' matches no branch of %s", where, branchName, schema.String())
	case avro.Array:
		list, _ := node.([]any)
		out := make([]any, 0, len(list))
		for _, e := range list {
			v, err := a.value(schema.(*avro.ArraySchema).Items(), e, where)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case avro.Map:
		m, _ := node.(*jsonx.Object)
		out := jsonx.NewObject()
		for _, k := range keys(m) {
			v, err := a.value(schema.(*avro.MapSchema).Values(), get(m, k), where)
			if err != nil {
				return nil, err
			}
			out.Set(k, v)
		}
		return out, nil
	case avro.String, avro.Enum:
		return jvalue.Stringify(toJSONX(node)), nil
	case avro.Int, avro.Long:
		return jsonx.LongNumber(nodeLong(node)), nil
	case avro.Float, avro.Double:
		return jsonx.DoubleNumber(nodeDouble(node)), nil
	case avro.Boolean:
		b, _ := node.(bool)
		return b, nil
	case avro.Null:
		return nil, nil
	}
	return nil, adoptError("%s: adopt does not support bytes/fixed fields yet (schema type %s)", where, strings.ToUpper(string(schema.Type())))
}
