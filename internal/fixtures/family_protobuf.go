package fixtures

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// protobufFamily is family: protobuf: record-shaped fixtures emitted as
// canonical proto-JSON, governed by a message descriptor. factory.schema is
// <file>.proto#pkg.Message (compiled in-process) or <set>.desc#pkg.Message (a
// FileDescriptorSet from protoc --descriptor_set_out). The oracle is the
// protobuf library's own proto-JSON parser. proto3 semantics apply: nothing
// is required and unresolved fields are omitted. Authors may write the proto
// field name (order_id) or the JSON name (orderId); output uses the JSON
// name, int64-class values emit as strings, enums as names, bytes as base64,
// and well-known types pass through verbatim for the oracle to check.
type protobufFamily struct{}

func (protobufFamily) Name() string { return "protobuf" }

func loadMessage(baseDir, ref string) (protoreflect.MessageDescriptor, error) {
	if err := checkRef(ref); err != nil {
		return nil, err
	}
	hash := strings.IndexByte(ref, '#')
	if hash < 0 {
		return nil, schemaError("protobuf schema ref must be <file>.proto#<fully.qualified.Message> or <set>.desc#<fully.qualified.Message> (a FileDescriptorSet from protoc --descriptor_set_out), got '%s'", ref)
	}
	file, name := ref[:hash], ref[hash+1:]
	var desc protoreflect.Descriptor
	var err error
	if strings.HasSuffix(file, ".proto") {
		desc, err = compileProto(baseDir, file, name)
	} else {
		desc, err = descriptorSet(baseDir, file, name)
	}
	if err != nil {
		return nil, err
	}
	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok || desc == nil {
		return nil, schemaError("%s has no message '%s' (use the fully-qualified name)", file, name)
	}
	return md, nil
}

func compileProto(baseDir, file, name string) (protoreflect.Descriptor, error) {
	path := filepath.Join(baseDir, filepath.FromSlash(file))
	source, importPaths := filepath.Base(path), []string{filepath.Dir(path), baseDir}
	if _, err := os.Stat(path); err != nil {
		// Not on disk: a standard import such as google/protobuf/type.proto.
		source, importPaths = file, []string{baseDir}
	}
	c := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: importPaths})}
	files, err := c.Compile(context.Background(), source)
	if err != nil {
		return nil, schemaError("cannot compile %s: %v", file, err)
	}
	d, err := files.AsResolver().FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, schemaError("%s has no message '%s' (use the fully-qualified name)", file, name)
	}
	return d, nil
}

func descriptorSet(baseDir, file, name string) (protoreflect.Descriptor, error) {
	data, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(file)))
	if err != nil {
		return nil, schemaError("cannot read descriptor set %s: %v", file, err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, schemaError("cannot read descriptor set %s: %v", file, err)
	}
	protos := map[string]*descriptorpb.FileDescriptorProto{}
	for _, f := range set.GetFile() {
		protos[f.GetName()] = f
	}
	reg := new(protoregistry.Files)
	var build func(n string) error
	build = func(n string) error {
		if _, err := reg.FindFileByPath(n); err == nil {
			return nil
		}
		fp, ok := protos[n]
		if !ok {
			if _, err := protoregistry.GlobalFiles.FindFileByPath(n); err == nil {
				return nil // a well-known import the set omitted
			}
			return schemaError("%s: dependency '%s' missing - regenerate with --include_imports", file, n)
		}
		for _, dep := range fp.GetDependency() {
			if err := build(dep); err != nil {
				return err
			}
		}
		fd, err := protodesc.NewFile(fp, resolverChain{reg, protoregistry.GlobalFiles})
		if err != nil {
			return schemaError("%s is not a valid descriptor set: %v", file, err)
		}
		return reg.RegisterFile(fd)
	}
	for _, f := range set.GetFile() {
		if err := build(f.GetName()); err != nil {
			return nil, err
		}
	}
	d, err := reg.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, schemaError("%s has no message '%s' (use the fully-qualified name)", file, name)
	}
	return d, nil
}

