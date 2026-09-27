package fixtures

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Structural references between fixtures: a node holding "$ref" is replaced
// by what it points at, so a document can take a value, or a whole subtree,
// from another instead of restating it.
//
//	checkoutId:
//	  $ref: ./created.fixture.yaml#/checkoutId   # a value from a sibling document
//	customer:
//	  $ref: ../shared/gold.fixture.yaml          # a whole document
//	  tier: PLATINUM                             # siblings layer over what it holds
//
// The path before '#' is relative to the referring document (a leading '/'
// means the resource root); the fragment is a JSON Pointer into the target's
// RESOLVED values. A pointer with no path points into the same document and
// resolves once the document is otherwise finished.

// refKeyword is the reference key.
const refKeyword = "$ref"

// References resolves a value of another document: document is the target's
// path relative to the resource root ("" for the referring document itself),
// pointer a JSON Pointer ("" for the whole document). It returns nil when the
// target holds nothing there.
type References func(document, pointer string) (any, error)

// resolveExternalRefs replaces every $ref pointing at another document.
func resolveExternalRefs(tree *jsonx.Object, documentDir string, lookup References) error {
	return walkRefs(tree, documentDir, lookup, false)
}

// resolveSelfRefs replaces every pathless $ref against the finished tree.
func resolveSelfRefs(tree *jsonx.Object) error {
	return resolveSelfRefsAgainst(tree, tree)
}

// resolveSelfRefsAgainst replaces every pathless $ref against scope.
func resolveSelfRefsAgainst(tree *jsonx.Object, scope any) error {
	return walkRefs(tree, "", func(_, pointer string) (any, error) {
		return jsonPointer(scope, pointer), nil
	}, true)
}

func walkRefs(node *jsonx.Object, documentDir string, lookup References, self bool) error {
	for _, k := range node.Keys() {
		v, err := resolveRefValue(get(node, k), documentDir, lookup, self)
		if err != nil {
			return err
		}
		node.Set(k, v)
	}
	return nil
}

func resolveRefValue(value any, documentDir string, lookup References, self bool) (any, error) {
	switch x := value.(type) {
	case []any:
		for i := range x {
			v, err := resolveRefValue(x[i], documentDir, lookup, self)
			if err != nil {
				return nil, err
			}
			x[i] = v
		}
		return x, nil
	case *jsonx.Object:
		reference := get(x, refKeyword)
		if reference == nil {
			return x, walkRefs(x, documentDir, lookup, self)
		}
		refText := valueOf(reference)
		path, pointer := splitRef(refText)
		pointsHere := path == ""
		if pointsHere != self {
			// Same-document references wait until the document is finished.
			return x, nil
		}
		target := ""
		if !pointsHere {
			target = normalizeRef(documentDir, path)
		}
		referenced, err := lookup(target, pointer)
		if nd, ok := errors.AsType[*noDocument](err); ok {
			return nil, noDocumentError(refText, documentDir, nd)
		}
		if err != nil {
			return nil, err
		}
		if referenced == nil {
			return nil, genError("%s %s resolved to nothing", refKeyword, refText)
		}
		overrides := copyObjectShallow(x)
		overrides.Delete(refKeyword)
		if overrides.Len() == 0 {
			return referenced, nil
		}
		referencedMap, ok := referenced.(*jsonx.Object)
		if !ok {
			return nil, genError("%s %s resolved to a value, so it cannot carry %s", refKeyword, refText, javaSetString(overrides))
		}
		if err := walkRefs(overrides, documentDir, lookup, self); err != nil {
			return nil, err
		}
		return deepMerge(referencedMap, overrides), nil
	}
	return value, nil
}

func copyObjectShallow(o *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range o.Keys() {
		out.Set(k, get(o, k))
	}
	return out
}

// splitRef splits a reference into its document path and its pointer.
func splitRef(reference string) (path, pointer string) {
	hash := strings.IndexByte(reference, '#')
	if hash < 0 {
		return reference, ""
	}
	return reference[:hash], reference[hash+1:]
}

// normalizeRef rebases a reference path onto the resource root the way
// factory.schema is rebased; a leading '/' means the resource root.
func normalizeRef(documentDir, path string) string {
	var joined string
	switch {
	case strings.HasPrefix(path, "/"):
		joined = path[1:]
	case documentDir == "":
		joined = path
	default:
		joined = documentDir + "/" + path
	}
	return normalizeSegments(joined)
}

// normalizeSegments drops "." and empty segments and folds "..", keeping
// leading ".." segments.
func normalizeSegments(joined string) string {
	var stack []string
	for _, seg := range strings.Split(joined, "/") {
		switch {
		case seg == "" || seg == ".":
		case seg == ".." && len(stack) > 0 && stack[len(stack)-1] != "..":
			stack = stack[:len(stack)-1]
		default:
			stack = append(stack, seg)
		}
	}
	return strings.Join(stack, "/")
}

// jsonPointer applies a JSON Pointer; nil when it leads nowhere.
func jsonPointer(root any, pointer string) any {
	if pointer == "" || pointer == "/" {
		return root
	}
	current := root
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if current == nil {
			return nil
		}
		seg := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch x := current.(type) {
		case *jsonx.Object:
			current = get(x, seg)
		case []any:
			i, err := strconv.ParseInt(seg, 10, 32)
			if err != nil || i < 0 || int(i) >= len(x) {
				return nil
			}
			current = x[i]
		default:
			return nil
		}
	}
	return current
}

// noDocumentError says what a reference to no document wrote, where it was
// looked for and what it came to, and the few documents it most likely
// meant, rather than every document there is.
func noDocumentError(written, documentDir string, nd *noDocument) error {
	from := documentDir
	if from == "" {
		from = "the resource root"
	}
	msg := fmt.Sprintf("%s %q names no document\n  written:     %s (relative to %s)\n  looked for:  %s", refKeyword, written, written, from, nd.document)
	if likely := likelyDocuments(nd.document, nd.known, 3); len(likely) > 0 {
		msg += "\n  did you mean " + strings.Join(likely, ", ") + "?"
	}
	return genError("%s", msg)
}

// likelyDocuments are up to n known documents closest to a path: those with
// its file name first, then by edit distance, within a third of its length.
func likelyDocuments(path string, known []string, n int) []string {
	type scored struct {
		doc  string
		dist int
	}
	base := path[strings.LastIndexByte(path, '/')+1:]
	var out []scored
	seen := map[string]bool{}
	for _, k := range known {
		if seen[k] || k == path {
			continue
		}
		seen[k] = true
		d := editDistance(path, k)
		if kb := k[strings.LastIndexByte(k, '/')+1:]; kb == base {
			d = 0
		}
		if d <= max(3, len(path)/3) {
			out = append(out, scored{k, d})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].dist != out[j].dist {
			return out[i].dist < out[j].dist
		}
		return out[i].doc < out[j].doc
	})
	var docs []string
	for i := 0; i < len(out) && i < n; i++ {
		docs = append(docs, out[i].doc)
	}
	return docs
}

// editDistance is the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
