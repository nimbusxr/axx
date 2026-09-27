package fixtures

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// Local schema files a JSON Schema references, bundled into it.
//
// A $ref may name another schema file, relative to the file that holds the
// $ref, with or without a fragment: "address.schema.json" or
// "common/money.schema.json#/$defs/Money". Each file a document reaches goes
// under the document's own $defs (definitions before draft 2019-09), keyed
// by its path relative to the document, and every reference into it points
// there. The document then resolves and validates on its own, so the walker
// and the oracle read the same schema. Remote schemas are not fetched.

// bundled maps the $defs keys of bundled files to their paths, relative to
// the governing schema's directory.
type bundled map[string]string

// bundleLocalRefs rewrites doc, the schema at path (named ref in messages),
// to hold the local files its references reach.
func bundleLocalRefs(doc *jsonx.Object, path, ref string) (bundled, error) {
	b := &bundler{
		root:    doc,
		rootDir: filepath.Dir(path),
		rootRef: ref,
		defs:    defsKeyword(doc),
		keys:    map[string]string{},
		files:   bundled{},
	}
	if err := b.rewrite(doc, b.rootDir, ref, ""); err != nil {
		return nil, err
	}
	return b.files, nil
}

type bundler struct {
	root    *jsonx.Object
	rootDir string
	rootRef string
	defs    string
	keys    map[string]string // absolute path -> $defs key
	files   bundled
}

// defsKeyword is where a document keeps definitions: $defs from draft
// 2019-09 on, definitions before.
func defsKeyword(doc *jsonx.Object) string {
	s, _ := get(doc, "$schema").(string)
	for _, old := range []string{"draft-03", "draft-04", "draft-06", "draft-07"} {
		if strings.Contains(s, old) {
			return "definitions"
		}
	}
	return "$defs"
}

// Keywords whose values are data, not schemas: a "$ref" in them is not a
// reference.
var schemaData = map[string]bool{"const": true, "enum": true, "default": true, "examples": true, "example": true}

// rewrite walks a node of the document at dir (named from in messages):
// references to other files point into their bundled copies, and internal
// references of a bundled file (prefix, its place in the root) into it.
func (b *bundler) rewrite(node any, dir, from, prefix string) error {
	switch x := node.(type) {
	case []any:
		for _, e := range x {
			if err := b.rewrite(e, dir, from, prefix); err != nil {
				return err
			}
		}
	case *jsonx.Object:
		if ref, ok := get(x, "$ref").(string); ok {
			rewritten, err := b.reference(ref, dir, from, prefix)
			if err != nil {
				return err
			}
			x.Set("$ref", rewritten)
		}
		for _, k := range x.Keys() {
			if k == "$ref" || schemaData[k] {
				continue
			}
			if err := b.rewrite(get(x, k), dir, from, prefix); err != nil {
				return err
			}
		}
	}
	return nil
}

// reference is where a $ref written in a file at dir points in the root.
func (b *bundler) reference(ref, dir, from, prefix string) (string, error) {
	if strings.HasPrefix(ref, "#") {
		if prefix == "" {
			return ref, nil
		}
		return "#" + prefix + ref[1:], nil
	}
	file, fragment, _ := strings.Cut(ref, "#")
	if u, err := url.Parse(file); err == nil && u.Scheme != "" && len(u.Scheme) > 1 {
		return "", schemaError("%s: $ref '%s' names a remote schema, and remote schemas are not fetched: save it next to %s and reference it by its path", from, ref, b.rootRef)
	}
	abs := filepath.Clean(filepath.Join(dir, filepath.FromSlash(file)))
	if filepath.IsAbs(filepath.FromSlash(file)) {
		abs = filepath.Clean(filepath.FromSlash(file))
	}
	key, err := b.load(abs, file, from)
	if err != nil {
		return "", err
	}
	return "#/" + b.defs + "/" + escapePointer(key) + fragment, nil
}

// load bundles one file, once, and returns its $defs key.
func (b *bundler) load(abs, written, from string) (string, error) {
	if key, ok := b.keys[abs]; ok {
		return key, nil
	}
	rel, err := filepath.Rel(b.rootDir, abs)
	if err != nil {
		rel = abs
	}
	key := filepath.ToSlash(rel)
	data, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return "", schemaError("%s: $ref '%s': there is no %s", from, written, path.Clean(path.Join(path.Dir(b.rootRef), key)))
	}
	if err != nil {
		return "", schemaError("%s: $ref '%s': cannot read %s: %v", from, written, key, err)
	}
	var parsed any
	if strings.HasSuffix(strings.ToLower(abs), ".json") {
		parsed, err = jvalue.ParseJackson(string(data))
	} else {
		parsed, err = jyaml.Unmarshal(data)
	}
	if err != nil {
		return "", schemaError("%s: $ref '%s': cannot read %s: %v", from, written, key, err)
	}
	doc, ok := parsed.(*jsonx.Object)
	if !ok {
		return "", schemaError("%s: $ref '%s': %s must be a JSON Schema object", from, written, key)
	}
	// A bundled file is part of the root: its own identity and draft go.
	doc.Delete("$id")
	doc.Delete("$schema")
	b.keys[abs] = key
	b.files[key] = key
	defs, ok := get(b.root, b.defs).(*jsonx.Object)
	if !ok {
		defs = jsonx.NewObject()
		b.root.Set(b.defs, defs)
	}
	defs.Set(key, doc)
	if err := b.rewrite(doc, filepath.Dir(abs), key, "/"+b.defs+"/"+escapePointer(key)); err != nil {
		return "", err
	}
	return key, nil
}

// escapePointer escapes a JSON Pointer reference token.
func escapePointer(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

// fileOf is the bundled file a schema location lies in, if any: the
// location is the governing schema's URL with a pointer fragment.
func (b bundled) fileOf(schemaURL, defs string) string {
	_, fragment, ok := strings.Cut(schemaURL, "#")
	if !ok || len(b) == 0 {
		return ""
	}
	rest, ok := strings.CutPrefix(fragment, "/"+defs+"/")
	if !ok {
		return ""
	}
	token, _, _ := strings.Cut(rest, "/")
	key := strings.NewReplacer("~1", "/", "~0", "~").Replace(token)
	return b[key]
}
