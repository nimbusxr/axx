package jyaml

import (
	"errors"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// NodeKind is what a [Node] holds.
type NodeKind byte

// The kinds of [Node].
const (
	ScalarNode NodeKind = iota + 1
	MappingNode
	SequenceNode
)

// Node is one value of a YAML document as [Unmarshal] reads it, with where
// it starts, for tools that point at a value in the file. A scalar's Value is
// typed as Unmarshal types it (nil for null); a mapping keeps every entry in
// order, repeated keys included, and an alias is a copy of its anchor's
// nodes placed at the alias.
type Node struct {
	Kind  NodeKind
	Value any
	Keys  []string // a mapping's keys
	Kids  []*Node  // a mapping's values, in Keys order, or a sequence's elements
	// Line and Column are 1-based.
	Line, Column int
}

// UnmarshalNodes reads every document of data the way Unmarshal reads the
// first, keeping each value's position. Empty documents are left out. A
// syntax error is an *Error with its position.
func UnmarshalNodes(data []byte) ([]*Node, error) {
	f, err := parser.ParseBytes(data, 0, parser.AllowDuplicateMapKey())
	if err != nil {
		var syntax interface {
			GetMessage() string
			GetToken() *token.Token
		}
		if errors.As(err, &syntax) {
			return nil, errAt(syntax.GetToken(), "%s", syntax.GetMessage())
		}
		return nil, err
	}
	var docs []*Node
	for _, doc := range f.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		d := &nodeDecoder{decoder: decoder{anchors: map[string]any{}}, anchors: map[string]*Node{}}
		n, err := d.node(doc.Body)
		if err != nil {
			return nil, err
		}
		if n != nil {
			docs = append(docs, n)
		}
	}
	return docs, nil
}

type nodeDecoder struct {
	decoder
	anchors map[string]*Node
}

func at(n ast.Node, node *Node) *Node {
	if tk := n.GetToken(); tk != nil && tk.Position != nil {
		node.Line, node.Column = tk.Position.Line, tk.Position.Column
	}
	return node
}

func (d *nodeDecoder) node(n ast.Node) (*Node, error) {
	switch x := n.(type) {
	case nil, *ast.CommentGroupNode, *ast.CommentNode:
		return nil, nil
	case *ast.DocumentNode:
		return d.node(x.Body)
	case *ast.MappingNode:
		m := at(x, &Node{Kind: MappingNode})
		for _, mv := range x.Values {
			if err := d.entry(m, mv); err != nil {
				return nil, err
			}
		}
		return m, nil
	case *ast.MappingValueNode:
		m := at(x, &Node{Kind: MappingNode})
		return m, d.entry(m, x)
	case *ast.SequenceNode:
		s := at(x, &Node{Kind: SequenceNode})
		for _, v := range x.Values {
			e, err := d.node(v)
			if err != nil {
				return nil, err
			}
			if e == nil {
				e = at(v, &Node{Kind: ScalarNode})
			}
			s.Kids = append(s.Kids, e)
		}
		return s, nil
	case *ast.AnchorNode:
		v, err := d.node(x.Value)
		if err != nil {
			return nil, err
		}
		if name := anchorName(x.Name); name != "" && v != nil {
			d.anchors[name] = v
		}
		return v, nil
	case *ast.AliasNode:
		name := anchorName(x.Value)
		v, ok := d.anchors[name]
		if !ok {
			return nil, errAt(x.GetToken(), "unknown alias *%s", name)
		}
		return copyAt(v, at(x, &Node{})), nil
	case *ast.TagNode:
		switch x.Value.(type) {
		case *ast.MappingNode, *ast.SequenceNode, *ast.MappingValueNode, nil:
			return d.node(x.Value) // Jackson ignores tags on collections
		}
	}
	// A scalar, typed as Unmarshal types it.
	v, err := d.decoder.node(n)
	if err != nil {
		return nil, err
	}
	return at(n, &Node{Kind: ScalarNode, Value: v}), nil
}

func (d *nodeDecoder) entry(m *Node, mv *ast.MappingValueNode) error {
	key, err := d.key(mv.Key)
	if err != nil {
		return err
	}
	v, err := d.node(mv.Value)
	if err != nil {
		return err
	}
	if v == nil { // an empty value is null
		v = &Node{Kind: ScalarNode}
		if mv.Value != nil {
			at(mv.Value, v)
		} else {
			at(mv, v)
		}
	}
	m.Keys = append(m.Keys, key)
	m.Kids = append(m.Kids, v)
	return nil
}

// copyAt copies n, every copied node placed at pos.
func copyAt(n, pos *Node) *Node {
	c := &Node{Kind: n.Kind, Value: deepCopy(n.Value), Keys: append([]string(nil), n.Keys...), Line: pos.Line, Column: pos.Column}
	for _, k := range n.Kids {
		c.Kids = append(c.Kids, copyAt(k, pos))
	}
	return c
}
