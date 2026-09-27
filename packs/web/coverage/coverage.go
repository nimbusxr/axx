// Package coverage is the web-coverage pack: how much of the web apps'
// JavaScript the scenarios ran. It collects the coverage of the pages'
// scripts from Chromium, Chrome and Edge as the scenarios run, through their
// source maps, and writes lcov and Istanbul reports at the end of the run,
// the same as Node's tools (c8) write from the same coverage. It builds on
// the web-core pack, and has no steps: it is settings only.
package coverage

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// Name is the pack's name.
const Name = "web-coverage"

// Pack returns the web-coverage pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{webcore.Name},
		ConfigSchema: []byte(configSchema), Steps: []core.StepDef{},
		// Step hooks, not scenario ones: those show in every scenario's report.
		Hooks: []core.Hook{
			{ID: Name + ".start", Phase: core.BeforeStep, Run: start},
			{ID: Name + ".take", Phase: core.AfterStep, Run: take},
		},
	}
}

const packDoc = `Find out how much of the web apps' JavaScript the scenarios run. The pack collects the coverage of the scripts the pages load, in Chromium, Chrome and Edge, as the scenarios run, and writes it when the run ends: ` + "`lcov.info`" + `, for editors and coverage services, and Istanbul's ` + "`coverage-final.json`" + ` and ` + "`coverage-summary.json`" + `, in ` + "`.axx/web/coverage`" + ` (` + "`folder`" + `). It has no steps: add it to the project, and every scenario's pages count. It builds on the ` + "`web-core`" + ` pack.

- **What counts.** The scripts the pages load from their web app's origin, named by their URL path, like ` + "`js/track.js`" + `; for a script with a source map, its original files instead, TypeScript and all. Scripts of other origins, inline scripts and event handlers do not count. With ` + "`sources`" + `, only the scripts under those URL paths count, named as the files of the project they are.
- **How it counts.** Chromium's own coverage (V8's): the lines, functions and branches that ran, and how many times, over every page of every scenario, taken after each step and before a page leaves its document. The reports are those Node's tools (c8) write from the same coverage. A tab a page opens counts from when it opens. In Firefox and WebKit, the pages run without coverage.

` + "```yaml" + `
packs:
  web-coverage:
    sources:
      /portal/js/: ../app/web/js     # the scripts under /portal/js/, in the project
      /portal/src/: ../app/web/src   # the original files of their source maps
` + "```" + `

A source map without its original files' content finds them through ` + "`sources`" + ` too.`

// configSchema is the pack's section of axx.yaml, packs.web-coverage.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "folder": {"type": "string", "description": "The folder the reports go to, in the project: lcov.info, coverage-final.json and coverage-summary.json (default .axx/web/coverage)."},
    "sources": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Where the scripts are in the project, by the start of their URL path: {\"/portal/js/\": \"../app/web/js\"}, a folder relative to the project. Only the scripts (or, through their source maps, the original files) whose URL path starts with one count, named by its folder and the rest of the path. Without it, every script the pages load from their web app's origin counts, named by its URL path without the leading slash."}
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	Folder  string            `json:"folder"`
	Sources map[string]string `json:"sources"`
}

type settings struct {
	folder     string // absolute
	projectDir string
	sources    []sourceFolder // the longest prefix first
}

// sourceFolder is where the scripts under a URL path are in the project.
type sourceFolder struct {
	prefix string // the start of a URL path, like /portal/js/
	folder string // a slash-separated path, relative to the project where it can be
}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		return parseConfig(c, s.ProjectDir())
	})
}

func parseConfig(c Config, projectDir string) (*settings, error) {
	st := &settings{folder: c.Folder, projectDir: projectDir}
	if st.folder == "" {
		st.folder = filepath.Join(".axx", "web", "coverage")
	}
	if !filepath.IsAbs(st.folder) {
		st.folder = filepath.Join(projectDir, filepath.FromSlash(st.folder))
	}
	for prefix, folder := range c.Sources {
		if !strings.HasPrefix(prefix, "/") {
			return nil, fmt.Errorf("packs.%s.sources: %q is not the start of a URL path: start it with /, like \"/portal/js/\"", Name, prefix)
		}
		if strings.TrimSpace(folder) == "" {
			return nil, fmt.Errorf("packs.%s.sources: %q has no folder: give the folder of the project its scripts are in, like \"../app/web/js\"", Name, prefix)
		}
		if filepath.IsAbs(folder) {
			if rel, err := filepath.Rel(projectDir, folder); err == nil && projectDir != "" {
				folder = rel
			}
		}
		st.sources = append(st.sources, sourceFolder{prefix: prefix, folder: filepath.ToSlash(filepath.Clean(folder))})
	}
	slices.SortFunc(st.sources, func(a, b sourceFolder) int {
		if n := len(b.prefix) - len(a.prefix); n != 0 {
			return n
		}
		return strings.Compare(a.prefix, b.prefix)
	})
	return st, nil
}

// counts reports whether a script of a page counts, before its source map
// says what its original files are: a script of the app's origin, under
// sources when it has no source map.
func (st *settings) counts(origin, script string, sourceMap bool) bool {
	u, err := url.Parse(script)
	if err != nil || originOf(u) != origin {
		return false
	}
	if len(st.sources) == 0 || sourceMap {
		return true
	}
	_, ok := st.source(u.Path)
	return ok
}

