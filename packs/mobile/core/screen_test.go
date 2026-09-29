package mobilecore

import (
	"strings"
	"testing"
)

// screenOf builds a screen of nodes, children after their parents.
func screenOf(nodes ...*Node) *Screen {
	for _, n := range nodes {
		n.Displayed = true
		for _, c := range n.Children {
			c.Parent = n
		}
	}
	return &Screen{Nodes: nodes}
}

func TestNamesAreWhatPeopleSee(t *testing.T) {
	label := &Node{Role: RoleText, Text: "Sign  in"}
	button := &Node{Role: RoleButton, Clickable: true, Children: []*Node{label}}
	heading := &Node{Role: RoleText, Text: "Sign in"}
	s := screenOf(heading, button, label)

	// The button is named by the text it contains; its own label counts once.
	if got := matches(s, kinds["button"], "Sign in"); len(got) != 1 || got[0] != button {
		t.Errorf("button: %v", got)
	}
	// An element named "Sign in" is the heading and the button: two.
	if got := matches(s, kinds["element"], "Sign in"); len(got) != 2 {
		t.Errorf("elements: %d", len(got))
	}
	if !s.Shows("Sign in") || s.Shows("Sign out") {
		t.Error("shows")
	}
}

func TestFieldsByTheirLabel(t *testing.T) {
	field := &Node{Role: RoleField, Text: "CR-LEJ-12", Children: []*Node{{Role: RoleText, Text: "Courier ID"}}}
	hinted := &Node{Role: RoleField, Hint: "Signed by"}
	s := screenOf(field, field.Children[0], hinted)
	if got := matches(s, kinds["field"], "Courier ID"); len(got) != 1 || value(got[0]) != "CR-LEJ-12" {
		t.Errorf("field by its label: %v", got)
	}
	if got := matches(s, kinds["field"], "Signed by"); len(got) != 1 || value(got[0]) != "" {
		t.Errorf("field by its hint: %v", got)
	}
	// A field is not named by what it holds.
	if got := matches(s, kinds["field"], "CR-LEJ-12"); len(got) != 0 {
		t.Errorf("field by its value: %v", got)
	}
}

func TestListItemsByOneOfTheirTexts(t *testing.T) {
	item := &Node{Role: RoleListItem, Clickable: true, Children: []*Node{{Role: RoleText, Text: "PX-MOB-9401"}, {Role: RoleText, Text: "Prager Str. 3, 01069 Dresden"}}}
	s := screenOf(item, item.Children[0], item.Children[1])
	for _, name := range []string{"PX-MOB-9401", "PX-MOB-9401 Prager Str. 3, 01069 Dresden"} {
		if got := matches(s, kinds["list item"], name); len(got) != 1 {
			t.Errorf("%q: %v", name, got)
		}
	}
}

func TestApostrophes(t *testing.T) {
	s := screenOf(&Node{Role: RoleButton, Text: "Don’t allow"})
	if len(matches(s, kinds["button"], "Don't allow")) != 1 {
		t.Error("a typographic apostrophe is a straight one")
	}
}

func TestMissingListsWhatThereIs(t *testing.T) {
	s := screenOf(&Node{Role: RoleButton, Text: "Sign in"}, &Node{Role: RoleButton, Text: "Sign out"})
	err := missing("courier", s, kinds["button"], "Log in")
	if !strings.Contains(err.Error(), `No button named "Log in" in the courier app`) || !strings.Contains(err.Error(), "Sign out") {
		t.Errorf("err: %v", err)
	}
}
