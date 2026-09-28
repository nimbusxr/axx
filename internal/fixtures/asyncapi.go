package fixtures

import (
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

const messagePrefix = "#/components/messages/"

// loadAsyncAPIMessage governs fixtures by the payload schema of a message of
// an AsyncAPI 2.x or 3.x document (spec.yaml#/components/messages/Name),
// the fixtures of the messages a scenario sends. The payload becomes a
// standalone JSON Schema document, draft 7 unless it says otherwise, with
// the specification's components beside it, so that its references into
// them (#/components/schemas/Scan) resolve; its references to other files
// are bundled as a standalone schema's are.
func loadAsyncAPIMessage(baseDir, ref string) (*jsonSchemaDoc, error) {
	hash := strings.IndexByte(ref, '#')
	specPath, name := ref[:hash], ref[hash+len(messagePrefix):]
	path := filepath.Join(baseDir, filepath.FromSlash(specPath))
	root, err := readSchemaObject(path, specPath, "AsyncAPI document")
	if err != nil {
		return nil, err
	}
	if _, ok := get(root, "asyncapi").(string); !ok {
		return nil, schemaError("%s is not an AsyncAPI document: it has no asyncapi version", specPath)
	}
	msg, ok := jsonPointer(root, ref[hash+1:]).(*jsonx.Object)
	if !ok {
		return nil, schemaError("%s has no message '%s' under #/components/messages", specPath, name)
	}
	// A message that refers to another: here, or in another file, whose
	// references are then relative to it.
	for range 32 {
		r, ok := get(msg, "$ref").(string)
		if !ok {
			break
		}
		file, pointer, _ := strings.Cut(r, "#")
		if file != "" {
			path = filepath.Join(filepath.Dir(path), filepath.FromSlash(file))
			if root, err = readSchemaObject(path, file, "AsyncAPI message"); err != nil {
				return nil, err
			}
		}
		if msg, ok = jsonPointer(root, pointer).(*jsonx.Object); !ok {
			return nil, schemaError("%s: the message's $ref '%s' resolves to nothing", ref, r)
		}
	}
	payload, ok := get(msg, "payload").(*jsonx.Object)
	if !ok {
		return nil, schemaError("the message %s has no payload schema", ref)
	}
	format, _ := get(msg, "schemaFormat").(string)
	if f, ok := get(payload, "schemaFormat").(string); ok {
		if inner, ok := get(payload, "schema").(*jsonx.Object); ok {
			format, payload = f, inner
		}
	}
	if !jsonSchemaFormat(format) {
		return nil, schemaError("the payload of the message %s is a %s schema: fixtures take a JSON Schema payload, "+
			"or an Avro one through the avro family and its .avsc file", ref, format)
	}
	doc := jsonx.NewObject()
	for _, k := range payload.Keys() {
		doc.Set(k, get(payload, k))
	}
	if !doc.Has("$schema") {
		doc.Set("$schema", "http://json-schema.org/draft-07/schema#")
	}
	if comps, ok := get(root, "components").(*jsonx.Object); ok && !doc.Has("components") {
		doc.Set("components", comps)
	}
	if n, ok := get(msg, "name").(string); ok && n != "" {
		name = n
	}
	return compileSchemaDoc(doc, path, ref, name)
}

// jsonSchemaFormat reports whether an AsyncAPI schemaFormat is JSON Schema
// (or AsyncAPI's own schema, a superset of it).
func jsonSchemaFormat(format string) bool {
	mt, _, _ := strings.Cut(strings.ToLower(format), ";")
	switch strings.TrimSpace(mt) {
	case "", "application/vnd.aai.asyncapi", "application/vnd.aai.asyncapi+json", "application/vnd.aai.asyncapi+yaml",
		"application/schema+json", "application/schema+yaml":
		return true
	}
	return false
}
