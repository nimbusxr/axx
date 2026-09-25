package avrojson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// node is a parsed JSON value that keeps what Avro's JSON decoder observes
// and encoding/json would lose: member order, repeated keys, and whether a
// number was written as an integer or a decimal literal (Jackson's
// VALUE_NUMBER_INT and VALUE_NUMBER_FLOAT).
type node struct {
	kind    nodeKind
	b       bool
	s       string // string value or number literal
	members []member
	items   []*node
}

type member struct {
	key string
	val *node
}

type nodeKind uint8

const (
	jNull nodeKind = iota
	jBool
	jInt
	jFloat
	jString
	jObject
	jArray
)

func (k nodeKind) String() string {
	switch k {
	case jNull:
		return "null"
	case jBool:
		return "boolean"
	case jInt, jFloat:
		return "number"
	case jString:
		return "string"
	case jObject:
		return "object"
	}
	return "array"
}

// describe names a JSON value in messages: `string "x"`, `number 1.5`.
func (n *node) describe() string {
	switch n.kind {
	case jNull:
		return "null"
	case jBool:
		return fmt.Sprintf("boolean %t", n.b)
	case jInt, jFloat:
		return "number " + n.s
	case jString:
		s := n.s
		if len([]rune(s)) > 40 {
			s = string([]rune(s)[:40]) + "…"
		}
		b, _ := json.Marshal(s)
		return "string " + string(b)
	case jObject:
		return "an object"
	}
	return "an array"
}

// first returns the first member named key, as Avro's decoder reads records.
func (n *node) first(key string) (*node, bool) {
	for _, m := range n.members {
		if m.key == key {
			return m.val, true
		}
	}
	return nil, false
}

// parseJSON reads the first JSON value of data strictly (as Jackson does by
// default); anything after it is ignored, like Avro's decoder ignores it.
func parseJSON(data []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := parseValue(dec)
	if err != nil {
		if errors.Is(err, io.EOF) {
			if len(bytes.TrimSpace(data)) == 0 {
				return nil, &Error{Path: "$", Msg: "empty input: expected a JSON value"}
			}
			return nil, &Error{Path: "$", Msg: "invalid JSON: unexpected end of input"}
		}
		return nil, &Error{Path: "$", Msg: fmt.Sprintf("invalid JSON at offset %d: %v", dec.InputOffset(), err)}
	}
	return n, nil
}

func parseValue(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return parseToken(dec, tok)
}

func parseToken(dec *json.Decoder, tok json.Token) (*node, error) {
	switch t := tok.(type) {
	case nil:
		return &node{kind: jNull}, nil
	case bool:
		return &node{kind: jBool, b: t}, nil
	case json.Number:
		k := jInt
		if strings.ContainsAny(string(t), ".eE") {
			k = jFloat
		}
		return &node{kind: k, s: string(t)}, nil
	case string:
		return &node{kind: jString, s: t}, nil
	case json.Delim:
		switch t {
		case '{':
			n := &node{kind: jObject}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.members = append(n.members, member{key: key, val: v})
			}
			if _, err := dec.Token(); err != nil { // '}'
				return nil, err
			}
			return n, nil
		case '[':
			n := &node{kind: jArray}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, v)
			}
			if _, err := dec.Token(); err != nil { // ']'
				return nil, err
			}
			return n, nil
		}
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}
