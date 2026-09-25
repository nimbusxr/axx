package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// nodeJSON renders an example value (a YAML or JSON node of the
// specification) as compact JSON text, keeping member order and the
// original number literals ("1.0" stays "1.0"). A string example is
// returned as-is, since it may be a non-JSON payload.
func nodeJSON(n *yaml.Node) (string, error) {
	for n != nil && (n.Kind == yaml.DocumentNode || n.Kind == yaml.AliasNode) {
		switch {
		case n.Kind == yaml.AliasNode:
			n = n.Alias
		case len(n.Content) > 0:
			n = n.Content[0]
		default:
			n = nil
		}
	}
	if n == nil {
		return "null", nil
	}
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return n.Value, nil
	}
	var b bytes.Buffer
	if err := writeNode(&b, n, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeNode(b *bytes.Buffer, n *yaml.Node, depth int) error {
	if depth > 1000 {
		return fmt.Errorf("example nests too deeply")
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			b.WriteString("null")
			return nil
		}
		return writeNode(b, n.Content[0], depth+1)
	case yaml.AliasNode:
		if n.Alias == nil {
			b.WriteString("null")
			return nil
		}
		return writeNode(b, n.Alias, depth+1)
	case yaml.MappingNode:
		b.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, n.Content[i].Value)
			b.WriteByte(':')
			if err := writeNode(b, n.Content[i+1], depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		b.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeNode(b, c, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	}
	writeScalar(b, n)
	return nil
}

func writeScalar(b *bytes.Buffer, n *yaml.Node) {
	v := n.Value
	switch n.ShortTag() {
	case "!!null":
		b.WriteString("null")
		return
	case "!!bool":
		if strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return
	case "!!int":
		if jsonNumber.MatchString(v) {
			b.WriteString(v)
			return
		}
		if i, err := strconv.ParseInt(strings.ReplaceAll(v, "_", ""), 0, 64); err == nil {
			b.WriteString(strconv.FormatInt(i, 10))
			return
		}
	case "!!float":
		if jsonNumber.MatchString(v) {
			b.WriteString(v)
			return
		}
		if f, err := strconv.ParseFloat(strings.ReplaceAll(v, "_", ""), 64); err == nil {
			if s, err := json.Marshal(f); err == nil {
				b.Write(s)
				return
			}
		}
	}
	writeString(b, v)
}

func writeString(b *bytes.Buffer, s string) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Write(bytes.TrimRight(buf.Bytes(), "\n"))
}
