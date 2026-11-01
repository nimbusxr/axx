// Package fixtures produces instances of the fixture factory's fixtures for
// packs: one fixture's values, evaluated for an instance the pack names (a
// record of a stream, say), as `axx fixtures check` resolves them. A pack
// loads a Library, finds a fixture by its reference ("<factory>#<fixture>")
// and asks it for as many instances as it needs, each with its own scope,
// overrides and documents for its $refs.
//
// An instance is deterministic: the same fixture, scope, overrides and
// documents give the same trees, byte for byte. Expression functions run as
// check runs them: pure ones are called, recorded ones answer from the
// committed pairings lock only.
package fixtures

import (
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/fixtures"
)

// Config is the fixtures section of axx.yaml with every default applied: the
// resource root (BaseDir), the globs locating factory specs and the other
// roots walked for them.
type Config = fixtures.Config

// ConformanceRule validates files no factory manages; Load does not use it.
type ConformanceRule = fixtures.ConformanceRule

// DefaultConfig is the configuration of a project whose axx.yaml has no
// fixtures section: every convention, rooted at baseDir.
func DefaultConfig(baseDir string) Config { return fixtures.DefaultConfig(baseDir) }

// Function is an expression function a pack supplies: a spec value
// "${ name(args) }" calls it with its arguments and the instance's scope.
// A pure function (Recorded false) is called for every instance and must be
// a deterministic function of its arguments and scope. A recorded function's
// results come from the committed pairings lock, which `axx fixtures
// generate` records; an instance whose arguments it has no pairing for
// fails.
type Function = fixtures.Function

// InstanceScope is the instance an expression produces values for: its
// Discriminator identifies it, and its Attributes are what the pack offers
// its own functions (run.id, run.position...).
type InstanceScope = fixtures.InstanceScope

// Identity is a factory's identity declaration: the dotted path of a field
// whose values identify fixtures, and how a value it lacks is derived
// (Derive "fixture-key" or "authored"; Format "literal" or
// "uuid-name-based"; Prefix in front of it).
type Identity = fixtures.Identity

// Library is a module's factory specs, its committed pairings lock and the
// functions their expressions call. It is safe for concurrent use.
type Library struct{ g *fixtures.Generator }

// Load loads every factory spec under cfg, and the pairings lock, refused
// when it was edited by hand as `axx fixtures check` refuses it, with
// functions for their expressions.
func Load(cfg Config, functions ...Function) (*Library, error) {
	g, err := fixtures.NewGenerator(cfg, fixtures.Options{Functions: functions})
	if err != nil {
		return nil, err
	}
	return &Library{g: g}, nil
}

// BaseDir is the resource root the documents' paths are relative to.
func (l *Library) BaseDir() string { return l.g.BaseDir() }

// Fixture finds the fixture a reference names: "<factory>#<fixture>", the
// factory by its path from the resource root without .factory.yaml
// (orders/orders#created), or by its base name (orders#created) when no
// other factory has it.
func (l *Library) Fixture(ref string) (*Fixture, error) {
	spec, key, err := l.g.Fixture(ref)
	if err != nil {
		return nil, err
	}
	return &Fixture{spec: spec, key: key}, nil
}

// Fixtures lists every fixture: factories in the order they load, each
// one's fixtures in key order.
func (l *Library) Fixtures() []*Fixture {
	var out []*Fixture
	for _, spec := range l.g.Specs() {
		for _, k := range spec.Fixtures.SortedKeys() {
			out = append(out, &Fixture{spec: spec, key: k})
		}
	}
	return out
}

// Fixture is one fixture of a factory.
type Fixture struct {
	spec *fixtures.Spec
	key  string
}

// Ref is the fixture's reference: "<factory>#<key>", the factory by its
// path.
func (f *Fixture) Ref() string { return f.spec.Name() + "#" + f.key }

// Factory is the name of the fixture's factory: its path from the resource
// root without .factory.yaml (orders/orders).
func (f *Fixture) Factory() string { return f.spec.Name() }

// Key is the fixture's key within its factory.
func (f *Fixture) Key() string { return f.key }

// Dir is the fixture's directory relative to the resource root ("" at the
// root), which its $refs are relative to.
func (f *Fixture) Dir() string { return f.spec.Fixtures.Get(f.key).Dir }

// Document is where the fixture is written: its *.fixture.yaml relative to
// the resource root, the document other fixtures' $refs name; or "inline in
// <factory file>".
func (f *Fixture) Document() string { return f.spec.Fixtures.Get(f.key).Source }

// Family is the factory's family (json, avro, yaml, xml, protobuf,
// dataset).
func (f *Fixture) Family() string { return f.spec.Family }

// SchemaRef is the factory's schema relative to the resource root, a
// #fragment kept ("" when it declares none).
func (f *Fixture) SchemaRef() string { return f.spec.SchemaRef() }

// Identities are the factory's identity declarations.
func (f *Fixture) Identities() []Identity { return append([]Identity(nil), f.spec.Identity...) }

// Input is what an instance is produced from.
type Input struct {
	// Scope is the instance. Expression functions receive it, and an
	// identity the instance has no value for derives one from
	// Scope.Material(<identity path>), with the identity's prefix and
	// format, as a fixture's derives from its key.
	Scope InstanceScope
	// Overrides set values at dotted paths of the data (customer.tier,
	// items[0].sku) before anything resolves: plain Go values (see
	// Tree.Value), which may be $refs and "${ fn(args) }" expressions too.
	// A list index must exist; missing maps are made.
	Overrides map[string]any
	// MetadataOverrides set values at dotted paths of the metadata.
	MetadataOverrides map[string]any
	// Documents resolves the $refs to other documents: document is the
	// target's path relative to the resource root (another fixture's
	// Document, or a factory file, whose fixtures it holds by key), and the
	// Tree returned that document as the pack produced it for the same
	// instance; nil when it has not.
	Documents func(document string) (*Tree, error)
}

// Instance is one instance of a fixture.
type Instance struct {
	// Data is the fixture's values for the instance, its identities set.
	Data *Tree
	// Metadata is what travels beside them (a record's key, headers...).
	Metadata *Tree
	// Identities are the instance's identity values by identity path.
	Identities map[string]string
}

// Instance produces an instance of a fixture. Its data is the fixture's
// values: the factory's prototype for the fixture's directory deep-merged
// under the fixture's data (not defaults: or the schema, which its family
// applies when it generates files). Its metadata is the factory's for that
// directory deep-merged under the fixture's own. Each gets its overrides,
// then its $refs to other documents resolved through in.Documents, its
// expressions evaluated for in.Scope (the metadata's after the data's,
// reading $data and $metadata), and its same-document $refs. Then each
// identity keeps the value the data gives it or derives one for the
// instance; Instance claims none, so keeping the values of many instances
// unique is the caller's to do. A failure is an *axxerr.Error naming the
// fixture.
func (l *Library) Instance(f *Fixture, in Input) (*Instance, error) {
	input := fixtures.InstanceInput{Scope: in.Scope, Overrides: in.Overrides, MetadataOverrides: in.MetadataOverrides}
	if in.Documents != nil {
		input.Documents = func(document string) (*jsonx.Object, error) {
			t, err := in.Documents(document)
			if err != nil || t == nil {
				return nil, err
			}
			return t.o, nil
		}
	}
	x, err := l.g.Instance(f.spec, f.key, input)
	if err != nil {
		return nil, err
	}
	return &Instance{Data: &Tree{o: x.Data}, Metadata: &Tree{o: x.Metadata}, Identities: x.Identities}, nil
}
