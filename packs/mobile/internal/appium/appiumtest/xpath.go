package appiumtest

import (
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/antchfx/xpath"
)

// element is an element of a page source, which an XPath finds as the drivers do: the
// UiAutomator2 server and WebDriverAgent evaluate a locator against the screen's XML.
type element struct {
	name     string
	attrs    []xml.Attr
	children []*element
}

// tree is a page source's elements under a document root; nil when it is not XML.
func tree(src string) *element {
	root := &element{}
	stack := []*element{root}
	dec := xml.NewDecoder(strings.NewReader(src))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return root
		}
		if err != nil {
			return nil
		}
		switch t := tok.(type) {
		case xml.StartElement:
			e := &element{name: t.Name.Local, attrs: slices.Clone(t.Attr)}
			top := stack[len(stack)-1]
			top.children = append(top.children, e)
			stack = append(stack, e)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

func (e *element) attr(name string) string {
	for _, a := range e.attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

var androidBoundsRE = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)

// rect is where the element is: UiAutomator2's bounds, or XCUITest's frame.
func (e *element) rect() map[string]float64 {
	if m := androidBoundsRE.FindStringSubmatch(e.attr("bounds")); m != nil {
		n := func(i int) float64 { f, _ := strconv.ParseFloat(m[i], 64); return f }
		return map[string]float64{"x": n(1), "y": n(2), "width": n(3) - n(1), "height": n(4) - n(2)}
	}
	n := func(k string) float64 { f, _ := strconv.ParseFloat(e.attr(k), 64); return f }
	return map[string]float64{"x": n("x"), "y": n("y"), "width": n("width"), "height": n("height")}
}

// first is the first element of the source an XPath finds; nil when it finds none, or is no XPath.
func first(src, locator string) *element {
	root := tree(src)
	expr, err := xpath.Compile(locator)
	if root == nil || err != nil {
		return nil
	}
	it := expr.Select(&navigator{path: []*element{root}, attr: -1})
	if !it.MoveNext() {
		return nil
	}
	n, _ := it.Current().(*navigator)
	if n == nil || len(n.path) < 2 {
		return nil
	}
	return n.path[len(n.path)-1]
}

// navigator walks a source's elements for the xpath package: path is the elements from the
// document root to the current one, attr the current attribute (-1 on the element).
type navigator struct {
	path []*element
	attr int
}

func (n *navigator) node() *element { return n.path[len(n.path)-1] }

func (n *navigator) NodeType() xpath.NodeType {
	switch {
	case len(n.path) == 1:
		return xpath.RootNode
	case n.attr >= 0:
		return xpath.AttributeNode
	}
	return xpath.ElementNode
}

func (n *navigator) LocalName() string {
	if n.attr >= 0 {
		return n.node().attrs[n.attr].Name.Local
	}
	return n.node().name
}

func (n *navigator) Prefix() string { return "" }

func (n *navigator) Value() string {
	if n.attr >= 0 {
		return n.node().attrs[n.attr].Value
	}
	return ""
}

func (n *navigator) Copy() xpath.NodeNavigator {
	return &navigator{path: slices.Clone(n.path), attr: n.attr}
}

func (n *navigator) MoveToRoot() { n.path, n.attr = n.path[:1], -1 }

func (n *navigator) MoveToParent() bool {
	switch {
	case n.attr >= 0:
		n.attr = -1
	case len(n.path) == 1:
		return false
	default:
		n.path = n.path[:len(n.path)-1]
	}
	return true
}

func (n *navigator) MoveToNextAttribute() bool {
	if len(n.path) == 1 || n.attr+1 >= len(n.node().attrs) {
		return false
	}
	n.attr++
	return true
}

func (n *navigator) MoveToChild() bool {
	if n.attr >= 0 || len(n.node().children) == 0 {
		return false
	}
	n.path = append(n.path, n.node().children[0])
	return true
}

// siblings are the current element's parent's children, and its place among them.
func (n *navigator) siblings() ([]*element, int) {
	if n.attr >= 0 || len(n.path) < 2 {
		return nil, -1
	}
	kids := n.path[len(n.path)-2].children
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
	n.path, n.attr = slices.Clone(o.path), o.attr
	return true
}
