// Package schemadoc reads the contract documents that hold JSON Schemas
// (AsyncAPI, OpenRPC): YAML or JSON, by file or URL, with $refs across
// documents, the schemas in them compiled where they are, and validation
// failures keyed by the JSON Schema keyword they break.
package schemadoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/nimbusxr/axx/core"
)

// Loader reads documents by URL, once each: a document's $refs to other
// files are relative to it. The jsonschema compiler reads the schemas'
// $refs through it too.
type Loader struct {
	mu   sync.Mutex
	docs map[string]any
	http *http.Client
}

// NewLoader returns a loader of file, http and https URLs.
func NewLoader() *Loader {
	return &Loader{docs: map[string]any{}, http: &http.Client{Timeout: 30 * time.Second}}
}

// Add makes doc the document at u, for documents that come from elsewhere,
// such as a service's answer.
func (l *Loader) Add(u string, doc any) {
	l.mu.Lock()
	l.docs[u] = doc
	l.mu.Unlock()
}

// Load reads a document, without the URL's fragment.
func (l *Loader) Load(u string) (any, error) {
	u, _, _ = strings.Cut(u, "#")
	l.mu.Lock()
	defer l.mu.Unlock()
	if d, ok := l.docs[u]; ok {
		return d, nil
	}
	var raw []byte
	var err error
	switch {
	case strings.HasPrefix(u, "file://"):
		// file:///C:/... on Windows.
		p, perr := jsonschema.FileLoader{}.ToFile(u)
		if perr != nil {
			return nil, perr
		}
		raw, err = os.ReadFile(p)
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		// A document is read once per run, whatever scenario asks first:
		// the client's timeout bounds it, not the scenario.
		var req *http.Request
		var res *http.Response
		if req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil); err == nil {
			res, err = l.http.Do(req)
		}
		if err == nil {
			raw, err = io.ReadAll(io.LimitReader(res.Body, 16<<20))
			_ = res.Body.Close()
			if err == nil && res.StatusCode != http.StatusOK {
				err = fmt.Errorf("%s answered %d", u, res.StatusCode)
			}
		}
	default:
		err = fmt.Errorf("cannot read %s", u)
	}
	if err != nil {
		return nil, err
	}
	doc, err := Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	l.docs[u] = doc
	return doc, nil
}

// Compiler is a JSON Schema compiler that reads through the loader, with
// draft as the default draft.
func (l *Loader) Compiler(draft *jsonschema.Draft) *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(draft)
	c.UseLoader(jsonschema.SchemeURLLoader{"file": l, "http": l, "https": l})
	return c
}

// Decode reads YAML (or JSON, which is YAML) into what the jsonschema
// package validates: plain maps and json.Number numbers.
func Decode(raw []byte) (any, error) {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	j, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(j))
}

// Locate is the URL of a document a registration names: a URL, or a file
// of the project.
func Locate(s *core.Suite, source string) (string, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return source, nil
	}
	p, err := s.ResolvePath(source)
	if err != nil {
		return "", err
	}
	if p, err = filepath.Abs(p); err != nil {
		return "", err
	}
	return FileURL(p), nil
}

// FileURL is the URL of a file, from its absolute path: file:///C:/... for
// a Windows path, whose drive would otherwise read as the URL's host.
func FileURL(p string) string {
	u := path.Clean(filepath.ToSlash(p))
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return (&url.URL{Scheme: "file", Path: u}).String()
}

// Node is a value of a document, and where it is: its document's URL and
// its JSON pointer.
type Node struct {
	URL, Pointer string
	V            any
}

// Ref is the node's URL, with its pointer as the fragment.
func (n Node) Ref() string { return n.URL + "#" + n.Pointer }

// Get is the node's member of that key.
func (n Node) Get(key string) Node {
	m, _ := n.V.(map[string]any)
	return Node{URL: n.URL, Pointer: n.Pointer + "/" + escape(key), V: m[key]}
}

// Str is the text of the node's member of that key, or "".
func (n Node) Str(key string) string {
	s, _ := n.Get(key).V.(string)
	return s
}

