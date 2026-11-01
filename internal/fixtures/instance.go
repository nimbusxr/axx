package fixtures

import (
	"encoding/json"
	"errors"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Instances: one fixture's values produced for an instance a caller names
// (a record of a stream, say) outside an expansion, as `axx fixtures check`
// resolves them. The public package github.com/nimbusxr/axx/fixtures wraps
// them for packs.

// Name is the factory's name: its path relative to the resource root
// without .factory.yaml (orders/orders).
func (s *Spec) Name() string { return strings.TrimSuffix(s.SourceName, FactorySuffix) }

// Fixture finds the fixture a reference names: "<factory>#<fixture>", the
// factory by its Name, or by its base name when no other factory has it.
func (g *Generator) Fixture(ref string) (*Spec, string, error) {
	name, key, ok := strings.Cut(ref, "#")
	name = strings.TrimSuffix(strings.TrimSpace(name), FactorySuffix)
	if !ok || name == "" || key == "" {
		return nil, "", refError("fixture reference %q is not <factory>#<fixture>", ref)
	}
	var found, all []string
	var spec *Spec
	for _, s := range g.specs {
		all = append(all, s.Name())
		if s.Name() == name || !strings.Contains(name, "/") && s.BaseName == name {
			found = append(found, s.Name())
			spec = s
		}
	}
	sortStrings(all)
	switch {
	case len(found) == 0:
		return nil, "", refError("fixture reference %q names no factory (factories: %s)", ref, javaListString(all))
	case len(found) > 1:
		sortStrings(found)
		return nil, "", refError("fixture reference %q names %d factories: %s - name one by its path", ref, len(found), javaListString(found))
	}
	if spec.Fixtures.Get(key) == nil {
		msg := "fixture reference %q names no fixture of " + spec.SourceName
		if likely := likelyDocuments(key, spec.Fixtures.SortedKeys(), 3); len(likely) > 0 {
			msg += "; did you mean " + strings.Join(likely, ", ") + "?"
		}
		return nil, "", refError(msg, ref)
	}
	return spec, key, nil
}

// InstanceInput is what one instance of a fixture is produced from.
type InstanceInput struct {
	// Scope is the instance. Expression functions receive it, and an
	// identity left without a value derives from its Material for the
	// identity's path.
	Scope InstanceScope
	// Overrides set values at dotted paths of the data before anything
	// resolves, so they may be $refs and expressions too; parents are set
	// before their children. A value is a plain Go value or one of the
	// model (see ModelValue).
	Overrides map[string]any
	// MetadataOverrides set values at dotted paths of the metadata.
	MetadataOverrides map[string]any
	// Documents resolves the $refs to other documents: document is the
	// target's path relative to the resource root, and the tree returned is
	// that document as the caller produced it for the same instance; nil
	// when it has none.
	Documents func(document string) (*jsonx.Object, error)
}

// Instance is one instance of a fixture: its data and metadata, evaluated,
// and the value of each identity by path.
type Instance struct {
	Data       *jsonx.Object
	Metadata   *jsonx.Object
	Identities map[string]string
}

// Instance produces one instance of a fixture of spec. Its data is the
// fixture's values (the prototype for its directory deep-merged under its
// data: not defaults: or the schema, which its family applies), its metadata
// the factory's for its directory deep-merged under its own. The data gets
// its overrides, its $refs to other documents resolved through in.Documents,
// its expressions evaluated for in.Scope (recorded functions served from the
// committed pairings only, as check serves them), its same-document $refs,
// and its identities. The metadata then gets its overrides, its $refs to
// other documents, its pathless $refs resolved against the finished data
// (the instance it travels with), and its expressions, which see $data and
// $metadata. Each identity keeps a value the data gives it and otherwise derives
// one for the instance: claimed by no one, since the uniqueness of
// instances is the caller's to keep. Safe for concurrent use.
func (g *Generator) Instance(spec *Spec, fixtureKey string, in InstanceInput) (*Instance, error) {
	fx := spec.Fixtures.Get(fixtureKey)
	if fx == nil {
		return nil, refError("%s has no fixture %s", spec.SourceName, fixtureKey)
	}
	w := where(spec, fixtureKey)
	data := deepMerge(spec.PrototypeFor(fx.Dir), fx.Data)
	meta := deepMerge(spec.MetadataFor(fx.Dir), fx.Metadata)
	if err := override(data, in.Overrides, "override path"); err != nil {
		return nil, naming(err, w)
	}
	if err := override(meta, in.MetadataOverrides, "metadata override path"); err != nil {
		return nil, naming(err, w)
	}
	refs := g.documents(in.Documents)
	scope := in.Scope
	if scope.Attributes == nil {
		scope.Attributes = map[string]string{}
	}
	eval := newEvaluator(g.functions, ModeVerify, g.committed)
	eval.scope = &scope

	// The data first, finished: its $refs to other documents, its expressions, its own $refs,
	// and its identities.
	if err := resolveExternalRefs(data, fx.Dir, refs); err != nil {
		return nil, naming(err, w)
	}
	if err := eval.evaluateTree(data, w, nil); err != nil {
		return nil, naming(err, w)
	}
	if err := resolveSelfRefs(data); err != nil {
		return nil, naming(err, w)
	}
	ids := make(map[string]string, len(spec.Identity))
	for _, id := range spec.Identity {
		present, err := pathGet(data, id.Path)
		if err != nil {
			return nil, naming(err, w)
		}
		var value string
		switch {
		case present != nil:
			value = valueOf(present)
		case id.Derive == "authored":
			return nil, genError("%s: identity %s is derive: authored but the instance has no value at it", w, id.Path)
		default:
			value = deriveIdentity(id, scope.Material(id.Path))
		}
		if err := pathSet(data, id.Path, value); err != nil {
			return nil, naming(err, w)
		}
		ids[id.Path] = value
	}

	// Then the metadata, which travels with that data: its $refs to other documents, then its
	// pathless $refs, which name this instance's finished data (key: {$ref: "#/orderId"}), and
	// only then its expressions, so that $metadata.key is the value a $ref brought in.
	if err := resolveExternalRefs(meta, fx.Dir, refs); err != nil {
		return nil, naming(err, w)
	}
	if err := resolveSelfRefsAgainst(meta, data); err != nil {
		return nil, naming(err, w)
	}
	if err := eval.evaluateTree(meta, w+" -> metadata", map[string]*jsonx.Object{ScopeData: data, ScopeMetadata: meta}); err != nil {
		return nil, naming(err, w)
	}
	return &Instance{Data: data, Metadata: meta, Identities: ids}, nil
}

// override sets each value at its path, parents first.
func override(tree *jsonx.Object, overrides map[string]any, what string) error {
	paths := slices.Collect(maps.Keys(overrides))
	sortStrings(paths)
	for _, p := range paths {
		v, err := ModelValue(overrides[p])
		if err != nil {
			return genError("%s '%s': %s", what, p, errText(err))
		}
		if err := setPath(tree, p, v, what); err != nil {
			return err
		}
	}
	return nil
}

// documents resolves an instance's $refs to other documents through the
// caller's trees, each value a copy.
func (g *Generator) documents(trees func(string) (*jsonx.Object, error)) References {
	return func(document, pointer string) (any, error) {
		var tree *jsonx.Object
		if trees != nil {
			t, err := trees(document)
			if err != nil {
				return nil, withCode(err)
			}
			tree = t
		}
		if tree != nil {
			return copyValue(jsonPointer(tree, pointer)), nil
		}
		nd := g.noDocument(document)
		if slices.Contains(nd.known, document) {
			return nil, genError("%s is not produced for this instance, so a %s cannot take values from it", document, refKeyword)
		}
		return nil, nd
	}
}

// naming makes an error name the fixture it concerns, keeping its code and
// hint.
func naming(err error, where string) error {
	ae, ok := errors.AsType[*axxerr.Error](withCode(err))
	if !ok {
		return err
	}
	if strings.Contains(ae.Message, where) {
		return ae
	}
	named := *ae
	named.Message = where + ": " + ae.Message
	return &named
}

// ValueAt is the value at a dotted path of a tree (payments[0].id), nil
// when there is none.
func ValueAt(tree *jsonx.Object, path string) (any, error) { return pathGet(tree, path) }

// CanonicalJSON is a value as the factory writes JSON files: keys in their
// order, two-space indentation, numbers as their kind prints them and one
// trailing newline; the same tree always gives the same bytes.
func CanonicalJSON(v any) []byte { return canonicalJSON(v) }

// CompactJSON is CanonicalJSON without whitespace.
func CompactJSON(v any) []byte { return compactJSON(v) }

// PlainValue is a value of the factory's model as plain Go values: maps as
// map[string]any, lists as []any, Integer as int, Long as int64, Double as
// float64, Float as float32, BigInteger as *big.Int, BigDecimal as
// json.Number; strings, booleans, nil and []byte as they are.
func PlainValue(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := make(map[string]any, x.Len())
		for _, k := range x.Keys() {
			out[k] = PlainValue(get(x, k))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = PlainValue(e)
		}
		return out
	case jsonx.Number:
		switch x.Kind() {
		case jsonx.Integer:
			i, _ := x.Int64()
			return int(i)
		case jsonx.Long:
			i, _ := x.Int64()
			return i
		case jsonx.BigInteger:
			b, _ := x.BigInt()
			return b
		case jsonx.Float:
			return float32(x.Float64())
		case jsonx.BigDecimal:
			return json.Number(x.String())
		}
		return x.Float64()
	}
	return v
}

