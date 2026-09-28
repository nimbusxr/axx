package webcore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/packs/web/internal/driver"
)

// Actions wait this long for their element (tests shorten it).
var actionTimeout = cloudstep.DefaultWait

// Pages load within this.
const navigationTimeout = 30 * time.Second

// launchTimeout bounds starting a browser, which takes longer than loading
// a page: a first start sets up a profile, and a busy machine starts slowly.
const launchTimeout = 2 * time.Minute

// driverDir prepares the Playwright driver, once per run.
func driverDir(s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/driver", func() (string, error) {
		dir, err := driver.Ensure(context.Background(), driver.Options{})
		if err != nil {
			return "", fmt.Errorf("cannot prepare the browser driver: %w", err)
		}
		return dir, nil
	})
}

// traceViewerDir is where the driver has Playwright's trace viewer, a
// static web app an IDE can serve itself, after the run too.
func traceViewerDir(s *core.Suite) string {
	dir, err := driverDir(s)
	if err != nil {
		return ""
	}
	viewer := filepath.Join(dir, "package", "lib", "vite", "traceViewer")
	if _, err := os.Stat(filepath.Join(viewer, "index.html")); err != nil {
		return ""
	}
	return viewer
}

// playwrightFor runs the Playwright driver, once per run.
func playwrightFor(s *core.Suite) (*playwright.Playwright, error) {
	return core.Cached(s, Name+"/playwright", func() (*playwright.Playwright, error) {
		dir, err := driverDir(s)
		if err != nil {
			return nil, err
		}
		log := s.Logger()
		// The Inspector's recorder writes the steps it records there, for an
		// IDE (a patch of the driver): the run's only.
		recording := recordingPath(s)
		_ = os.MkdirAll(filepath.Dir(recording), 0o755)
		_ = os.Remove(recording)
		_ = os.Setenv("AXX_WEB_RECORDING", recording)
		// A logger keeps playwright-go from redirecting the process's log output.
		opts := &playwright.RunOptions{DriverDirectory: dir, SkipInstallBrowsers: true, Logger: log, Stderr: debugWriter(s), Stdout: io.Discard}
		// The driver is in place: Install only applies playwright-go's patch to it.
		if err := playwright.Install(opts); err != nil {
			return nil, fmt.Errorf("cannot prepare the browser driver: %w", err)
		}
		pw, err := playwright.Run(opts)
		if err != nil {
			return nil, fmt.Errorf("cannot start the browser driver: %w", err)
		}
		if st, err := settingsFor(s); err == nil {
			pw.Selectors.SetTestIdAttribute(st.testIDAttribute)
		}
		// The driver has Playwright's trace viewer, a static web app an IDE
		// can serve itself, after the run too.
		if viewer := traceViewerDir(s); viewer != "" {
			s.Announce("trace-viewer", "dir", viewer)
		}
		s.OnClose(func(context.Context) error { return pw.Stop() })
		return pw, nil
	})
}

// debugWriter logs what the driver prints, at debug level.
func debugWriter(s *core.Suite) io.Writer {
	r, w := io.Pipe()
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			s.Logger().Debug("playwright driver", "line", sc.Text())
		}
	}()
	s.OnClose(func(context.Context) error { return w.Close() })
	return w
}

// browserFor launches an engine's browser, once per run: headless, or in
// windows on the screen while watching, where a slowdown slows every action
// down. Playwright's browsers are downloaded the first time one is used;
// chrome and msedge are the Google Chrome and Microsoft Edge installed on
// the machine.
func browserFor(s *core.Suite, a *App) (playwright.Browser, error) {
	return core.Cached(s, Name+"/browser/"+a.key(), func() (playwright.Browser, error) {
		st, err := settingsFor(s)
		if err != nil {
			return nil, err
		}
		pw, err := playwrightFor(s)
		if err != nil {
			return nil, err
		}
		// A run that pauses shows its browsers, for the Inspector.
		opts := playwright.BrowserTypeLaunchOptions{
			Headless: playwright.Bool(!st.watch && !st.pause && !s.Pausing()),
			Timeout:  playwright.Float(float64(launchTimeout.Milliseconds())),
		}
		if st.slowdown > 0 {
			opts.SlowMo = playwright.Float(float64(st.slowdown.Milliseconds()))
		}
		port, err := debuggingPort(s, a)
		if err != nil {
			return nil, err
		}
		if port != 0 {
			opts.Args = append(opts.Args, fmt.Sprintf("--remote-debugging-port=%d", port))
		}
		if a.Engine == "chromium" || branded(a.Engine) {
			// Text is smoothed in grayscale. Otherwise Chromium on Linux
			// switches between grayscale and subpixel (LCD) smoothing as it
			// composites a page, and a screenshot of one page differs from
			// run to run.
			opts.Args = append(opts.Args, "--disable-lcd-text")
		}
		bt := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}[a.Engine]
		switch {
		case branded(a.Engine):
			bt, opts.Channel = pw.Chromium, playwright.String(a.Engine)
		case a.Engine == "chromium":
			// The whole of Chromium, with a window or without: not Playwright's
			// smaller headless build, which behaves otherwise (it never asks
			// a site for its icon, say), so that a run behaves the same
			// whether someone watches it or not.
			opts.Channel = playwright.String("chromium")
		}
		if !branded(a.Engine) {
			if err := wholeBrowser(s, a.Engine, bt); err != nil {
				return nil, launchError(a, err)
			}
		}
		b, err := bt.Launch(opts)
		if err != nil && !branded(a.Engine) && strings.Contains(err.Error(), "Executable doesn't exist") {
			if err = installBrowser(s, a.Engine); err == nil {
				b, err = bt.Launch(opts)
			}
		}
		if err != nil {
			return nil, launchError(a, err)
		}
		s.OnClose(func(context.Context) error { return b.Close() })
		// Playwright's Inspector, where a run pauses, is a Chromium window,
		// whatever the engine.
		if a.Engine != "chromium" && (s.Pausing() || st.pause) {
			if err := inspectorBrowser(s, pw); err != nil {
				return nil, err
			}
		}
		return b, nil
	})
}

