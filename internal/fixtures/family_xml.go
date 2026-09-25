package fixtures

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacoelho/xsd"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// xmlFamily is family: xml: record-shaped XML documents governed by an XSD.
// The value tree renders directly: a map entry is a child element, "@name"
// keys are attributes, "#text" is the text of an element that also carries
// attributes, a scalar is a text-only element and a list repeats the element.
// The root element is the XSD's single global element (options.root picks
// one when there are several). The oracle is an XML Schema 1.0 validator
// (pure Go), so missing required elements, wrong types and unknown elements
// fail at generate time with the validator's own messages. Adoption is not
// supported.
type xmlFamily struct{}

func (xmlFamily) Name() string { return "xml" }

func readXSD(baseDir, ref string) ([]byte, *xsd.Engine, error) {
	if err := checkRef(ref); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(baseDir, filepath.FromSlash(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, schemaError("cannot read XSD %s: %v", ref, err)
	}
	engine, err := xsd.Compile(xsd.File(path))
	if err != nil {
		return nil, nil, schemaError("cannot compile XSD %s: %v", ref, err)
	}
	return data, engine, nil
}

func xmlOracle(engine *xsd.Engine, data []byte, fixtureName string) error {
	if err := engine.Validate(bytes.NewReader(data)); err != nil {
		return genError("%s does not conform to the XSD: %v", fixtureName, err)
	}
	return nil
}

func (f xmlFamily) Expand(spec *Spec, baseDir string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	if err := requireSchema(spec, f.Name()); err != nil {
		return nil, err
	}
	schema, engine, err := readXSD(baseDir, spec.SchemaRef())
	if err != nil {
		return nil, err
	}
	root, err := xmlRootElement(spec, schema)
	if err != nil {
		return nil, err
	}
	for _, k := range spec.Defaults.Keys() {
		if !strings.Contains(k, ".") && !strings.HasPrefix(k, "@") {
			return nil, specError("%s: bare-name default '%s' needs a schema walker the xml family does not have - use a dotted path default", spec.SourceName, k)
		}
	}
	out := map[string]map[string][]byte{}
	for _, key := range spec.Fixtures.SortedKeys() {
		tree, err := resolveFixture(spec, key, ctx)
		if err != nil {
			return nil, err
		}
		applyDottedDefaults(spec.Defaults, tree)
		w := where(spec, key)
		var b strings.Builder
		b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		if err := xmlElement(&b, root, tree, 0, w); err != nil {
			return nil, err
		}
		data := []byte(b.String())
		if err := xmlOracle(engine, data, w); err != nil {
			return nil, err
		}
		out[key] = map[string][]byte{key + ".xml": data}
	}
	return out, nil
}

func (xmlFamily) Validate(data []byte, baseDir, schemaRef, fixtureName string) error {
	_, engine, err := readXSD(baseDir, schemaRef)
	if err != nil {
		return err
	}
	return xmlOracle(engine, data, fixtureName)
}

func (xmlFamily) LintFilePatterns(*Spec) []string { return nil }

func (xmlFamily) LintJSONPath(_ *Spec, p string) string { return p }

// xmlRootElement is the XSD's single global element, or options.root.
func xmlRootElement(spec *Spec, schema []byte) (string, error) {
	if declared := get(spec.Options, "root"); declared != nil {
		return valueOf(declared), nil
	}
	dec := xml.NewDecoder(bytes.NewReader(schema))
	var roots []string
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", schemaError("%s: cannot parse XSD for the root element: %v", spec.SourceName, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && t.Name.Local == "element" {
				for _, a := range t.Attr {
					if a.Name.Space == "" && a.Name.Local == "name" {
						roots = append(roots, a.Value)
					}
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	if len(roots) == 1 {
		return roots[0], nil
	}
	count := "no"
	if len(roots) > 0 {
		count = itoa(len(roots)) + " (" + javaListString(roots) + ")"
	}
	return "", specError("%s: the XSD declares %s global element(s) - set options.root to pick the document root", spec.SourceName, count)
}

// xmlElement renders deterministically: authored order, 2-space indent, LF.
func xmlElement(b *strings.Builder, name string, value any, depth int, where string) error {
	indent := strings.Repeat("  ", depth)
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if err := xmlElement(b, name, item, depth, where); err != nil {
				return err
			}
		}
		return nil
	}
	m, ok := value.(*jsonx.Object)
	if !ok {
		b.WriteString(indent + "<" + name + ">" + escapeXMLText(valueOf(value)) + "</" + name + ">\n")
		return nil
	}
	b.WriteString(indent + "<" + name)
	var text any
	hasText := false
	var children []string
	for _, k := range m.Keys() {
		v := get(m, k)
		switch {
		case strings.HasPrefix(k, "@"):
			if v == nil {
				return genError("%s: attribute '%s' of <%s> is null - omit it", where, k, name)
			}
			b.WriteString(" " + k[1:] + "=\"" + escapeXMLAttr(valueOf(v)) + "\"")
		case k == "#text":
			text, hasText = v, v != nil
		default:
			children = append(children, k)
		}
	}
	if !hasText && len(children) == 0 {
		b.WriteString("/>\n")
		return nil
	}
	b.WriteString(">")
	if hasText {
		if len(children) > 0 {
			return genError("%s: <%s> mixes #text with child elements - not supported", where, name)
		}
		b.WriteString(escapeXMLText(valueOf(text)) + "</" + name + ">\n")
		return nil
	}
	b.WriteString("\n")
	for _, k := range children {
		v := get(m, k)
		if v == nil {
			return genError("%s: element '%s' of <%s> is null - omit it (or use xsi:nil via attributes)", where, k, name)
		}
		if err := xmlElement(b, k, v, depth+1, where); err != nil {
			return err
		}
	}
	b.WriteString(indent + "</" + name + ">\n")
	return nil
}

// applyDottedDefaults fills absent dotted paths (order.@id works).
func applyDottedDefaults(defaults, tree *jsonx.Object) {
	for _, path := range defaults.Keys() {
		if !strings.Contains(path, ".") {
			continue
		}
		segs := strings.Split(path, ".")
		node := tree
		ok := true
		for _, s := range segs[:len(segs)-1] {
			next, isMap := get(node, s).(*jsonx.Object)
			if !isMap {
				ok = false
				break
			}
			node = next
		}
		last := segs[len(segs)-1]
		if ok && (!node.Has(last) || get(node, last) == nil) {
			node.Set(last, get(defaults, path))
		}
	}
}
