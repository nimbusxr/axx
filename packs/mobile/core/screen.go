package mobilecore

import (
	"strings"
)

// Role is what a control on the screen is, as the platform tells it.
type Role int

// The roles a platform gives the controls it reads.
const (
	RoleOther Role = iota
	RoleButton
	RoleField
	RoleCheckbox
	RoleSwitch
	RoleTab
	RoleListItem
	RoleImage
	RoleText
)

// Rect is where something is on the screen, in the platform's points.
type Rect struct {
	X, Y, Width, Height float64
}

// Center is the middle of the rectangle.
func (r Rect) Center() (float64, float64) { return r.X + r.Width/2, r.Y + r.Height/2 }

// Node is a control on the screen, read from the platform's page source.
type Node struct {
	Role      Role
	Class     string // the platform's class, like android.widget.Button
	Text      string // what it shows
	Label     string // its accessibility label: content-desc on Android, label on iOS
	Hint      string // what an empty field shows
	ID        string // its resource ID (Android) or accessibility identifier (iOS)
	Enabled   bool
	Displayed bool
	Clickable bool
	Checkable bool
	Checked   bool
	Scrolling bool // it scrolls: a list, or a scrolling screen
	Selected  bool
	Password  bool
	Bounds    Rect
	// ByPoint is a control a tap reaches at its middle, and the platform's own tap of it does not:
	// iOS 27 says an action sheet's buttons are not visible, and tapping one does nothing.
	ByPoint bool
	Parent  *Node
	Children  []*Node
	// Using and Value find the node through Appium.
	Using, Value string
}

// Name is what people call the node: its label, or else its text.
func (n *Node) Name() string {
	if n.Label != "" {
		return clean(n.Label)
	}
	return clean(n.Text)
}

// Texts are the names of the node and of what it contains, as a screen
// reader merges a control with its content: the names of what is shown.
// A wrapper the platform calls not shown can hold what is (iOS 27 wraps a
// list's rows so), so the walk goes on inside it; what is hidden with its
// content, like the keyboard, is hidden node by node.
func (n *Node) Texts() []string {
	var out []string
	var walk func(*Node)
	walk = func(m *Node) {
		if m.Displayed {
			if s := m.Name(); s != "" {
				out = append(out, s)
			}
		}
		for _, c := range m.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// Merged is the node's name, or the names it contains, joined.
func (n *Node) Merged() string {
	if s := n.Name(); s != "" {
		return s
	}
	return strings.Join(n.Texts(), " ")
}

// Contains reports whether m is n or inside it.
func (n *Node) Contains(m *Node) bool {
	for ; m != nil; m = m.Parent {
		if m == n {
			return true
		}
	}
	return false
}

// Screen is what an app shows: its controls, in the order the platform
// lists them.
type Screen struct {
	Nodes []*Node // every node, parents before their children
	Size  Rect
}

// Visible is the nodes the screen shows.
func (s *Screen) Visible() []*Node {
	var out []*Node
	for _, n := range s.Nodes {
		if n.Displayed {
			out = append(out, n)
		}
	}
	return out
}

// Shows reports whether the screen shows text: in a control's text or
// label, whitespace collapsed.
func (s *Screen) Shows(text string) bool {
	want := clean(text)
	for _, n := range s.Visible() {
		if strings.Contains(clean(n.Text), want) || strings.Contains(clean(n.Label), want) {
			return true
		}
	}
	return false
}

// clean collapses whitespace, as people read text, and reads a typographic
// apostrophe as a straight one: "Don’t allow" is what people type as
// "Don't allow".
func clean(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("’", "'", "‘", "'").Replace(s)), " ")
}
