package jsonrpc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/schemadoc"
)

// document is an OpenRPC document, reduced to what checking calls needs.
type document struct {
	source  string
	methods map[string]*method
}

type method struct {
	name      string
	params    []*param
	structure string             // by-name, by-position or either
	result    *jsonschema.Schema // nil: any result
	errors    map[int]bool       // the errors it declares
}

type param struct {
	name     string
	required bool
	schema   *jsonschema.Schema
	text     bool // its schema's type is string
}

// readDocument reads an OpenRPC document at u through l.
func readDocument(l *schemadoc.Loader, source, u string) (*document, error) {
	root, err := l.Root(u)
	if err != nil {
		return nil, err
	}
	if v := root.Str("openrpc"); !strings.HasPrefix(v, "1.") {
		return nil, fmt.Errorf("%s is not an OpenRPC 1.x document (openrpc: %q)", source, v)
	}
	c := l.Compiler(jsonschema.Draft7)
	d := &document{source: source, methods: map[string]*method{}}
	for _, mn := range root.Get("methods").Items() {
		m, err := readMethod(l, c, mn)
		if err != nil {
			return nil, err
		}
		d.methods[m.name] = m
	}
	return d, nil
}

func readMethod(l *schemadoc.Loader, c *jsonschema.Compiler, n schemadoc.Node) (*method, error) {
	n, err := l.Deref(n)
	if err != nil {
		return nil, err
	}
	m := &method{name: n.Str("name"), structure: n.Str("paramStructure")}
	if m.structure == "" {
		m.structure = "either"
	}
	for _, pn := range n.Get("params").Items() {
		pn, err := l.Deref(pn)
		if err != nil {
			return nil, fmt.Errorf("the method %s: %w", m.name, err)
		}
		p := &param{name: pn.Str("name")}
		p.required, _ = pn.Get("required").V.(bool)
		if p.schema, err = compile(c, pn.Get("schema")); err != nil {
			return nil, fmt.Errorf("the %s param of the method %s: %w", p.name, m.name, err)
		}
		if sn, err := l.Deref(pn.Get("schema")); err == nil {
			p.text = sn.Str("type") == "string"
		}
		m.params = append(m.params, p)
	}
	if rn := n.Get("result"); rn.V != nil {
		rn, err := l.Deref(rn)
		if err != nil {
			return nil, fmt.Errorf("the result of the method %s: %w", m.name, err)
		}
		if m.result, err = compile(c, rn.Get("schema")); err != nil {
			return nil, fmt.Errorf("the result of the method %s: %w", m.name, err)
		}
	}
	for _, en := range n.Get("errors").Items() {
		en, err := l.Deref(en)
		if err != nil {
			return nil, fmt.Errorf("the errors of the method %s: %w", m.name, err)
		}
		if code, ok := number(en.Get("code").V); ok {
			if m.errors == nil {
				m.errors = map[int]bool{}
			}
			m.errors[code] = true
		}
	}
	return m, nil
}

func compile(c *jsonschema.Compiler, n schemadoc.Node) (*jsonschema.Schema, error) {
	if n.V == nil {
		return nil, nil
	}
	return c.Compile(n.Ref())
}

func number(v any) (int, bool) {
	switch x := v.(type) {
	case json.Number:
		i, err := strconv.Atoi(x.String())
		return i, err == nil
	case float64:
		return int(x), true
	}
	return 0, false
}

type finding = schemadoc.Finding

// conform gives a table's params by name the types of their schemas: the
// text of a number for a param whose type is string.
// textParam reports whether a path of a table's params is a param whose
// schema's type is string.
func (d *document) textParam(name, path string) bool {
	m, ok := d.methods[name]
	if !ok || strings.ContainsAny(path, ".[") {
		return false
	}
	for _, p := range m.params {
		if p.name == path {
			return p.text
		}
	}
	return false
}

func (d *document) conform(name string, params map[string]any) {
	m, ok := d.methods[name]
	if !ok {
		return
	}
	for _, p := range m.params {
		if !p.text {
			continue
		}
		switch v := params[p.name].(type) {
		case json.Number:
			params[p.name] = v.String()
		case bool:
			params[p.name] = strconv.FormatBool(v)
		}
	}
}

