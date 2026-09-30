package lighthouse

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	webcore "github.com/nimbusxr/axx/packs/web/core"
	"github.com/nimbusxr/axx/packs/web/internal/driver"
)

// helper is the Node.js script that runs Lighthouse.
//
//go:embed axx-lighthouse.mjs
var helper []byte

// An audit that takes longer than this has gone wrong: Lighthouse waits
// 45 seconds at most for a page to load.
const auditTimeout = 3 * time.Minute

// performanceRuns is how many times Lighthouse audits a page for its
// performance: a load's metrics vary with the machine's load, and the pack
// judges the median run, as Lighthouse recommends
// (https://github.com/GoogleChrome/lighthouse/blob/main/docs/variability.md).
const performanceRuns = 3

// tool is Lighthouse, installed, and the Node.js that runs it.
type tool struct {
	node   string
	script string
}

// lighthouseFor prepares Lighthouse, once per run: it is downloaded the
// first time, into axx's cache beside the web-core pack's browser driver, whose
// Node.js runs it.
func lighthouseFor(s *core.Suite) (*tool, error) {
	return core.Cached(s, Name+"/lighthouse", func() (*tool, error) {
		ctx := context.Background()
		driverDir, err := driver.Ensure(ctx, driver.Options{})
		if err != nil {
			return nil, fmt.Errorf("cannot prepare the browser driver: %w", err)
		}
		packages := npm.InstallOptions{
			Lock: lockfile, LeaveOut: unused,
			CacheDir: filepath.Dir(driverDir), Name: "lighthouse-" + lighthouseVersion,
			Downloading: func() { s.Logger().Warn(fmt.Sprintf("downloading Lighthouse %s, once", lighthouseVersion)) },
		}
		if _, err := npm.Packages(packages); err != nil {
			return nil, fmt.Errorf("cannot read Lighthouse's lockfile: %w", err)
		}
		dir, err := npm.Install(ctx, packages)
		if err != nil {
			return nil, fmt.Errorf("cannot download Lighthouse %s: %w\n  npm_config_registry points the download at a mirror of npm's registry", lighthouseVersion, err)
		}
		sum := sha256.Sum256(helper)
		script := filepath.Join(dir, "axx-lighthouse-"+hex.EncodeToString(sum[:])[:8]+".mjs")
		if err := npm.WriteOnce(script, helper); err != nil {
			return nil, fmt.Errorf("cannot prepare Lighthouse: %w", err)
		}
		return &tool{node: filepath.Join(driverDir, npm.NodeName(runtime.GOOS)), script: script}, nil
	})
}

// request is what the helper audits.
type request struct {
	BrowserURL string   `json:"browserURL"`
	TargetID   string   `json:"targetId"`
	URL        string   `json:"url"`
	Device     string   `json:"device"`
	Categories []string `json:"categories"`
	Runs       int      `json:"runs"`
}

// run runs the helper: Lighthouse's result and its HTML report.
func (t *tool) run(sc *core.Scenario, req request) (json.RawMessage, string, error) {
	in, err := json.Marshal(req)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(sc.Context(), time.Duration(max(req.Runs, 1))*auditTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.node, t.script)
	cmd.Dir = filepath.Dir(t.script)
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	if log := strings.TrimSpace(stderr.String()); log != "" {
		sc.Suite().Logger().Debug("lighthouse", "stderr", log)
	}
	var out struct {
		LHR   json.RawMessage `json:"lhr"`
		HTML  string          `json:"html"`
		Error string          `json:"error"`
	}
	jerr := json.Unmarshal(stdout.Bytes(), &out)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, "", fmt.Errorf("Lighthouse did not finish within %s", time.Duration(max(req.Runs, 1))*auditTimeout) //nolint:staticcheck // user-facing message
	case out.Error != "":
		return nil, "", fmt.Errorf("Lighthouse failed: %s", out.Error) //nolint:staticcheck // user-facing message
	case err != nil:
		return nil, "", fmt.Errorf("Lighthouse failed: %s", nodeError(stderr.String(), err)) //nolint:staticcheck // user-facing message
	case jerr != nil:
		return nil, "", fmt.Errorf("cannot read Lighthouse's result: %w", jerr)
	case len(out.LHR) == 0:
		return nil, "", errors.New("Lighthouse gave no result") //nolint:staticcheck // user-facing message
	}
	return out.LHR, out.HTML, nil
}

var errorLine = regexp.MustCompile(`^\w*Error\b`)

// nodeError is why Node.js failed, from what it printed: the line of the
// error it stopped at, or its last line.
func nodeError(stderr string, err error) string {
	var last string
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(l)
		if errorLine.MatchString(l) {
			return l
		}
		if l != "" {
			last = l
		}
	}
	return cmp.Or(last, err.Error())
}

// turnsFor makes a run's audits take turns: Lighthouse times how the page
// loads, which another audit in the same browser would slow down.
func turnsFor(s *core.Suite) *sync.Mutex {
	m, _ := core.Cached(s, Name+"/turns", func() (*sync.Mutex, error) { return &sync.Mutex{}, nil })
	return m
}

// scenarioAudits is what the pack keeps of a scenario: the names of its
// reports.
type audits struct {
	mu      sync.Mutex
	reports map[string]int // how many by name
}