// inspectorBrowser makes sure Playwright's Chromium is there, for the
// Inspector, once per run.
func inspectorBrowser(s *core.Suite, pw *playwright.Playwright) error {
	return wholeBrowser(s, "chromium", pw.Chromium)
}

// wholeBrowser makes sure Playwright's browser of an engine is there, and
// whole, before it starts, once per run. Another axx, a run beside this one,
// may be installing it: a browser started half written fails ("text file
// busy"). Playwright marks a browser installed whole; without the mark, its
// installer runs, which waits for another's to end and does nothing for a
// browser installed whole.
func wholeBrowser(s *core.Suite, engine string, bt playwright.BrowserType) error {
	_, err := core.Cached(s, Name+"/installed/"+engine, func() (bool, error) {
		if installedWhole(bt.ExecutablePath(), engine) {
			return true, nil
		}
		return true, installBrowser(s, engine)
	})
	return err
}

// installedWhole reports whether the browser of an executable has
// Playwright's mark of a finished install, in its folder (like
// chromium-1234) of Playwright's browsers.
func installedWhole(executable, engine string) bool {
	for dir := filepath.Dir(executable); filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if strings.HasPrefix(filepath.Base(dir), engine+"-") {
			_, err := os.Stat(filepath.Join(dir, "INSTALLATION_COMPLETE"))
			return err == nil
		}
	}
	return false
}

// branded reports whether an engine is a browser installed on the machine,
// Google Chrome or Microsoft Edge, rather than one of Playwright's.
func branded(engine string) bool { return engine == "chrome" || engine == "msedge" }

// installBrowser downloads Playwright's browser of an engine, where
// Playwright keeps its browsers ($PLAYWRIGHT_BROWSERS_PATH, or its folder in
// the user's cache), for the runs after this one too.
func installBrowser(s *core.Suite, engine string) error {
	dir, err := driverDir(s)
	if err != nil {
		return err
	}
	d, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: dir, SkipInstallBrowsers: true, Logger: s.Logger()})
	if err != nil {
		return err
	}
	s.Logger().Warn(fmt.Sprintf("downloading the %s browser of Playwright %s, once", engine, PlaywrightVersion))
	var out bytes.Buffer
	// Chromium without Playwright's headless build, which the pack does not run.
	cmd := d.Command("install", "--no-shell", engine)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		return fmt.Errorf("the download failed: %s", strings.Join(lines[max(0, len(lines)-5):], "\n  "))
	}
	return nil
}

func launchError(a *App, err error) error {
	msg := fmt.Sprintf("cannot start the %s browser: %s", a.Engine, firstLine(err))
	low := strings.ToLower(err.Error())
	switch {
	case branded(a.Engine) && (strings.Contains(low, "distribution") || strings.Contains(low, "executable doesn't exist")):
		name := map[string]string{"chrome": "Google Chrome", "msedge": "Microsoft Edge"}[a.Engine]
		return fmt.Errorf("%s\n  the %s engine is the %s installed on this machine: install it, or use chromium", msg, a.Engine, name)
	case strings.Contains(low, "missing dependencies") || strings.Contains(low, "shared libraries"):
		return fmt.Errorf("%s\n  the browser needs system libraries this machine lacks: install them with `sudo npx playwright@%s install-deps %s`", msg, PlaywrightVersion, a.Engine)
	case strings.Contains(low, "xserver") || strings.Contains(low, "$display"):
		return fmt.Errorf("%s\n  watching opens browser windows, and this machine has no screen: run without packs.web-core.watch", msg)
	}
	return errors.New(msg)
}