// checkParams checks a call's params (nil, an object or an array) against
// its method.
func (d *document) checkParams(name string, params any) []finding {
	m, ok := d.methods[name]
	if !ok {
		return []finding{d.unknown(name)}
	}
	var out []finding
	switch ps := params.(type) {
	case map[string]any:
		if m.structure == "by-position" {
			out = append(out, finding{Key: "validation.params.structure", Message: fmt.Sprintf("%s takes its params by position, in an array", name)})
		}
		declared := map[string]bool{}
		for _, p := range m.params {
			declared[p.name] = true
			v, present := ps[p.name]
			out = append(out, p.check(present, v)...)
		}
		var extra []string
		for k := range ps {
			if !declared[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		for _, k := range extra {
			out = append(out, finding{Key: "validation.params.unknown", Message: fmt.Sprintf("%s has no param %q; it has %s", name, k, m.paramNames())})
		}
	case []any:
		if m.structure == "by-name" {
			out = append(out, finding{Key: "validation.params.structure", Message: fmt.Sprintf("%s takes its params by name, in an object", name)})
		}
		for i, p := range m.params {
			var v any
			present := i < len(ps)
			if present {
				v = ps[i]
			}
			out = append(out, p.check(present, v)...)
		}
		if len(ps) > len(m.params) {
			out = append(out, finding{Key: "validation.params.unknown", Message: fmt.Sprintf("%s takes %d params, not %d", name, len(m.params), len(ps))})
		}
	default: // no params
		for _, p := range m.params {
			out = append(out, p.check(false, nil)...)
		}
	}
	return out
}

func (p *param) check(present bool, v any) []finding {
	if !present {
		if p.required {
			return []finding{{Key: "validation.params.missing", Message: fmt.Sprintf("the required param %q is missing", p.name)}}
		}
		return nil
	}
	if p.schema == nil {
		return nil
	}
	return schemadoc.Validate(p.schema, v, "validation.params.schema", "param "+p.name)
}

func (m *method) paramNames() string {
	if len(m.params) == 0 {
		return "none"
	}
	names := make([]string, len(m.params))
	for i, p := range m.params {
		names[i] = p.name
	}
	return strings.Join(names, ", ")
}

// checkResult checks a call's result against its method.
func (d *document) checkResult(name string, result any) []finding {
	m, ok := d.methods[name]
	if !ok || m.result == nil {
		return nil
	}
	return schemadoc.Validate(m.result, result, "validation.result.schema", "result")
}

// checkError checks that a call's error is one its method declares, or
// one JSON-RPC itself defines.
func (d *document) checkError(name string, code int) []finding {
	m, ok := d.methods[name]
	if !ok || protocolError(code) || m.errors[code] {
		return nil
	}
	declared := make([]string, 0, len(m.errors))
	for c := range m.errors {
		declared = append(declared, strconv.Itoa(c))
	}
	sort.Strings(declared)
	what := "it declares none"
	if len(declared) > 0 {
		what = "it declares " + strings.Join(declared, ", ")
	}
	return []finding{{Key: "validation.error.unknown", Message: fmt.Sprintf("%s answered the error %d, which %s does not declare: %s", name, code, d.source, what)}}
}

// protocolError reports whether code is one of JSON-RPC's own errors:
// -32700, -32600 to -32603, and the servers' -32000 to -32099.
func protocolError(code int) bool {
	return code == -32700 || (code >= -32603 && code <= -32600) || (code >= -32099 && code <= -32000)
}

func (d *document) unknown(name string) finding {
	msg := fmt.Sprintf("%s has no method %s", d.source, name)
	names := make([]string, 0, len(d.methods))
	for n := range d.methods {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 0 {
		msg += "; it has " + strings.Join(names, ", ")
	}
	return finding{Key: "validation.method.unknown", Message: msg}
}

// levelKeys are the keys OpenRPC levels can be set on.
var levelKeys = oaslevel.NewKeys([]string{
	"validation.method.unknown",
	"validation.params.structure",
	"validation.params.missing",
	"validation.params.unknown",
	"validation.params.schema.{keyword}",
	"validation.result.schema.{keyword}",
	"validation.error.unknown",
}, map[string][]string{"{keyword}": schemadoc.Keywords}, nil).Named("OpenRPC", "validation.params")
