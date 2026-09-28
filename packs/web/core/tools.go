package webcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// tools are what the web-core pack lets coding agents do through `axx mcp`:
// look at the page the agent's session is on, to write the steps that use
// it.
func tools() []core.Tool {
	return []core.Tool{{
		Name: "web_page",
		Description: "The page the web app of the agent's session is on, once steps_try opened it: its address and title, " +
			"the elements steps can name, as they name them (the \"Get a quote\" button, the \"Weight (grams)\" field), " +
			"what a screen reader reads (an ARIA snapshot), its script errors and a screenshot. " +
			"Try steps with steps_try, look with web_page, then write the steps into the feature.",
		ReadOnly: true,
		Input: json.RawMessage(`{
  "type": "object",
  "properties": {
    "app": {"type": "string", "description": "the web app to look at, when the session has several (default: the one the last step used)"},
    "screenshot": {"type": "boolean", "description": "include a screenshot of what shows of the page (default true)"}
  },
  "additionalProperties": false
}`),
		Run: lookAtPage,
	}}
}

// pageView is what web_page says of a page.
type pageView struct {
	App      string   `json:"app"`
	URL      string   `json:"url"`
	Title    string   `json:"title,omitempty"`
	Tabs     int      `json:"tabs"`
	Dialog   string   `json:"dialog,omitempty"`
	Elements []string `json:"elements"`
	// Structure is the page's ARIA snapshot.
	Structure    string   `json:"structure,omitempty"`
	ScriptErrors []string `json:"scriptErrors,omitempty"`
}

func lookAtPage(call *core.ToolCall) (*core.ToolResult, error) {
	var in struct {
		App        string `json:"app"`
		Screenshot *bool  `json:"screenshot"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return nil, err
	}
	st := scenarioPages.Of(call.Scenario)
	// Secrets the steps used stay masked, as in failures.
	mask := secrets.Replacer(call.Scenario)
	st.mu.Lock()
	s := st.current
	if in.App != "" {
		s = st.byApp[in.App]
	}
	st.mu.Unlock()
	if mask == nil {
		mask = strings.NewReplacer()
	}
	if s == nil {
		if in.App != "" {
			return nil, fmt.Errorf("the session has no page of the %s web app: open one with steps_try", in.App)
		}
		return nil, errors.New(`the session has no page open: register the web app and open a page with steps_try, like ` +
			"`Given the portal web app with the following properties:\\n  | url | http://localhost:8400 |\\nWhen the \"/quote\" page is opened`")
	}
	pg, err := s.page()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	v := pageView{App: s.app.Name, URL: pg.URL(), Tabs: len(s.tabs), ScriptErrors: append([]string(nil), s.errors...)}
	s.mu.Unlock()
	if d := s.openDialog(); d != nil {
		// While a dialog waits, the page answers nothing else.
		v.Dialog = d.Message()
		v.masked(mask)
		return &core.ToolResult{Data: v}, nil
	}
	v.Title, _ = pg.Title()
	v.Elements = []string{}
	for _, k := range []kind{button, field, checkbox, option, link, tab, menuItem} {
		for _, name := range s.names(k) {
			v.Elements = append(v.Elements, "the "+name+" "+k.noun)
		}
	}
	if snap, err := pg.Locator("body").AriaSnapshot(playwright.LocatorAriaSnapshotOptions{Timeout: playwright.Float(5000)}); err == nil {
		v.Structure = strings.TrimSpace(snap)
	}
	v.masked(mask)
	res := &core.ToolResult{Data: v}
	if in.Screenshot == nil || *in.Screenshot {
		if b, err := pg.Screenshot(playwright.PageScreenshotOptions{Timeout: playwright.Float(5000)}); err == nil {
			res.Images = append(res.Images, core.Image{MediaType: "image/png", Data: b})
		}
	}
	return res, nil
}

func (v *pageView) masked(r *strings.Replacer) {
	v.URL, v.Title, v.Dialog, v.Structure = r.Replace(v.URL), r.Replace(v.Title), r.Replace(v.Dialog), r.Replace(v.Structure)
	for i := range v.Elements {
		v.Elements[i] = r.Replace(v.Elements[i])
	}
	for i := range v.ScriptErrors {
		v.ScriptErrors[i] = r.Replace(v.ScriptErrors[i])
	}
}
