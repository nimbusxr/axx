package webcore

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// kind is a kind of element the steps name the way people do. Elements are
// found in every frame of the page, as people see them.
type kind struct {
	noun   string // "field", "button"...
	plural string
	find   func(f playwright.Frame, name string) playwright.Locator
	// then, when nothing of the kind is found, finds another way (text).
	then func(f playwright.Frame, name string) playwright.Locator
	// lists are the kinds namesJS lists, in failure messages.
	lists []string
}

var (
	// Fields are found by their label, or by their placeholder without one.
	field = kind{noun: "field", plural: "fields", lists: []string{"field"}, find: func(f playwright.Frame, name string) playwright.Locator {
		return f.GetByLabel(name, playwright.FrameGetByLabelOptions{Exact: playwright.Bool(true)}).
			Or(f.GetByPlaceholder(name, playwright.FrameGetByPlaceholderOptions{Exact: playwright.Bool(true)}))
	}}
	button   = roleKind("button", "buttons", playwright.AriaRoleButton)
	link     = roleKind("link", "links", playwright.AriaRoleLink)
	option   = roleKind("option", "options", playwright.AriaRoleRadio)
	checkbox = roleKind("checkbox", "checkboxes", playwright.AriaRoleCheckbox)
	tab      = roleKind("tab", "tabs", playwright.AriaRoleTab)
	// Menu items are the plain ones, and those that tick on and off or
	// pick one of a group.
	menuItem = kind{noun: "menu item", plural: "menu items", lists: []string{"menu item"}, find: func(f playwright.Frame, name string) playwright.Locator {
		return roleIn(f, playwright.AriaRoleMenuitem, name).
			Or(roleIn(f, playwright.AriaRoleMenuitemcheckbox, name)).
			Or(roleIn(f, playwright.AriaRoleMenuitemradio, name))
	}}

	// element is anything on the page: found by its text, its label, its
	// alternative text (images) or its title, or by a selector.
	element = kind{
		noun: "element", plural: "elements", lists: []string{"button", "link", "field"},
		find: func(f playwright.Frame, name string) playwright.Locator {
			exact := playwright.Bool(true)
			return f.GetByText(name, playwright.FrameGetByTextOptions{Exact: exact}).
				Or(f.GetByLabel(name, playwright.FrameGetByLabelOptions{Exact: exact})).
				Or(f.GetByAltText(name, playwright.FrameGetByAltTextOptions{Exact: exact})).
				Or(f.GetByTitle(name, playwright.FrameGetByTitleOptions{Exact: exact}))
		},
	}

	// kinds are the kinds of element the {element} parameter names, singular
	// and plural.
	kinds = map[string]kind{}

	// pointable is what the pointer is moved over: a button, link, tab or
	// menu item with that name, or else the text.
	pointable = kind{
		noun: "thing", plural: "things", lists: []string{"button", "link", "tab", "menu item"},
		find: func(f playwright.Frame, name string) playwright.Locator {
			l := roleIn(f, playwright.AriaRoleButton, name)
			for _, r := range []*playwright.AriaRole{
				playwright.AriaRoleLink, playwright.AriaRoleTab, playwright.AriaRoleMenuitem,
				playwright.AriaRoleMenuitemcheckbox, playwright.AriaRoleMenuitemradio,
			} {
				l = l.Or(roleIn(f, r, name))
			}
			return l
		},
		then: func(f playwright.Frame, name string) playwright.Locator {
			return f.GetByText(name, playwright.FrameGetByTextOptions{Exact: playwright.Bool(true)})
		},
	}
)

func init() {
	for _, k := range []kind{button, field, checkbox, option, link, tab, menuItem, element} {
		kinds[k.noun], kinds[k.plural] = k, k
	}
}

// elementParam is the {element} parameter: the kind of element a step
// names, like "button", or "buttons" after a number.
var elementParam = core.ParamType{
	Name:    "element",
	Regexps: []string{`(buttons?|fields?|checkbox(?:es)?|options?|links?|tabs?|menu items?|elements?)`},
	Doc: "a kind of element on the page, plural after a number (`buttons`): a `field` by its label or placeholder, " +
		"an `option` is a radio button, an `element` is anything, by its text, label, alternative text or title",
	Values:   []string{"button", "field", "checkbox", "option", "link", "tab", "menu item", "element"},
	Examples: []string{"button", "field"},
	Transform: func(_ *core.Scenario, s string, _ []*string) (any, error) {
		k, ok := kinds[s]
		if !ok {
			return nil, fmt.Errorf("%q is no kind of element", s)
		}
		return k, nil
	},
}

