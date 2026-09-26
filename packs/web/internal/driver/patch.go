package driver

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// patchRevision names the patched driver: prepared drivers of an earlier
// revision are prepared again.
const patchRevision = 3

// Playwright's Inspector and recorder speak Playwright's languages; these
// patches teach them the steps of the web-core pack. They apply to exactly the
// pinned playwright-core, each at an anchor that must occur as often as it
// says, and preparing the driver fails otherwise:
//
//   - a "gherkin" way of naming elements (the "Register" button, the
//     "Weight (grams)" field, the "testid=quote" element): picking an
//     element, the Inspector's call log, its locator box and the page's
//     tooltips use it;
//   - an "axx" recorder language, the Inspector's first, that writes the
//     steps people take as web steps;
//   - each call's location is the Gherkin step it belongs to, taken from the
//     step's trace group, so that a paused run shows its feature file, at
//     the step, highlighted as Gherkin;
//   - the trace viewer names elements the gherkin way too, and shows the
//     feature file of a step as Gherkin.

//go:embed patches/*.js
var patchFiles embed.FS

func patchSource(name string) string {
	b, err := patchFiles.ReadFile("patches/" + name)
	if err != nil {
		panic(err)
	}
	return strings.TrimRight(string(b), "\n")
}

type patch struct {
	file   string // below package/; a glob matching one file
	anchor string
	with   string
	count  int // how many times the anchor occurs; 0 means once
}

func patches() []patch {
	locator := patchSource("gherkin-locator.js")
	// The page's copy is inside a single-quoted string, with escaped newlines.
	injected := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`).Replace(locator)
	const core = "lib/coreBundle.js"
	const ui = "lib/vite/recorder/assets/index-*.js"
	const viewer = "lib/vite/traceViewer/assets/defaultSettingsView-*.js"
	return []patch{
		// The location of each call: its step's, when its context has one.
		{file: core, count: 2, anchor: "location: validMetadata.location,", with: "location: axxStepLocation(sdkObject) ?? validMetadata.location,"},
		{
			file: core, anchor: "      group(progress2, name, location2) {\n        if (!this._state)\n",
			with: "      group(progress2, name, location2) {\n        if (location2?.file?.endsWith(\".feature\"))\n          this._context.__axxStep = location2;\n        if (!this._state)\n",
		},
		{
			file: core, anchor: "      groupEnd(progress2) {\n        this._groupEnd();",
			with: "      groupEnd(progress2) {\n        this._context.__axxStep = void 0;\n        this._groupEnd();",
		},
		{
			file: core, anchor: "function languageForFile(file) {\n  if (file.endsWith(\".py\"))",
			with: axxHelpers + "function languageForFile(file) {\n  if (file.endsWith(\".feature\"))\n    return \"gherkin\";\n  if (file.endsWith(\".py\"))",
		},
		// The gherkin way of naming elements: for code, for the page, for the Inspector.
		{file: core, anchor: "      jsonl: JsonlLocatorFactory\n    };", with: "      jsonl: JsonlLocatorFactory,\n      gherkin: " + indent(locator, "      ") + "\n    };"},
		{file: core, anchor: `  jsonl: JsonlLocatorFactory\n};\nfunction isRegExp(obj)`, with: `  jsonl: JsonlLocatorFactory,\n  gherkin: ` + injected + `\n};\nfunction isRegExp(obj)`},
		{file: ui, anchor: "return JSON.stringify(t[0])}}};function j(e){return e instanceof RegExp}", with: "return JSON.stringify(t[0])}},gherkin:" + locator + "};function j(e){return e instanceof RegExp}"},
		{
			file: core, anchor: "function unsafeLocatorOrSelectorAsSelector(language, locator2, testIdAttributeName2 = \"data-testid\") {\n",
			with: "function unsafeLocatorOrSelectorAsSelector(language, locator2, testIdAttributeName2 = \"data-testid\") {\n" + patchSource("gherkin-parser.js") + "\n",
		},
		// The axx recorder language, the first, with the context's options (its base URL).
		{file: core, anchor: "    new JsonlLanguageGenerator()\n  ]);", with: "    new JsonlLanguageGenerator(),\n    new (" + indent(patchSource("gherkin-generator.js"), "    ") + ")()\n  ]);"},
		{
			file: core, anchor: "this._primaryGeneratorId = process.env.TEST_INSPECTOR_LANGUAGE || params2.language || determinePrimaryGeneratorId(params2.sdkLanguage);",
			with: "this._primaryGeneratorId = process.env.TEST_INSPECTOR_LANGUAGE || params2.language || \"axx\";",
		},
		{file: core, anchor: "_RecorderApp._show(recorder, context2, {})", with: "_RecorderApp._show(recorder, context2, { contextOptions: context2._options })"},
		// What the recorder records, as axx steps, is also in $AXX_WEB_RECORDING, for an IDE.
		{
			file: core, anchor: "this._throttledOutputFile = params2.outputFile ? new ThrottledFile(params2.outputFile) : null;",
			with: "const outputFile2 = params2.outputFile || process.env.AXX_WEB_RECORDING;\n        this._throttledOutputFile = outputFile2 ? new ThrottledFile(outputFile2) : null;",
		},
		// Gherkin in the Inspector: its highlighting, and call log entries without "page.".
		{file: ui, anchor: "java:`text/x-java`,markdown:`markdown`", with: "java:`text/x-java`,gherkin:`gherkin`,markdown:`markdown`"},
		{file: ui, anchor: "e.defineSimpleMode(`text/linkified`,{start:[{regex:y,token:`linkified`}]})", with: "e.defineSimpleMode(`text/linkified`,{start:[{regex:y,token:`linkified`}]})," + patchSource("gherkin-mode.js")},
		{file: ui, anchor: "title:`page.${o}`,children:`page.${o}`", with: "title:o.startsWith(`the \"`)?o:`page.${o}`,children:o.startsWith(`the \"`)?o:`page.${o}`"},
		// Traces keep the feature files of their steps, without the rest of the sources.
		{
			file: core, anchor: "  if (params2.includeSources) {\n    const sourceFiles = new Set(params2.additionalSources);",
			with: "  if (!params2.includeSources) {\n    for (const sourceFile of params2.additionalSources || []) {\n      if (sourceFile.endsWith(\".feature\"))\n        addFile(sourceFile, \"resources/src@\" + calculateSha1(sourceFile) + \".txt\");\n    }\n  }\n  if (params2.includeSources) {\n    const sourceFiles = new Set(params2.additionalSources);",
		},
		// The trace viewer: traces name elements the gherkin way, and show feature files as Gherkin.
		{file: viewer, anchor: "return JSON.stringify(t[0])}}};function kn(e){return e instanceof RegExp}", with: "return JSON.stringify(t[0])}},gherkin:" + locator + "};function kn(e){return e instanceof RegExp}"},
		{file: viewer, anchor: "this.sdkLanguage=n?.sdkLanguage", with: "this.sdkLanguage=`gherkin`"},
		{file: viewer, anchor: "xn(n||`javascript`,e.params.selector)", with: "xn(n||`gherkin`,e.params.selector)"},
		{file: viewer, anchor: "xn(t||`javascript`,e.params.selector)", with: "xn(t||`gherkin`,e.params.selector)"},
		{file: viewer, anchor: "Cn(r||`javascript`,e.params.selector)", with: "Cn(r||`gherkin`,e.params.selector)"},
		{file: viewer, anchor: "java:`text/x-java`,markdown:`markdown`", with: "java:`text/x-java`,gherkin:`gherkin`,markdown:`markdown`"},
		{file: viewer, anchor: "e.defineSimpleMode(`text/linkified`,{start:[{regex:j,token:`linkified`}]})", with: "e.defineSimpleMode(`text/linkified`,{start:[{regex:j,token:`linkified`}]})," + patchSource("gherkin-mode.js")},
		{file: viewer, anchor: "v=_.endsWith(`.md`)?`markdown`:`javascript`", with: "v=_.endsWith(`.md`)?`markdown`:_.endsWith(`.feature`)?`gherkin`:`javascript`"},
	}
}

