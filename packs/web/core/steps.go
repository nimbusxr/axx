package webcore

import (
	"fmt"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/filecontent"
)

const since = "0.1.1"

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "web-core.app", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} web app with the following properties:",
			Doc:  "Register a web app, and how the browser presents itself to it. The first web app registered is the default.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "where the browser finds the app; page paths are below it", Required: true},
					{Name: "engine", Takes: "the browser; `chrome` and `msedge` are those installed on the machine", Values: engines, Default: "chromium"},
					{Name: "device", Takes: "a device Playwright knows, like `iPhone 15` or `Pixel 7`: its screen, user agent and touch"},
					{Name: "viewport", Takes: "the size of the page, like `1280x720`", Default: "1280x720"},
					{Name: "locale", Takes: "the browser's language, like `de-DE`", Default: "en-US"},
					{Name: "timezone", Takes: "the browser's time zone, like `Europe/Berlin`", Default: "UTC"},
					{Name: "color scheme", Takes: "what `prefers-color-scheme` says", Values: colorSchemes},
					{Name: "reduced motion", Takes: "what `prefers-reduced-motion` says", Values: motions},
					{Name: "media", Takes: "the media the page's styles are for", Values: medias, Default: "screen"},
					{Name: "user agent", Takes: "the user agent the browser sends"},
					{Name: "location", Takes: "where the browser says it is: a latitude and a longitude, like `52.5200, 13.4050`"},
					{Name: "permissions", Takes: "the permissions the app has, like `notifications, clipboard-read`"},
					{Name: "tls.verify", Takes: "`false` accepts the app's certificate, whatever it is", Default: "true"},
					{Name: "username", Takes: "the username for the app's HTTP authentication, with `password`"},
					{Name: "password", Takes: "the password for the app's HTTP authentication, with `username`"},
					{Name: "cookie.<name>", Takes: "a cookie the browser starts with, for the app's address"},
					{Name: "header.<name>", Takes: "a header the browser sends the app"},
					{Name: "local storage.<key>", Takes: "an item of the app's local storage, from the start"},
					{Name: "session storage.<key>", Takes: "an item of the app's session storage, from the start"},
				},
			},
			Examples: []string{"Given the parcels web app with the following properties:\n  | url    | http://localhost:8400/portal |\n  | engine | webkit                       |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				// Values from ${env:..}, such as a session's token, are secrets.
				app, err := parseApp(func(v string) string { return expand(sc, v) }, a.String(0), a.Table)
				if err != nil {
					return err
				}
				return apps.Of(sc).Add(app.Name, app)
			},
		},
		{
			ID: "web-core.open", Keyword: "When", Since: since,
			Expr: "the {string} page[[ of the {word} web app]] is opened",
			Doc: "Open a page of the web app: a path below its `url`, or a whole URL. The steps that follow act on this page. " +
				"A page that answers with an error status (404, 500...) fails the step.",
			Examples: []string{`When the "/quotes" page is opened`, `When the "/parcels/PX-4101" page of the portal web app is opened`},
			Run:      openPage,
		},
		{
			ID: "web-core.fill", Keyword: "When", Since: since,
			Expr: "the {string} field is filled with {string}",
			Doc: "Type a value into the field with that label (or, without a label, that placeholder), replacing what it holds.\n\n" +
				"- Date fields take dates as `2026-10-01`.\n" +
				"- A value from `${env:…}`, such as a password, is a secret: failures and traces show it as `********`.",
			Examples: []string{`When the "Weight (grams)" field is filled with "1200"`, `When the "Password" field is filled with "${env:SHOP_PASSWORD}"`},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				name, value := text(sc, a, 0), text(sc, a, 1)
				return act(sc, s, field, name, "fill in", func(l playwright.Locator) error { return l.Fill(value) })
			}),
		},
		{
			ID: "web-core.select", Keyword: "When", Since: since,
			Expr:     "{string} is chosen in the {string} field",
			Doc:      "Choose the option with that text in a drop-down field.",
			Examples: []string{`When "Germany" is chosen in the "Destination country" field`},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				choice, name := text(sc, a, 0), text(sc, a, 1)
				loc, err := s.find(sc, field, name, actionTimeout)
				if err != nil {
					return err
				}
				if _, err := loc.SelectOption(playwright.SelectOptionValues{Labels: &[]string{choice}}); err != nil {
					return missingOption(loc, name, choice, err)
				}
				return nil
			}),
		},
		clickStep("web-core.option", option, "the {string} option is chosen", "choose",
			"Choose the radio button with that label.", `When the "Express" option is chosen`,
			func(l playwright.Locator) error { return l.Check() }),
		clickStep("web-core.check", checkbox, "the {string} checkbox is checked", "check",
			"Tick the checkbox with that label (a checkbox already ticked stays so).", `When the "Insure this parcel" checkbox is checked`,
			func(l playwright.Locator) error { return l.Check() }),
		clickStep("web-core.uncheck", checkbox, "the {string} checkbox is unchecked", "uncheck",
			"Clear the checkbox with that label (a checkbox already clear stays so).", `When the "Leave with a neighbour" checkbox is unchecked`,
			func(l playwright.Locator) error { return l.Uncheck() }),
		{
			ID: "web-core.upload", Keyword: "When", Since: since,
			Expr: "the {filepath} file is uploaded in the {string} field",
			Doc: "Choose a file of the project for the file field with that label, as if picked in the file dialog; or for a " +
				"button with that name that opens the file dialog.",
			Examples: []string{`When the invoices/INV-2041.pdf file is uploaded in the "Customs invoice" field`},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				path, err := sc.Suite().ResolvePath(a.String(0))
				if err != nil {
					return err
				}
				return s.upload(sc, text(sc, a, 1), path)
			}),
		},
		clickStep("web-core.button", button, "the {string} button is clicked", "click",
			"Click the button with that name: its text, or its label.", `When the "Get a quote" button is clicked`,
			func(l playwright.Locator) error { return l.Click() }),
		clickStep("web-core.link", link, "the {string} link is clicked", "click",
			"Click the link with that text. A link that opens a new browser tab makes it the current tab, as a browser shows it.",
			`When the "Track a parcel" link is clicked`,
			func(l playwright.Locator) error { return l.Click() }),
		clickStep("web-core.tab", tab, "the {string} tab is clicked", "click",
			"Click the tab with that name, in a row of tabs on the page.", `When the "History" tab is clicked`,
			func(l playwright.Locator) error { return l.Click() }),
		clickStep("web-core.menuitem", menuItem, "the {string} menu item is clicked", "click",
			"Click the item with that name in an open menu: a plain one, one that ticks on and off, or one that picks one of a "+
				"group (`menuitem`, `menuitemcheckbox`, `menuitemradio`).", `When the "Cancel the parcel" menu item is clicked`,
			func(l playwright.Locator) error { return l.Click() }),
		clickStep("web-core.element.click", element, "the {string} element is clicked", "click",
			"Click an element that is none of the above: by its text, its label, an image's alternative text or its title, or by "+
				"a selector (`css=…`, `xpath=…` or `testid=…`).",
			`When the "css=.parcel-card" element is clicked`,
			func(l playwright.Locator) error { return l.Click() }),
		{
			ID: "web-core.key", Keyword: "When", Since: since,
			Expr: "the {word} key is pressed[[ in the {string} field]]",
			Doc: "Press a key: `Enter`, `Escape`, `Tab`, `ArrowDown`, a letter, or a combination like `Control+A`. The key goes " +
				"to the field with that label, or to whatever has the focus.",
			Examples: []string{`When the Enter key is pressed in the "Find a parcel" field`, "When the Escape key is pressed"},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				key := text(sc, a, 0)
				if a.Present(1) {
					return act(sc, s, field, text(sc, a, 1), "press "+key+" in", func(l playwright.Locator) error { return l.Press(key) })
				}
				pg, err := s.page()
				if err != nil {
					return err
				}
				if err := pg.Keyboard().Press(key); err != nil {
					return fmt.Errorf("cannot press the %s key: %s", key, firstLine(err))
				}
				return nil
			}),
		},
		{
			ID: "web-core.hover", Keyword: "When", Since: since,
			Expr: "the pointer is moved over {string}",
			Doc: "Move the pointer over the button, link, tab or menu item with that name, or else over that text, so that what " +
				"opens on hover opens: a menu, a tip.",
			Examples: []string{`When the pointer is moved over "About Express"`},
			Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
				return act(sc, s, pointable, text(sc, a, 0), "move the pointer over", func(l playwright.Locator) error { return l.Hover() })
			}),
		},
		{
			ID: "web-core.reload", Keyword: "When", Since: since,
			Expr:     "the page is reloaded",
			Doc:      "Reload the current page. A page that answers with an error status fails the step.",
			Examples: []string{"When the page is reloaded"},
			Run: action(func(sc *core.Scenario, s *session, _ core.Args) error {
				pg, err := s.page()
				if err != nil {
					return err
				}
				resp, err := pg.Reload()
				if err != nil {
					return fmt.Errorf("cannot reload the page: %s", firstLine(err))
				}
				return answered(s.app, "the page", resp)
			}),
		},
		{
			ID: "web-core.back", Keyword: "When", Since: since,
			Expr:     "the browser's back button is clicked",
			Doc:      "Go back to the page before, as the browser's back button does.",
			Examples: []string{"When the browser's back button is clicked"},
			Run: action(func(sc *core.Scenario, s *session, _ core.Args) error {
				pg, err := s.page()
				if err != nil {
					return err
				}
				if _, err := pg.GoBack(); err != nil {
					return fmt.Errorf("cannot go back: %s", firstLine(err))
				}
				return nil
			}),
		},
		{
			ID: "web-core.closetab", Keyword: "When", Since: since,
			Expr: "the browser tab is closed",
			Doc: "Close the current browser tab, such as one a link opened; the tab it was opened from becomes the current tab " +
				"again.",
			Examples: []string{"When the browser tab is closed"},
			Run: action(func(sc *core.Scenario, s *session, _ core.Args) error {
				pg, err := s.page()
				if err != nil {
					return err
				}
				if err := pg.Close(); err != nil {
					return fmt.Errorf("cannot close the browser tab: %s", firstLine(err))
				}
				s.removeTab(pg)
				return nil
			}),
		},
		{
			ID: "web-core.accept", Keyword: "When", Since: since,
			Expr: "the dialog is accepted",
			Doc: "Accept the dialog the page shows (its OK): a message, a confirmation or a question. The page waits for an " +
				"answer, so every other step fails while a dialog is open. The step waits for the dialog: 10 seconds.",
			Examples: []string{"When the dialog is accepted"},
			Run: onDialog(func(sc *core.Scenario, s *session, _ core.Args) error {
				return s.answer(sc, actionTimeout, func(d playwright.Dialog) error { return d.Accept() })
			}),
		},
		{
			ID: "web-core.dismiss", Keyword: "When", Since: since,
			Expr:     "the dialog is dismissed",
			Doc:      "Dismiss the dialog the page shows (its Cancel). The step waits for the dialog: 10 seconds.",
			Examples: []string{"When the dialog is dismissed"},
			Run: onDialog(func(sc *core.Scenario, s *session, _ core.Args) error {
				return s.answer(sc, actionTimeout, func(d playwright.Dialog) error { return d.Dismiss() })
			}),
		},
		{
			ID: "web-core.answer", Keyword: "When", Since: since,
			Expr:     "the dialog is answered with {string}",
			Doc:      "Type an answer to the question the dialog asks, and accept it. The step waits for the dialog: 10 seconds.",
			Examples: []string{`When the dialog is answered with "The parcel arrived damaged"`},
			Run: onDialog(func(sc *core.Scenario, s *session, a core.Args) error {
				answer := text(sc, a, 0)
				d, err := s.waitDialog(sc, actionTimeout)
				if err != nil {
					return err
				}
				if d.Type() != "prompt" {
					return core.Failf("The dialog asks no question (it is a %s: %q); accept or dismiss it", d.Type(), d.Message())
				}
				return s.answer(sc, actionTimeout, func(d playwright.Dialog) error { return d.Accept(answer) })
			}),
		},
		{
			ID: "web-core.dialog", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the dialog shows {string}",
			Doc: "Check that the page shows a dialog with the text, as part of its message (case matters). The check waits for a " +
				"dialog: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the dialog shows "Cancel parcel PX-4101?"`},
			Run: onDialog(func(sc *core.Scenario, s *session, a core.Args) error {
				want := text(sc, a, 1)
				d, err := s.waitDialog(sc, cloudstep.Wait(a, 0))
				if err != nil {
					return err
				}
				if !strings.Contains(d.Message(), want) {
					return core.Fail(fmt.Sprintf("The dialog does not show %q", want), want, d.Message())
				}
				return nil
			}),
		},
		{
			ID: "web-core.shows", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the page shows {string}",
			Doc: "Check that the page shows the text, anywhere, as part of what it shows (case matters). The check waits for it: " +
				"10 seconds, or `within {duration}`.",
			Examples: []string{`Then the page shows "6.90 EUR"`, `Then within 30s the page shows "Parcel PX-4101 registered"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want := text(sc, a, 1)
				var pg playwright.Page
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					var err error
					if pg, err = s.page(); err != nil {
						return false, err
					}
					return shown(pg, want) > 0, nil
				})
				if err != nil || ok {
					return err
				}
				return core.Fail(fmt.Sprintf("The page does not show %q", want), want, visibleText(pg, want))
			}),
		},
		{
			ID: "web-core.hides", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the page does not show {string}",
			Doc: "Check that the page does not show the text anywhere (case matters). The check waits for the text to go: " +
				"10 seconds, or `within {duration}`.",
			Examples: []string{`Then the page does not show "Delivery not possible"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want := text(sc, a, 1)
				var pg playwright.Page
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					var err error
					if pg, err = s.page(); err != nil {
						return false, err
					}
					return shown(pg, want) == 0, nil
				})
				if err != nil || ok {
					return err
				}
				return core.Fail(fmt.Sprintf("The page shows %q", want), "no "+fmt.Sprintf("%q", want), visibleText(pg, want))
			}),
		},
		{
			ID: "web-core.url", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} page is shown",
			Doc: "Check which page the browser is on: its path below the web app's `url` (with the query, when the step gives one), " +
				"or its whole URL. The check waits for the browser to get there: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "/quotes/Q-3317" page is shown`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want := text(sc, a, 1)
				absolute := strings.HasPrefix(want, "http://") || strings.HasPrefix(want, "https://")
				if !absolute && !strings.HasPrefix(want, "/") {
					want = "/" + want
				}
				var got string
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					pg, err := s.page()
					if err != nil {
						return false, err
					}
					got = pg.URL()
					if absolute {
						got, _, _ = strings.Cut(got, "#")
					} else {
						got = s.app.pagePath(got, want)
					}
					return got == want, nil
				})
				if err != nil || ok {
					return err
				}
				return core.Fail("The browser is on another page", want, got)
			}),
		},
		{
			ID: "web-core.value", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} field has the value {string}",
			Doc: "Check the value a field shows: what it holds, or for a drop-down, the text of the option chosen. " +
				"The check waits for it: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the "Weight (grams)" field has the value "1200"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, want := text(sc, a, 1), text(sc, a, 2)
				d := cloudstep.Wait(a, 0)
				loc, err := s.find(sc, field, name, d)
				if err != nil {
					return err
				}
				var got string
				ok, err := waitUntil(sc, d, func() (bool, error) {
					v, err := loc.Evaluate(`(e) => e.tagName === 'SELECT' ? (e.selectedOptions[0] ? e.selectedOptions[0].text : '') : e.isContentEditable ? e.innerText : e.value`, nil)
					if err != nil {
						return false, err
					}
					got, _ = v.(string)
					return got == want, nil
				})
				if err != nil || ok {
					return err
				}
				return core.Fail(fmt.Sprintf("The %q field has another value", name), want, got)
			}),
		},
		enabledStep("web-core.disabled", button, "disabled", false, `Then the "Register the parcel" button is disabled`),
		enabledStep("web-core.enabled", button, "enabled", true, `Then within 5s the "Register the parcel" button is enabled`),
		{
			ID: "web-core.row", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the page shows a table row where:",
			Doc: "Check that a table on the page has a row with those values in those columns. " +
				"The check waits for it: 10 seconds, or `within {duration}`.",
			Table:    &core.TableDoc{Columns: []string{"column", "value"}, Note: "Each row names a column, by its header, and the value the table's row has in it."},
			Examples: []string{"Then the page shows a table row where:\n  | Parcel | PX-WEB-5116 |\n  | Status | IN_TRANSIT  |"},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				want, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				for i := range want {
					want[i].Value = expand(sc, want[i].Value)
				}
				var why string
				ok, err := waitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
					pg, err := s.page()
					if err != nil {
						return false, err
					}
					var ok bool
					ok, why = matchRow(tables(pg), want)
					return ok, nil
				})
				if err != nil || ok {
					return err
				}
				parts := make([]string, len(want))
				for i, p := range want {
					parts[i] = p.Key + "=" + p.Value
				}
				return core.Failf("No table row on the page has %s; %s", strings.Join(parts, ", "), why)
			}),
		},
		{
			ID: "web-core.downloaded", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {string} file is downloaded",
			Doc: "Check that the browser downloaded a file with that name, as the browser names it. The check waits for it: " +
				"10 seconds, or `within {duration}`. A failed scenario keeps the downloads its steps checked in `.axx/web/downloads`.",
			Examples: []string{`Then the "PX-4101-label.zpl" file is downloaded`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				_, err := s.downloaded(sc, text(sc, a, 1), cloudstep.Wait(a, 0))
				return err
			}),
		},
		{
			ID: "web-core.download.contains", Keyword: "Then", Since: since,
			Expr: "the downloaded {string} file contains {string}",
			Doc: "Check that a file the browser downloaded holds the text.\n\n" +
				"- Case matters; runs of spaces and line breaks count as one space.\n" +
				"- The text is read by the file's type: the text of a PDF's pages, the paragraphs and tables of a Word document " +
				"(.docx), the cells of an Excel workbook (.xlsx), or the text itself.",
			Examples: []string{`Then the downloaded "parcels.csv" file contains "PX-4101,Anna Weber,IN_TRANSIT"`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name, want := text(sc, a, 0), text(sc, a, 1)
				f, err := s.downloadedFile(sc, name)
				if err != nil {
					return err
				}
				if k := f.Kind(); k == filecontent.Binary {
					return fmt.Errorf("the downloaded %q file is %s (%d bytes), not a file whose text axx reads (%s)",
						name, k.Noun(), len(f.Body), filecontent.Readable)
				}
				ok, got, err := f.Contains(want)
				if err != nil {
					return fmt.Errorf("cannot read the downloaded %q file: %w", name, err)
				}
				if !ok {
					return core.Failf("The downloaded %q file does not contain %q. %s", name, want, filecontent.Nearest(got, want))
				}
				return nil
			}),
		},
		{
			ID: "web-core.download.row", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the downloaded {string} file has a row where:",
			Doc: "Check that the table of a file the browser downloaded has a row with those values in those columns: a CSV or TSV " +
				"file, or an Excel workbook (.xlsx, its first sheet), whose first row names the columns.",
			Table:    &core.TableDoc{Columns: []string{"column", "value"}, Note: "Each row names a column, as the file's first row names it, and the value the file's row has in it."},
			Examples: []string{"Then the downloaded \"parcels-birch-and-bloom.csv\" file has a row where:\n  | Parcel | PX-WEB-5401 |\n  | Status | REGISTERED  |"},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				name := text(sc, a, 0)
				want, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				for i := range want {
					want[i].Value = expand(sc, want[i].Value)
				}
				f, err := s.downloadedFile(sc, name)
				if err != nil {
					return err
				}
				if k := f.Kind(); !k.HasTable() {
					return fmt.Errorf("the downloaded %q file is %s, not a table axx reads (CSV, TSV, Excel .xlsx)", name, k.Noun())
				}
				t, err := f.Table("")
				if err != nil {
					return fmt.Errorf("cannot read the downloaded %q file: %w", name, err)
				}
				if ok, why := t.FindRow(want); !ok {
					return core.Failf("The downloaded %q file has no row where %s: %s", name, describePairs(want), why)
				}
				return nil
			}),
		},
		{
			ID: "web-core.download.identical", Keyword: "Then", Since: since,
			Expr:     "the downloaded {string} file is identical to the {filepath} file",
			Doc:      "Check that a file the browser downloaded has exactly the content of a file of the project.",
			Examples: []string{`Then the downloaded "PX-4101-label.zpl" file is identical to the labels/PX-4101.zpl file`},
			Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
				return s.identical(sc, text(sc, a, 0), a.String(1))
			}),
		},
	}
}