// ModelValue is a plain Go value in the factory's model, which every tree
// holds: maps with string keys become objects (keys sorted), slices lists,
// integers Integer (Long beyond its range), float64 Double, float32 Float,
// *big.Int BigInteger and json.Number what its text reads as; strings,
// booleans, nil and []byte stay as they are, as do values already of the
// model.
func ModelValue(v any) (any, error) {
	switch x := v.(type) {
	case nil, string, bool, []byte, jsonx.Number:
		return v, nil
	case *jsonx.Object:
		return copyValue(x), nil
	case float64:
		return jsonx.DoubleNumber(x), nil
	case float32:
		return jsonx.FloatNumber(x), nil
	case *big.Int:
		return jsonx.BigIntegerNumber(x), nil
	case json.Number:
		n, err := jsonx.Parse(x.String())
		if _, ok := n.(jsonx.Number); err != nil || !ok {
			return nil, genError("%q is not a number", x.String())
		}
		return n, nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return integer(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if u := rv.Uint(); u > math.MaxInt64 {
			return jsonx.BigIntegerNumber(new(big.Int).SetUint64(u)), nil
		}
		return integer(int64(rv.Uint())), nil
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			m, err := ModelValue(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out[i] = m
		}
		return out, nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			break
		}
		keys := make([]string, 0, rv.Len())
		for _, k := range rv.MapKeys() {
			keys = append(keys, k.String())
		}
		sortStrings(keys)
		out := jsonx.NewObject()
		for _, k := range keys {
			m, err := ModelValue(rv.MapIndex(reflect.ValueOf(k).Convert(rv.Type().Key())).Interface())
			if err != nil {
				return nil, err
			}
			out.Set(k, m)
		}
		return out, nil
	}
	return nil, genError("a value of type %T has no place in a fixture (strings, numbers, booleans, null, lists and maps do)", v)
}

// integer is an integer as the YAML reader reads one: an Integer when it
// fits, otherwise a Long.
func integer(i int64) jsonx.Number {
	if i >= math.MinInt32 && i <= math.MaxInt32 {
		return jsonx.IntegerNumber(int32(i))
	}
	return jsonx.LongNumber(i)
}
