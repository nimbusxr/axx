package webcore

import (
	"fmt"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// checkSteps check the state of what a page shows: its elements, their
// text, attributes and state, counts, the title, the page's script errors
// and the requests it sent. {element} steps take a name, or a selector.
func checkSteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "web-core.element.shown", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} {element} is shown",
			Doc: "Check that the page shows the element: by the name people see, or by a selector (`css=…`, `xpath=…` or " +
				"`testid=…`). The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "Cancel the parcel" button is shown`, `Then within 5s the "css=.parcel-card" element is shown`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, k := text(sc, a, 1), a.Value(2).(kind)
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					_, n, err := s.count(k, name)
					return n > 0, err
				})
				if err != nil || ok {
					return err
				}
				return s.none(k, name)
			}),
		},
		{
			ID: "web-core.element.hidden", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} {element} is not shown",
			Doc: "Check that the page does not show the element, by its name or a selector. The check waits for it to go: " +
				"10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "Cancel the parcel" menu item is not shown`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, k := text(sc, a, 1), a.Value(2).(kind)
				n := 0
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					var err error
					_, n, err = s.count(k, name)
					return n == 0, err
				})
				if err != nil || ok {
					return err
				}
				return core.Failf("The page shows %d %s %s", n, pluralOf(k, n), named(name))
			}),
		},
		{
			ID: "web-core.element.text", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} {element} shows {string}",
			Doc: "Check that the element shows the text, as part of what it shows (case matters). The check waits for it: " +
				"10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "css=#price" element shows "6.90 EUR"`, `Then the "Details" tab shows "3"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, k, want := text(sc, a, 1), a.Value(2).(kind), text(sc, a, 3)
				return s.expectOf(sc, k, name, cloudstep.Wait(a, 0), func(loc playwright.Locator) (bool, string, error) {
					got, err := loc.InnerText()
					got = collapse(got)
					return strings.Contains(got, want), got, err
				}, func(got string) error {
					return core.Fail(fmt.Sprintf("The %q %s does not show %q", name, k.noun, want), want, got)
				})
			}),
		},
		{
			ID: "web-core.element.attribute", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} {element} has the {word} attribute {string}",
			Doc: "Check the value of one of the element's attributes, such as `href` or `aria-expanded`. The check waits for it: " +
				"10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "Actions" button has the aria-expanded attribute "true"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, k, attr, want := text(sc, a, 1), a.Value(2).(kind), a.String(3), text(sc, a, 4)
				return s.expectOf(sc, k, name, cloudstep.Wait(a, 0), func(loc playwright.Locator) (bool, string, error) {
					v, err := loc.GetAttribute(attr)
					if err != nil {
						return false, "", err
					}
					if v == "" {
						if has, _ := loc.Evaluate(`(e, a) => e.hasAttribute(a)`, attr); has != true {
							return false, "no " + attr + " attribute", nil
						}
					}
					return v == want, v, nil
				}, func(got string) error {
					return core.Fail(fmt.Sprintf("The %q %s's %s attribute has another value", name, k.noun, attr), want, got)
				})
			}),
		},
		{
			ID: "web-core.element.focus", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} {element} has the focus",
			Doc:      "Check that the element has the keyboard's focus. The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "Find a parcel" field has the focus`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, k := text(sc, a, 1), a.Value(2).(kind)
				return s.expectOf(sc, k, name, cloudstep.Wait(a, 0), func(loc playwright.Locator) (bool, string, error) {
					v, err := loc.Evaluate(`(e) => {
  if (e === document.activeElement) return '';
  const f = document.activeElement;
  return !f || f === document.body ? 'nothing' : (f.getAttribute('aria-label') || f.innerText || f.value || f.tagName.toLowerCase()).trim().slice(0, 80);
}`, nil)
					got, _ := v.(string)
					return got == "", got, err
				}, func(got string) error {
					return core.Fail(fmt.Sprintf("The %q %s does not have the focus", name, k.noun), name, got)
				})
			}),
		},
		stateStep("web-core.checkbox.ticked", checkbox, "ticked", true, `Then the "Signature on delivery" checkbox is ticked`),
		stateStep("web-core.checkbox.unticked", checkbox, "not ticked", false, `Then the "Leave with a neighbour" checkbox is not ticked`),
		stateStep("web-core.option.selected", option, "selected", true, `Then the "Express" option is selected`),
		stateStep("web-core.option.unselected", option, "not selected", false, `Then the "Standard" option is not selected`),
		enabledStep("web-core.field.disabled", field, "disabled", false, `Then the "Parcel reference" field is disabled`),
		enabledStep("web-core.field.enabled", field, "enabled", true, `Then the "Weight (grams)" field is enabled`),
		{
			ID: "web-core.element.count", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the page shows {int} {string} {element}",
			Doc: "Check how many of the elements the page shows, by their name or a selector. The check waits for the number: " +
				"10 seconds, or `within {duration}`.",
			Examples: []string{`Then the page shows 2 "Remove" buttons`, `Then within 5s the page shows 3 "css=.parcel-card" elements`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want, name, k := a.Int(1), text(sc, a, 2), a.Value(3).(kind)
				n := 0
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					var err error
					_, n, err = s.count(k, name)
					return n == want, err
				})
				if err != nil || ok {
					return err
				}
				return core.Fail(fmt.Sprintf("The page shows another number of %s %s", k.plural, named(name)), want, n)
			}),
		},
		{
			ID: "web-core.row.count", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the page shows {int} table row(s) where:",
			Doc: "Check how many rows of the page's tables have those values in those columns. " +
				"The check waits for the number: 10 seconds, or `within {duration}`.",
			Table:    &core.TableDoc{Columns: []string{"column", "value"}, Note: "Each row names a column, by its header, and the value the tables' rows have in it."},
			Examples: []string{"Then the page shows 2 table rows where:\n  | Status | REGISTERED |"},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want := a.Int(1)
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				for i := range pairs {
					pairs[i].Value = expand(sc, pairs[i].Value)
				}
				n, why := 0, ""
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					pg, err := s.page()
					if err != nil {
						return false, err
					}
					n, why = countRows(tables(pg), pairs)
					return n == want, nil
				})
				if err != nil || ok {
					return err
				}
				return core.Fail(fmt.Sprintf("The page shows another number of table rows where %s; %s", describePairs(pairs), why), want, n)
			}),
		},
		{
			ID: "web-core.title", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the page title is {string}",
			Doc:      "Check the page's title, as the browser tab shows it. The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the page title is "Get a quote · Parcels"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want, got := text(sc, a, 1), ""
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					pg, err := s.page()
					if err != nil {
						return false, err
					}
					got, err = pg.Title()
					return got == want, err
				})
				if err != nil || ok {
					return err
				}
				return core.Fail("The page has another title", want, got)
			}),
		},
		{
			ID: "web-core.errors.none", Keyword: "Then", Since: since,
			Expr: "the page has no script errors",
			Doc: "Check that the web app's pages have had no errors in their scripts since the scenario opened them: no " +
				"uncaught errors, and none logged to the console.",
			Examples: []string{"Then the page has no script errors"},
			Run: check(func(_ *core.Scenario, s *session, _ core.Args) error {
				if errs := s.scriptErrors(); len(errs) > 0 {
					return core.Failf("The %s web app's pages had %s", s.app.Name, cloudstep.Shown("script errors", errs, 10))
				}
				return nil
			}),
		},
		{
			ID: "web-core.request.sent", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the browser sent a {word} request to {string}",
			Doc: "Check that the web app's pages sent a request with that method to that address.\n\n" +
				"- The address is a path below the web app's `url`, or a whole URL. The query counts only when the step gives one.\n" +
				"- The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the browser sent a POST request to "/api/parcels"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				method, want := strings.ToUpper(a.String(1)), text(sc, a, 2)
				absolute := strings.HasPrefix(want, "http://") || strings.HasPrefix(want, "https://")
				if !absolute && !strings.HasPrefix(want, "/") {
					want = "/" + want
				}
				where := func(address string) string {
					if absolute {
						return address
					}
					return s.app.pagePath(address, want)
				}
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					s.mu.Lock()
					defer s.mu.Unlock()
					for _, r := range s.requests {
						if r.method == method && where(r.url) == want {
							return true, nil
						}
					}
					return false, nil
				})
				if err != nil || ok {
					return err
				}
				s.mu.Lock()
				seen := make([]string, len(s.requests))
				for i, r := range s.requests {
					seen[i] = r.method + " " + where(r.url)
				}
				s.mu.Unlock()
				return core.Failf("The browser sent no %s request to %q; %s", method, want, cloudstep.Shown("requests", seen, 20))
			}),
		},
	}
}