// axxHelpers are the patches' functions: a call's step location, and, for
// the pack's tests, the gherkin language (globalThis.__axxGherkin).
const axxHelpers = `function axxStepLocation(sdkObject) {
  return sdkObject?.attribution?.context?.__axxStep;
}
globalThis.__axxGherkin = function() {
  init_locatorGenerators();
  init_locatorParser();
  init_languages();
  const generator = [...languageSet()].find((g) => g.id === "axx");
  return {
    asLocator: (selector) => asLocator("gherkin", selector),
    parse: (text, testIdAttributeName) => locatorOrSelectorAsSelector("gherkin", text, testIdAttributeName),
    generate: (actions, options) => generateCode(actions, generator, options).text
  };
};
`

func indent(s, prefix string) string {
	return strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// applyPatches patches the playwright-core package in dir.
func applyPatches(dir string) error {
	files := map[string]string{} // path -> content
	var order []string
	for _, p := range patches() {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(p.file)))
		if err != nil || len(matches) != 1 {
			return fmt.Errorf("patching playwright-core: %s matches %d files, want 1", p.file, len(matches))
		}
		path := matches[0]
		content, ok := files[path]
		if !ok {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content = string(b)
			order = append(order, path)
		}
		want := max(p.count, 1)
		if n := strings.Count(content, p.anchor); n != want {
			return fmt.Errorf("patching playwright-core: %s has %q %d times, want %d", p.file, firstLine(p.anchor), n, want)
		}
		files[path] = strings.ReplaceAll(content, p.anchor, p.with)
	}
	for _, path := range order {
		if err := os.WriteFile(path, []byte(files[path]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