// name is what the reports call a file, by its URL path (a script's, or an
// original file's through the script's source map): relative to the
// project under sources, the URL path without its leading slash otherwise;
// false for a file that does not count.
func (st *settings) name(urlPath string) (string, bool) {
	if len(st.sources) == 0 {
		name := strings.TrimPrefix(urlPath, "/")
		return name, name != ""
	}
	sf, ok := st.source(urlPath)
	if !ok {
		return "", false
	}
	return path.Join(sf.folder, strings.TrimPrefix(urlPath, sf.prefix)), true
}

func (st *settings) source(urlPath string) (sourceFolder, bool) {
	for _, sf := range st.sources {
		if strings.HasPrefix(urlPath, sf.prefix) {
			return sf, true
		}
	}
	return sourceFolder{}, false
}

// read reads an original file of a source map that has not its content,
// from the project.
func (st *settings) read(urlPath string) (string, error) {
	name, ok := st.name(urlPath)
	if !ok || len(st.sources) == 0 {
		return "", fmt.Errorf("its source map has not the content of %s, and packs.%s.sources does not say where it is in the project", urlPath, Name)
	}
	b, err := os.ReadFile(filepath.Join(st.projectDir, filepath.FromSlash(name)))
	if err != nil {
		return "", fmt.Errorf("its source map has not the content of %s, and the project has not %s", urlPath, name)
	}
	return string(b), nil
}

// originOf is a URL's scheme, host and port, the port left out where it is
// the scheme's own.
func originOf(u *url.URL) string {
	host := u.Host
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		host = u.Hostname()
	}
	return strings.ToLower(u.Scheme + "://" + host)
}

// scenarioCoverage is what a scenario collects, from its first step.
type scenarioCoverage struct {
	mu      sync.Mutex
	started bool
	run     *run
	apps    []*appCoverage
}

var scenarioState = core.NewStateKey(Name+"/scenario", func(*core.Scenario) *scenarioCoverage { return &scenarioCoverage{} }, nil)

// start collects the coverage of the scenario's pages from its first step.
func start(sc *core.Scenario) error {
	st := scenarioState.Of(sc)
	st.mu.Lock()
	started := st.started
	st.started = true
	st.mu.Unlock()
	if started {
		st.caughtUp()
		return nil
	}
	r, err := runFor(sc.Suite())
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.run = r
	st.mu.Unlock()
	if err := webcore.OnContext(sc, func(c webcore.Context) error { return st.context(sc, c) }); err != nil {
		return err
	}
	if err := webcore.OnPage(sc, func(c webcore.Context, p playwright.Page) error { return st.page(sc, c, p) }); err != nil {
		return err
	}
	// The last counts of the pages, before the web-core pack closes them: its
	// cleanup came first, so it runs after.
	sc.OnClose(Name, func() error {
		st.end()
		return nil
	})
	return nil
}

// take takes the coverage of the scenario's pages after each step, in the
// background.
func take(sc *core.Scenario) error {
	st, ok := scenarioState.Peek(sc)
	if !ok {
		return nil
	}
	for _, a := range st.appsNow() {
		a.trigger()
	}
	return nil
}

// caughtUp waits for the counts the step before started to take: a step
// that closes a tab would take the tab's last counts with it. It waits a
// while at most, and less while a page shows a dialog, whose scripts wait
// for the answer a step gives.
func (st *scenarioCoverage) caughtUp() {
	for _, a := range st.appsNow() {
		done, wait := a.pending()
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-time.After(wait):
		}
	}
}

func (st *scenarioCoverage) appsNow() []*appCoverage {
	st.mu.Lock()
	defer st.mu.Unlock()
	return slices.Clone(st.apps)
}

// context readies a web app's browser context, before its first page.
func (st *scenarioCoverage) context(sc *core.Scenario, c webcore.Context) error {
	if !chromium(c.Engine()) {
		st.run.unsupported(sc, c)
		return nil
	}
	hook := hookScript
	if err := c.AddInitScript(playwright.Script{Content: &hook}); err != nil {
		sc.Log("the %s web app's JavaScript coverage cannot be collected: %v", c.App(), err)
		return nil
	}
	a := newAppCoverage(st.run, c)
	st.mu.Lock()
	st.apps = append(st.apps, a)
	st.mu.Unlock()
	return nil
}

// page collects a page's coverage, before it loads anything (a page a page
// opens: as it opens).
func (st *scenarioCoverage) page(sc *core.Scenario, c webcore.Context, p playwright.Page) error {
	st.mu.Lock()
	var a *appCoverage
	for _, x := range st.apps {
		if x.ctx.BrowserContext == c.BrowserContext {
			a = x
		}
	}
	st.mu.Unlock()
	if a == nil {
		return nil
	}
	// Coverage is no reason for a step to fail.
	if err := a.attach(p); err != nil {
		sc.Log("the JavaScript coverage of a page of the %s web app cannot be collected: %v", c.App(), err)
	}
	return nil
}

// end takes the last coverage of the scenario's pages, and waits for their
// sources, before they close.
func (st *scenarioCoverage) end() {
	var wg sync.WaitGroup
	for _, a := range st.appsNow() {
		wg.Go(a.settle)
	}
	wg.Wait()
}

func chromium(engine string) bool {
	return engine == "chromium" || engine == "chrome" || engine == "msedge"
}