// selectorPrefixes are how a step names an element by a selector instead
// of by what people see: css=, xpath= or testid= (the test id attribute).
var selectorPrefixes = []string{"css=", "xpath=", "testid="}

// selectorOf finds what a selector selects, when name is one.
func selectorOf(name string) (func(playwright.Frame) playwright.Locator, bool) {
	for _, p := range selectorPrefixes {
		if v, ok := strings.CutPrefix(name, p); ok {
			if p == "testid=" {
				return func(f playwright.Frame) playwright.Locator { return f.GetByTestId(v) }, true
			}
			return func(f playwright.Frame) playwright.Locator { return f.Locator(p + v) }, true
		}
	}
	return nil, false
}

// finders are how to find the elements of kind k named name (or selected
// by it): the first way, and one to try when the first finds none.
func (k kind) finders(name string) (first, then func(playwright.Frame) playwright.Locator) {
	if sel, ok := selectorOf(name); ok {
		return sel, nil
	}
	first = func(f playwright.Frame) playwright.Locator { return k.find(f, name) }
	if k.then != nil {
		then = func(f playwright.Frame) playwright.Locator { return k.then(f, name) }
	}
	return first, then
}

// named is how failures name the element: `named "Save"`, or `matching
// "css=.save"`.
func named(name string) string {
	if _, ok := selectorOf(name); ok {
		return fmt.Sprintf("matching %q", name)
	}
	return fmt.Sprintf("named %q", name)
}

func roleKind(noun, plural string, role *playwright.AriaRole) kind {
	return kind{noun: noun, plural: plural, lists: []string{noun}, find: func(f playwright.Frame, name string) playwright.Locator {
		return roleIn(f, role, name)
	}}
}

func roleIn(f playwright.Frame, role *playwright.AriaRole, name string) playwright.Locator {
	return f.GetByRole(*role, playwright.FrameGetByRoleOptions{Name: name, Exact: playwright.Bool(true)})
}

var visible = playwright.LocatorFilterOptions{Visible: playwright.Bool(true)}

// find waits for the one visible element of kind k named name (or matching
// the selector name), in any frame of the current tab. When there is none,
// the failure lists the names the page has; when there are several, it says
// so.
func (s *session) find(sc *core.Scenario, k kind, name string, wait time.Duration) (playwright.Locator, error) {
	var found playwright.Locator
	count := 0
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		var err error
		found, count, err = s.count(k, name)
		return count > 0, err
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.none(k, name)
	}
	if count > 1 {
		return nil, core.Failf("%d %s on the page are %s; a step needs exactly one", count, k.plural, named(name))
	}
	return found, nil
}

// count counts the visible elements of kind k named name on the current
// tab, returning those of the first frame that has any.
func (s *session) count(k kind, name string) (playwright.Locator, int, error) {
	pg, err := s.page()
	if err != nil {
		return nil, 0, err
	}
	first, then := k.finders(name)
	found, n := countIn(pg, first)
	if n == 0 && then != nil {
		found, n = countIn(pg, then)
	}
	return found, n, nil
}

// none is the failure of finding no element of kind k named name.
func (s *session) none(k kind, name string) error {
	if _, ok := selectorOf(name); ok {
		return core.Failf("No %s on the page matches %q", k.noun, name)
	}
	return core.Failf("No %s named %q on the page; %s", k.noun, name, cloudstep.Shown(k.plural, s.names(k), 20))
}

// countIn counts the visible elements a finder finds in the page's frames,
// returning those of the first frame that has any.
func countIn(pg playwright.Page, finder func(playwright.Frame) playwright.Locator) (playwright.Locator, int) {
	var first playwright.Locator
	total := 0
	for _, f := range pg.Frames() {
		loc := finder(f).Filter(visible)
		n, err := loc.Count()
		if err != nil || n == 0 {
			continue
		}
		if first == nil {
			first = loc
		}
		total += n
	}
	return first, total
}