var scenarioAudits = core.NewStateKey(Name+"/audits", func(*core.Scenario) *audits {
	return &audits{reports: map[string]int{}}
}, nil)

// audit has Lighthouse audit a page of the current web app for categories,
// in a tab of its own, and keeps and attaches its report. It returns how
// failures name the page, and Lighthouse's result.
func audit(sc *core.Scenario, c *webcore.Current, page string, categories []string) (string, *lhr, error) {
	if !chromiumFamily(c.Engine()) {
		return "", nil, fmt.Errorf("Lighthouse audits pages in Chromium, Chrome and Edge only; the %s web app runs in %s", c.App(), c.Engine()) //nolint:staticcheck // user-facing message
	}
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return "", nil, err
	}
	base := c.URL()
	address := pageURL(base, page)
	browser, err := c.DebuggingURL()
	if err != nil {
		return "", nil, err
	}
	lh, err := lighthouseFor(sc.Suite())
	if err != nil {
		return "", nil, err
	}
	turn := turnsFor(sc.Suite())
	turn.Lock()
	defer turn.Unlock()
	tab, err := c.AsideTab()
	if err != nil {
		return "", nil, fmt.Errorf("cannot open a tab for Lighthouse: %s", firstLine(err))
	}
	defer func() { _ = tab.Close() }()
	target, err := targetID(tab)
	if err != nil {
		return "", nil, fmt.Errorf("cannot open a tab for Lighthouse: %s", firstLine(err))
	}
	runs := 1
	if slices.Contains(categories, "performance") {
		runs = performanceRuns
	}
	raw, html, err := lh.run(sc, request{BrowserURL: browser, TargetID: target, URL: address, Device: cfg.device, Categories: categories, Runs: runs})
	if err != nil {
		return "", nil, fmt.Errorf("cannot audit the %q page: %w", page, err)
	}
	var r lhr
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", nil, fmt.Errorf("cannot read Lighthouse's result: %w", err)
	}
	keep(sc, c, page, html)
	if r.RuntimeError != nil {
		return "", nil, core.Failf("Lighthouse could not audit the %q page: %s", page, r.RuntimeError.Message)
	}
	for _, w := range r.RunWarnings {
		sc.Log("Lighthouse: %s", strings.ReplaceAll(w, "\u00a0", " "))
	}
	what := fmt.Sprintf("The %q page", page)
	if final := r.FinalURL; final != "" && trimURL(final) != trimURL(address) {
		what += " (redirected to " + pathOf(base, final) + ")"
	}
	return what, &r, nil
}

// pageURL is the address of a page of the app, as the web-core pack has it: a
// path below the app's URL, or an absolute URL.
func pageURL(base, page string) string {
	if strings.HasPrefix(page, "http://") || strings.HasPrefix(page, "https://") {
		return page
	}
	return base + "/" + strings.TrimLeft(page, "/")
}

// pathOf is where an address is: a path below the app's URL, or the
// whole address when it is not the app's.
func pathOf(base, address string) string {
	rest, ok := strings.CutPrefix(address, base)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "?")) {
		return address
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return rest
}

// trimURL is an address without its fragment and trailing slash, which
// Lighthouse adds to an origin.
func trimURL(u string) string {
	u, _, _ = strings.Cut(u, "#")
	return strings.TrimRight(u, "/")
}

// targetID is the DevTools protocol's id of a tab, which Lighthouse finds
// it by.
func targetID(p playwright.Page) (string, error) {
	s, err := p.Context().NewCDPSession(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = s.Detach() }()
	res, err := s.Send("Target.getTargetInfo", nil)
	if err != nil {
		return "", err
	}
	m, _ := res.(map[string]any)
	info, _ := m["targetInfo"].(map[string]any)
	id, _ := info["targetId"].(string)
	if id == "" {
		return "", errors.New("the browser does not say which tab it is")
	}
	return id, nil
}

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)

// keep keeps Lighthouse's report of a page in .axx/web/lighthouse, a file
// for each audit of the scenario, and attaches it.
func keep(sc *core.Scenario, c *webcore.Current, page, html string) {
	name := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(page), "-"), "-")
	if len(name) > 40 {
		name = strings.TrimRight(name[:40], "-")
	}
	if name == "" {
		name = "home"
	}
	st := scenarioAudits.Of(sc)
	st.mu.Lock()
	st.reports[c.App()+"/"+name]++
	n := st.reports[c.App()+"/"+name]
	st.mu.Unlock()
	suffix := "-" + name
	if n > 1 {
		suffix += fmt.Sprintf("-%d", n)
	}
	path := c.File(sc, "lighthouse", suffix+".html")
	if err := os.WriteFile(path, []byte(html), 0o644); err == nil {
		sc.Log("the Lighthouse report of the %q page: %s", page, webcore.Relative(sc, path))
	}
	sc.Attach("text/html", []byte(html), "Lighthouse report")
}

// chromiumFamily reports whether an engine is Chromium or a browser built
// on it, which Lighthouse can drive.
func chromiumFamily(engine string) bool {
	return engine == "chromium" || engine == "chrome" || engine == "msedge"
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(s)
}