// upload chooses a file for the file field named name, or for the button
// named name that opens the file dialog.
func (s *session) upload(sc *core.Scenario, name, path string) error {
	var loc playwright.Locator
	isButton := false
	ok, err := waitUntil(sc, actionTimeout, func() (bool, error) {
		var n int
		var err error
		if loc, n, err = s.count(field, name); err != nil || n > 0 {
			return n > 0, err
		}
		loc, n, err = s.count(button, name)
		isButton = n > 0
		return n > 0, err
	})
	if err != nil {
		return err
	}
	if !ok {
		return s.none(field, name)
	}
	if !isButton {
		if err := loc.SetInputFiles(path); err != nil {
			return fmt.Errorf("cannot upload a file in the %q field: %s", name, firstLine(err))
		}
		return nil
	}
	pg, err := s.page()
	if err != nil {
		return err
	}
	chooser, err := pg.ExpectFileChooser(func() error { return loc.Click() })
	if err != nil {
		return fmt.Errorf("the %q button opens no file dialog: %s", name, firstLine(err))
	}
	return chooser.SetFiles(path)
}

func clickStep(id string, k kind, expr, verb, doc, example string, do func(playwright.Locator) error) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "When", Since: since, Expr: expr, Doc: doc, Examples: []string{example},
		Run: action(func(sc *core.Scenario, s *session, a core.Args) error {
			return act(sc, s, k, text(sc, a, 0), verb, do)
		}),
	}
}

