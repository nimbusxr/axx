package fixtures

import (
	"embed"
	"sort"
)

//go:embed schemas/*.schema.json
var schemaFS embed.FS

// SchemaBase is where the spec file schemas are published.
const SchemaBase = "https://axx.nimbusxr.us/schemas/v0/"

// SchemaKinds are the spec file kinds with a JSON Schema.
var SchemaKinds = []string{"factory", "fixture", "prototype"}

// SchemaFile is the published file name of a kind's schema.
func SchemaFile(kind string) string { return "axx-" + kind + ".schema.json" }

// SchemaID is the published URL of a kind's schema.
func SchemaID(kind string) string { return SchemaBase + SchemaFile(kind) }

// SchemaJSON returns the JSON Schema of a spec file kind (factory, fixture
// or prototype).
func SchemaJSON(kind string) ([]byte, bool) {
	b, err := schemaFS.ReadFile("schemas/" + SchemaFile(kind))
	return b, err == nil
}

// Schemas returns every spec file schema keyed by published file name.
func Schemas() map[string][]byte {
	out := map[string][]byte{}
	kinds := append([]string(nil), SchemaKinds...)
	sort.Strings(kinds)
	for _, k := range kinds {
		b, _ := SchemaJSON(k)
		out[SchemaFile(k)] = b
	}
	return out
}
