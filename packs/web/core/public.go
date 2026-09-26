package webcore

import (
	"fmt"
	"slices"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
)

// The web-core pack's public context, for the packs that build on it
// (web-screenshots and the like): the page a scenario is on, found the way
// the web-core pack's steps find things.

// Kind is a kind of element steps name, the value of the {element}
// parameter: a button, a field, an element...
type Kind = kind

// Noun is the kind's name, as steps say it: "button", "menu item".
func (k kind) Noun() string { return k.noun }

// Current is the scenario's current web app, on its current tab.
type Current struct {
	s     *session
	suite *core.Suite
}

// Page is the current tab.
func (c *Current) Page() (playwright.Page, error) { return c.s.page() }

// App is the web app's name.
func (c *Current) App() string { return c.s.app.Name }

// Engine is the web app's browser engine: chromium, firefox, webkit, chrome
// or msedge.
func (c *Current) Engine() string { return c.s.app.Engine }

// URL is the web app's address, without a trailing slash: the pages steps
// name are below it.
func (c *Current) URL() string { return c.s.app.URL }

// Find finds the one element of kind k named name (or a selector), waiting
// for it and failing as the pack's steps do.
func (c *Current) Find(sc *core.Scenario, k Kind, name string, wait time.Duration) (playwright.Locator, error) {
	return c.s.find(sc, k, name, wait)
}

// Anything is what anything named name (or a selector) is, in each frame of
// the current tab, found or not: its text, its label, an image's
// alternative text or its title.
func (c *Current) Anything(name string) ([]playwright.Locator, error) {
	pg, err := c.s.page()
	if err != nil {
		return nil, err
	}
	first, _ := element.finders(name)
	var out []playwright.Locator
	for _, f := range pg.Frames() {
		out = append(out, first(f))
	}
	return out, nil
}

// File is where the scenario keeps a file of kind (a folder of .axx/web,
// such as "screenshots") for the web app, ending in suffix.
func (c *Current) File(sc *core.Scenario, kind, suffix string) string {
	return artifactPath(sc, kind, c.s.app, suffix)
}

// Check is a step that checks the scenario's current web app: it fails
// while a dialog waits for an answer, as the pack's checks do.
func Check(fn func(sc *core.Scenario, c *Current, a core.Args) error) core.StepFunc {
	return check(func(sc *core.Scenario, s *session, a core.Args) error {
		return fn(sc, &Current{s: s, suite: sc.Suite()}, a)
	})
}

// Text is the step's i-th argument, a string, with its ${env:..} and
// ${sys:..} references expanded (those from the environment are secrets).
func Text(sc *core.Scenario, a core.Args, i int) string { return text(sc, a, i) }

// Expand expands the ${env:..} and ${sys:..} references of a value from a
// step's table.
func Expand(sc *core.Scenario, s string) string { return expand(sc, s) }

// WaitUntil calls check until it reports true, up to d, as the pack's checks
// wait; false means the time ran out.
func WaitUntil(sc *core.Scenario, d time.Duration, check func() (bool, error)) (bool, error) {
	return waitUntil(sc, d, check)
}

// Relative is a path of the project, relative to it where it can be.
func Relative(sc *core.Scenario, path string) string { return relative(sc, path) }

// Context is a browser context the web-core pack opened for one of a
// scenario's web apps: its own cookies, storage and pages.
type Context struct {
	playwright.BrowserContext
	app *App
}

// App is the web app's name.
func (c Context) App() string { return c.app.Name }

// URL is the web app's address, without a trailing slash.
func (c Context) URL() string { return c.app.URL }

// Engine is the web app's browser engine.
func (c Context) Engine() string { return c.app.Engine }

// File is where the scenario keeps a file of kind (a folder of .axx/web)
// for the web app, ending in suffix.
func (c Context) File(sc *core.Scenario, kind, suffix string) string {
	return artifactPath(sc, kind, c.app, suffix)
}

// OnContext runs fn on each browser context of the scenario's web apps:
// those open now, and those opened later, before their first page. Packs
// that build on the web-core pack call it from their steps and scenario hooks.
func OnContext(sc *core.Scenario, fn func(Context) error) error {
	st := scenarioPages.Of(sc)
	st.mu.Lock()
	st.onContext = append(st.onContext, fn)
	open := make([]*session, len(st.order))
	copy(open, st.order)
	st.mu.Unlock()
	for _, s := range open {
		if err := fn(Context{BrowserContext: s.ctx, app: s.app}); err != nil {
			return err
		}
	}
	return nil
}

// OnPage runs fn on each page of the scenario's web apps: those open now,
// the first page of each web app opened later, before it loads anything,
// and the pages the pages open (tabs, windows), as they open: those load at
// once, so fn may come after their first requests. Tabs set aside for
// tools (AsideTab) are none of these. An error from fn fails
// the step that opens the page, or is logged for a page a page opened.
func OnPage(sc *core.Scenario, fn func(Context, playwright.Page) error) error {
	st := scenarioPages.Of(sc)
	st.mu.Lock()
	st.onPage.mu.Lock()
	st.onPage.fns = append(st.onPage.fns, fn)
	st.onPage.mu.Unlock()
	open := make([]*session, len(st.order))
	copy(open, st.order)
	st.mu.Unlock()
	for _, s := range open {
		s.mu.Lock()
		tabs := slices.Clone(s.tabs)
		s.mu.Unlock()
		for _, p := range tabs {
			if err := fn(Context{BrowserContext: s.ctx, app: s.app}, p); err != nil {
				return err
			}
			if err := s.present(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoteDebugging opens Chromium's remote debugging port on the Chromium,
// Chrome and Edge browsers the run launches from now on, on 127.0.0.1, for
// tools that drive a browser themselves (Lighthouse). A browser is launched
// once per run: call it before a scenario opens one, from a step hook.
func RemoteDebugging(s *core.Suite) { debuggingFor(s).set(true) }

// DebuggingURL is where tools reach the current web app's browser, like
// http://127.0.0.1:9222, once RemoteDebugging asked for it.
func (c *Current) DebuggingURL() (string, error) {
	if !chromiumFamily(c.s.app.Engine) {
		return "", fmt.Errorf("the %s web app's browser is %s: only Chromium, Chrome and Edge can be driven by other tools", c.s.app.Name, c.s.app.Engine)
	}
	port := debuggingFor(c.suite).port(c.s.app.key())
	if port == 0 {
		return "", fmt.Errorf("the %s web app's browser was launched without its remote debugging port", c.s.app.Name)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// AsideTab opens a browser tab of the current web app, with its cookies
// and storage, that is not one of the scenario's tabs: the steps go on in
// the current tab, its script errors, downloads and video are not the
// scenario's, and OnPage hooks do not run on it. Close it when done.
func (c *Current) AsideTab() (playwright.Page, error) {
	c.s.mu.Lock()
	c.s.asking++
	c.s.mu.Unlock()
	p, err := c.s.ctx.NewPage()
	if err != nil {
		c.s.mu.Lock()
		c.s.asking = max(0, c.s.asking-1)
		c.s.mu.Unlock()
		return nil, err
	}
	// Claimed as it opened (claimAside); should it have come otherwise, it
	// is set aside now.
	c.s.setAside(p)
	return p, nil
}
