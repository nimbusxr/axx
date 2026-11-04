// Package schemas holds the JSON Schemas of axx's YAML files: axx.yaml and the
// fixture spec files. Their paths here are public: Base and the file names make
// the URLs that axx.yaml files name, and GitHub serves them from main. The
// editor plugins bundle these files too, and need no network.
package schemas

import "embed"

// Base is where the schemas are published.
const Base = "https://raw.githubusercontent.com/nimbusxr/axx/main/schemas/v0/"

//go:embed v0/*.schema.json
var files embed.FS

// Read returns the schema with a published file name, like axx.schema.json.
func Read(name string) ([]byte, error) {
	return files.ReadFile("v0/" + name)
}