// announce tells the IDE running axx about a file a scenario keeps.
func announce(sc *core.Scenario, kind, path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sc.Suite().Announce(kind, "path", abs, "location", fmt.Sprintf("%s:%d", sc.URI, sc.Line))
}

// session is a scenario's browser context for one app: its tabs, the last
// one opened being the current one, and the dialog and downloads its pages
// brought up.
type session struct {
	app *App
	ctx playwright.BrowserContext

	mu     sync.Mutex
	tabs   []playwright.Page // open, in the order they opened
	opened []playwright.Page // every tab the session had, for videos
	aside  []playwright.Page // tabs set aside for tools (AsideTab)
	asking int               // tabs asked for tools, not opened yet

	// media are the CDP sessions that give Chromium's pages their media
	// (present).
	mediaMu sync.Mutex
	media   map[playwright.Page]playwright.CDPSession
	// dialog is the dialog a page shows, waiting for an answer; dialogOpened
	// is closed when one opens.
	dialog       playwright.Dialog
	dialogOpened chan struct{}
	// pending is the result of the action a dialog interrupted.
	pending   <-chan error
	downloads []*download
	// errors are the script errors of its pages: uncaught ones, and the
	// errors logged to the console.
	errors []string
	// requests are the requests its pages sent, as "METHOD url".
	requests []sent
	// clock is whether the pages' clock is Playwright's, set by a step.
	clock bool
	// tracing is whether the context records a trace, and group whether
	// the trace has a group open for the current step.
	tracing, group bool
}

type sent struct{ method, url string }

type download struct {
	d    playwright.Download
	name string
	path string // where it is saved, once a step asked for it
}

// page is the current tab.
func (s *session) page() (playwright.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tabs) == 0 {
		return nil, fmt.Errorf(`every browser tab of the %s web app is closed; open a page with "the {string} page is opened"`, s.app.Name)
	}
	return s.tabs[len(s.tabs)-1], nil
}

// addTab makes a page the current tab: the first one, or one a page opened,
// as a browser shows the tab a link opens.
func (s *session) addTab(p playwright.Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.opened, p) {
		return
	}
	s.tabs = append(s.tabs, p)
	s.opened = append(s.opened, p)
	p.OnClose(s.removeTab)
	// What happens in a tab set aside for a tool is not the scenario's.
	p.OnDownload(func(d playwright.Download) {
		s.mu.Lock()
		if !slices.Contains(s.aside, p) {
			s.downloads = append(s.downloads, &download{d: d, name: d.SuggestedFilename()})
		}
		s.mu.Unlock()
	})
	p.OnPageError(func(err error) {
		if !s.isAside(p) {
			s.scriptError(firstLine(err))
		}
	})
	p.OnConsole(func(m playwright.ConsoleMessage) {
		if m.Type() == "error" && !s.isAside(p) {
			s.scriptError(m.Text())
		}
	})
}

// claimAside claims a tab that opened while one was asked for tools
// (AsideTab): a tab no page opened. It comes to the page event before the
// tab is the scenario's, or the packs' hooks see it.
func (s *session) claimAside(p playwright.Page) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.asking == 0 {
		return false
	}
	if opener, _ := p.Opener(); opener != nil {
		return false
	}
	s.asking--
	s.aside = append(s.aside, p)
	return true
}

// setAside makes a tab one of the tabs set aside for tools (AsideTab): not
// one of the scenario's tabs, with no video.
func (s *session) setAside(p playwright.Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs = slices.DeleteFunc(s.tabs, func(t playwright.Page) bool { return t == p })
	s.opened = slices.DeleteFunc(s.opened, func(t playwright.Page) bool { return t == p })
	if !slices.Contains(s.aside, p) {
		s.aside = append(s.aside, p)
		s.asking = max(0, s.asking-1)
	}
}

func (s *session) isAside(p playwright.Page) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.aside, p)
}

func (s *session) scriptError(msg string) {
	s.mu.Lock()
	s.errors = append(s.errors, msg)
	s.mu.Unlock()
}

func (s *session) removeTab(p playwright.Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs = slices.DeleteFunc(s.tabs, func(t playwright.Page) bool { return t == p })
}

func (s *session) onDialog(d playwright.Dialog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialog != nil { // one dialog at a time: another tab's dialog is dismissed
		go func() { _ = d.Dismiss() }()
		return
	}
	s.dialog = d
	close(s.dialogOpened)
}