// Keys are an object node's keys, sorted.
func (n Node) Keys() []string {
	m, _ := n.V.(map[string]any)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Items are an array node's items.
func (n Node) Items() []Node {
	a, _ := n.V.([]any)
	out := make([]Node, len(a))
	for i, v := range a {
		out[i] = Node{URL: n.URL, Pointer: n.Pointer + "/" + strconv.Itoa(i), V: v}
	}
	return out
}

func escape(k string) string { return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1") }

// Root is the whole document at u.
func (l *Loader) Root(u string) (Node, error) {
	doc, err := l.Load(u)
	if err != nil {
		return Node{}, err
	}
	return Node{URL: u, V: doc}, nil
}

// Deref follows a node's $refs, across documents.
func (l *Loader) Deref(n Node) (Node, error) {
	for range 32 {
		m, ok := n.V.(map[string]any)
		if !ok {
			return n, nil
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return n, nil
		}
		file, pointer, _ := strings.Cut(ref, "#")
		target := n.URL
		if file != "" {
			base, err := url.Parse(n.URL)
			if err != nil {
				return n, err
			}
			rel, err := url.Parse(file)
			if err != nil {
				return n, fmt.Errorf("the $ref %q: %w", ref, err)
			}
			target = base.ResolveReference(rel).String()
		}
		doc, err := l.Load(target)
		if err != nil {
			return n, err
		}
		v, err := lookup(doc, pointer)
		if err != nil {
			return n, fmt.Errorf("the $ref %q: %w", ref, err)
		}
		n = Node{URL: target, Pointer: pointer, V: v}
	}
	return n, fmt.Errorf("the $refs of %s go round in circles", n.Ref())
}

func lookup(doc any, pointer string) (any, error) {
	v := doc
	if pointer == "" || pointer == "/" {
		return v, nil
	}
	for _, seg := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[seg]
			if !ok {
				return nil, fmt.Errorf("no %q", pointer)
			}
			v = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, fmt.Errorf("no %q", pointer)
			}
			v = x[i]
		default:
			return nil, fmt.Errorf("no %q", pointer)
		}
	}
	return v, nil
}

// Finding is a rule a value breaks, keyed for levels.
type Finding struct {
	Key, Message string
}

var printer = message.NewPrinter(language.English)

// Validate validates v against s. Each failure is keyed <prefix>.<keyword>
// (validation.message.payload.schema.required) and says where it is in
// what, like "payload $.status: value must be one of ...".
func Validate(s *jsonschema.Schema, v any, prefix, what string) []Finding {
	err := s.Validate(v)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []Finding{{Key: prefix, Message: err.Error()}}
	}
	var out []Finding
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "$"
			for _, seg := range e.InstanceLocation {
				loc += "." + seg
			}
			out = append(out, Finding{
				Key:     prefix + "." + Keyword(e),
				Message: fmt.Sprintf("%s %s: %s", what, loc, e.ErrorKind.LocalizedString(printer)),
			})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Message < out[j].Message })
	return out
}

// Keyword is the JSON Schema keyword a failure is keyed by.
func Keyword(e *jsonschema.ValidationError) string {
	switch e.ErrorKind.(type) {
	case *kind.Dependency:
		return "dependencies"
	case *kind.Not:
		return "not"
	case *kind.FalseSchema:
		return "false"
	case *kind.RefCycle:
		return "$ref"
	}
	if p := e.ErrorKind.KeywordPath(); len(p) > 0 {
		return p[0]
	}
	return "unknownError"
}

// Keywords are the keywords failures are keyed by, for the keys levels can
// be set on.
var Keywords = []string{
	"$ref", "additionalItems", "additionalProperties", "allOf", "anyOf", "const", "contains",
	"contentEncoding", "contentMediaType", "contentSchema", "dependencies", "dependentRequired", "enum",
	"exclusiveMaximum", "exclusiveMinimum", "false", "format", "maxContains", "maxItems", "maxLength",
	"maxProperties", "maximum", "minContains", "minItems", "minLength", "minProperties", "minimum",
	"multipleOf", "not", "oneOf", "pattern", "propertyNames", "required", "type", "uniqueItems", "unknownError",
}
