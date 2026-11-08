package appiumtest

import (
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// element is an element of a page source, which a locator finds as the drivers do: the
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

func (e *element) attr(name string) (string, bool) {
	for _, a := range e.attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

var androidBoundsRE = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)

// rect is where the element is: UiAutomator2's bounds, or XCUITest's frame.
func (e *element) rect() map[string]float64 {
	bounds, _ := e.attr("bounds")
	if m := androidBoundsRE.FindStringSubmatch(bounds); m != nil {
		n := func(i int) float64 { f, _ := strconv.ParseFloat(m[i], 64); return f }
		return map[string]float64{"x": n(1), "y": n(2), "width": n(3) - n(1), "height": n(4) - n(2)}
	}
	n := func(k string) float64 { v, _ := e.attr(k); f, _ := strconv.ParseFloat(v, 64); return f }
	return map[string]float64{"x": n("x"), "y": n("y"), "width": n("width"), "height": n("height")}
}

// condition is one of a locator's conditions: an attribute of the element, or of one of its
// descendants, has a value.
type condition struct {
	attr, value string
	descendant  bool
}

// first is the first element, in the source's order, that a locator finds; nil when it finds none,
// or is not a locator the packs build. The packs find an element by its name and conditions on
// its attributes, as an XPath does: //name[@a="v" and @b='w' and .//*[@c=concat("x", '"', "y")]].
// The fake reads those itself rather than handing what a client sent to an XPath engine; the
// tests hold it to what an XPath engine finds (xpath_test.go).
func first(src, locator string) *element {
	root := tree(src)
	if root == nil {
		return nil
	}
	return find(root, locator)
}

// find is the first element under root, in the source's order, that a locator finds.
func find(root *element, locator string) *element {
	name, conds, ok := parseLocator(locator)
	if !ok {
		return nil
	}
	var found *element
	var walk func(*element)
	walk = func(e *element) {
		for _, c := range e.children {
			if found != nil {
				return
			}
			if c.name == name && c.meets(conds) {
				found = c
				return
			}
			walk(c)
		}
	}
	walk(root)
	return found
}

// meets reports whether the element meets every condition.
func (e *element) meets(conds []condition) bool {
	for _, c := range conds {
		if !c.descendant {
			if v, ok := e.attr(c.attr); !ok || v != c.value {
				return false
			}
			continue
		}
		if !e.hasDescendant(c) {
			return false
		}
	}
	return true
}

func (e *element) hasDescendant(c condition) bool {
	for _, d := range e.children {
		if v, ok := d.attr(c.attr); ok && v == c.value {
			return true
		}
		if d.hasDescendant(c) {
			return true
		}
	}
	return false
}

var locatorRE = regexp.MustCompile(`^//([A-Za-z_][\w.\-]*)\[(.*)\]$`)

// parseLocator reads a locator of the packs' shape: its element name and its conditions.
func parseLocator(locator string) (string, []condition, bool) {
	m := locatorRE.FindStringSubmatch(locator)
	if m == nil {
		return "", nil, false
	}
	p := &parser{s: m[2]}
	var conds []condition
	for {
		c, ok := p.condition()
		if !ok {
			return "", nil, false
		}
		conds = append(conds, c)
		if p.done() {
			return m[1], conds, true
		}
		if !p.take(" and ") {
			return "", nil, false
		}
	}
}

// parser reads a locator's conditions.
type parser struct {
	s string
	i int
}

func (p *parser) done() bool { return p.i == len(p.s) }

func (p *parser) take(s string) bool {
	if strings.HasPrefix(p.s[p.i:], s) {
		p.i += len(s)
		return true
	}
	return false
}

var attrRE = regexp.MustCompile(`^@([\w\-]+)=`)

// condition reads @attr=literal, or .//*[@attr=literal].
func (p *parser) condition() (condition, bool) {
	descendant := p.take(".//*[")
	m := attrRE.FindStringSubmatch(p.s[p.i:])
	if m == nil {
		return condition{}, false
	}
	p.i += len(m[0])
	value, ok := p.literal()
	if !ok || (descendant && !p.take("]")) {
		return condition{}, false
	}
	return condition{attr: m[1], value: value, descendant: descendant}, true
}

// literal reads an XPath 1.0 string literal: in double or single quotes, or concat() of them.
func (p *parser) literal() (string, bool) {
	if p.take("concat(") {
		var b strings.Builder
		for {
			part, ok := p.quoted()
			if !ok {
				return "", false
			}
			b.WriteString(part)
			if p.take(")") {
				return b.String(), true
			}
			if !p.take(", ") {
				return "", false
			}
		}
	}
	return p.quoted()
}

func (p *parser) quoted() (string, bool) {
	if p.done() || (p.s[p.i] != '"' && p.s[p.i] != '\'') {
		return "", false
	}
	quote := p.s[p.i]
	end := strings.IndexByte(p.s[p.i+1:], quote)
	if end < 0 {
		return "", false
	}
	v := p.s[p.i+1 : p.i+1+end]
	p.i += end + 2
	return v, true
}