func enabledStep(id string, k kind, state string, enabled bool, example string) core.StepDef {
	other := map[bool]string{true: "disabled", false: "enabled"}[enabled]
	return core.StepDef{
		ID: id, Keyword: "Then", Since: since,
		Expr: "[[within {duration} ]]the {string} " + k.noun + " is " + state,
		Doc: fmt.Sprintf("Check that the %s with that name is %s. The check waits for it: 10 seconds, or `within {duration}`.",
			k.noun, state),
		Examples: []string{example},
		Run: check(func(sc *core.Scenario, s *session, a core.Args) error {
			name := text(sc, a, 1)
			d := cloudstep.Wait(a, 0)
			loc, err := s.find(sc, k, name, d)
			if err != nil {
				return err
			}
			ok, err := waitUntil(sc, d, func() (bool, error) {
				on, err := loc.IsEnabled()
				return on == enabled, err
			})
			if err != nil || ok {
				return err
			}
			return core.Fail(fmt.Sprintf("The %q %s is %s", name, k.noun, other), state, other)
		}),
	}
}

func openPage(sc *core.Scenario, a core.Args) error {
	var app *App
	var err error
	if a.Present(1) {
		app, err = apps.Of(sc).Get(a.String(1))
	} else {
		app, err = apps.Of(sc).Default()
	}
	if err != nil {
		return err
	}
	if _, err := open(sc, app); err != nil {
		return err
	}
	return action(func(sc *core.Scenario, s *session, a core.Args) error {
		page := text(sc, a, 0)
		address := app.pageURL(page)
		pg, err := s.page()
		if err != nil {
			return err
		}
		resp, err := pg.Goto(address)
		if err != nil && strings.Contains(err.Error(), "ERR_INVALID_AUTH_CREDENTIALS") {
			// Chromium gives up on a page that asks for credentials it has not.
			return core.Failf("The %q page of the %s web app asks for a username and a password (HTTP authentication): give the web app its username and password", page, app.Name)
		}
		if err != nil {
			return fmt.Errorf("cannot open %s: %s", address, firstLine(err))
		}
		return answered(app, fmt.Sprintf("the %q page", page), resp)
	})(sc, a)
}