// openDialog is the dialog waiting for an answer, or nil.
func (s *session) openDialog() playwright.Dialog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dialog
}

// dialogOpen is the failure of a step that cannot go on while a dialog
// waits for an answer.
func dialogOpen(d playwright.Dialog) error {
	return core.Failf("The page shows a dialog: %q; answer it first, with %q or %q", d.Message(), "the dialog is accepted", "the dialog is dismissed")
}

// run runs a step's work on the page. A dialog that opens meanwhile blocks
// the page: the action it interrupted goes on once the dialog is answered,
// while a check fails.
func (s *session) run(action bool, work func() error) error {
	s.mu.Lock()
	opened := s.dialogOpened
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- work() }()
	select {
	case err := <-done:
		return err
	case <-opened:
		d := s.openDialog()
		if d == nil {
			return <-done
		}
		if !action {
			return dialogOpen(d)
		}
		s.mu.Lock()
		s.pending = done
		s.mu.Unlock()
		return nil
	}
}

// answer waits for a dialog, answers it, and waits for the action it
// interrupted to finish.
func (s *session) answer(sc *core.Scenario, wait time.Duration, how func(playwright.Dialog) error) error {
	d, err := s.waitDialog(sc, wait)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.dialog, s.dialogOpened = nil, make(chan struct{})
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	if err := how(d); err != nil {
		return fmt.Errorf("cannot answer the dialog: %s", firstLine(err))
	}
	if pending == nil {
		return nil
	}
	select {
	case err := <-pending:
		return err
	case <-time.After(navigationTimeout):
		return nil
	}
}

func (s *session) waitDialog(sc *core.Scenario, wait time.Duration) (playwright.Dialog, error) {
	var d playwright.Dialog
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		d = s.openDialog()
		return d != nil, nil
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, core.Failf("No dialog is open")
	}
	return d, nil
}

// pages are a scenario's sessions, and the one whose page was opened last.
type pages struct {
	mu      sync.Mutex
	byApp   map[string]*session
	order   []*session
	current *session
	// clock is the time a step set the browser's clock to, when it did, and
	// offline whether a step took the browser offline: they hold for every
	// web app of the scenario, those opened later too.
	clock   *clockSet
	offline bool
	// pauseOnOpen pauses the scenario in the first page it opens: the run
	// pauses before a step that opens it.
	pauseOnOpen bool
	// lastStep is the last step the scenario ran.
	lastStep *core.StepInfo
	// onContext are what packs that build on the web-core pack do with each
	// browser context, before its first page (OnContext).
	onContext []func(Context) error
	// onPage are what they do with each page, before it loads anything
	// (OnPage).
	onPage pageHooks
}

// pageHooks run what packs that build on the web-core pack do with each page,
// once a page.
type pageHooks struct {
	mu    sync.Mutex
	fns   []func(Context, playwright.Page) error
	ready map[playwright.Page]*pageReady
}

type pageReady struct {
	once sync.Once
	err  error
}

// prepare runs the hooks on a page, then then, once: a second call waits
// for the first.
func (h *pageHooks) prepare(c Context, p playwright.Page, then func() error) error {
	h.mu.Lock()
	if h.ready == nil {
		h.ready = map[playwright.Page]*pageReady{}
	}
	r, ok := h.ready[p]
	if !ok {
		r = &pageReady{}
		h.ready[p] = r
	}
	fns := slices.Clone(h.fns)
	h.mu.Unlock()
	r.once.Do(func() {
		for _, fn := range fns {
			if r.err = fn(c, p); r.err != nil {
				return
			}
		}
		r.err = then()
	})
	return r.err
}

type clockSet struct{ at, when time.Time }

// now is the browser clock's time now.
func (c *clockSet) now() time.Time { return c.at.Add(time.Since(c.when)) }

var scenarioPages = core.NewStateKey(Name+"/pages", func(*core.Scenario) *pages {
	return &pages{byApp: map[string]*session{}}
}, closePages)

