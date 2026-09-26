//go:build integration

package driver

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// gherkinCheck runs in the patched driver's Node.js, with the gherkin
// language of the patched playwright-core.
const gherkinCheck = `
require(process.argv[1] + "/package/lib/coreBundle.js");
const g = globalThis.__axxGherkin();
const selectors = JSON.parse(process.argv[2]);
const phrases = JSON.parse(process.argv[3]);
const actions = JSON.parse(process.argv[4]);
console.log(JSON.stringify({
  locators: selectors.map(g.asLocator),
  selectors: phrases.map((p) => g.parse(p, "data-testid")),
  steps: g.generate(actions.map((action) => ({ pageGuid: "page@1", action, startTime: 0 })),
    { contextOptions: { baseURL: "http://localhost:8400/portal/" }, saveStorage: undefined }),
}));
`

// The gherkin language names elements the way the web-core pack's steps do, and
// records what people do on a page as web steps.
func TestTheGherkinLanguage(t *testing.T) {
	dir, err := Ensure(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	selectors := map[string]string{
		`internal:role=button[name="Get a quote"s]`:                                               `the "Get a quote" button`,
		`internal:role=link[name="Track"i]`:                                                       `the "Track" link`,
		`internal:role=tab[name="History"s]`:                                                      `the "History" tab`,
		`internal:role=menuitem[name="Cancel the parcel"s]`:                                       `the "Cancel the parcel" menu item`,
		`internal:role=checkbox[name="Insure this parcel"s]`:                                      `the "Insure this parcel" checkbox`,
		`internal:role=radio[name="Express"s]`:                                                    `the "Express" option`,
		`internal:role=textbox[name="Postcode"s]`:                                                 `the "Postcode" field`,
		`internal:role=heading[name="Parcels"s]`:                                                  `the "Parcels" element`,
		`internal:label="Weight (grams)"s`:                                                        `the "Weight (grams)" field`,
		`internal:attr=[placeholder="Search parcels"i]`:                                           `the "Search parcels" field`,
		`internal:text="Price: 6.90 EUR"s`:                                                        `the "Price: 6.90 EUR" element`,
		`internal:testid=[data-testid="quote"s]`:                                                  `the "testid=quote" element`,
		`.quote-result`:                                                                           `the "css=.quote-result" element`,
		`xpath=//tbody/tr[2]`:                                                                     `the "xpath=//tbody/tr[2]" element`,
		`iframe[name="pay"] >> internal:control=enter-frame >> internal:role=button[name="Pay"s]`: `the "Pay" button`,
		`internal:role=button[name="Say \"hi\""s]`:                                                `the "Say \"hi\"" button`,
		// The pack's own locators: its ways of finding one name, and visible ones only.
		`internal:role=button[name="Get a quote"s] >> visible=true`:                                                                                                                                                   `the "Get a quote" button`,
		`internal:label="Weight (grams)"s >> internal:or="internal:attr=[placeholder=\"Weight (grams)\"s]" >> visible=true`:                                                                                           `the "Weight (grams)" field`,
		`internal:text="Ella Brandt"s >> internal:or="internal:label=\"Ella Brandt\"s" >> internal:or="internal:attr=[alt=\"Ella Brandt\"s]" >> internal:or="internal:attr=[title=\"Ella Brandt\"s]" >> visible=true`: `the "Ella Brandt" element`,
		`internal:role=menuitem[name="Cancel"s] >> internal:or="internal:role=menuitemcheckbox[name=\"Cancel\"s]" >> visible=true`:                                                                                    `the "Cancel" menu item`,
		`internal:label="Weight"s >> internal:or="internal:attr=[placeholder=\"Height\"s]"`:                                                                                                                           `internal:label="Weight"s >> internal:or="internal:attr=[placeholder=\"Height\"s]"`,
		// What no step can name comes back as it is.
		`internal:role=button[name="Delete"s] >> nth=1`: `internal:role=button[name="Delete"s] >> nth=1`,
	}
	phrases := map[string]string{
		`the "Get a quote" button`:          `internal:role=button[name="Get a quote"s]`,
		`the "Express" option`:              `internal:role=radio[name="Express"s]`,
		`the "Cancel the parcel" menu item`: `internal:role=menuitem[name="Cancel the parcel"s]`,
		`the "Weight (grams)" field`:        `internal:label="Weight (grams)"s`,
		`the "Price: 6.90 EUR" element`:     `internal:text="Price: 6.90 EUR"s`,
		`the "testid=quote" element`:        `internal:testid=[data-testid="quote"s]`,
		`the "css=.quote-result" element`:   `css=.quote-result`,
		`page.getByRole('button')`:          ``,
	}
	actions := []map[string]any{
		{"name": "openPage", "url": "about:blank", "signals": []any{}},
		{"name": "navigate", "url": "http://localhost:8400/portal/quote", "signals": []any{}},
		{"name": "click", "selector": `internal:label="Weight (grams)"s`, "button": "left", "clickCount": 1, "modifiers": 0, "signals": []any{}},
		{"name": "fill", "selector": `internal:label="Weight (grams)"s`, "text": "1200", "signals": []any{}},
		{"name": "select", "selector": `internal:label="Destination country"s`, "options": []string{"Germany"}, "signals": []any{}},
		{"name": "setInputFiles", "selector": `internal:label="Customs invoice"s`, "files": []string{"INV-2041.pdf"}, "signals": []any{}},
		{"name": "check", "selector": `internal:role=radio[name="Express"s]`, "signals": []any{}},
		{"name": "check", "selector": `internal:role=checkbox[name="Insure this parcel"s]`, "signals": []any{}},
		{"name": "press", "selector": `internal:label="Postcode"s`, "key": "Enter", "modifiers": 0, "signals": []any{}},
		{"name": "click", "selector": `internal:role=button[name="Get a quote"s]`, "button": "left", "clickCount": 1, "modifiers": 0, "signals": []any{}},
		{"name": "assertText", "selector": `internal:testid=[data-testid="quote"s]`, "text": "Price: 13.40 EUR", "substring": true, "signals": []any{}},
		{"name": "assertValue", "selector": `internal:label="Weight (grams)"s`, "value": "1200", "signals": []any{}},
		{"name": "click", "selector": `internal:role=button[name="Cancel"s]`, "button": "left", "clickCount": 1, "modifiers": 0, "signals": []any{map[string]any{"name": "dialog", "dialogAlias": "dialog"}}},
		{"name": "click", "selector": `internal:role=button[name="Delete"s] >> nth=1`, "button": "left", "clickCount": 1, "modifiers": 0, "signals": []any{}},
		{"name": "assertSnapshot", "selector": `internal:role=form[name="Get a quote"s]`, "ariaSnapshot": "- spinbutton \"Weight (grams)\"\n- button \"Get a quote\"", "signals": []any{}},
		{"name": "closePage", "signals": []any{}},
	}
	var sel, phr []string
	for s := range selectors {
		sel = append(sel, s)
	}
	for p := range phrases {
		phr = append(phr, p)
	}
	args := []string{"-e", gherkinCheck, dir, mustJSON(t, sel), mustJSON(t, phr), mustJSON(t, actions)}
	out, err := exec.Command(filepath.Join(dir, "node"), args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got struct {
		Locators, Selectors []string
		Steps               string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for i, s := range sel {
		if got.Locators[i] != selectors[s] {
			t.Errorf("%s names %s, want %s", s, got.Locators[i], selectors[s])
		}
	}
	for i, p := range phr {
		if got.Selectors[i] != phrases[p] {
			t.Errorf("%s selects %q, want %q", p, got.Selectors[i], phrases[p])
		}
	}
	want := `    # The steps, as you use the page: copy them into a scenario.
    When the "/quote" page is opened
    And the "Weight (grams)" field is filled with "1200"
    And "Germany" is chosen in the "Destination country" field
    And the INV-2041.pdf file is uploaded in the "Customs invoice" field
    # INV-2041.pdf: the browser gives only the file's name; give its path in the project (relative to axx.yaml's directory or a resources directory, with no spaces)
    And the "Express" option is chosen
    And the "Insure this parcel" checkbox is checked
    And the Enter key is pressed in the "Postcode" field
    And the "Get a quote" button is clicked
    Then the "testid=quote" element shows "Price: 13.40 EUR"
    And the "Weight (grams)" field has the value "1200"
    When the "Cancel" button is clicked
    And the dialog is dismissed
    # click: no step finds this element by what people see; give it a label, a name or a test id (Playwright: getByRole('button', { name: 'Delete', exact: true }).nth(1))
    Then the "Get a quote" element's accessible structure is:
      """
      - spinbutton "Weight (grams)"
      - button "Get a quote"
      """
    When the browser tab is closed
`
	if !reflect.DeepEqual(got.Steps, want) {
		t.Errorf("recorded:\n%s\nwant:\n%s", got.Steps, want)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