// answered fails a page that answered with an error status.
func answered(app *App, what string, resp playwright.Response) error {
	if resp != nil && resp.Status() >= 400 {
		return core.Fail(fmt.Sprintf("%s of the %s web app answered with an error", strings.ToUpper(what[:1])+what[1:], app.Name),
			"a page", fmt.Sprintf("%d %s", resp.Status(), resp.StatusText()))
	}
	return nil
}

// action is a step that does something on the current page. A dialog the
// page opens meanwhile waits for "the dialog is accepted" or dismissed.
func action(fn func(sc *core.Scenario, s *session, a core.Args) error) core.StepFunc {
	return onPage(true, fn)
}

// check is a step that checks the current page.
func check(fn func(sc *core.Scenario, s *session, a core.Args) error) core.StepFunc {
	return onPage(false, fn)
}

// onPage runs a step on the current page: none while a dialog waits for an
// answer. A failed step attaches a screenshot of the page, and its error
// masks the scenario's secrets.
func onPage(isAction bool, fn func(sc *core.Scenario, s *session, a core.Args) error) core.StepFunc {
	return func(sc *core.Scenario, a core.Args) error {
		s, err := current(sc)
		if err != nil {
			return err
		}
		if d := s.openDialog(); d != nil {
			return hide(sc, dialogOpen(d))
		}
		if err := s.run(isAction, func() error { return fn(sc, s, a) }); err != nil {
			screenshot(sc, s)
			return hide(sc, err)
		}
		return nil
	}
}