// open returns the scenario's session for the app, starting it (a browser
// context of its own, recording what the settings keep) the first time, and
// makes it the current one.
func open(sc *core.Scenario, a *App) (*session, error) {
	st := scenarioPages.Of(sc)
	// A pause waits for a person: not while holding the scenario's pages.
	var pauseIn *session
	defer func() {
		if pauseIn != nil {
			pauseIn.pause(sc, fmt.Sprintf("%s:%d", sc.URI, sc.Step().Line))
		}
	}()
	st.mu.Lock()
	defer st.mu.Unlock()
	if s, ok := st.byApp[a.Name]; ok {
		st.current = s
		return s, nil
	}
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	b, err := browserFor(sc.Suite(), a)
	if err != nil {
		return nil, err
	}
	pw, err := playwrightFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	opts, err := contextOptions(pw, a, cfg)
	if err != nil {
		return nil, err
	}
	ctx, err := b.NewContext(opts)
	if err != nil {
		return nil, fmt.Errorf("cannot open a browser for the %s web app: %s", a.Name, firstLine(err))
	}
	ctx.SetDefaultTimeout(float64(actionTimeout.Milliseconds()))
	ctx.SetDefaultNavigationTimeout(float64(navigationTimeout.Milliseconds()))
	s := &session{app: a, ctx: ctx, dialogOpened: make(chan struct{})}
	if cfg.traces != "never" {
		err := ctx.Tracing().Start(playwright.TracingStartOptions{Screenshots: playwright.Bool(true), Snapshots: playwright.Bool(true)})
		s.tracing = err == nil
	}
	// A page a page opens loads at once: the hooks run beside it. (Page
	// events come on Playwright's dispatcher, which must not wait.)
	ctx.OnPage(func(p playwright.Page) {
		// A tab opened for a tool is not the scenario's, nor the packs'.
		if s.claimAside(p) {
			return
		}
		s.addTab(p)
		go func() {
			if err := st.onPage.prepare(Context{BrowserContext: ctx, app: a}, p, func() error { return s.present(p) }); err != nil {
				sc.Log("a page the %s web app opened: %v", a.Name, err)
			}
		}()
	})
	ctx.OnDialog(s.onDialog)
	ctx.OnRequest(func(r playwright.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, sent{r.Method(), r.URL()})
		s.mu.Unlock()
	})
	if err := startSession(ctx, a); err != nil {
		_ = ctx.Close()
		return nil, fmt.Errorf("cannot start the %s web app's session: %s", a.Name, firstLine(err))
	}
	for _, fn := range st.onContext {
		if err := fn(Context{BrowserContext: ctx, app: a}); err != nil {
			_ = ctx.Close()
			return nil, err
		}
	}
	if st.clock != nil {
		if err := s.setClock(st.clock.now()); err != nil {
			_ = ctx.Close()
			return nil, err
		}
	}
	if st.offline {
		_ = ctx.SetOffline(true)
	}
	pg, err := ctx.NewPage()
	if err != nil {
		_ = ctx.Close()
		return nil, fmt.Errorf("cannot open a page of the %s web app: %s", a.Name, firstLine(err))
	}
	present := func() error {
		if err := s.present(pg); err != nil {
			return fmt.Errorf("cannot open a page of the %s web app: %s", a.Name, firstLine(err))
		}
		return nil
	}
	if err := st.onPage.prepare(Context{BrowserContext: ctx, app: a}, pg, present); err != nil {
		_ = ctx.Close()
		return nil, err
	}
	s.addTab(pg)
	// The step that opened the session is its first group.
	if step := sc.Step(); step != nil {
		st.lastStep = step
		s.startGroup(sc, step)
		if st.pauseOnOpen {
			st.pauseOnOpen, pauseIn = false, s
		}
	}
	st.byApp[a.Name], st.current = s, s
	st.order = append(st.order, s)
	sc.Describe(Name, func() any { return describe(sc, st) })
	return s, nil
}

// present presents a page as the app's properties say, where a browser
// context cannot: the media its styles are for.
//
// In Chromium, the last CDP session opened on a page gives the page its
// media each time it loads a document, and a session that says none sets it
// back to the screen: a pack's page hook opens such sessions (OnPage). So
// the media has a session of its own, opened after the hooks' and opened
// again after a hook that comes later.
func (s *session) present(p playwright.Page) error {
	if s.app.Media == "" {
		return nil
	}
	media := map[string]*playwright.Media{"screen": playwright.MediaScreen, "print": playwright.MediaPrint}[s.app.Media]
	if err := p.EmulateMedia(playwright.PageEmulateMediaOptions{Media: media}); err != nil {
		return err
	}
	if !chromiumFamily(s.app.Engine) {
		return nil
	}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	if old := s.media[p]; old != nil {
		_ = old.Detach()
	}
	cdp, err := s.ctx.NewCDPSession(p)
	if err != nil {
		return err
	}
	if _, err := cdp.Send("Emulation.setEmulatedMedia", map[string]any{"media": s.app.Media}); err != nil {
		_ = cdp.Detach()
		return err
	}
	if s.media == nil {
		s.media = map[playwright.Page]playwright.CDPSession{}
	}
	s.media[p] = cdp
	return nil
}

