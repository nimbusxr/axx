package mobileios

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// parseSource reads the XCUITest driver's page source into a screen: each
// element's type (XCUIElementTypeButton, TextField, Cell...) tells its role,
// in points.
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
			return nil, fmt.Errorf("the page source is not XCUITest's XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "AppiumAUT" {
				stack = append(stack, nil)
				continue
			}
			n := nodeOf(t)
			if len(stack) > 0 {
				if p := stack[len(stack)-1]; p != nil {
					n.Parent = p
					p.Children = append(p.Children, n)
					n.Displayed = n.Displayed && p.Displayed
				}
			}
			if n.Class == "XCUIElementTypeApplication" && s.Size.Width == 0 {
				s.Size = n.Bounds
			}
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
		n.Clickable = n.Role != mobilecore.RoleOther && n.Role != mobilecore.RoleText && n.Role != mobilecore.RoleImage
	}
	return s, nil
}

func nodeOf(t xml.StartElement) *mobilecore.Node {
	a := t.Attr
	class := attr(a, "type")
	if class == "" {
		class = t.Name.Local
	}
	n := &mobilecore.Node{
		Class:     class,
		Label:     attr(a, "label"),
		Text:      attr(a, "value"),
		Hint:      attr(a, "placeholderValue"),
		Enabled:   attr(a, "enabled") != "false",
		Displayed: attr(a, "visible") == "true",
		Selected:  attr(a, "selected") == "true",
		Password:  class == "XCUIElementTypeSecureTextField",
		Bounds: mobilecore.Rect{
			X: attrFloat(a, "x"), Y: attrFloat(a, "y"), Width: attrFloat(a, "width"), Height: attrFloat(a, "height"),
		},
	}
	if name := attr(a, "name"); name != n.Label {
		n.ID = name // an accessibility identifier, not the label again
	}
	switch class {
	case "XCUIElementTypeKeyboard":
		n.Displayed = false // the system's, over the app: not what the app shows
	case "XCUIElementTypeOther":
		if attr(a, "traits") == "Adjustable" && attr(a, "accessible") == "false" {
			n.Displayed = false // a scroll indicator, which says where a list is scrolled to
		}
	case "XCUIElementTypeSwitch", "XCUIElementTypeToggle", "XCUIElementTypeCheckBox":
		n.Checkable = true
		n.Checked = n.Text == "1"
	case "XCUIElementTypeStaticText":
		if n.Text == n.Label {
			n.Text = "" // its label says it
		}
	case "XCUIElementTypeScrollView", "XCUIElementTypeTable", "XCUIElementTypeCollectionView":
		n.Scrolling = true
	}
	// Finds the node through the XCUITest driver: by its type and where it is.
	n.Using = "xpath"
	n.Value = fmt.Sprintf(`//%s[@x="%s" and @y="%s" and @width="%s" and @height="%s"]`,
		class, attr(a, "x"), attr(a, "y"), attr(a, "width"), attr(a, "height"))
	return n
}

// roleOf is the role an element's type tells: a button in a tab bar is a
// tab, a cell of a list or collection a list item.
func roleOf(n *mobilecore.Node) mobilecore.Role {
	switch strings.TrimPrefix(n.Class, "XCUIElementType") {
	case "Button", "Link", "MenuItem", "SegmentedControl":
		if n.Parent != nil && n.Parent.Class == "XCUIElementTypeTabBar" {
			return mobilecore.RoleTab
		}
		return mobilecore.RoleButton
	case "TextField", "SecureTextField", "SearchField", "TextView":
		return mobilecore.RoleField
	case "Switch", "Toggle":
		return mobilecore.RoleSwitch
	case "CheckBox":
		return mobilecore.RoleCheckbox
	case "Tab":
		return mobilecore.RoleTab
	case "Cell":
		return mobilecore.RoleListItem
	case "Image", "Icon":
		return mobilecore.RoleImage
	case "StaticText":
		return mobilecore.RoleText
	}
	return mobilecore.RoleOther
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
