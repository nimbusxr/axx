package desktopcore

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/antchfx/xpath"
)

// sel is an id= or xpath= selector, for a control no name tells apart.
type sel struct {
	id   string
	expr *xpath.Expr
}

// selector is the selector a name stands for, if it is one.
func selector(name string) (*sel, bool) {
	switch {
	case strings.HasPrefix(name, "id="):
		return &sel{id: strings.TrimPrefix(name, "id=")}, true
	case strings.HasPrefix(name, "xpath="):
		expr, err := xpath.Compile(strings.TrimPrefix(name, "xpath="))
		if err != nil {
			return &sel{}, true // finds nothing; find says why
		}
		return &sel{expr: expr}, true
	}
	return nil, false
}

// find is the controls the selector finds in the tree.
func (s *sel) find(tree *Node) ([]Control, error) {
	var out []Control
	switch {
	case s.id != "":
		walkNodes(tree, func(n *Node) {
			if n.ID == s.id && n.Control != nil {
				out = append(out, n.Control)
			}
		})
	case s.expr != nil:
		it := s.expr.Select(&navigator{path: []*Node{tree}, attr: -1})
		for it.MoveNext() {
			if nav, ok := it.Current().(*navigator); ok && nav.attr < 0 {
				if n := nav.node(); n.Control != nil {
					out = append(out, n.Control)
				}
			}
		}
	default:
		return nil, fmt.Errorf("the xpath= selector is not an XPath expression")
	}
	return out, nil
}

func walkNodes(n *Node, visit func(*Node)) {
	if n == nil {
		return
	}
	visit(n)
	for _, k := range n.Children {
		walkNodes(k, visit)
	}
}

var notInName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// elementName is a role as an XML element's name: AT-SPI's "push button"
// is push_button.
func elementName(role string) string {
	n := notInName.ReplaceAllString(strings.TrimSpace(role), "_")
	if n == "" {
		return "unknown"
	}
	return n
}

// attrs are a node's attributes in xpath= selectors, in a fixed order:
// name, id, value, then the OS's own.
func attrs(n *Node) [][2]string {
	var out [][2]string
	for _, a := range [][2]string{{"name", n.Name}, {"id", n.ID}, {"value", n.Value}} {
		if a[1] != "" {
			out = append(out, a)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(n.Attrs)) {
		out = append(out, [2]string{k, n.Attrs[k]})
	}
	return out
}

// navigator walks a Node tree for the xpath package: path is the nodes
// from the root to the current one, attr the current attribute (-1 when on
// the element).
type navigator struct {
	path []*Node
	attr int
	root bool // on the document, above the tree's root
}

func (n *navigator) node() *Node { return n.path[len(n.path)-1] }

func (n *navigator) NodeType() xpath.NodeType {
	switch {
	case n.root:
		return xpath.RootNode
	case n.attr >= 0:
		return xpath.AttributeNode
	}
	return xpath.ElementNode
}

func (n *navigator) LocalName() string {
	if n.attr >= 0 {
		return attrs(n.node())[n.attr][0]
	}
	return elementName(n.node().Role)
}

func (n *navigator) Prefix() string { return "" }

func (n *navigator) Value() string {
	if n.attr >= 0 {
		return attrs(n.node())[n.attr][1]
	}
	return n.node().Name
}

func (n *navigator) Copy() xpath.NodeNavigator {
	c := *n
	c.path = slices.Clone(n.path)
	return &c
}

func (n *navigator) MoveToRoot() {
	n.path, n.attr, n.root = n.path[:1], -1, true
}

func (n *navigator) MoveToParent() bool {
	switch {
	case n.attr >= 0:
		n.attr = -1
		return true
	case n.root:
		return false
	case len(n.path) == 1:
		n.root = true
		return true
	}
	n.path = n.path[:len(n.path)-1]
	return true
}

func (n *navigator) MoveToNextAttribute() bool {
	if n.root || n.attr+1 >= len(attrs(n.node())) {
		return false
	}
	n.attr++
	return true
}

func (n *navigator) MoveToChild() bool {
	if n.attr >= 0 {
		return false
	}
	if n.root {
		n.root = false
		return true
	}
	kids := n.node().Children
	if len(kids) == 0 {
		return false
	}
	n.path = append(n.path, kids[0])
	return true
}

// siblings are the current node's parent's children, and its place among
// them.
func (n *navigator) siblings() ([]*Node, int) {
	if n.root || n.attr >= 0 || len(n.path) < 2 {
		return nil, -1
	}
	kids := n.path[len(n.path)-2].Children
	return kids, slices.Index(kids, n.node())
}

func (n *navigator) MoveToFirst() bool {
	kids, i := n.siblings()
	if i <= 0 {
		return false
	}
	n.path[len(n.path)-1] = kids[0]
	return true
}

func (n *navigator) MoveToNext() bool {
	kids, i := n.siblings()
	if i < 0 || i+1 >= len(kids) {
		return false
	}
	n.path[len(n.path)-1] = kids[i+1]
	return true
}

func (n *navigator) MoveToPrevious() bool {
	kids, i := n.siblings()
	if i <= 0 {
		return false
	}
	n.path[len(n.path)-1] = kids[i-1]
	return true
}

func (n *navigator) MoveTo(other xpath.NodeNavigator) bool {
	o, ok := other.(*navigator)
	if !ok || o.path[0] != n.path[0] {
		return false
	}
	n.path, n.attr, n.root = slices.Clone(o.path), o.attr, o.root
	return true
}

// Outline is the tree as a failure shows it: an element a line, its role
// and its attributes, as xpath= selectors name them.
func Outline(tree *Node) string {
	var b strings.Builder
	var write func(n *Node, depth int)
	write = func(n *Node, depth int) {
		if depth > 60 {
			return
		}
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString(elementName(n.Role))
		for _, a := range attrs(n) {
			fmt.Fprintf(&b, " %s=%q", a[0], wordSpace.ReplaceAllString(a[1], " "))
		}
		b.WriteByte('\n')
		for _, k := range n.Children {
			write(k, depth+1)
		}
	}
	write(tree, 0)
	return b.String()
}