// contextOptions present the browser as the app's properties say.
func contextOptions(pw *playwright.Playwright, a *App, cfg *settings) (playwright.BrowserNewContextOptions, error) {
	var o playwright.BrowserNewContextOptions
	if a.Device != "" {
		d, ok := pw.Devices[a.Device]
		if !ok {
			return o, unknownDevice(pw, a)
		}
		o.UserAgent = playwright.String(d.UserAgent)
		o.DeviceScaleFactor = playwright.Float(d.DeviceScaleFactor)
		o.IsMobile = playwright.Bool(d.IsMobile)
		o.HasTouch = playwright.Bool(d.HasTouch)
		if d.Viewport != nil {
			o.Viewport = &playwright.Size{Width: d.Viewport.Width, Height: d.Viewport.Height}
		}
		if d.Screen != nil {
			o.Screen = &playwright.Size{Width: d.Screen.Width, Height: d.Screen.Height}
		}
	}
	if a.Viewport != nil {
		o.Viewport = &playwright.Size{Width: a.Viewport.Width, Height: a.Viewport.Height}
	}
	// Without them, the browser takes the machine's language and time zone,
	// and the same scenario would run differently on another machine.
	locale, timezone := a.Locale, a.Timezone
	if locale == "" {
		locale = "en-US"
	}
	if timezone == "" {
		timezone = "UTC"
	}
	o.Locale = playwright.String(locale)
	o.TimezoneId = playwright.String(timezone)
	if a.ColorScheme != "" {
		o.ColorScheme = map[string]*playwright.ColorScheme{
			"light": playwright.ColorSchemeLight, "dark": playwright.ColorSchemeDark,
			"no-preference": playwright.ColorSchemeNoPreference,
		}[a.ColorScheme]
	}
	if a.ReducedMotion != "" {
		o.ReducedMotion = map[string]*playwright.ReducedMotion{
			"reduce":        playwright.ReducedMotionReduce,
			"no-preference": playwright.ReducedMotionNoPreference,
		}[a.ReducedMotion]
	}
	if a.UserAgent != "" {
		o.UserAgent = playwright.String(a.UserAgent)
	}
	if a.Location != nil {
		o.Geolocation = &playwright.Geolocation{Latitude: a.Location.Latitude, Longitude: a.Location.Longitude}
	}
	if a.InsecureTLS {
		o.IgnoreHttpsErrors = playwright.Bool(true)
	}
	origin := a.origin()
	if a.Username != "" {
		o.HttpCredentials = &playwright.HttpCredentials{Username: a.Username, Password: a.Password, Origin: playwright.String(origin)}
	}
	// Cookies and local storage for the app's address, from the start.
	if len(a.Cookies) > 0 || len(a.LocalStorage) > 0 {
		state := &playwright.OptionalStorageState{}
		for _, c := range a.Cookies {
			state.Cookies = append(state.Cookies, playwright.OptionalCookie{Name: c.Name, Value: c.Value, URL: playwright.String(a.URL + "/")})
		}
		if len(a.LocalStorage) > 0 {
			o := playwright.Origin{Origin: origin}
			for _, it := range a.LocalStorage {
				o.LocalStorage = append(o.LocalStorage, playwright.NameValue{Name: it.Name, Value: it.Value})
			}
			state.Origins = append(state.Origins, o)
		}
		o.StorageState = state
	}
	if cfg.videos != "never" {
		dir, err := os.MkdirTemp("", "axx-web-video-")
		if err != nil {
			return o, err
		}
		o.RecordVideo = &playwright.RecordVideo{Dir: playwright.String(dir)}
	}
	return o, nil
}

// sessionStorageJS fills a tab's session storage for the app's address,
// once per tab, before the page's own scripts run.
const sessionStorageJS = `(() => {
  const origin = %s, items = %s, done = 'axx:session-storage';
  if (location.origin !== origin || sessionStorage.getItem(done)) return;
  for (const [k, v] of items) sessionStorage.setItem(k, v);
  sessionStorage.setItem(done, '1');
})();`

// startSession gives the context what the app's properties start it with
// beyond its options: permissions, headers and session storage, for the
// app's address only.
func startSession(ctx playwright.BrowserContext, a *App) error {
	origin := a.origin()
	perms := slices.Clone(a.Permissions)
	if a.Location != nil && !slices.Contains(perms, "geolocation") {
		perms = append(perms, "geolocation")
	}
	if len(perms) > 0 {
		if err := ctx.GrantPermissions(perms, playwright.BrowserContextGrantPermissionsOptions{Origin: playwright.String(origin)}); err != nil {
			return err
		}
	}
	if len(a.Headers) > 0 {
		err := ctx.Route("**/*", func(r playwright.Route) {
			if !sameOrigin(r.Request().URL(), origin) {
				_ = r.Continue()
				return
			}
			h := r.Request().Headers()
			for _, it := range a.Headers {
				h[strings.ToLower(it.Name)] = it.Value
			}
			_ = r.Continue(playwright.RouteContinueOptions{Headers: h})
		})
		if err != nil {
			return err
		}
	}
	if len(a.SessionStorage) > 0 {
		items := make([][2]string, len(a.SessionStorage))
		for i, it := range a.SessionStorage {
			items[i] = [2]string{it.Name, it.Value}
		}
		o, _ := json.Marshal(origin)
		b, _ := json.Marshal(items)
		script := fmt.Sprintf(sessionStorageJS, o, b)
		if err := ctx.AddInitScript(playwright.Script{Content: &script}); err != nil {
			return err
		}
	}
	return nil
}

