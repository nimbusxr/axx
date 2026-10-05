package appcore

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
)

// Kind is a kind of control a step names: the "Register" button, the
// "Reference" field. Each family finds its kinds by its platforms' roles.
type Kind struct {
	Noun, Plural string
}

// Kinds are the {control} words, in the order the docs list them: every kind
// a family finds. A platform without one (a phone has no menu bar) fails the
// step that names it.
var Kinds = []Kind{
	{"button", "buttons"},
	{"field", "fields"},
	{"checkbox", "checkboxes"},
	{"radio button", "radio buttons"},
	{"switch", "switches"},
	{"tab", "tabs"},
	{"menu", "menus"},
	{"menu item", "menu items"},
	{"list item", "list items"},
	{"row", "rows"},
	{"link", "links"},
	{"image", "images"},
	{"text", "texts"},
	{"element", "elements"},
}

// KindOf is the kind a {control} word names, singular or plural.
func KindOf(word string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Noun == word || k.Plural == word {
			return k, true
		}
	}
	return Kind{}, false
}

// Field is the kind of the fields that fill and value steps name.
var Field = Kind{"field", "fields"}

var controlParam = core.ParamType{
	Name: "control",
	Regexps: []string{`(buttons?|fields?|checkbox(?:es)?|radio buttons?|switch(?:es)?|tabs?|menu items?|menus?|list items?|rows?|links?|` +
		`images?|texts?|elements?)`},
	Doc: "a kind of control an app shows, found by the name people see: a `button` by its text, a `field` by its label, " +
		"a `list item` or a `row` by one of its texts, a `menu item` in its open menu, an `element` is anything, by its text or accessibility label. " +
		"A kind an app's platform does not have (a phone has no menu bar) fails the step",
	Values:   []string{"button", "field", "checkbox", "radio button", "switch", "tab", "menu", "menu item", "list item", "row", "link", "image", "text", "element"},
	Examples: []string{"button", "list item"},
	Transform: func(_ *core.Scenario, match string, _ []*string) (any, error) {
		k, ok := KindOf(match)
		if !ok {
			return nil, fmt.Errorf("%q is no kind of control", match)
		}
		return k, nil
	},
}
