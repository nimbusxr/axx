package mobileandroid

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

var boundsRE = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)

// parseSource reads UiAutomator2's page source into a screen. Jetpack
// Compose and Android's views both give controls a class a role tells:
// android.widget.Button, EditText, CheckBox, Switch...
func parseSource(src string) (*mobilecore.Screen, error) {
	dec := xml.NewDecoder(strings.NewReader(src))
	s := &mobilecore.Screen{}
	var stack []*mobilecore.Node
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the page source is not UiAutomator2's XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "hierarchy" {
				s.Size = mobilecore.Rect{Width: attrFloat(t.Attr, "width"), Height: attrFloat(t.Attr, "height")}
				stack = append(stack, nil)
				continue
			}
			n := nodeOf(t)
			bounds := attr(t.Attr, "bounds")
			n.Bounds = parseBounds(bounds)
			if len(stack) > 0 {
				if p := stack[len(stack)-1]; p != nil {
					n.Parent = p
					p.Children = append(p.Children, n)
				}
			}
			n.Using, n.Value = "xpath", fmt.Sprintf(`//%s[@bounds=%q]`, xpathName(n.Class, t.Name.Local), bounds)
			s.Nodes = append(s.Nodes, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	for _, n := range s.Nodes {
		n.Role = roleOf(n)
	}
	return s, nil
}

// nodeOf is an element of the source as a node, its role still unknown.
func nodeOf(t xml.StartElement) *mobilecore.Node {
	a := t.Attr
	class := attr(a, "class")
	if class == "" {
		class = t.Name.Local
	}
	n := &mobilecore.Node{
		Class:     class,
		Text:      attr(a, "text"),
		Label:     attr(a, "content-desc"),
		Hint:      attr(a, "hint"),
		ID:        attr(a, "resource-id"),
		Enabled:   attr(a, "enabled") == "true",
		Displayed: attr(a, "displayed") != "false",
		Clickable: attr(a, "clickable") == "true",
		Checked:   attr(a, "checked") == "true",
		Selected:  attr(a, "selected") == "true",
		Password:  attr(a, "password") == "true",
	}
	if attr(a, "showing-hint") == "true" && n.Hint == "" {
		// An empty field shows its hint as its text.
		n.Hint = n.Text
	}
	n.Checkable = attr(a, "checkable") == "true"
	n.Scrolling = attr(a, "scrollable") == "true"
	return n
}

// roleOf tells what a node is. Android's views say it by their class:
// android.widget.Button, EditText, CheckBox... Jetpack Compose says it by a
// child of that class, without text, in the control it marks: a tappable
// node with an android.widget.Button child is a button. A tappable node that
// no class marks is what people call a list item: a row or a card.
func roleOf(n *mobilecore.Node) mobilecore.Role {
	if r := classRole(n); r != mobilecore.RoleOther && r != mobilecore.RoleText && r != mobilecore.RoleImage {
		return r
	}
	if !n.Clickable {
		return classRole(n)
	}
	for _, c := range n.Children {
		if c.Name() == "" && len(c.Children) == 0 {
			if r := classRole(c); r != mobilecore.RoleOther && r != mobilecore.RoleText {
				return r
			}
		}
	}
	switch classRole(n) {
	case mobilecore.RoleText, mobilecore.RoleImage:
		return mobilecore.RoleButton // a tappable text or picture
	}
	return mobilecore.RoleListItem
}

// classRole is the role a node's class tells.
func classRole(n *mobilecore.Node) mobilecore.Role {
	short := n.Class[strings.LastIndex(n.Class, ".")+1:]
	switch {
	case strings.Contains(short, "EditText") || strings.Contains(short, "AutoCompleteTextView"):
		return mobilecore.RoleField
	case strings.Contains(short, "CheckBox"):
		return mobilecore.RoleCheckbox
	case strings.Contains(short, "Switch"):
		return mobilecore.RoleSwitch
	case strings.Contains(short, "Tab"):
		return mobilecore.RoleTab
	case strings.Contains(short, "Button"):
		return mobilecore.RoleButton
	case strings.Contains(short, "Image"):
		return mobilecore.RoleImage
	case strings.Contains(short, "TextView"):
		return mobilecore.RoleText
	}
	return mobilecore.RoleOther
}

// xpathName is the element name XPath finds a node by: its class, as
// UiAutomator2 names elements.
func xpathName(class, local string) string {
	if class == "" || strings.ContainsAny(class, "$ ") {
		return local
	}
	return class
}

func attr(a []xml.Attr, name string) string {
	for _, x := range a {
		if x.Name.Local == name {
			return x.Value
		}
	}
	return ""
}

func attrFloat(a []xml.Attr, name string) float64 {
	f, _ := strconv.ParseFloat(attr(a, name), 64)
	return f
}

func parseBounds(s string) mobilecore.Rect {
	m := boundsRE.FindStringSubmatch(s)
	if m == nil {
		return mobilecore.Rect{}
	}
	v := make([]float64, 4)
	for i := range v {
		v[i], _ = strconv.ParseFloat(m[i+1], 64)
	}
	return mobilecore.Rect{X: v[0], Y: v[1], Width: v[2] - v[0], Height: v[3] - v[1]}
}