func sameOrigin(address, origin string) bool {
	u, err := url.Parse(address)
	return err == nil && u.Scheme+"://"+u.Host == origin
}

func unknownDevice(pw *playwright.Playwright, a *App) error {
	var like []string
	want := strings.ToLower(strings.Fields(a.Device + " x")[0])
	for name := range pw.Devices {
		if strings.Contains(strings.ToLower(name), want) && !strings.HasSuffix(name, " landscape") {
			like = append(like, fmt.Sprintf("%q", name))
		}
	}
	slices.Sort(like)
	if len(like) == 0 {
		like = []string{`"Desktop Chrome"`, `"Galaxy S24"`, `"iPad Pro 11"`, `"iPhone 15"`, `"Pixel 7"`}
	}
	return fmt.Errorf("the %s web app's device %q is not one Playwright knows; %s", a.Name, a.Device, cloudstep.Shown("devices like it", like, 12))
}

// current is the scenario's current session.
func current(sc *core.Scenario) (*session, error) {
	st := scenarioPages.Of(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.current == nil {
		return nil, errors.New(`no page is open; open one with "the {string} page is opened"`)
	}
	return st.current, nil
}

// describe is the pages' state for failure reports.
func describe(sc *core.Scenario, st *pages) any {
	r := secrets.Replacer(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	if r == nil {
		r = strings.NewReplacer()
	}
	out := map[string]any{}
	for _, s := range st.order {
		pg, err := s.page()
		if err != nil {
			out[s.app.Name] = map[string]string{"tabs": "all closed"}
			continue
		}
		d := map[string]string{"url": r.Replace(pg.URL())}
		if dl := s.openDialog(); dl != nil {
			d["dialog"] = r.Replace(dl.Message())
		} else if title, err := pg.Title(); err == nil {
			d["title"] = r.Replace(title)
		}
		out[s.app.Name] = d
	}
	return out
}

// closePages ends the scenario's browser contexts. A failed scenario may
// pause first, and keeps what the settings say: its traces (secrets
// masked), videos and downloads.
func closePages(sc *core.Scenario, st *pages) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		cfg = &settings{traces: "failed", videos: "never"}
	}
	failed := sc.Status() == "failed"
	if failed && cfg.pause {
		pauseWhereFailed(sc, st)
	}
	scrubber := secrets.Replacer(sc)
	mask := scrubber
	st.mu.Lock()
	defer st.mu.Unlock()
	if mask == nil {
		mask = strings.NewReplacer()
	}
	var errs []error
	for _, s := range st.order {
		if d := s.openDialog(); d != nil {
			_ = d.Dismiss()
		}
		if scriptErrs := s.scriptErrors(); len(scriptErrs) > 0 {
			shown := mask.Replace(cloudstep.Shown("script errors", scriptErrs, 10))
			switch {
			case cfg.failOnScriptErrors:
				errs = append(errs, core.Failf("The %s web app's pages had %s", s.app.Name, shown))
			case failed:
				sc.Log("the %s web app's pages had %s", s.app.Name, shown)
			}
		}
		if cfg.traces != "never" {
			if keep(cfg.traces, failed) {
				keepTrace(sc, s, scrubber)
			} else {
				_ = s.ctx.Tracing().Stop()
			}
		}
		if err := s.ctx.Close(); err != nil {
			errs = append(errs, err)
		}
		if cfg.videos != "never" {
			keepVideos(sc, s, keep(cfg.videos, failed))
		}
		keepDownloads(sc, s, failed)
	}
	return errors.Join(errs...)
}

// scriptErrors are the session's script errors, each once.
func (s *session) scriptErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.errors {
		if q := fmt.Sprintf("%q", e); !slices.Contains(out, q) {
			out = append(out, q)
		}
	}
	return out
}

