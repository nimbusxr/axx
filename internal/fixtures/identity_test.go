package fixtures

import (
	"strings"
	"testing"
)

// Identity values are unique among the identity fields of one name, or of
// one namespace, across every factory.

func placesWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.write("places/place.schema.json", `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object",
 "properties": {"id": {"type": "string"}, "key": {"type": "string"}, "shopId": {"type": "string"}, "ref": {"type": "string"}, "name": {"type": "string"}}}`)
	return w
}

// factory writes a json factory of places with the given identity entries
// and one fixture, leipzig.
func (w *workspace) placeFactory(name, identity string) {
	w.t.Helper()
	w.write("places/"+name+"/"+name+".factory.yaml", "factory:\n  family: json\n  schema: ../place.schema.json\nidentity:\n"+identity+"fixtures:\n  leipzig:\n    name: Leipzig\n")
}

func TestIdentitiesOfOtherFieldsMayShareValues(t *testing.T) {
	w := placesWorkspace(t)
	w.placeFactory("shops", "  - path: key\n")
	w.placeFactory("accounts", "  - path: id\n")
	files := w.expand()
	expect(t, str(at(jsonOf(t, files["places/shops/leipzig.json"]), "key")), "leipzig")
	expect(t, str(at(jsonOf(t, files["places/accounts/leipzig.json"]), "id")), "leipzig")
}

func TestIdentitiesOfOneFixtureAreIndependent(t *testing.T) {
	w := placesWorkspace(t)
	w.placeFactory("shops", "  - path: id\n  - path: ref\n")
	doc := jsonOf(t, w.expand()["places/shops/leipzig.json"])
	expect(t, str(at(doc, "id")), "leipzig")
	expect(t, str(at(doc, "ref")), "leipzig")
}

func TestIdentitiesOfOneFieldNameCollideAcrossFactories(t *testing.T) {
	w := placesWorkspace(t)
	w.placeFactory("shops", "  - path: id\n")
	w.placeFactory("depots", "  - path: id\n")
	err := w.expandErr()
	mustErrContain(t, err, `identity collision: id "leipzig" is claimed by both`, "depots.factory.yaml -> fixtures.leipzig (id)",
		"shops.factory.yaml -> fixtures.leipzig (id)", "different namespaces (namespace: in identity:)")
}

func TestIdentityFieldNamesIgnoreParentsAndIndices(t *testing.T) {
	for path, want := range map[string]string{"id": "id", "shops.id": "id", "payments[0].payment_id": "payment_id", "labels[2]": "labels", "a.b[1].c": "c"} {
		if got := fieldName(path); got != want {
			t.Errorf("fieldName(%s) = %s, want %s", path, got, want)
		}
	}
}

func TestNamespacesSeparateIdentitiesOfOneFieldName(t *testing.T) {
	w := placesWorkspace(t)
	w.placeFactory("shops", "  - path: id\n    namespace: shops\n")
	w.placeFactory("depots", "  - path: id\n    namespace: depots\n")
	files := w.expand()
	expect(t, str(at(jsonOf(t, files["places/shops/leipzig.json"]), "id")), "leipzig")
	expect(t, str(at(jsonOf(t, files["places/depots/leipzig.json"]), "id")), "leipzig")
}

func TestANamespaceJoinsIdentitiesOfOtherFieldNames(t *testing.T) {
	w := placesWorkspace(t)
	w.placeFactory("shops", "  - path: id\n    namespace: shops\n")
	w.placeFactory("accounts", "  - path: shopId\n    namespace: shops\n")
	err := w.expandErr()
	mustErrContain(t, err, `identity collision: value "leipzig" in namespace shops is claimed by both`,
		"accounts.factory.yaml -> fixtures.leipzig (shopId)", "shops.factory.yaml -> fixtures.leipzig (id)")
	if strings.Contains(err.Error(), "different namespaces") {
		t.Errorf("a collision in one namespace suggests namespaces: %v", err)
	}
}

func TestANamespaceIsAString(t *testing.T) {
	w := placesWorkspace(t)
	w.placeFactory("shops", "  - path: id\n    namespace: [shops]\n")
	mustErrContain(t, w.expandErr(), "identity.namespace must be a string")
}
