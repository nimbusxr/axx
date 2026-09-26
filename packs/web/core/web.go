// Package web is the web-core pack: web apps used through real browsers, the way
// the people who use them do. It drives Playwright, and the browsers run on
// the machine that runs axx: the pack downloads each one the first time a
// scenario uses it.
package webcore

import (
	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/web/internal/driver"
)

// Name is the pack's name.
const Name = "web-core"

// PlaywrightVersion is the Playwright version the pack drives, and whose
// browsers it runs.
const PlaywrightVersion = driver.CoreVersion

const packDoc = `Use web apps in real browsers, the way the people who use them do: open pages, fill in forms, click buttons and links, answer dialogs, download files, and check what the page shows.

Each web app is registered with its address:

` + "```gherkin" + `
Given the parcels web app with the following properties:
  | url | http://localhost:8400 |
` + "```" + `

The browsers run on the machine that runs axx. The pack drives Playwright ` + PlaywrightVersion + `, and downloads its browsers the first time a scenario uses each one. The web app can use another ` + "`engine`" + ` (` + "`firefox`" + `, ` + "`webkit`" + `, or the Google Chrome or Microsoft Edge installed on the machine), and present the browser as a ` + "`device`" + ` (like ` + "`iPhone 15`" + `), with a ` + "`viewport`" + `, a ` + "`locale`" + ` and a ` + "`timezone`" + `.

- **Pages.** ` + "`the {string} page is opened`" + ` opens a page of the app; the other steps act on it. A link that opens a new browser tab makes it the current tab, until it is closed.
- **Finding things.** Fields are found by their label (or, without one, their placeholder), buttons, links, tabs and menu items by their name, and text anywhere on the page, in every frame: the names people see. Names must match exactly. Where a name is not enough, a step takes a selector in its place: ` + "`css=…`" + `, ` + "`xpath=…`" + ` or ` + "`testid=…`" + `.
- **Waiting.** Every step waits for what it needs: an action for its element to be ready, a check for the page to show what it expects, 10 seconds unless ` + "`within {duration}`" + ` says otherwise. There is no step to sleep.
- **Dialogs.** A dialog the page opens (an alert, a confirmation, a question) waits for ` + "`the dialog is accepted`" + `, dismissed or answered; other steps fail while it is open.
- **Isolation.** Each scenario has its own browser context: its own cookies and storage, so scenarios run in parallel.
- **Failures.** A failed step attaches a screenshot of the page. A failed scenario keeps a Playwright trace in ` + "`.axx/web/traces`" + `, step by step, with the feature file: open it with ` + "`npx playwright show-trace`" + ` or at trace.playwright.dev.
- **Watching.** With ` + "`packs.web-core.watch: true`" + ` in axx.yaml, the browsers open in windows on your screen: watch the scenarios, and open DevTools. ` + "`slowdown`" + ` slows them down.
- **Pausing.** ` + "`axx run --pause-at <file>:<line>`" + ` pauses before a step, and ` + "`packs.web-core.pauseOnFailure: true`" + ` where a scenario fails, in Playwright's Inspector: resume, step through the browser's actions, pick an element, which the Inspector names as steps do (` + "`the \"Register\" button`" + `), or record what you do on the page as steps.
- **Sessions.** A web app can start signed in: ` + "`cookie.<name>`" + `, ` + "`header.<name>`" + `, ` + "`local storage.<key>`" + `, ` + "`session storage.<key>`" + `, and ` + "`username`" + ` and ` + "`password`" + ` for HTTP authentication, for the app's address only.
- **Secrets.** Take passwords and tokens from the environment, as ` + "`${env:…}`" + `: whatever a step takes from the environment is shown as ` + "`********`" + ` in failures and traces.

Values in the steps and the table expand ` + "`${env:..}`" + ` and ` + "`${sys:..}`" + `.`

// Pack returns the web-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	all := append(steps(), checkSteps()...)
	all = append(all, gestureSteps()...)
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, ConfigSchema: []byte(configSchema),
		Params: []core.ParamType{elementParam}, Steps: all, Hooks: stepHooks(), Tools: tools(),
	}
}