// resolverChain resolves from the set first, then the well-known types.
type resolverChain []*protoregistry.Files

func (r resolverChain) FindFileByPath(p string) (protoreflect.FileDescriptor, error) {
	for _, f := range r {
		if d, err := f.FindFileByPath(p); err == nil {
			return d, nil
		}
	}
	return nil, protoregistry.NotFound
}

func (r resolverChain) FindDescriptorByName(n protoreflect.FullName) (protoreflect.Descriptor, error) {
	for _, f := range r {
		if d, err := f.FindDescriptorByName(n); err == nil {
			return d, nil
		}
	}
	return nil, protoregistry.NotFound
}

func protoOracle(md protoreflect.MessageDescriptor, data []byte, fixtureName string) (proto.Message, error) {
	msg := dynamicpb.NewMessage(md)
	if err := protojson.Unmarshal(data, msg); err != nil {
		return nil, genError("%s does not conform to %s: %v", fixtureName, md.FullName(), err)
	}
	return msg, nil
}

func (f protobufFamily) Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	if err := requireSchema(spec, f.Name()); err != nil {
		return nil, err
	}
	md, err := loadMessage(baseDir, spec.SchemaRef())
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]byte{}
	for _, key := range spec.Fixtures.SortedKeys() {
		tree, err := resolveFixture(spec, key, ctx)
		if err != nil {
			return nil, err
		}
		w := &protoWalk{defaults: spec.Defaults, key: key, source: spec.SourceName}
		node, err := w.errText(md, tree, "")
		if err != nil {
			return nil, err
		}
		data := canonicalJSON(node)
		if _, err := protoOracle(md, data, where(spec, key)); err != nil {
			return nil, err
		}
		out[key] = map[string][]byte{key + ".json": data}
	}
	return out, nil
}

func (protobufFamily) Validate(data []byte, baseDir, schemaRef, fixtureName string) error {
	md, err := loadMessage(baseDir, schemaRef)
	if err != nil {
		return err
	}
	_, err = protoOracle(md, data, fixtureName)
	return err
}

type protoWalk struct {
	defaults *jsonx.Object
	key      string
	source   string
}

func (w *protoWalk) at(path string) string {
	if path == "" {
		return "<root>"
	}
	return path
}

func (w *protoWalk) wrongType(path, expected string, value any) error {
	return genError("'%s' expects %s but got %s in fixtures.%s (%s)", path, expected, describe(value), w.key, w.source)
}

func isWellKnown(md protoreflect.MessageDescriptor) bool {
	return strings.HasPrefix(string(md.FullName()), "google.protobuf.")
}

func (w *protoWalk) errText(md protoreflect.MessageDescriptor, values *jsonx.Object, path string) (*jsonx.Object, error) {
	remaining := copyObjectShallow(values)
	node := jsonx.NewObject()
	oneofUsed := map[string]bool{}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name, jsonName := string(fd.Name()), fd.JSONName()
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}
		present := remaining.Has(name)
		value := get(remaining, name)
		if present {
			remaining.Delete(name)
			if name != jsonName && remaining.Has(jsonName) {
				return nil, genError("both '%s' and '%s' are set at '%s' in fixtures.%s - they are the same protobuf field (%s)", name, jsonName, w.at(path), w.key, w.source)
			}
		}
		if !present && remaining.Has(jsonName) {
			present, value = true, get(remaining, jsonName)
			remaining.Delete(jsonName)
		}
		if !present {
			if fb := defaultsLookup(w.defaults, fieldPath, name); fb != nil {
				value, present = fb, true
			}
		}
		if !present {
			continue // proto3: nothing is required; the wire default applies
		}
		if oo := fd.ContainingOneof(); oo != nil {
			if oneofUsed[string(oo.Name())] {
				return nil, genError("oneof '%s' has more than one member set at '%s' in fixtures.%s (%s)", oo.Name(), w.at(path), w.key, w.source)
			}
			oneofUsed[string(oo.Name())] = true
		}
		v, err := w.value(fd, value, fieldPath)
		if err != nil {
			return nil, err
		}
		node.Set(jsonName, v)
	}
	if remaining.Len() > 0 {
		return nil, genError("unknown field(s) %s at '%s' in fixtures.%s - not declared by %s (%s)", javaSetString(remaining), w.at(path), w.key, md.FullName(), w.source)
	}
	return node, nil
}