func keepTrace(sc *core.Scenario, s *session, mask *strings.Replacer) {
	path := artifactPath(sc, "traces", s.app, ".zip")
	if err := s.ctx.Tracing().Stop(path); err != nil {
		return
	}
	if mask != nil {
		if err := scrubTrace(path, mask); err != nil {
			_ = os.Remove(path)
			sc.Log("the %s web app's trace was not kept: its secrets could not be masked: %v", s.app.Name, err)
			return
		}
	}
	sc.Log("the %s web app's trace: %s (open it with `npx playwright show-trace`, or on trace.playwright.dev)",
		s.app.Name, relative(sc, path))
	attachFile(sc, path, "application/zip", s.app.Name+" trace")
	announce(sc, "trace", path)
}

// attachFile attaches a file the scenario keeps, for the reports.
func attachFile(sc *core.Scenario, path, mediaType, name string) {
	if b, err := os.ReadFile(path); err == nil {
		sc.Attach(mediaType, b, name)
	}
}

// keepVideos saves (or drops) the videos of the session's tabs, complete
// once the context is closed.
func keepVideos(sc *core.Scenario, s *session, save bool) {
	s.mu.Lock()
	tabs := slices.Clone(s.opened)
	aside := slices.Clone(s.aside)
	s.mu.Unlock()
	for _, p := range aside {
		if v := p.Video(); v != nil {
			_ = v.Delete()
		}
	}
	for i, p := range tabs {
		v := p.Video()
		if v == nil {
			continue
		}
		if !save {
			_ = v.Delete()
			continue
		}
		ext := ".webm"
		if len(tabs) > 1 {
			ext = fmt.Sprintf("-tab%d.webm", i+1)
		}
		path := artifactPath(sc, "videos", s.app, ext)
		if err := v.SaveAs(path); err == nil {
			sc.Log("the %s web app's video: %s", s.app.Name, relative(sc, path))
			attachFile(sc, path, "video/webm", s.app.Name+" video")
			announce(sc, "video", path)
		}
	}
}

// keepDownloads keeps the files a failed scenario downloaded, and removes
// those of a scenario that passed.
func keepDownloads(sc *core.Scenario, s *session, failed bool) {
	s.mu.Lock()
	dls := slices.Clone(s.downloads)
	s.mu.Unlock()
	for _, d := range dls {
		if d.path == "" {
			continue
		}
		if failed {
			sc.Log("the %s web app downloaded %s", s.app.Name, relative(sc, d.path))
			continue
		}
		_ = os.Remove(d.path)
		_ = os.Remove(filepath.Dir(d.path))
	}
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9]+`)

// scenarioName names a scenario's files: its name, and the end of its id.
func scenarioName(sc *core.Scenario) (name, id string) {
	name = strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(sc.Name), "-"), "-")
	if len(name) > 60 {
		name = name[:60]
	}
	id = unsafeName.ReplaceAllString(sc.ID, "")
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	return name, id
}

// artifactPath is where a scenario keeps a file for an app, in .axx/web/<kind>.
func artifactPath(sc *core.Scenario, kind string, a *App, ext string) string {
	dir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "web", kind)
	_ = os.MkdirAll(dir, 0o755)
	name, id := scenarioName(sc)
	return filepath.Join(dir, fmt.Sprintf("%s-%s-%s%s", name, a.Name, id, ext))
}

func relative(sc *core.Scenario, path string) string {
	if rel, err := filepath.Rel(sc.Suite().ProjectDir(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// screenshot attaches the current page as it is, for a failed step.
func screenshot(sc *core.Scenario, s *session) {
	if s.openDialog() != nil {
		return
	}
	pg, err := s.page()
	if err != nil {
		return
	}
	b, err := pg.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true), Timeout: playwright.Float(5000)})
	if err == nil {
		sc.Attach("image/png", b, "screenshot")
	}
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(s)
}

// chromiumFamily reports whether an engine is Chromium or a browser built
// on it.
func chromiumFamily(engine string) bool {
	return engine == "chromium" || branded(engine)
}

// debugging is the remote debugging ports of a run's browsers, when packs
// asked for them (RemoteDebugging).
type debugging struct {
	mu    sync.Mutex
	asked bool
	ports map[string]int // by browser key
}

func debuggingFor(s *core.Suite) *debugging {
	d, _ := core.Cached(s, Name+"/debugging", func() (*debugging, error) { return &debugging{ports: map[string]int{}}, nil })
	return d
}

func (d *debugging) set(asked bool) {
	d.mu.Lock()
	d.asked = asked
	d.mu.Unlock()
}

func (d *debugging) port(key string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ports[key]
}

// debuggingPort is the remote debugging port an app's browser opens: a free
// one when a pack asked for it, and 0 otherwise.
func debuggingPort(s *core.Suite, a *App) (int, error) {
	d := debuggingFor(s)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.asked || !chromiumFamily(a.Engine) {
		return 0, nil
	}
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("no free port for the browser's remote debugging: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	d.ports[a.key()] = port
	return port, nil
}
