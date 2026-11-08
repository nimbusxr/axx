package appiumtest

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/antchfx/xpath"

	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// The fake finds, for every locator of the packs' shapes, the element an XPath engine finds, on
// the couriers' apps' screens as UiAutomator2 and XCUITest read them: by bounds or frame alone,
// and with what the element shows (its own text, description, label or value, or a
// descendant's), quoted any way XPath quotes; and none where the element shows something else.
func TestTheFakeFindsWhatXPathFinds(t *testing.T) {
	sources, _ := filepath.Glob("../../../*/testdata/courier/*.xml")
	if len(sources) < 10 {
		t.Fatalf("found %d screens", len(sources))
	}
	sources = append(sources, "") // a screen of quotes, below
	checked := 0
	for _, path := range sources {
		src := `<hierarchy><android.widget.TextView text="Hanna's &quot;fragile&quot; parcel" bounds="[0,0][10,10]"/></hierarchy>`
		if path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			src = string(b)
		}
		root := tree(src)
		var all []*element
		var walk func(*element)
		walk = func(e *element) {
			for _, c := range e.children {
				all = append(all, c)
				walk(c)
			}
		}
		walk(root)
		for _, e := range all {
			for _, locator := range locators(e) {
				if got, want := find(root, locator), byXPath(t, root, locator); got != want {
					t.Errorf("%s: %s: the fake found element %d, XPath %d", filepath.Base(path), locator, slices.Index(all, got), slices.Index(all, want))
				}
				checked++
			}
		}
	}
	if checked < 200 {
		t.Errorf("checked %d locators", checked)
	}
}

// locators are the locators the packs build for an element, of its bounds or frame, and of what
// it shows; and one of something it does not show.
func locators(e *element) []string {
	var where string
	if b, ok := e.attr("bounds"); ok {
		where = "@bounds=" + appium.XPathString(b)
	} else if x, ok := e.attr("x"); ok {
		y, _ := e.attr("y")
		w, _ := e.attr("width")
		h, _ := e.attr("height")
		where = `@x="` + x + `" and @y="` + y + `" and @width="` + w + `" and @height="` + h + `"`
	} else {
		return nil
	}
	base := "//" + e.name + "[" + where
	out := []string{base + "]", base + ` and @text="not shown"]`}
	for _, attr := range []string{"text", "content-desc", "label", "value"} {
		if v, ok := e.attr(attr); ok && v != "" {
			out = append(out, base+" and @"+attr+"="+appium.XPathString(v)+"]", base+" and .//*[@"+attr+"="+appium.XPathString(v)+"]]")
		}
	}
	for _, c := range e.children {
		for _, attr := range []string{"text", "label"} {
			if v, ok := c.attr(attr); ok && v != "" {
				out = append(out, base+" and .//*[@"+attr+"="+appium.XPathString(v)+"]]")
				// As a person writes it in an xpath= selector, in single quotes.
				if !containsQuote(v) {
					out = append(out, base+" and .//*[@"+attr+"='"+v+"']]")
				}
			}
		}
	}
	return out
}

func containsQuote(s string) bool { return slices.Contains([]rune(s), '\'') }

// byXPath is the first element an XPath engine finds.
func byXPath(t *testing.T, root *element, locator string) *element {
	t.Helper()
	expr, err := xpath.Compile(locator)
	if err != nil {
		t.Fatalf("%s: %v", locator, err)
	}
	it := expr.Select(&navigator{path: []*element{root}, attr: -1})
	if !it.MoveNext() {
		return nil
	}
	n := it.Current().(*navigator)
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
