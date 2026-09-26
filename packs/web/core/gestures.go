package webcore

import (
	"fmt"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// gestureSteps do what people do beyond a click: double and right clicks,
// taps, dragging and scrolling, choosing several options; and set what the
// browser lives with: its clock and its connection.
func gestureSteps() []core.StepDef {
	return []core.StepDef{
		elementStep("web-core.element.dblclick", "double-clicked", "double-click",
			"Double-click the element, by its name or a selector.", `When the "PX-4101" element is double-clicked`,
			func(l playwright.Locator) error { return l.Dblclick() }),
		elementStep("web-core.element.rightclick", "right-clicked", "right-click",
			"Right-click the element, by its name or a selector: a menu of the page's own may open.",
			`When the "PX-4101" link is right-clicked`,
			func(l playwright.Locator) error {
				return l.Click(playwright.LocatorClickOptions{Button: playwright.MouseButtonRight})
			}),
		elementStep("web-core.element.tap", "tapped", "tap",
			"Tap the element, by its name or a selector, on a touch screen: the web app's `device` must have one (like `iPhone 15`).",
			`When the "Register a parcel" link is tapped`,
			func(l playwright.Locator) error { return l.Tap() }),
		elementStep("web-core.element.scroll", "scrolled into view", "scroll to",
			"Scroll the page (or the list it is in) until the element shows.", `When the "css=.parcel-card:last-child" element is scrolled into view`,
			func(l playwright.Locator) error { return l.ScrollIntoViewIfNeeded() }),
		{
			ID: "web-core.element.drag", Keyword: "When", Since: since,
			Expr:     "the {string} {element} is dragged onto the {string} {element}",
			Doc:      "Drag an element onto another, each by its name or a selector, as with a mouse.",
			Examples: []string{`When the "PX-4101" element is dragged onto the "Out for delivery" element`},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				from, fk, to, tk := text(sc, a, 0), a.Value(1).(kind), text(sc, a, 2), a.Value(3).(kind)
				src, err := s.find(sc, fk, from, actionTimeout)
				if err != nil {
					return err
				}
				dst, err := s.find(sc, tk, to, actionTimeout)
				if err != nil {
					return err
				}
				if err := src.DragTo(dst); err != nil {
					return fmt.Errorf("cannot drag the %q %s onto the %q %s: %s", from, fk.noun, to, tk.noun, firstLine(err))
				}
				return nil
			}),
		},
		scrollStep("bottom", "document.scrollingElement.scrollHeight"),
		scrollStep("top", "0"),
		{
			ID: "web-core.select.many", Keyword: "When", Arg: core.ArgTable, Since: since,
			Expr:     "the following options are chosen in the {string} field:",
			Doc:      "Choose several options in a drop-down field that takes more than one.",
			Table:    &core.TableDoc{Columns: []string{"option"}, Note: "An option a row, by its text."},
			Examples: []string{"When the following options are chosen in the \"Pickup days\" field:\n  | Monday   |\n  | Thursday |"},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				name := text(sc, a, 0)
				var choices []string
				for _, row := range a.Table.Rows {
					for _, c := range row {
						if c = strings.TrimSpace(expand(sc, c)); c != "" {
							choices = append(choices, c)
						}
					}
				}
				loc, err := s.find(sc, field, name, actionTimeout)
				if err != nil {
					return err
				}
				if _, err := loc.SelectOption(playwright.SelectOptionValues{Labels: &choices}); err != nil {
					return missingOption(loc, name, strings.Join(choices, `", "`), err)
				}
				return nil
			}),
		},
		{
			ID: "web-core.clock.set", Keyword: "Given", Since: since,
			Expr: "the browser's clock is set to {string}",
			Doc: "Set the time the web apps' pages see, from then on.\n\n" +
				"- The time is like `2026-09-25T14:00:00+02:00`, or `2026-09-25 14:00` in UTC.\n" +
				"- Time goes on from there, so the pages' timers still run.\n" +
				"- Set it before opening the pages whose timers a step moves forward.",
			Examples: []string{`Given the browser's clock is set to "2026-09-25T14:00:00+02:00"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				t, err := parseTime(text(sc, a, 0))
				if err != nil {
					return err
				}
				st := scenarioPages.Of(sc)
				st.mu.Lock()
				defer st.mu.Unlock()
				st.clock = &clockSet{at: t, when: time.Now()}
				for _, s := range st.order {
					if err := s.setClock(t); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			ID: "web-core.clock.forward", Keyword: "When", Since: since,
			Expr: "the browser's clock is moved forward by {duration}",
			Doc: "Move the time the web apps' pages see forward, as if the computer slept that long.\n\n" +
				"- The timers due in between fire once, and what expires, such as a session, expires.\n" +
				"- The pages' timers are the browser's own from when its clock is set: set it before opening them.",
			Examples: []string{"When the browser's clock is moved forward by 31m"},
			Run: func(sc *core.Scenario, a core.Args) error {
				d := a.Value(0).(time.Duration)
				st := scenarioPages.Of(sc)
				st.mu.Lock()
				defer st.mu.Unlock()
				if st.clock == nil {
					st.clock = &clockSet{at: time.Now(), when: time.Now()}
				}
				st.clock.at = st.clock.at.Add(d)
				for _, s := range st.order {
					if !s.clock {
						if err := s.setClock(time.Now()); err != nil {
							return err
						}
					}
					if err := s.ctx.Clock().FastForward(d.Milliseconds()); err != nil {
						return fmt.Errorf("cannot move the browser's clock forward: %s", firstLine(err))
					}
				}
				return nil
			},
		},
		connectionStep("offline", true, "Take the browser offline: the web apps' pages cannot reach anything until it is online again."),
		connectionStep("online", false, "Bring the browser back online."),
	}
}

// elementStep does something to an element of any kind.
func elementStep(id, done, verb, doc, example string, do func(playwright.Locator) error) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "When", Since: since,
		Expr: "the {string} {element} is " + done, Doc: doc, Examples: []string{example},
		Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
			k := a.Value(1).(kind)
			err := act(sc, s, k, text(sc, a, 0), verb, do)
			if err != nil && verb == "tap" && strings.Contains(err.Error(), "hasTouch") {
				return fmt.Errorf("%w\n  tapping needs a touch screen: give the %s web app a device that has one, like iPhone 15", err, s.app.Name)
			}
			return err
		}),
	}
}

func scrollStep(where, top string) core.StepDef {
	return core.StepDef{
		ID: "web-core.scroll." + where, Keyword: "When", Since: since,
		Expr:     "the page is scrolled to the " + where,
		Doc:      "Scroll the page to its " + where + ", as far as it goes: a page that shows more as it scrolls shows it.",
		Examples: []string{"When the page is scrolled to the " + where},
		Run: action(func(_ *core.Scenario, s *session, _ core.Args) error {
			pg, err := s.page()
			if err != nil {
				return err
			}
			_, err = pg.Evaluate(`() => window.scrollTo({ top: ` + top + `, behavior: 'instant' })`)
			return err
		}),
	}
}

func connectionStep(state string, offline bool, doc string) core.StepDef {
	return core.StepDef{
		ID: "web-core.connection." + state, Keyword: "When", Since: since,
		Expr: "the browser is " + state, Doc: doc, Examples: []string{"When the browser is " + state},
		Run: func(sc *core.Scenario, _ core.Args) error {
			st := scenarioPages.Of(sc)
			st.mu.Lock()
			defer st.mu.Unlock()
			st.offline = offline
			for _, s := range st.order {
				if err := s.ctx.SetOffline(offline); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func (s *session) setClock(t time.Time) error {
	c := s.ctx.Clock()
	var err error
	if s.clock {
		err = c.SetSystemTime(t.UnixMilli())
	} else {
		err = c.Install(playwright.ClockInstallOptions{Time: t.UnixMilli()})
	}
	if err != nil {
		return fmt.Errorf("cannot set the browser's clock: %s", firstLine(err))
	}
	s.clock = true
	return nil
}

var timeLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

func parseTime(v string) (time.Time, error) {
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, strings.TrimSpace(v)); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a time like 2026-09-25T14:00:00+02:00 or 2026-09-25 14:00", v)
}

// missingOption explains an option that could not be chosen.
func missingOption(loc playwright.Locator, name, choice string, err error) error {
	opts, _ := loc.Locator("option").AllInnerTexts()
	for i, o := range opts {
		opts[i] = fmt.Sprintf("%q", collapse(o))
	}
	if len(opts) == 0 {
		return fmt.Errorf("cannot choose %q in the %q field: %s", choice, name, firstLine(err))
	}
	return core.Failf("The %q field has no option %q; %s", name, choice, cloudstep.Shown("options", opts, 30))
}
