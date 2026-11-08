package mobileios

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// parseSource reads the XCUITest driver's page source into a screen: each
// element's type (XCUIElementTypeButton, TextField, Cell...) tells its role,
// in points.
func parseSource(src string) (*mobilecore.Screen, error) {
	dec := xml.NewDecoder(strings.NewReader(src))
	s := &mobilecore.Screen{}
	var stack []*mobilecore.Node
	keyboard := map[*mobilecore.Node]bool{}
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
			if n.Class == "XCUIElementTypeKeyboard" {
				keyboard[n] = true
			}
			if len(stack) > 0 {
				if p := stack[len(stack)-1]; p != nil {
					n.Parent = p
					p.Children = append(p.Children, n)
					if keyboard[p] {
						// The keyboard's keys are the system's, not what the app shows.
						keyboard[n], n.Displayed = true, false
					}
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
		if inSheet(n) && onScreen(n, s.Size) && !strings.Contains(n.Label, "scroll bar") {
			// What an action sheet shows, iOS 27 says is not visible, or now and then that it is: the
			// sheet, in front of the app, shows what is on the screen in it, and a finger taps it there
			// either way. XCTest's own tap of a sheet's button does nothing (the Save Password sheet's
			// Not Now, when iOS said it was visible).
			n.Displayed, n.ByPoint = true, true
		}
		n.Role = roleOf(n)
		n.Clickable = n.Role != mobilecore.RoleOther && n.Role != mobilecore.RoleText && n.Role != mobilecore.RoleImage
		n.Value = strings.TrimSuffix(n.Value, "]") + shows(n) + "]"
	}
	for _, n := range s.Nodes {
		if c := innerSwitch(n); c != nil {
			// A SwiftUI Toggle is a switch as wide as its row, its label in it: XCTest's tap of it
			// lands on the label, which does not toggle it. The switch in it is the control a
			// person taps, and where a tap goes.
			n.Bounds, n.ByPoint = c.Bounds, true
		}
	}
	return s, nil
}

// shows is what a node's locator says it shows, beside its type and frame: its own label or
// value, or else the first of its descendants'. A list still scrolling as the screen is read moves
// another row to where the node was by the time XCUITest finds it, and a tap then lands on that
// row: found by what it shows too, the node is found where it is, or not at all, and the screen is
// read again.
func shows(n *mobilecore.Node) string {
	if s := own(n); s != "" {
		return " and " + s
	}
	var first string
	var walk func(*mobilecore.Node)
	walk = func(m *mobilecore.Node) {
		for _, c := range m.Children {
			if first != "" {
				return
			}
			if c.Displayed {
				first = own(c)
			}
			walk(c)
		}
	}
	walk(n)
	if first == "" {
		return ""
	}
	return " and .//*[" + first + "]"
}

// own is the predicate of what a node itself shows: its label, else its value. A text's value
// that its label says is left out of the node's text, and found by the label.
func own(n *mobilecore.Node) string {
	switch {
	case n.Label != "":
		return "@label=" + appium.XPathString(n.Label)
	case n.Text != "":
		return "@value=" + appium.XPathString(n.Text)
	}
	return ""
}

// innerSwitch is the switch control in a switch that holds one, like a SwiftUI Toggle's.
func innerSwitch(n *mobilecore.Node) *mobilecore.Node {
	if n.Class != "XCUIElementTypeSwitch" {
		return nil
	}
	var find func(*mobilecore.Node) *mobilecore.Node
	find = func(p *mobilecore.Node) *mobilecore.Node {
		for _, c := range p.Children {
			if c.Class == "XCUIElementTypeSwitch" && c.Bounds.Width > 0 && c.Bounds.Width < n.Bounds.Width {
				return c
			}
			if f := find(c); f != nil {
				return f
			}
		}
		return nil
	}
	return find(n)
}

// inSheet is whether the node is in an action sheet: SwiftUI's
// confirmationDialog, UIKit's UIAlertController as a sheet.
func inSheet(n *mobilecore.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Class == "XCUIElementTypeSheet" {
			return true
		}
	}
	return false
}

// onScreen is whether the node has a size and is on the screen, all of it.
func onScreen(n *mobilecore.Node, screen mobilecore.Rect) bool {
	b := n.Bounds
	if b.Width <= 0 || b.Height <= 0 || screen.Width <= 0 {
		return false
	}
	return b.X >= screen.X && b.Y >= screen.Y && b.X+b.Width <= screen.X+screen.Width && b.Y+b.Height <= screen.Y+screen.Height
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
		n.Displayed = false // the system's, over the app: not what the app shows; its keys too
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