func (w *protoWalk) value(fd protoreflect.FieldDescriptor, value any, path string) (any, error) {
	if fd.IsMap() {
		m, ok := value.(*jsonx.Object)
		if !ok {
			return nil, w.wrongType(path, "a map", value)
		}
		node := jsonx.NewObject()
		for _, k := range sortedKeys(m) {
			v, err := w.single(fd.MapValue(), get(m, k), path+"."+k)
			if err != nil {
				return nil, err
			}
			node.Set(k, v)
		}
		return node, nil
	}
	if fd.IsList() {
		list, ok := value.([]any)
		if !ok {
			return nil, w.wrongType(path, "a list", value)
		}
		out := make([]any, len(list))
		for i, e := range list {
			v, err := w.single(fd, e, path+"["+itoa(i)+"]")
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	return w.single(fd, value, path)
}

func (w *protoWalk) single(fd protoreflect.FieldDescriptor, value any, path string) (any, error) {
	if value == nil {
		return nil, genError("'%s' is null - proto-JSON has no null fields; omit the field instead (fixtures.%s, %s)", path, w.key, w.source)
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if isWellKnown(fd.Message()) {
			// Well-known types have special JSON forms (Timestamp = RFC 3339
			// string, wrappers = plain values): pass through, the oracle checks.
			return plainNode(value), nil
		}
		m, ok := value.(*jsonx.Object)
		if !ok {
			return nil, w.wrongType(path, "a map", value)
		}
		return w.errText(fd.Message(), m, path)
	case protoreflect.EnumKind:
		name := valueOf(value)
		if fd.Enum().Values().ByName(protoreflect.Name(name)) == nil {
			vals := fd.Enum().Values()
			names := make([]string, vals.Len())
			for i := range names {
				names[i] = string(vals.Get(i).Name())
			}
			return nil, genError("'%s' = \"%s\" is not a value of %s %s in fixtures.%s (%s)", path, name, fd.Enum().FullName(), javaListString(names), w.key, w.source)
		}
		return name, nil
	case protoreflect.StringKind, protoreflect.BytesKind:
		return valueOf(value), nil // bytes are authored as base64
	case protoreflect.BoolKind:
		b, ok := value.(bool)
		if !ok {
			return nil, w.wrongType(path, "a boolean", value)
		}
		return b, nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind, protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		if !isNumber(value) {
			return nil, w.wrongType(path, "an integer", value)
		}
		return longNode(longValue(value)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		// Canonical proto-JSON writes 64-bit integers as strings.
		if isNumber(value) {
			return itoa64(longValue(value)), nil
		}
		return valueOf(value), nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		if !isNumber(value) {
			return nil, w.wrongType(path, "a number", value)
		}
		return doubleNode(doubleValue(value)), nil
	}
	return nil, genError("'%s': unsupported protobuf kind %s", path, fd.Kind())
}

// LintFilePatterns keeps the JSON default.
func (protobufFamily) LintFilePatterns(spec *Spec) []string {
	var out []string
	for _, dir := range spec.OutputDirs() {
		if dir == "" {
			out = append(out, "*.json")
		} else {
			out = append(out, dir+"/*.json")
		}
	}
	return out
}

// LintJSONPath rebases identity paths authored with proto field names onto
// the emitted JSON names, segment by segment (custom json_name options are
// not visible here: author such paths in JSON-name form).
func (protobufFamily) LintJSONPath(_ *Spec, identityPath string) string {
	segs := strings.Split(identityPath, ".")
	for i, s := range segs {
		segs[i] = defaultJSONName(s)
	}
	return strings.Join(segs, ".")
}

func defaultJSONName(name string) string {
	var b strings.Builder
	up := false
	for _, c := range name {
		if c == '_' {
			up = true
			continue
		}
		if up {
			b.WriteString(strings.ToUpper(string(c)))
		} else {
			b.WriteRune(c)
		}
		up = false
	}
	return b.String()
}

func (protobufFamily) Adoption(baseDir, schemaRef string) (Adoption, error) {
	md, err := loadMessage(baseDir, schemaRef)
	if err != nil {
		return nil, err
	}
	return &protoAdoption{md: md}, nil
}

type protoAdoption struct {
	md protoreflect.MessageDescriptor
}

func (a *protoAdoption) Decode(data []byte, where string) (any, error) {
	msg, err := protoOracle(a.md, data, where)
	if err != nil {
		return nil, err
	}
	return protoValue{msg}, nil
}

// protoValue compares decoded messages with proto.Equal.
type protoValue struct{ m proto.Message }

func (p protoValue) JavaString() string { return protojson.Format(p.m) }

func (a *protoAdoption) AuthoringTree(data []byte, where string) (*jsonx.Object, error) {
	node, err := parseJSON(data, where)
	if err != nil {
		return nil, err
	}
	return readProtoMessage(a.md, node, where)
}

func (a *protoAdoption) Shape() (*FieldShape, error) { return shapeRoot(protoChildren(a.md)), nil }

func nestable(fd protoreflect.FieldDescriptor) bool {
	return (fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind) && !fd.IsList() && !fd.IsMap() && !isWellKnown(fd.Message())
}

func protoChildren(md protoreflect.MessageDescriptor) []*FieldShape {
	var out []*FieldShape
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if nestable(fd) {
			out = append(out, shapeNested(fd.JSONName(), protoChildren(fd.Message())))
		} else {
			out = append(out, shapeLeaf(fd.JSONName(), fd.Kind() == protoreflect.StringKind && !fd.IsList()))
		}
	}
	return out
}

func readProtoMessage(md protoreflect.MessageDescriptor, node any, where string) (*jsonx.Object, error) {
	obj, ok := node.(*jsonx.Object)
	if !ok {
		return nil, adoptError("%s: expected a JSON object", where)
	}
	out := jsonx.NewObject()
	known := map[string]bool{}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		known[string(fd.Name())] = true
		known[fd.JSONName()] = true
	}
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		var child any
		switch {
		case obj.Has(fd.JSONName()):
			child = get(obj, fd.JSONName())
		case obj.Has(string(fd.Name())):
			child = get(obj, string(fd.Name()))
		default:
			continue
		}
		if child == nil && !obj.Has(fd.JSONName()) && !obj.Has(string(fd.Name())) {
			continue
		}
		if m, isMap := child.(*jsonx.Object); isMap && nestable(fd) {
			v, err := readProtoMessage(fd.Message(), m, where)
			if err != nil {
				return nil, err
			}
			out.Set(fd.JSONName(), v)
		} else {
			out.Set(fd.JSONName(), plainJSON(child))
		}
	}
	for _, name := range obj.Keys() {
		if !known[name] {
			return nil, adoptError("%s: field '%s' is not declared by %s", where, name, md.FullName())
		}
	}
	return out, nil
}

var errNoProto = errors.New("not a protobuf message")

func protoEquals(a, b any) (bool, error) {
	pa, oka := a.(protoValue)
	pb, okb := b.(protoValue)
	if !oka || !okb {
		return false, errNoProto
	}
	return proto.Equal(pa.m, pb.m), nil
}
