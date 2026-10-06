//go:build darwin

package desktopmacos

import (
	"regexp"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

var space = regexp.MustCompile(`\s+`)

// collapse is a text with its whitespace collapsed, as steps compare texts.
func collapse(s string) string { return strings.TrimSpace(space.ReplaceAllString(s, " ")) }

// names are what an element is called: its title, description, tooltip,
// placeholder, a text's value, and the text of the label that titles it.
func names(e *ax.Element) []string {
	var out []string
	add := func(v string) {
		if v = collapseKeepLines(v); v == "" {
			return
		}
		out = append(out, collapse(v))
		// And each of its lines: Flutter joins the texts it merges into one
		// label, and a hint after a label, with new lines.
		if strings.Contains(v, "\n") {
			for _, l := range strings.Split(v, "\n") {
				if l = collapse(l); l != "" {
					out = append(out, l)
				}
			}
		}
	}
	for _, a := range []string{"AXTitle", "AXDescription", "AXHelp", "AXPlaceholderValue"} {
		add(e.String(a))
	}
	if e.String("AXRole") == "AXStaticText" {
		add(e.String("AXValue"))
	}
	if v, err := e.Attribute("AXTitleUIElement"); err == nil {
		if label, ok := v.(*ax.Element); ok {
			add(label.String("AXValue"))
			add(label.String("AXTitle"))
		}
	}
	return out
}

// collapseKeepLines trims a text, keeping its lines apart.
func collapseKeepLines(s string) string { return strings.TrimSpace(s) }

// name is the name people see: the element's first name.
func name(e *ax.Element) string {
	if n := names(e); len(n) > 0 {
		return n[0]
	}
	return ""
}

func named(e *ax.Element, name string) bool { return slices.Contains(names(e), name) }

// hasText is whether e, or an element under it that shows, is named text or
// holds it.
func hasText(e *ax.Element, text string) bool {
	found := false
	ax.Walk(e, func(x *ax.Element) bool {
		found = found || (named(x, text) || collapse(x.String("AXValue")) == text) && shown(x)
		return !found
	})
	return found
}

// shown is whether an element shows: Java has hidden components in the
// tree, of no size.
func shown(e *ax.Element) bool {
	_, size, err := e.Frame()
	return err != nil || size.Width > 0 || size.Height > 0
}

// kinds are the {control} words, and whether an element under parent is a
// control of the kind, whatever its name.
var kinds = map[string]func(e, parent *ax.Element) bool{
	"button": func(e, _ *ax.Element) bool {
		sub := e.String("AXSubrole")
		return e.String("AXRole") == "AXButton" && sub != "AXSwitch" && sub != "AXCloseButton" &&
			sub != "AXMinimizeButton" && sub != "AXFullScreenButton" && sub != "AXZoomButton"
	},
	"field": func(e, _ *ax.Element) bool {
		r := e.String("AXRole")
		return r == "AXTextField" || r == "AXTextArea" || r == "AXComboBox"
	},
	"checkbox": func(e, _ *ax.Element) bool {
		return e.String("AXRole") == "AXCheckBox" && e.String("AXSubrole") != "AXSwitch"
	},
	"switch": func(e, _ *ax.Element) bool {
		return e.String("AXRole") == "AXSwitch" || e.String("AXSubrole") == "AXSwitch"
	},
	"radio button": func(e, parent *ax.Element) bool { return e.String("AXRole") == "AXRadioButton" && !isTab(e, parent) },
	"tab":          func(e, parent *ax.Element) bool { return e.String("AXRole") == "AXRadioButton" && isTab(e, parent) },
	"menu":         func(e, _ *ax.Element) bool { return e.String("AXRole") == "AXMenuBarItem" },
	"menu item":    func(e, _ *ax.Element) bool { return e.String("AXRole") == "AXMenuItem" },
	"list item":    func(_, parent *ax.Element) bool { return parent != nil && parent.String("AXRole") == "AXList" },
	"row": func(e, parent *ax.Element) bool {
		// A row; in a table that has no rows (Qt 5), a cell.
		r, pr := e.String("AXRole"), ""
		if parent != nil {
			pr = parent.String("AXRole")
		}
		return r == "AXRow" || (pr == "AXTable" || pr == "AXOutline") && (r == "AXStaticText" || r == "AXCell")
	},
	"link":    func(e, _ *ax.Element) bool { return e.String("AXRole") == "AXLink" },
	"image":   func(e, _ *ax.Element) bool { return e.String("AXRole") == "AXImage" },
	"text":    func(e, _ *ax.Element) bool { return e.String("AXRole") == "AXStaticText" },
	"element": func(*ax.Element, *ax.Element) bool { return true },
}

// byText are the kinds a control of is named by a text it holds too: a list
// item, a row, a link.
var byText = map[string]bool{"list item": true, "row": true, "link": true}

// matches is whether e, under parent, is a control of the kind named name.
func matches(kind, name string, e, parent *ax.Element) bool {
	is, ok := kinds[kind]
	if !ok || !is(e, parent) {
		return false
	}
	if named(e, name) {
		return true
	}
	if kind == "row" && e.String("AXRole") != "AXRow" {
		return false // a cell is named by its own text only
	}
	return byText[kind] && hasText(e, name)
}

// label is what names an element of the kind in a failure: its name, or
// for a control named by its texts, the first of them.
func label(kind string, e *ax.Element) string {
	if n := name(e); n != "" || !byText[kind] {
		return n
	}
	var first string
	ax.Walk(e, func(x *ax.Element) bool {
		if x != e && shown(x) {
			first = name(x)
			if first == "" {
				first = collapse(x.String("AXValue"))
			}
		}
		return first == ""
	})
	return first
}

// isTab is whether a radio button is a tab: a tab button (AppKit), one with
// no on or off of its own (Qt), or one of the tabs its tab group lists
// (WebKit). A tab group holds its tab's controls too (AppKit), radio buttons
// among them.
func isTab(e, parent *ax.Element) bool {
	if e.String("AXSubrole") == "AXTabButton" {
		return true
	}
	if _, err := e.Attribute("AXValue"); err != nil {
		return true
	}
	if parent == nil || parent.String("AXRole") != "AXTabGroup" {
		return false
	}
	tabs, _ := parent.Elements("AXTabs")
	pos, _, err := e.Frame()
	for _, t := range tabs {
		if p, _, terr := t.Frame(); err == nil && terr == nil && p == pos && t.String("AXTitle") == e.String("AXTitle") {
			return true
		}
	}
	return false
}

// search adds the controls of the kind named name under e (under parent)
// to found, in the tree's order: those that show, when only shown. A
// control in another that matches counts once, as the outer one.
func search(e, parent *ax.Element, kind, name string, onlyShown bool, found *[]*ax.Element) {
	if (!onlyShown || shown(e)) && matches(kind, name, e, parent) {
		// A text that holds a control of its name: the element a step
		// names is the control.
		if kind == "element" && e.String("AXRole") == "AXStaticText" {
			var inner []*ax.Element
			for _, k := range children(e) {
				search(k, e, kind, name, onlyShown, &inner)
			}
			if inner = captionsOut(inner); len(inner) > 0 && inner[0].String("AXRole") != "AXStaticText" {
				*found = append(*found, inner...)
				return
			}
		}
		*found = append(*found, e)
		return
	}
	kids, _ := e.Children()
	for _, k := range kids {
		search(k, e, kind, name, onlyShown, found)
	}
	if e.String("AXRole") == "AXTabGroup" && hasDead(kids) && (kind == "tab" || kind == "element") {
		for _, tab := range tabsDrawn(e) {
			if named(tab, name) {
				*found = append(*found, tab)
			}
		}
	}
}

// each visits the controls of the kind under e that show, whatever their
// names.
func each(e, parent *ax.Element, kind string, visit func(*ax.Element)) {
	if is := kinds[kind]; is != nil && is(e, parent) && shown(e) {
		visit(e)
		if kind != "element" {
			return
		}
	}
	kids, _ := e.Children()
	for _, k := range kids {
		each(k, e, kind, visit)
	}
}

// hasDead is whether some of the elements are gone as they are read (Java
// gives a tab group's tabs that way).
func hasDead(es []*ax.Element) bool {
	for _, e := range es {
		if _, err := e.Attribute("AXRole"); err != nil {
			return true
		}
	}
	return false
}

// tabsDrawn are the tabs drawn along the top of a tab group, found where a
// person sees them: what the screen has at points along its top edge.
func tabsDrawn(g *ax.Element) []*ax.Element {
	pos, size, err := g.Frame()
	if err != nil {
		return nil
	}
	app, err := g.PID()
	if err != nil {
		return nil
	}
	root, err := ax.Application(app)
	if err != nil {
		return nil
	}
	var tabs []*ax.Element
	seen := map[string]bool{}
	for y := pos.Y + 4; y < pos.Y+40; y += 8 {
		for x := pos.X + 4; x < pos.X+size.Width; x += 12 {
			e, err := root.ElementAt(ax.Point{X: x, Y: y})
			if err != nil || e.String("AXRole") != "AXRadioButton" {
				continue
			}
			if n := strings.Join(names(e), "|"); !seen[n] {
				seen[n] = true
				tabs = append(tabs, e)
			}
		}
	}
	return tabs
}

func parentOf(e *ax.Element) (*ax.Element, bool) {
	v, err := e.Attribute("AXParent")
	p, ok := v.(*ax.Element)
	return p, err == nil && ok
}

func children(e *ax.Element) []*ax.Element {
	if e == nil {
		return nil
	}
	kids, _ := e.Children()
	return kids
}
