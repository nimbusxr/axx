package mobilecore

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// kind is a kind of control a step names: the "Scan" button, the
// "Reference" field.
type kind struct {
	noun, plural string
	match        func(n *Node, name string) bool
}

var kinds = map[string]kind{}

// controlKinds are the {control} words, in the order the docs list them.
var controlKinds = []kind{
	{noun: "button", plural: "buttons", match: func(n *Node, name string) bool {
		return n.Role == RoleButton && named(n, name)
	}},
	{noun: "field", plural: "fields", match: func(n *Node, name string) bool {
		return n.Role == RoleField && labelled(n, name)
	}},
	{noun: "checkbox", plural: "checkboxes", match: func(n *Node, name string) bool {
		return n.Role == RoleCheckbox && named(n, name)
	}},
	{noun: "switch", plural: "switches", match: func(n *Node, name string) bool {
		return n.Role == RoleSwitch && named(n, name)
	}},
	{noun: "tab", plural: "tabs", match: func(n *Node, name string) bool {
		return n.Role == RoleTab && named(n, name)
	}},
	{noun: "list item", plural: "list items", match: func(n *Node, name string) bool {
		return n.Role == RoleListItem && (named(n, name) || slices.Contains(n.Texts(), name))
	}},
	{noun: "image", plural: "images", match: func(n *Node, name string) bool {
		return n.Role == RoleImage && n.Name() == name
	}},
	{noun: "text", plural: "texts", match: func(n *Node, name string) bool {
		return n.Role == RoleText && n.Name() == name
	}},
	{noun: "element", plural: "elements", match: func(n *Node, name string) bool {
		return n.Name() == name || (n.Clickable && n.Merged() == name)
	}},
}

func init() {
	for _, k := range controlKinds {
		kinds[k.noun] = k
		kinds[k.plural] = k
	}
}

// named reports a control whose name, or the names it contains, is name.
func named(n *Node, name string) bool { return n.Name() == name || n.Merged() == name }

// labelled reports a field labelled name: by its label or hint, by the
// label it shows while empty, or by the text next to it.
func labelled(n *Node, name string) bool {
	if clean(n.Label) == name || clean(n.Hint) == name {
		return true
	}
	for _, t := range n.Texts() {
		if t == name && t != clean(n.Text) {
			return true
		}
	}
	return false
}

var directionParam = core.ParamType{
	Name:     "direction",
	Regexps:  []string{`(up|down|left|right)`},
	Doc:      "where a swipe goes, as the finger moves: `down` from the top pulls a list to refresh",
	Values:   []string{"up", "down", "left", "right"},
	Examples: []string{"down", "left"},
}

// selector is the Appium locator a name that is a selector stands for.
func selector(name string) (using, value string, ok bool) {
	switch {
	case strings.HasPrefix(name, "id="):
		return "id", strings.TrimPrefix(name, "id="), true
	case strings.HasPrefix(name, "xpath="):
		return "xpath", strings.TrimPrefix(name, "xpath="), true
	}
	return "", "", false
}

// matches finds the visible controls of kind k named name: a control inside
// another that matches counts once, as the outer one.
func matches(s *Screen, k kind, name string) []*Node {
	name = clean(name)
	var found []*Node
	for _, n := range s.Visible() {
		if k.match(n, name) {
			found = append(found, n)
		}
	}
	var out []*Node
	for _, n := range found {
		inner := false
		for _, m := range found {
			if m != n && m.Contains(n) {
				inner = true
				break
			}
		}
		if !inner {
			out = append(out, n)
		}
	}
	return out
}

// covered reports whether the screen has a control of kind k named name that it does not show:
// one the keyboard covers, say.
func covered(s *Screen, k kind, name string) bool {
	name = clean(name)
	for _, n := range s.Nodes {
		if !n.Displayed && k.match(n, name) {
			return true
		}
	}
	return false
}

// missing is the failure of a control the screen does not have: it lists
// the names of the controls of that kind it has.
func missing(app string, s *Screen, k kind, name string) error {
	var names []string
	for _, n := range s.Visible() {
		if k.match(n, n.Merged()) || (k.noun == "field" && n.Role == RoleField) {
			if m := n.Merged(); m != "" && !slices.Contains(names, m) {
				names = append(names, m)
			}
		}
	}
	return core.Fail(fmt.Sprintf("No %s named %q in the %s app; %s", k.noun, name, app, cloudstep.Shown(k.plural, names, 20)), name, strings.Join(names, ", "))
}

// several is the failure of a name that more than one control has.
func several(app string, found []*Node, k kind, name string) error {
	return core.Failf("%d %s in the %s app are named %q; a step needs exactly one: use an id= or xpath= selector", len(found), k.plural, app, name)
}

var wordSpace = regexp.MustCompile(`\s+`)

// quoted names a control in a message.
func quoted(name string) string { return fmt.Sprintf("%q", wordSpace.ReplaceAllString(name, " ")) }