// namesJS lists the names of a frame's visible elements of some kinds, as
// the steps name them, for failure messages.
const namesJS = `(kinds) => {
  const visible = (e) => !!(e.offsetWidth || e.offsetHeight || e.getClientRects().length);
  const label = (e) => (e.getAttribute('aria-label') || (e.labels && e.labels.length ? e.labels[0].innerText : '')).trim();
  const text = (e) => (e.getAttribute('aria-label') || e.innerText || e.value || '').trim();
  const all = {
    field: ['input:not([type=hidden]):not([type=checkbox]):not([type=radio]):not([type=submit]):not([type=button]):not([type=reset]), select, textarea',
      (e) => label(e) || (e.getAttribute('placeholder') || '').trim()],
    button: ['button:not([role]), input[type=submit], input[type=button], input[type=reset], [role=button]', text],
    link: ['a[href]:not([role]), [role=link]', text],
    option: ['input[type=radio], [role=radio]', (e) => label(e) || text(e)],
    checkbox: ['input[type=checkbox], [role=checkbox]', (e) => label(e) || text(e)],
    tab: ['[role=tab]', text],
    'menu item': ['[role=menuitem], [role=menuitemcheckbox], [role=menuitemradio]', text],
  };
  return kinds.flatMap((kind) => {
    const [selector, name] = all[kind];
    return [...document.querySelectorAll(selector)]
      .filter((e) => visible(e) || (e.labels && [...e.labels].some(visible)))
      .map(name).filter(Boolean);
  });
}`

// names are the names of the current tab's elements of kind k.
func (s *session) names(k kind) []string {
	pg, err := s.page()
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range pg.Frames() {
		v, err := f.Evaluate(namesJS, k.lists)
		if err != nil {
			continue
		}
		list, _ := v.([]any)
		for _, x := range list {
			if n, ok := x.(string); ok {
				if q := fmt.Sprintf("%q", collapse(n)); !slices.Contains(out, q) {
					out = append(out, q)
				}
			}
		}
	}
	return out
}

// textOf matches text case-sensitively, anywhere in an element's text.
func textOf(text string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(text))
}

// shown counts the visible elements showing the text, in every frame.
func shown(pg playwright.Page, text string) int {
	_, n := countIn(pg, func(f playwright.Frame) playwright.Locator { return f.GetByText(textOf(text)) })
	return n
}

// visibleText is what the page shows, in all its frames, for a failure
// about the text want: the lines most like it, or else all of it, shortened.
func visibleText(pg playwright.Page, want string) string {
	var lines []string
	for _, f := range pg.Frames() {
		v, err := f.Evaluate(`() => document.body ? document.body.innerText : ''`)
		if err != nil {
			continue
		}
		t, _ := v.(string)
		for l := range strings.SplitSeq(t, "\n") {
			if l = collapse(l); l != "" {
				lines = append(lines, l)
			}
		}
	}
	s := strings.Join(nearest(lines, want), " … ")
	if s == "" {
		s = strings.Join(lines, " ")
	}
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}

// nearest are the (up to 3) lines that have the most in common with want:
// the longest run of the same text, at least a third of it.
func nearest(lines []string, want string) []string {
	type scored struct {
		line  string
		score int
	}
	var found []scored
	least := max(4, len([]rune(want))/3)
	for _, l := range lines {
		if n := commonRun(l, want); n >= least {
			found = append(found, scored{l, n})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return b.score - a.score })
	var out []string
	for _, f := range found {
		if len(out) == 3 {
			break
		}
		if !slices.Contains(out, f.line) {
			out = append(out, f.line)
		}
	}
	return out
}

// commonRun is the length of the longest text a and b have in common.
func commonRun(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev, cur := make([]int, len(rb)+1), make([]int, len(rb)+1)
	best := 0
	for i := range ra {
		for j := range rb {
			if ra[i] == rb[j] {
				cur[j+1] = prev[j] + 1
				best = max(best, cur[j+1])
			} else {
				cur[j+1] = 0
			}
		}
		prev, cur = cur, prev
	}
	return best
}

var spaces = regexp.MustCompile(`\s+`)

func collapse(s string) string { return strings.TrimSpace(spaces.ReplaceAllString(s, " ")) }
