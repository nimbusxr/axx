package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// schemaOutline writes a JSON Schema as the keys a file may have, nested,
// each with the first sentence of its description: what an agent needs to
// write the file, at a fraction of the schema's size.
func schemaOutline(w io.Writer, schema []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(schema, &root); err != nil {
		return err
	}
	defs := map[string]json.RawMessage{}
	if raw, ok := root["$defs"]; ok {
		_ = json.Unmarshal(raw, &defs)
	}
	o := outliner{w: w, defs: defs}
	o.node(schema, 0)
	return o.err
}

type outliner struct {
	w    io.Writer
	defs map[string]json.RawMessage
	err  error
}

// maxOutlineDepth keeps the outline to the keys people write.
const maxOutlineDepth = 4

type schemaNode struct {
	Ref                  string            `json:"$ref"`
	Description          string            `json:"description"`
	Type                 json.RawMessage   `json:"type"`
	Properties           json.RawMessage   `json:"properties"`
	AdditionalProperties json.RawMessage   `json:"additionalProperties"`
	Items                json.RawMessage   `json:"items"`
	OneOf                []json.RawMessage `json:"oneOf"`
}

func (o *outliner) resolve(raw json.RawMessage) schemaNode {
	var n schemaNode
	_ = json.Unmarshal(raw, &n)
	for depth := 0; n.Ref != "" && depth < 8; depth++ {
		desc := n.Description
		def, ok := o.defs[strings.TrimPrefix(n.Ref, "#/$defs/")]
		if !ok {
			break
		}
		n = schemaNode{}
		_ = json.Unmarshal(def, &n)
		if desc != "" {
			n.Description = desc
		}
	}
	return n
}

// node writes the keys of the object raw describes, at depth.
func (o *outliner) node(raw json.RawMessage, depth int) {
	n := o.resolve(raw)
	if len(n.Properties) > 0 {
		var props map[string]json.RawMessage
		_ = json.Unmarshal(n.Properties, &props)
		for _, k := range orderedKeys(n.Properties) {
			o.key(k, props[k], depth)
		}
	}
	if add := o.resolve(n.AdditionalProperties); len(add.Properties) > 0 && depth < maxOutlineDepth {
		o.line(depth, "<name>:", "")
		o.node(n.AdditionalProperties, depth+1)
	}
}

func (o *outliner) key(k string, raw json.RawMessage, depth int) {
	n := o.resolve(raw)
	kind := ""
	switch {
	case len(n.Properties) > 0:
	case len(n.AdditionalProperties) > 0 && !bytes.Equal(n.AdditionalProperties, []byte("false")):
		kind = " (map)"
	case len(n.Items) > 0:
		kind = " (list)"
	}
	o.line(depth, k+":"+kind, firstSentence(n.Description))
	if depth+1 < maxOutlineDepth {
		o.node(raw, depth+1)
	}
}

func (o *outliner) line(depth int, key, desc string) {
	if o.err != nil {
		return
	}
	text := strings.Repeat("  ", depth) + key
	if desc != "" {
		text += " " + desc
	}
	_, o.err = fmt.Fprintln(o.w, text)
}

// firstSentence is a description's first sentence, on one line ("e.g." and
// "i.e." end none).
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for from := 0; ; {
		i := strings.Index(s[from:], ". ")
		if i < 0 {
			return s
		}
		end := from + i + 1
		if !strings.HasSuffix(s[:end], "e.g.") && !strings.HasSuffix(s[:end], "i.e.") {
			return s[:end]
		}
		from = end
	}
}

// orderedKeys are the keys of a JSON object in the order it lists them.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}