// onDialog runs a step on the dialog the page shows.
func onDialog(fn func(sc *core.Scenario, s *session, a core.Args) error) core.StepFunc {
	return func(sc *core.Scenario, a core.Args) error {
		s, err := current(sc)
		if err != nil {
			return err
		}
		return hide(sc, fn(sc, s, a))
	}
}

// act does something to the one element of kind k named name.
func act(sc *core.Scenario, s *session, k kind, name, verb string, do func(playwright.Locator) error) error {
	loc, err := s.find(sc, k, name, actionTimeout)
	if err != nil {
		return err
	}
	if err := do(loc); err != nil {
		return fmt.Errorf("cannot %s the %q %s: %s", verb, name, k.noun, firstLine(err))
	}
	return nil
}

// text is string argument i, with ${env:..} and ${sys:..} expanded; the
// values of its ${env:..} references are secrets.
func text(sc *core.Scenario, a core.Args, i int) string { return expand(sc, a.String(i)) }

// waitUntil calls check until it reports true or d has passed.
func waitUntil(sc *core.Scenario, d time.Duration, check func() (bool, error)) (bool, error) {
	deadline := time.Now().Add(d)
	for {
		ok, err := check()
		if err != nil || ok {
			return ok, err
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
		select {
		case <-sc.Context().Done():
			return false, sc.Context().Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