// expectOf finds the one element of kind k named name, and waits for check
// to pass on it; failed words the failure from what check last saw.
func (s *session) expectOf(sc *core.Scenario, k kind, name string, d time.Duration,
	check func(playwright.Locator) (bool, string, error), failed func(got string) error,
) error {
	loc, err := s.find(sc, k, name, d)
	if err != nil {
		return err
	}
	var got string
	ok, err := waitUntil(sc, d, func() (bool, error) {
		var ok bool
		var err error
		ok, got, err = check(loc)
		return ok, err
	})
	if err != nil || ok {
		return err
	}
	return failed(got)
}

// stateStep checks whether a checkbox or radio button is ticked.
func stateStep(id string, k kind, state string, want bool, example string) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then", Since: since,
		Expr: "[[within {duration} ]]the {string} " + k.noun + " is " + state,
		Doc: fmt.Sprintf("Check that the %s with that name is %s. The check waits for it: 10 seconds, or `within {duration}`.",
			k.noun, state),
		Examples: []string{example},
		Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
			name := text(sc, a, 1)
			other := map[bool]string{true: "is not", false: "is"}[want]
			return s.expectOf(sc, k, name, cloudstep.Wait(a, 0), func(loc playwright.Locator) (bool, string, error) {
				on, err := loc.IsChecked()
				return on == want, "", err
			}, func(string) error {
				return core.Failf("The %q %s %s %s", name, k.noun, other, strings.TrimPrefix(state, "not "))
			})
		}),
	}
}

func pluralOf(k kind, n int) string {
	if n == 1 {
		return k.noun
	}
	return k.plural
}

func describePairs(pairs []core.Pair) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.Key + "=" + p.Value
	}
	return strings.Join(parts, ", ")
}
