package fixtures

import (
	"encoding/base64"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// where names a fixture in messages.
func where(spec *Spec, fixtureKey string) string {
	return spec.SourceName + " -> fixtures." + fixtureKey
}

// resolveValues is FixtureTree.resolveValues: the prototype deep-merged
// under the fixture's data, external references resolved, expressions
// evaluated for this fixture's instance, then same-document references.
// Identities are not claimed, so resolving a fixture because something
// references it never claims them twice.
func resolveValues(spec *Spec, fixtureKey string, ctx *ExpansionContext) (*jsonx.Object, error) {
	fx := spec.Fixtures.Get(fixtureKey)
	tree := deepMerge(spec.PrototypeFor(fx.Dir), fx.Data)
	w := where(spec, fixtureKey)
	if ctx.eval.references != nil {
		if err := resolveExternalRefs(tree, fx.Dir, ctx.eval.references); err != nil {
			return nil, err
		}
	}
	previous := ctx.eval.scope
	ctx.eval.scope = &InstanceScope{Discriminator: w, Attributes: map[string]string{}}
	err := ctx.eval.evaluateTree(tree, w, nil)
	ctx.eval.scope = previous
	if err != nil {
		return nil, err
	}
	if err := resolveSelfRefs(tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// resolveFixture is FixtureTree.resolve: resolveValues, then each identity
// pinned by presence (a value at the path is the fixture's authored identity)
// or derived from the fixture key, and claimed module-wide.
func resolveFixture(spec *Spec, fixtureKey string, ctx *ExpansionContext) (*jsonx.Object, error) {
	tree, err := resolveValues(spec, fixtureKey, ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range spec.Identity {
		present, err := pathGet(tree, id.Path)
		if err != nil {
			return nil, err
		}
		var value string
		switch {
		case present != nil:
			value = valueOf(present)
		case id.Derive == "authored":
			return nil, genError("identity %s is derive: authored but fixtures.%s does not provide a value (%s)", id.Path, fixtureKey, spec.SourceName)
		default:
			value = ctx.identities.derive(id, fixtureKey)
		}
		if err := ctx.identities.claim(value, spec.SourceName, fixtureKey, id.Path); err != nil {
			return nil, err
		}
		if err := pathSet(tree, id.Path, value); err != nil {
			return nil, err
		}
	}
	return tree, nil
}
